package client

import (
	"net"
	"slices"
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/overlay"
)

func TestUDPPeerPathDeduplicatesCandidateWhileProbing(t *testing.T) {
	m := newTestUDPManager()
	endpoint := testUDPAddr(t, "127.0.0.1:30001")
	key := testUDPKey(1)
	now := time.Unix(100, 0)

	path, shouldProbe, generation := m.updatePeerPathLocked("10.66.1.11", []*net.UDPAddr{endpoint}, "pair-1", key, now)
	if !shouldProbe {
		t.Fatalf("first candidate did not start probing")
	}
	if generation != 1 || !path.probing {
		t.Fatalf("bad first probe state: generation=%d probing=%v", generation, path.probing)
	}

	_, shouldProbe, generation = m.updatePeerPathLocked("10.66.1.11", []*net.UDPAddr{endpoint}, "pair-1", key, now.Add(time.Second))
	if shouldProbe {
		t.Fatalf("duplicate candidate started another probe")
	}
	if generation != 0 {
		t.Fatalf("duplicate candidate returned generation=%d", generation)
	}
}

func TestUDPPeerPathHonorsProbeCooldownAndLimit(t *testing.T) {
	m := newTestUDPManager()
	endpoint := testUDPAddr(t, "127.0.0.1:30001")
	key := testUDPKey(1)
	now := time.Unix(100, 0)

	path, _, _ := m.updatePeerPathLocked("10.66.1.11", []*net.UDPAddr{endpoint}, "pair-1", key, now)
	path.probing = false
	path.failedProbes = 1
	path.nextProbeAt = now.Add(udpProbeRetryAfter)

	_, shouldProbe, _ := m.updatePeerPathLocked("10.66.1.11", []*net.UDPAddr{endpoint}, "pair-1", key, now.Add(5*time.Second))
	if shouldProbe {
		t.Fatalf("candidate started probing during cooldown")
	}

	_, shouldProbe, generation := m.updatePeerPathLocked("10.66.1.11", []*net.UDPAddr{endpoint}, "pair-1", key, now.Add(udpProbeRetryAfter+time.Second))
	if !shouldProbe {
		t.Fatalf("candidate did not retry after cooldown")
	}
	if generation != 2 {
		t.Fatalf("retry generation=%d, want 2", generation)
	}

	path.probing = false
	path.failedProbes = udpProbeMaxFailures
	path.nextProbeAt = time.Time{}
	_, shouldProbe, _ = m.updatePeerPathLocked("10.66.1.11", []*net.UDPAddr{endpoint}, "pair-1", key, now.Add(2*udpProbeRetryAfter))
	if shouldProbe {
		t.Fatalf("candidate retried after reaching failure limit")
	}
}

func TestUDPPeerPathEndpointChangeResetsFailures(t *testing.T) {
	m := newTestUDPManager()
	endpoint := testUDPAddr(t, "127.0.0.1:30001")
	key := testUDPKey(1)
	now := time.Unix(100, 0)

	path, _, _ := m.updatePeerPathLocked("10.66.1.11", []*net.UDPAddr{endpoint}, "pair-1", key, now)
	path.probing = false
	path.failedProbes = udpProbeMaxFailures

	nextEndpoint := testUDPAddr(t, "127.0.0.1:30002")
	path, shouldProbe, _ := m.updatePeerPathLocked("10.66.1.11", []*net.UDPAddr{nextEndpoint}, "pair-1", key, now.Add(time.Second))
	if !shouldProbe {
		t.Fatalf("endpoint change did not restart probing")
	}
	if path.failedProbes != 0 {
		t.Fatalf("endpoint change kept failedProbes=%d", path.failedProbes)
	}
	if path.endpoint.String() != nextEndpoint.String() {
		t.Fatalf("endpoint=%s, want %s", path.endpoint, nextEndpoint)
	}
}

func TestResolvePeerCandidateEndpointsPrefersLAN(t *testing.T) {
	candidate := overlay.UDPPeerCandidate{
		Endpoint:     "203.0.113.9:30001",
		LANEndpoints: []string{"192.168.1.20:30001", "127.0.0.1:30001", "10.0.0.5:30001"},
	}

	endpoints := resolvePeerCandidateEndpoints(candidate)
	got := udpAddrStrings(endpoints)
	want := []string{"10.0.0.5:30001", "192.168.1.20:30001", "203.0.113.9:30001"}
	if !slices.Equal(got, want) {
		t.Fatalf("endpoints=%v, want %v", got, want)
	}
}

func newTestUDPManager() *udpManager {
	return &udpManager{
		pathsByIP: make(map[string]*udpPeerPath),
		pathsByID: make(map[string]*udpPeerPath),
	}
}

func testUDPAddr(t *testing.T, value string) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", value)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func testUDPKey(seed byte) []byte {
	key := make([]byte, 32)
	key[0] = seed
	return key
}

func udpAddrStrings(addrs []*net.UDPAddr) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.String())
	}
	return out
}
