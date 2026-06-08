package client

import (
	"testing"

	"github.com/LwyV-Labs/LwyLink/internal/overlay"
	"github.com/LwyV-Labs/LwyLink/internal/room"
)

func TestNormalizeConnectOptionsDefaultsP2PEnabled(t *testing.T) {
	opts := normalizeConnectOptions(ConnectOptions{
		Room: room.Room{
			NodeOverlayAddr: "node.example:35768",
			NodePublicKey:   "node-key",
		},
	})
	if opts.P2PEnabled == nil {
		t.Fatalf("P2PEnabled was not defaulted")
	}
	if !opts.P2PEnabled() {
		t.Fatalf("P2P should default to enabled")
	}
}

func TestNormalizeConnectOptionsKeepsExplicitP2PDisabled(t *testing.T) {
	opts := normalizeConnectOptions(ConnectOptions{
		P2PEnabled: func() bool { return false },
		Room: room.Room{
			NodeOverlayAddr: "node.example:35768",
			NodePublicKey:   "node-key",
		},
	})
	if opts.P2PEnabled == nil || opts.P2PEnabled() {
		t.Fatalf("explicit disabled P2P was not preserved")
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
