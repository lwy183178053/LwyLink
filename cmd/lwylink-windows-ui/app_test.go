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
			Diagnostics: []client.TransportPeerDiagnostic{
				{VirtualIP: "172.16.1.11", CandidateCount: 2, SelectedType: "lan", FailedProbes: 1},
			},
		})
	})

	got := app.CurrentState().Connection
	if got.Status != statusConnected {
		t.Fatalf("status=%q, want %q", got.Status, statusConnected)
	}
	if got.Message != "udp direct" {
		t.Fatalf("message=%q", got.Message)
	}
	if got.TransportMode != client.TransportModeUDPDirect {
		t.Fatalf("transport mode=%q", got.TransportMode)
	}
	if !slices.Equal(got.DirectPeerIPs, []string{"172.16.1.11"}) {
		t.Fatalf("direct peers=%v", got.DirectPeerIPs)
	}
	if len(got.TransportDiagnostics) != 1 || got.TransportDiagnostics[0].SelectedType != "lan" || got.TransportDiagnostics[0].CandidateCount != 2 {
		t.Fatalf("transport diagnostics=%+v", got.TransportDiagnostics)
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
