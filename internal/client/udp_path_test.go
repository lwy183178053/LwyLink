package client

import (
	"bytes"
	"context"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/overlay"
)

func TestUDPManagerCloseCancelsWorkerAndWaits(t *testing.T) {
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &udpManager{ctx: ctx, cancel: cancel, conn: conn}
	entered := make(chan struct{})
	var released atomic.Bool

	if !m.run(func() {
		close(entered)
		<-m.ctx.Done()
		released.Store(true)
	}) {
		t.Fatalf("worker did not start")
	}
	<-entered

	if err := m.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !released.Load() {
		t.Fatalf("Close returned before worker observed cancellation")
	}
	if m.ctx.Err() == nil {
		t.Fatalf("manager context was not canceled")
	}
	if m.run(func() {}) {
		t.Fatalf("worker started after manager was closed")
	}
}

func TestUDPDirectionalKeysAreReciprocalAndDistinct(t *testing.T) {
	pairKey := testUDPKey(7)
	aSend, aRecv := udpDirectionalKeys(pairKey, "172.16.1.10", "172.16.1.11")
	bSend, bRecv := udpDirectionalKeys(pairKey, "172.16.1.11", "172.16.1.10")

	if !bytes.Equal(aSend, bRecv) {
		t.Fatalf("A send key does not match B receive key")
	}
	if !bytes.Equal(aRecv, bSend) {
		t.Fatalf("A receive key does not match B send key")
	}
	if bytes.Equal(aSend, aRecv) {
		t.Fatalf("directional keys should differ")
	}

	raw, err := overlay.EncodeUDPPacket(overlay.UDPTypePing, "pair-1", aSend, 1, []byte("ping"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := overlay.DecodeUDPPacket(raw, bRecv); err != nil {
		t.Fatalf("decode with reciprocal key: %v", err)
	}
	if _, err := overlay.DecodeUDPPacket(raw, bSend); err == nil {
		t.Fatalf("decode with opposite direction key unexpectedly succeeded")
	}
}

func TestUDPManagerRejectsReplayedPacket(t *testing.T) {
	m := newTestUDPManager()
	var delivered int
	m.cfg.OnPacket = func(pkt []byte) {
		delivered++
	}
	endpoint := testUDPAddr(t, "127.0.0.1:30001")
	pairKey := testUDPKey(1)
	path := m.updatePeerPathLocked("172.16.1.11", []*net.UDPAddr{endpoint}, "pair-1", pairKey)
	raw, err := overlay.EncodeUDPPacket(overlay.UDPTypeIP, "pair-1", path.recvKey, 1, []byte("payload"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	m.handlePacket(endpoint, raw)
	m.handlePacket(endpoint, raw)

	if delivered != 1 {
		t.Fatalf("delivered packets=%d, want 1", delivered)
	}
}

func TestUDPPeerPathDeduplicatesCandidateWhileProbing(t *testing.T) {
	m := newTestUDPManager()
	endpoint := testUDPAddr(t, "127.0.0.1:30001")
	key := testUDPKey(1)
	now := time.Unix(100, 0)

	path := m.updatePeerPathLocked("172.16.1.11", []*net.UDPAddr{endpoint}, "pair-1", key)
	probe, shouldProbe := m.startProbeTargetLocked(path, now)
	if !shouldProbe {
		t.Fatalf("first candidate did not start probing")
	}
	if probe.generation != 1 || !path.probing {
		t.Fatalf("bad first probe state: generation=%d probing=%v", probe.generation, path.probing)
	}

	path = m.updatePeerPathLocked("172.16.1.11", []*net.UDPAddr{endpoint}, "pair-1", key)
	probe, shouldProbe = m.startProbeTargetLocked(path, now.Add(time.Second))
	if shouldProbe {
		t.Fatalf("duplicate candidate started another probe")
	}
	if probe.generation != 0 {
		t.Fatalf("duplicate candidate returned generation=%d", probe.generation)
	}
}

func TestUDPPeerPathHonorsProbeCooldownAndContinuesRetrying(t *testing.T) {
	m := newTestUDPManager()
	endpoint := testUDPAddr(t, "127.0.0.1:30001")
	key := testUDPKey(1)
	now := time.Unix(100, 0)

	path := m.updatePeerPathLocked("172.16.1.11", []*net.UDPAddr{endpoint}, "pair-1", key)
	path.probing = false
	path.probeGeneration = 1
	path.failedProbes = 1
	path.nextProbeAt = now.Add(udpProbeRetryDelay(path.failedProbes))

	path = m.updatePeerPathLocked("172.16.1.11", []*net.UDPAddr{endpoint}, "pair-1", key)
	_, shouldProbe := m.startProbeTargetLocked(path, now.Add(time.Second))
	if shouldProbe {
		t.Fatalf("candidate started probing during cooldown")
	}

	path = m.updatePeerPathLocked("172.16.1.11", []*net.UDPAddr{endpoint}, "pair-1", key)
	probe, shouldProbe := m.startProbeTargetLocked(path, now.Add(udpProbeRetryDelay(path.failedProbes)+time.Second))
	if !shouldProbe {
		t.Fatalf("candidate did not retry after cooldown")
	}
	if probe.generation != 2 {
		t.Fatalf("retry generation=%d, want 2", probe.generation)
	}

	path.probing = false
	path.failedProbes = 100
	path.nextProbeAt = time.Time{}
	path = m.updatePeerPathLocked("172.16.1.11", []*net.UDPAddr{endpoint}, "pair-1", key)
	probe, shouldProbe = m.startProbeTargetLocked(path, now.Add(2*udpProbeRetryMax))
	if !shouldProbe {
		t.Fatalf("candidate stopped retrying after many failures")
	}
	if probe.generation != 3 {
		t.Fatalf("continuous retry generation=%d, want 3", probe.generation)
	}
}

func TestUDPProbeRetryDelayCaps(t *testing.T) {
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 0, want: udpProbeRetryMin},
		{failures: 1, want: udpProbeRetryMin},
		{failures: 2, want: 2 * udpProbeRetryMin},
		{failures: 3, want: 4 * udpProbeRetryMin},
		{failures: 10, want: udpProbeRetryMax},
	}
	for _, tt := range tests {
		if got := udpProbeRetryDelay(tt.failures); got != tt.want {
			t.Fatalf("failures=%d delay=%s, want %s", tt.failures, got, tt.want)
		}
	}
}

func TestUDPPathFallsBackBeforeItExpires(t *testing.T) {
	now := time.Unix(100, 0)
	path := &udpPeerPath{
		active:   true,
		lastSeen: now,
	}

	if !udpPathDirectReady(path, now.Add(udpDirectFreshFor)) {
		t.Fatalf("path should stay direct-ready inside fresh window")
	}
	if udpPathDirectReady(path, now.Add(udpDirectFreshFor+time.Millisecond)) {
		t.Fatalf("path remained direct-ready after fresh window")
	}
	if udpPathExpired(path, now.Add(udpDirectFreshFor+time.Millisecond)) {
		t.Fatalf("path expired during tcp fallback grace window")
	}
	if !udpPathExpired(path, now.Add(udpDirectTimeout+time.Millisecond)) {
		t.Fatalf("path did not expire after direct timeout")
	}
}

func TestUDPPeerPathEndpointChangeResetsFailures(t *testing.T) {
	m := newTestUDPManager()
	endpoint := testUDPAddr(t, "127.0.0.1:30001")
	key := testUDPKey(1)
	now := time.Unix(100, 0)

	path := m.updatePeerPathLocked("172.16.1.11", []*net.UDPAddr{endpoint}, "pair-1", key)
	path.probing = false
	path.failedProbes = 100

	nextEndpoint := testUDPAddr(t, "127.0.0.1:30002")
	path = m.updatePeerPathLocked("172.16.1.11", []*net.UDPAddr{nextEndpoint}, "pair-1", key)
	_, shouldProbe := m.startProbeTargetLocked(path, now.Add(time.Second))
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

func TestResolvePeerCandidateEndpointsSkipsVirtualIP(t *testing.T) {
	candidate := overlay.UDPPeerCandidate{
		VirtualIP:    "172.16.1.11",
		Endpoint:     "203.0.113.9:30001",
		LANEndpoints: []string{"172.16.1.11:30001", "192.168.1.20:30001"},
	}

	endpoints := resolvePeerCandidateEndpoints(candidate)
	got := udpAddrStrings(endpoints)
	want := []string{"192.168.1.20:30001", "203.0.113.9:30001"}
	if !slices.Equal(got, want) {
		t.Fatalf("endpoints=%v, want %v", got, want)
	}
}

func newTestUDPManager() *udpManager {
	return &udpManager{
		cfg:       udpManagerConfig{VirtualIP: "172.16.1.10"},
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
