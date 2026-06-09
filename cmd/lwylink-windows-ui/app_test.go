package main

import (
	"context"
	"slices"
	"testing"

	"github.com/LwyV-Labs/LwyLink/internal/client"
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
}
