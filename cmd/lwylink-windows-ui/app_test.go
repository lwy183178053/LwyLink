package main

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/client"
	"github.com/LwyV-Labs/LwyLink/internal/overlay"
	"github.com/LwyV-Labs/LwyLink/internal/room"
)

func TestUpdateConnectionAppliesTransportMessageAndDirectPeers(t *testing.T) {
	app := NewApp()
	app.mu.Lock()
	app.connectSeq = 7
	app.connection = ConnectionState{Status: statusConnected, Message: "已连接", IP: "172.16.1.10"}
	app.mu.Unlock()

	app.updateConnection(7, func(state *ConnectionState) {
		applyTransportStatus(state, client.TransportStatus{
			Mode:      client.TransportModeUDPDirect,
			Message:   "udp direct",
			DirectIPs: []string{"172.16.1.11"},
		})
	})

	got := app.CurrentState().Connection
	if got.Status != statusConnected {
		t.Fatalf("status=%q, want %q", got.Status, statusConnected)
	}
	if got.Message != "udp direct" {
		t.Fatalf("message=%q", got.Message)
	}
	if !slices.Equal(got.DirectPeerIPs, []string{"172.16.1.11"}) {
		t.Fatalf("direct peers=%v", got.DirectPeerIPs)
	}
}

func TestAcceptVoicePacketDropsDuplicateAndAllowsReorderedSeq(t *testing.T) {
	app := NewApp()
	app.mu.Lock()
	defer app.mu.Unlock()
	now := time.Now()
	if !app.acceptVoicePacketLocked(10, 2, now) {
		t.Fatalf("first voice packet was rejected")
	}
	if app.acceptVoicePacketLocked(10, 2, now.Add(time.Millisecond)) {
		t.Fatalf("duplicate voice packet was accepted")
	}
	if !app.acceptVoicePacketLocked(10, 1, now.Add(2*time.Millisecond)) {
		t.Fatalf("reordered voice packet was rejected")
	}
	if !app.acceptVoicePacketLocked(10, 3, now.Add(3*time.Millisecond)) {
		t.Fatalf("newer voice packet was rejected")
	}
	if !app.acceptVoicePacketLocked(11, 1, now.Add(4*time.Millisecond)) {
		t.Fatalf("other speaker seq should be tracked independently")
	}
}

func TestVoiceParticipantSpeakingRequiresJoinedOpenMic(t *testing.T) {
	state := room.VoiceState{
		Participants: []room.VoiceParticipant{
			{UserID: 10, Speaking: false},
			{UserID: 11, Speaking: true},
		},
	}
	if voiceParticipantSpeaking(state, 10) {
		t.Fatalf("muted participant was treated as speaking")
	}
	if !voiceParticipantSpeaking(state, 11) {
		t.Fatalf("speaking participant was rejected")
	}
	if voiceParticipantSpeaking(state, 12) {
		t.Fatalf("missing participant was treated as speaking")
	}
}

func TestSendVoiceAudioFrameRequiresOpenMicAndTargetsVoicePeers(t *testing.T) {
	app := NewApp()
	app.mu.Lock()
	app.user = &account.PublicUser{ID: 10, Username: "owner@example.com"}
	app.currentRoom = &room.Room{ID: 7}
	app.connection = ConnectionState{Status: statusConnected, RoomID: 7}
	app.members = []room.MemberView{
		{UserID: 10, VirtualIP: "172.16.1.10"},
		{UserID: 11, VirtualIP: "172.16.1.11"},
		{UserID: 12},
	}
	app.voiceState = room.VoiceState{
		RoomID: 7,
		Participants: []room.VoiceParticipant{
			{UserID: 10, Speaking: false},
			{UserID: 11, Speaking: true},
			{UserID: 12, Speaking: true},
		},
	}
	app.mu.Unlock()

	if _, err := app.SendVoiceAudioFrame(VoiceAudioFrameInput{RoomID: 7, Seq: 1, TimestampMS: time.Now().UnixMilli(), SampleRate: 16000, Payload: []byte{1, 2}}); err == nil {
		t.Fatalf("voice frame sent while local mic was closed")
	}

	app.mu.Lock()
	app.voiceState.Participants[0].Speaking = true
	app.mu.Unlock()
	if _, err := app.SendVoiceAudioFrame(VoiceAudioFrameInput{RoomID: 7, Seq: 2, TimestampMS: time.Now().UnixMilli(), Codec: "opus", SampleRate: 48000, Payload: []byte{3, 4}}); err != nil {
		t.Fatalf("send voice frame: %v", err)
	}

	select {
	case pkt := <-app.voiceSend:
		if pkt.PeerVirtualIP != "172.16.1.11" {
			t.Fatalf("peer ip=%q, want 172.16.1.11", pkt.PeerVirtualIP)
		}
		var voice overlay.VoicePayload
		if err := json.Unmarshal(pkt.Payload, &voice); err != nil {
			t.Fatalf("voice payload: %v", err)
		}
		if voice.FromUserID != 10 || voice.ToUserID != 11 || voice.RoomID != 7 {
			t.Fatalf("voice route=%d->%d room=%d", voice.FromUserID, voice.ToUserID, voice.RoomID)
		}
		if voice.Codec != "opus" || voice.SampleRate != 48000 {
			t.Fatalf("voice codec=%q sampleRate=%d", voice.Codec, voice.SampleRate)
		}
	default:
		t.Fatalf("voice packet was not queued")
	}
	select {
	case pkt := <-app.voiceSend:
		t.Fatalf("unexpected extra voice packet to %s", pkt.PeerVirtualIP)
	default:
	}
}

func TestCancelConnectionResetsVoiceStateAndQueue(t *testing.T) {
	app := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.mu.Lock()
	app.connectCancel = cancel
	app.voiceState = room.VoiceState{
		RoomID: 7,
		Participants: []room.VoiceParticipant{
			{UserID: 10, Speaking: true},
		},
	}
	app.voiceMode = "sending"
	app.voiceMessage = "语音发送中"
	app.voiceSend <- client.VoicePacket{PeerVirtualIP: "172.16.1.11", Payload: []byte{1}}
	app.cancelConnectionLocked()
	app.mu.Unlock()

	select {
	case <-ctx.Done():
	default:
		t.Fatalf("connection context was not canceled")
	}
	got := app.CurrentState()
	if got.Voice.RoomID != 0 || len(got.Voice.Participants) != 0 {
		t.Fatalf("voice state was not reset: %+v", got.Voice)
	}
	if got.VoiceMode != "idle" || got.VoiceMessage != "未加入语音" {
		t.Fatalf("voice mode/message=%q/%q", got.VoiceMode, got.VoiceMessage)
	}
	select {
	case pkt := <-app.voiceSend:
		t.Fatalf("stale voice packet remained queued for %s", pkt.PeerVirtualIP)
	default:
	}
}
