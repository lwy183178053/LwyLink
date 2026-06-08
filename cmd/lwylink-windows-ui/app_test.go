package main

import (
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

func TestVoiceFrameFreshRejectsOldFrames(t *testing.T) {
	now := time.Now()
	if !voiceFrameFresh(now.UnixMilli(), now) {
		t.Fatalf("current voice frame was rejected")
	}
	if voiceFrameFresh(now.Add(-maxVoiceFrameAge-time.Millisecond).UnixMilli(), now) {
		t.Fatalf("expired voice frame was accepted")
	}
	if voiceFrameFresh(0, now) {
		t.Fatalf("missing timestamp was accepted")
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
	if _, err := app.SendVoiceAudioFrame(VoiceAudioFrameInput{RoomID: 7, Seq: 2, TimestampMS: time.Now().UnixMilli(), SampleRate: 16000, Payload: []byte{3, 4}}); err != nil {
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
	default:
		t.Fatalf("voice packet was not queued")
	}
	select {
	case pkt := <-app.voiceSend:
		t.Fatalf("unexpected extra voice packet to %s", pkt.PeerVirtualIP)
	default:
	}
}
