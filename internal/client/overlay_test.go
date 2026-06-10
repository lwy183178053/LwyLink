package client

import (
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/overlay"
	"github.com/LwyV-Labs/LwyLink/internal/room"
)

func TestNormalizeConnectOptionsFillsRoomDefaults(t *testing.T) {
	opts := normalizeConnectOptions(ConnectOptions{
		Room: room.Room{
			NodeOverlayAddr: "node.example:35768",
			NodeUDPAddr:     "node.example:35769",
			NodePublicKey:   "node-key",
		},
	})
	if opts.OverlayAddr != "node.example:35768" || opts.UDPOverlayAddr != "node.example:35769" || opts.ServerPublicKey != "node-key" {
		t.Fatalf("room defaults were not applied: %+v", opts)
	}
}

func TestSendLatestUDPConfigKeepsNewestWhenChannelFull(t *testing.T) {
	ch := make(chan overlay.UDPControl, 1)
	ch <- overlay.UDPControl{Type: overlay.UDPControlConfig, Token: "old"}

	sendLatestUDPConfig(ch, overlay.UDPControl{Type: overlay.UDPControlConfig, Token: "new"})

	got := <-ch
	if got.Token != "new" {
		t.Fatalf("config token=%q, want newest", got.Token)
	}
}

func TestSendLatestUDPPeerKeepsNewestWhenChannelFull(t *testing.T) {
	ch := make(chan overlay.UDPPeerCandidate, 1)
	ch <- overlay.UDPPeerCandidate{VirtualIP: "172.16.1.10"}

	sendLatestUDPPeer(ch, overlay.UDPPeerCandidate{VirtualIP: "172.16.1.11"})

	got := <-ch
	if got.VirtualIP != "172.16.1.11" {
		t.Fatalf("peer ip=%q, want newest", got.VirtualIP)
	}
}

func TestRuntimeRelayProbeMeasuresPerPeerRTT(t *testing.T) {
	runtimeState := &connectionRuntime{}
	now := time.Unix(100, 0)
	probe := runtimeState.nextRelayProbe("172.16.1.10", "172.16.1.11", now)
	if probe.FromIP != "172.16.1.10" || probe.ToIP != "172.16.1.11" || probe.Seq == 0 || probe.Reply {
		t.Fatalf("bad relay probe: %+v", probe)
	}

	runtimeState.noteRelayProbeReply(overlay.RelayProbePayload{
		Seq:    probe.Seq,
		FromIP: "172.16.1.11",
		ToIP:   "172.16.1.10",
		Reply:  true,
	}, now.Add(85*time.Millisecond))

	if got := runtimeState.relayPeerRTTValue("172.16.1.11"); got != 85*time.Millisecond {
		t.Fatalf("peer relay rtt=%s, want 85ms", got)
	}
}

func TestRuntimeRelayProbeRejectsMismatchedReply(t *testing.T) {
	runtimeState := &connectionRuntime{}
	now := time.Unix(100, 0)
	probe := runtimeState.nextRelayProbe("172.16.1.10", "172.16.1.11", now)

	runtimeState.noteRelayProbeReply(overlay.RelayProbePayload{
		Seq:    probe.Seq,
		FromIP: "172.16.1.12",
		ToIP:   "172.16.1.10",
		Reply:  true,
	}, now.Add(85*time.Millisecond))

	if got := runtimeState.relayPeerRTTValue("172.16.1.11"); got != 0 {
		t.Fatalf("mismatched reply recorded rtt=%s", got)
	}
}

func TestRuntimeRelayProbePeersPruneRTTs(t *testing.T) {
	runtimeState := &connectionRuntime{}
	runtimeState.relayPeerRTTs = map[string]time.Duration{
		"172.16.1.11": 40 * time.Millisecond,
		"172.16.1.12": 50 * time.Millisecond,
	}

	runtimeState.setRelayProbePeers([]string{"172.16.1.12"})

	if got := runtimeState.relayPeerRTTValue("172.16.1.11"); got != 0 {
		t.Fatalf("removed peer rtt remained=%s", got)
	}
	if got := runtimeState.relayPeerRTTValue("172.16.1.12"); got != 50*time.Millisecond {
		t.Fatalf("kept peer rtt=%s, want 50ms", got)
	}
}
