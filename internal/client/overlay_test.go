package client

import (
	"testing"

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
