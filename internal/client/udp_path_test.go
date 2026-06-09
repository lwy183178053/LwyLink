package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/overlay"
	"github.com/LwyV-Labs/LwyLink/internal/room"
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
	path := m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(endpoint), "pair-1", pairKey)
	raw, err := overlay.EncodeUDPPacket(overlay.UDPTypeIP, "pair-1", path.recvKey, 1, overlay.PackIPPayload(1, testIPv4Packet("172.16.1.11", "172.16.1.10")))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	m.handlePacket(endpoint, raw)
	m.handlePacket(endpoint, raw)

	if delivered != 1 {
		t.Fatalf("delivered packets=%d, want 1", delivered)
	}
}

func TestUDPManagerAcceptsOnlyExpectedPeerIPPacket(t *testing.T) {
	m := newTestUDPManager()
	var delivered [][]byte
	m.cfg.OnPacket = func(pkt []byte) {
		delivered = append(delivered, append([]byte(nil), pkt...))
	}
	endpoint := testUDPAddr(t, "127.0.0.1:30001")
	pairKey := testUDPKey(1)
	path := m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(endpoint), "pair-1", pairKey)

	valid := overlay.PackIPPayload(1, testIPv4Packet("172.16.1.11", "172.16.1.10"))
	forgedSource := overlay.PackIPPayload(2, testIPv4Packet("172.16.1.99", "172.16.1.10"))
	wrongDestination := overlay.PackIPPayload(3, testIPv4Packet("172.16.1.11", "172.16.1.12"))
	for _, pkt := range [][]byte{valid, forgedSource, wrongDestination} {
		raw, err := overlay.EncodeUDPPacket(overlay.UDPTypeIP, "pair-1", path.recvKey, m.nextSeqLocked(), pkt)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		m.handlePacket(endpoint, raw)
	}

	if len(delivered) != 1 {
		t.Fatalf("delivered packets=%d, want 1", len(delivered))
	}
	if !bytes.Equal(delivered[0], valid) {
		t.Fatalf("delivered packet mismatch")
	}
}

func TestUDPManagersProbeAndDeliverDirectIPPacket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pairKey := testUDPKey(3)
	pairID := "pair-direct"
	aDelivered := make(chan []byte, 1)
	bDelivered := make(chan []byte, 1)
	statuses := &udpStatusRecorder{}

	a := startTestUDPManager(t, ctx, udpManagerConfig{
		ServerAddr: "127.0.0.1:9",
		Token:      "token-a",
		Key:        testUDPKey(11),
		RoomID:     1,
		UserID:     10,
		VirtualIP:  "172.16.1.10",
		OnPacket: func(pkt []byte) {
			aDelivered <- append([]byte(nil), pkt...)
		},
		OnStatus: statuses.add,
	})
	defer a.Close()
	b := startTestUDPManager(t, ctx, udpManagerConfig{
		ServerAddr: "127.0.0.1:9",
		Token:      "token-b",
		Key:        testUDPKey(12),
		RoomID:     1,
		UserID:     11,
		VirtualIP:  "172.16.1.11",
		OnPacket: func(pkt []byte) {
			bDelivered <- append([]byte(nil), pkt...)
		},
		OnStatus: statuses.add,
	})
	defer b.Close()

	a.AddPeer(overlay.UDPPeerCandidate{
		VirtualIP: "172.16.1.11",
		Endpoint:  udpLoopbackEndpoint(t, b.conn),
		SessionID: pairID,
		Key:       overlay.EncodeUDPKey(pairKey),
	})
	b.AddPeer(overlay.UDPPeerCandidate{
		VirtualIP: "172.16.1.10",
		Endpoint:  udpLoopbackEndpoint(t, a.conn),
		SessionID: pairID,
		Key:       overlay.EncodeUDPKey(pairKey),
	})

	waitUntil(t, time.Second, func() bool {
		a.mu.Lock()
		aReady := udpPathDirectReady(a.pathsByIP["172.16.1.11"], time.Now())
		a.mu.Unlock()
		b.mu.Lock()
		bReady := udpPathDirectReady(b.pathsByIP["172.16.1.10"], time.Now())
		b.mu.Unlock()
		return aReady && bReady
	})

	pkt := testIPv4Packet("172.16.1.10", "172.16.1.11")
	payload := overlay.PackIPPayload(99, pkt)
	if got := a.SendToPeer("172.16.1.11", payload); got == udpSendRelay {
		t.Fatalf("direct send returned relay")
	}
	select {
	case got := <-bDelivered:
		if !bytes.Equal(got, payload) {
			t.Fatalf("delivered packet mismatch")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for direct packet")
	}
	select {
	case pkt := <-aDelivered:
		t.Fatalf("sender unexpectedly received packet length=%d", len(pkt))
	default:
	}
	if !statuses.sawMode(TransportModeUDPDirect) {
		t.Fatalf("did not observe udp-direct status; got %v", statuses.modes())
	}
}

func TestUDPPathNeedsMirrorOnlyDuringWarmup(t *testing.T) {
	now := time.Unix(100, 0)
	path := &udpPeerPath{
		active:   true,
		readyAt:  now,
		lastSeen: now,
	}

	if !udpPathNeedsMirror(path, now.Add(time.Second)) {
		t.Fatalf("path should mirror during warmup")
	}
	path.lastSeen = now.Add(udpWarmupDuration)
	if udpPathNeedsMirror(path, now.Add(udpWarmupDuration+time.Second)) {
		t.Fatalf("path should stop mirroring after warmup while fresh")
	}
	if udpPathNeedsMirror(path, now.Add(udpWarmupDuration*2+time.Second)) {
		t.Fatalf("path should not resume mirroring after warmup")
	}
}

func TestUDPManagerDisablesAfterRepeatedProbeFailures(t *testing.T) {
	m := newTestUDPManager()
	var disabled atomic.Bool
	m.cfg.OnDisabled = func() {
		disabled.Store(true)
	}
	endpoint := testUDPAddr(t, "127.0.0.1:30001")
	path := m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(endpoint), "pair-1", testUDPKey(1))
	now := time.Unix(100, 0)

	for i := 0; i < udpDisableFailureLimit; i++ {
		m.recordFailureLocked(now.Add(time.Duration(i) * time.Second))
	}
	if !m.disabled {
		t.Fatalf("manager was not disabled after repeated failures")
	}
	waitUntil(t, time.Second, disabled.Load)
	if path.active || path.probing {
		t.Fatalf("path remained active/probing after disable")
	}
	if got := m.SendToPeer("172.16.1.11", overlay.PackIPPayload(1, testIPv4Packet("172.16.1.10", "172.16.1.11"))); got != udpSendRelay {
		t.Fatalf("disabled manager send=%v, want relay", got)
	}
}

func TestUDPPeerPathDeduplicatesCandidateWhileProbing(t *testing.T) {
	m := newTestUDPManager()
	endpoint := testUDPAddr(t, "127.0.0.1:30001")
	key := testUDPKey(1)
	now := time.Unix(100, 0)

	path := m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(endpoint), "pair-1", key)
	probe, shouldProbe := m.startProbeTargetLocked(path, now)
	if !shouldProbe {
		t.Fatalf("first candidate did not start probing")
	}
	if probe.generation != 1 || !path.probing {
		t.Fatalf("bad first probe state: generation=%d probing=%v", probe.generation, path.probing)
	}

	path = m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(endpoint), "pair-1", key)
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

	path := m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(endpoint), "pair-1", key)
	path.probing = false
	path.probeGeneration = 1
	path.failedProbes = 1
	path.nextProbeAt = now.Add(udpProbeRetryDelay(path.failedProbes))

	path = m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(endpoint), "pair-1", key)
	_, shouldProbe := m.startProbeTargetLocked(path, now.Add(time.Second))
	if shouldProbe {
		t.Fatalf("candidate started probing during cooldown")
	}

	path = m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(endpoint), "pair-1", key)
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
	path = m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(endpoint), "pair-1", key)
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

	path := m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(endpoint), "pair-1", key)
	path.probing = false
	path.failedProbes = 100

	nextEndpoint := testUDPAddr(t, "127.0.0.1:30002")
	path = m.updatePeerPathLocked("172.16.1.11", testUDPCandidates(nextEndpoint), "pair-1", key)
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
		Endpoint:           "203.0.113.9:30001",
		LANEndpoints:       []string{"192.168.1.20:30001", "127.0.0.1:30001", "10.0.0.5:30001"},
		ReflexiveEndpoints: []string{"198.51.100.10:30001"},
	}

	endpoints := resolvePeerCandidateEndpoints(candidate)
	got := udpAddrStrings(endpoints)
	want := []string{"10.0.0.5:30001", "192.168.1.20:30001", "198.51.100.10:30001", "203.0.113.9:30001"}
	if !slices.Equal(got, want) {
		t.Fatalf("endpoints=%v, want %v", got, want)
	}
	if gotTypes, wantTypes := udpCandidateTypes(endpoints), []string{udpCandidateTypeLAN, udpCandidateTypeLAN, udpCandidateTypeSTUN, udpCandidateTypeObserved}; !slices.Equal(gotTypes, wantTypes) {
		t.Fatalf("endpoint types=%v, want %v", gotTypes, wantTypes)
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

func TestParseSTUNBindingResponseXORMappedAddress(t *testing.T) {
	var txID [12]byte
	copy(txID[:], []byte("abcdefghijkl"))
	raw := make([]byte, 32)
	binary.BigEndian.PutUint16(raw[0:2], stunBindingSuccess)
	binary.BigEndian.PutUint16(raw[2:4], 12)
	binary.BigEndian.PutUint32(raw[4:8], stunMagicCookie)
	copy(raw[8:20], txID[:])
	binary.BigEndian.PutUint16(raw[20:22], stunAttrXORMappedAddr)
	binary.BigEndian.PutUint16(raw[22:24], 8)
	raw[25] = 0x01
	binary.BigEndian.PutUint16(raw[26:28], 40000^uint16(stunMagicCookie>>16))
	cookie := make([]byte, 4)
	binary.BigEndian.PutUint32(cookie, stunMagicCookie)
	ip := net.IPv4(198, 51, 100, 9).To4()
	for i := 0; i < 4; i++ {
		raw[28+i] = ip[i] ^ cookie[i]
	}

	endpoint, ok := parseSTUNBindingResponse(raw, txID)
	if !ok || endpoint != "198.51.100.9:40000" {
		t.Fatalf("stun endpoint=%q ok=%v, want 198.51.100.9:40000 true", endpoint, ok)
	}
}

func TestNormalizeSTUNServer(t *testing.T) {
	tests := map[string]string{
		"":                         defaultOverlaySTUNAddr,
		"stun:stun.example.com":    "stun.example.com:19302",
		"stun.example.com:3478":    "stun.example.com:3478",
		"192.0.2.10":               "192.0.2.10:19302",
		"stun:192.0.2.10:3478":     "192.0.2.10:3478",
		"  stun:example.net:9999 ": "example.net:9999",
	}
	for input, want := range tests {
		if got := normalizeSTUNServer(input); got != want {
			t.Fatalf("normalizeSTUNServer(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestNormalizeConnectOptionsDefaultsSTUNToUDPOverlayAddr(t *testing.T) {
	opts := normalizeConnectOptions(ConnectOptions{
		OverlayAddr:     "node.example.com:35768",
		UDPOverlayAddr:  "node.example.com:35768",
		ServerPublicKey: "key",
		Room: room.Room{
			ID: 1,
		},
	})
	if opts.STUNAddr != "node.example.com:35768" {
		t.Fatalf("STUNAddr=%q, want UDP overlay addr", opts.STUNAddr)
	}
}

func newTestUDPManager() *udpManager {
	return &udpManager{
		cfg:       udpManagerConfig{VirtualIP: "172.16.1.10"},
		pathsByIP: make(map[string]*udpPeerPath),
		pathsByID: make(map[string]*udpPeerPath),
	}
}

func startTestUDPManager(t *testing.T, ctx context.Context, cfg udpManagerConfig) *udpManager {
	t.Helper()
	m, err := startUDPManager(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = m.Close()
	})
	return m
}

func listenTestUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})
	return conn
}

func udpLoopbackEndpoint(t *testing.T, conn *net.UDPConn) string {
	t.Helper()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.Port <= 0 {
		t.Fatalf("bad udp local addr: %v", conn.LocalAddr())
	}
	return net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", addr.Port))
}

type udpStatusRecorder struct {
	mu       sync.Mutex
	statuses []TransportStatus
}

func (r *udpStatusRecorder) add(status TransportStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, status)
}

func (r *udpStatusRecorder) sawMode(mode string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, status := range r.statuses {
		if status.Mode == mode {
			return true
		}
	}
	return false
}

func (r *udpStatusRecorder) modes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.statuses))
	for _, status := range r.statuses {
		out = append(out, status.Mode)
	}
	return out
}

func waitUntil(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func testUDPAddr(t *testing.T, value string) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", value)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func testUDPCandidates(addrs ...*net.UDPAddr) []udpCandidateEndpoint {
	out := make([]udpCandidateEndpoint, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, udpCandidateEndpoint{Addr: addr, Type: udpCandidateTypeObserved})
	}
	return out
}

func testUDPKey(seed byte) []byte {
	key := make([]byte, 32)
	key[0] = seed
	return key
}

func udpAddrStrings(endpoints []udpCandidateEndpoint) []string {
	out := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		out = append(out, endpoint.Addr.String())
	}
	return out
}

func udpCandidateTypes(endpoints []udpCandidateEndpoint) []string {
	out := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		out = append(out, endpoint.Type)
	}
	return out
}

func testIPv4Packet(src, dst string) []byte {
	src4 := net.ParseIP(src).To4()
	dst4 := net.ParseIP(dst).To4()
	if src4 == nil || dst4 == nil {
		panic("bad test ip")
	}
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	pkt[2] = 0
	pkt[3] = 20
	pkt[8] = 64
	pkt[9] = 17
	copy(pkt[12:16], src4)
	copy(pkt[16:20], dst4)
	return pkt
}
