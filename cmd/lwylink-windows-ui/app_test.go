package main

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

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

func TestDisconnectPreservesVoiceState(t *testing.T) {
	app := NewApp()
	app.mu.Lock()
	app.connection = ConnectionState{Status: statusConnected, Message: "已连接", RoomID: 7}
	app.voiceState = room.VoiceState{
		RoomID: 7,
		Participants: []room.VoiceParticipant{
			{UserID: 10, Speaking: true},
		},
	}
	app.mu.Unlock()

	got, err := app.Disconnect()
	if err != nil {
		t.Fatalf("Disconnect returned error: %v", err)
	}
	if got.Connection.Status != statusDisconnected {
		t.Fatalf("connection status=%q, want %q", got.Connection.Status, statusDisconnected)
	}
	if got.Voice.RoomID != 7 || len(got.Voice.Participants) != 1 || got.Voice.Participants[0].UserID != 10 {
		t.Fatalf("voice state was changed: %+v", got.Voice)
	}
}

func TestOfficialClientPinsAPIBaseURL(t *testing.T) {
	if got := normalizeSavedAPIBaseURL("http://fake.example.test:8080/"); got != defaultAPIBaseURL {
		t.Fatalf("normalizeSavedAPIBaseURL()=%q, want official %q", got, defaultAPIBaseURL)
	}

	app := NewApp()
	state, err := app.SetSettings(SettingsInput{
		APIBaseURL:    "http://fake.example.test:8080",
		InterfaceName: defaultIfName,
	})
	if err != nil {
		t.Fatalf("SetSettings returned error: %v", err)
	}
	if state.APIBaseURL != defaultAPIBaseURL {
		t.Fatalf("state APIBaseURL=%q, want %q", state.APIBaseURL, defaultAPIBaseURL)
	}
}

func TestManualUpdateCheckCooldown(t *testing.T) {
	app := NewApp()
	app.mu.Lock()
	app.lastManualUpdateAt = time.Now()
	beforeSeq := app.updateSeq
	app.mu.Unlock()

	_, err := app.CheckForUpdates()
	if err == nil {
		t.Fatalf("expected manual update cooldown error")
	}
	if !strings.Contains(err.Error(), "检查更新太频繁") {
		t.Fatalf("unexpected cooldown error: %v", err)
	}
	app.mu.Lock()
	afterSeq := app.updateSeq
	app.mu.Unlock()
	if afterSeq != beforeSeq {
		t.Fatalf("update check seq changed during cooldown: got %d, want %d", afterSeq, beforeSeq)
	}
}

func TestShouldRestartUpdateDownloadAfterAuth(t *testing.T) {
	if !shouldRestartUpdateDownloadAfterAuth(UpdateState{Status: updateStatusIdle, LatestVersion: "9.9.9"}) {
		t.Fatalf("expected auth to restart update download when latest release is waiting for token")
	}
	if shouldRestartUpdateDownloadAfterAuth(UpdateState{Status: updateStatusDownloading, LatestVersion: "9.9.9"}) {
		t.Fatalf("downloading update should not restart")
	}
	if shouldRestartUpdateDownloadAfterAuth(UpdateState{Status: updateStatusIdle}) {
		t.Fatalf("empty latest version should not restart")
	}
}
