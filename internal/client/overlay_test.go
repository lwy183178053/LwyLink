package client

import (
	"testing"

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
