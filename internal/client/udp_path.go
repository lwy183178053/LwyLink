package client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/overlay"
)

const (
	TransportModeTCPRelay    = "tcp-relay"
	TransportModeUDPProbing  = "udp-probing"
	TransportModeUDPDirect   = "udp-direct"
	TransportModeUDPDisabled = "udp-disabled"

	udpRegisterInterval      = 2 * time.Second
	udpProbeInterval         = 500 * time.Millisecond
	udpProbeWindow           = 6 * time.Second
	udpProbeRetryMin         = 5 * time.Second
	udpProbeRetryMax         = 30 * time.Second
	udpKeepaliveEvery        = time.Second
	udpWarmupDuration        = 3 * time.Second
	udpDirectFreshFor        = 8 * time.Second
	udpDirectTimeout         = 20 * time.Second
	udpDirectMaxHealthyRTT   = 350 * time.Millisecond
	udpDirectSwitchMargin    = 30 * time.Millisecond
	udpDirectHighRTTLimit    = 3
	udpDirectQualityCooldown = 20 * time.Second
	udpDisableFailureWindow  = 45 * time.Second
	udpDisableFailureLimit   = 3
)

type udpSendDecision int

const (
	udpSendRelay udpSendDecision = iota
	udpSendMirror
	udpSendDirect
)

type TransportStatus struct {
	Mode        string
	Message     string
	DirectIPs   []string
	Diagnostics []TransportPeerDiagnostic
}

type TransportPeerDiagnostic struct {
	VirtualIP           string
	CandidateCount      int
	CandidateTypes      []string
	SelectedType        string
	SelectedPath        string
	LastProbeAt         time.Time
	LastSuccessAt       time.Time
	LastFailureAt       time.Time
	LastRTT             time.Duration
	FailedProbes        int
	ConsecutiveFailures int
	FailureReason       string
	DisabledReason      string
	LastObservedPeerAt  time.Time
}

type udpManagerConfig struct {
	ServerAddr string
	Token      string
	Key        []byte
	STUNAddr   string
	RoomID     int64
	UserID     int64
	VirtualIP  string
	RelayRTT   func(peerIP string) time.Duration
	OnPacket   func([]byte)
	OnStatus   func(TransportStatus)
	OnDisabled func()
}

type udpManager struct {
	cfg    udpManagerConfig
	ctx    context.Context
	cancel context.CancelFunc
	conn   *net.UDPConn
	server *net.UDPAddr

	mu             sync.Mutex
	seq            uint64
	pathsByIP      map[string]*udpPeerPath
	pathsByID      map[string]*udpPeerPath
	closeOnce      sync.Once
	closeErr       error
	hasStatus      bool
	lastStatus     TransportStatus
	disabled       bool
	disabledReason string
	failures       []time.Time
	reflexive      []string
	controlSent    map[uint64]udpControlSent
	lifecycle      sync.Mutex
	closed         bool
	wg             sync.WaitGroup
}

type udpControlSent struct {
	sessionID string
	sentAt    time.Time
}

type udpPeerPath struct {
	virtualIP          string
	endpoint           *net.UDPAddr
	candidateEndpoints []udpCandidateEndpoint
	sessionID          string
	pairKey            []byte
	sendKey            []byte
	recvKey            []byte
	replay             *overlay.UDPReplayCache

	active   bool
	selected bool
	probing  bool
	lastSeen time.Time
	readyAt  time.Time

	failedProbes        int
	consecutiveFailures int
	nextProbeAt         time.Time
	lastProbeAt         time.Time
	lastSuccessAt       time.Time
	lastFailureAt       time.Time
	lastRTT             time.Duration
	highRTTSamples      int
	failureReason       string
	probeGeneration     uint64
}

type udpCandidateEndpoint struct {
	Addr *net.UDPAddr
	Type string
}

type udpPingTarget struct {
	endpoint  *net.UDPAddr
	sessionID string
	key       []byte
	seq       uint64
}

type udpProbeTarget struct {
	virtualIP  string
	sessionID  string
	generation uint64
}

func startUDPManager(ctx context.Context, cfg udpManagerConfig) (*udpManager, error) {
	if cfg.ServerAddr == "" || cfg.Token == "" || len(cfg.Key) != 32 || cfg.VirtualIP == "" {
		return nil, fmt.Errorf("udp transport config is incomplete")
	}
	server, err := net.ResolveUDPAddr("udp", cfg.ServerAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	managerCtx, cancel := context.WithCancel(ctx)
	m := &udpManager{
		cfg:         cfg,
		ctx:         managerCtx,
		cancel:      cancel,
		conn:        conn,
		server:      server,
		pathsByIP:   make(map[string]*udpPeerPath),
		pathsByID:   make(map[string]*udpPeerPath),
		controlSent: make(map[uint64]udpControlSent),
	}
	m.refreshSTUNReflexive()
	_ = conn.SetReadDeadline(time.Time{})
	m.emitStatusLocked()
	m.run(m.readLoop)
	m.run(m.registerLoop)
	m.run(m.keepaliveLoop)
	return m, nil
}

func (m *udpManager) Close() error {
	m.closeOnce.Do(func() {
		m.lifecycle.Lock()
		m.closed = true
		if m.cancel != nil {
			m.cancel()
		}
		m.closeErr = m.conn.Close()
		m.lifecycle.Unlock()
		m.wg.Wait()
	})
	return m.closeErr
}

func (m *udpManager) run(fn func()) bool {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	if m.closed {
		return false
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		fn()
	}()
	return true
}

func (m *udpManager) AddPeer(candidate overlay.UDPPeerCandidate) {
	if candidate.VirtualIP == "" || candidate.SessionID == "" || candidate.Key == "" {
		return
	}
	key, err := overlay.DecodeUDPKey(candidate.Key)
	if err != nil {
		return
	}
	endpoints := resolvePeerCandidateEndpoints(candidate)
	if len(endpoints) == 0 {
		return
	}
	now := time.Now()
	var probe udpProbeTarget
	var shouldProbe bool
	m.mu.Lock()
	path := m.updatePeerPathLocked(candidate.VirtualIP, endpoints, candidate.SessionID, key)
	probe, shouldProbe = m.startProbeTargetLocked(path, now)
	m.emitStatusLocked()
	m.mu.Unlock()
	if shouldProbe {
		m.launchProbe(probe)
	}
}

func (m *udpManager) SendToPeer(dstIP string, pkt []byte) udpSendDecision {
	return m.SendPayloadToPeer(dstIP, overlay.UDPTypeIP, pkt)
}

func (m *udpManager) SendPayloadToPeer(dstIP string, packetType byte, payload []byte) udpSendDecision {
	var probe udpProbeTarget
	var shouldProbe bool
	m.mu.Lock()
	if m.disabled {
		m.mu.Unlock()
		return udpSendRelay
	}
	now := time.Now()
	path := m.pathsByIP[dstIP]
	if !udpPathDirectReady(path, now) {
		if udpPathExpired(path, now) {
			resetInactiveUDPPath(path)
			probe, shouldProbe = m.startProbeTargetLocked(path, now)
			m.emitStatusLocked()
		}
		m.mu.Unlock()
		if shouldProbe {
			m.launchProbe(probe)
		}
		return udpSendRelay
	}
	relayPeerRTT := m.relayPeerRTT(dstIP)
	if !udpPathDirectSelected(path, now) {
		if !udpPathShouldSelectDirect(path, relayPeerRTT) {
			m.mu.Unlock()
			return udpSendRelay
		}
		path.selected = true
		m.emitStatusLocked()
	}
	endpoint := path.endpoint
	sessionID := path.sessionID
	key := cloneBytes(path.sendKey)
	seq := m.nextSeqLocked()
	decision := udpSendDirect
	if udpPathNeedsMirror(path, now) {
		decision = udpSendMirror
	}
	m.mu.Unlock()
	if err := m.sendEncrypted(endpoint, packetType, sessionID, key, seq, payload); err != nil {
		m.markInactive(sessionID)
		return udpSendRelay
	}
	return decision
}

func (m *udpManager) readLoop() {
	buf := make([]byte, 64*1024)
	for {
		_ = m.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, remote, err := m.conn.ReadFromUDP(buf)
		if err != nil {
			if m.ctx.Err() != nil {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Printf("udp transport read failed: %v", err)
			continue
		}
		raw := make([]byte, n)
		copy(raw, buf[:n])
		m.handlePacket(remote, raw)
	}
}

func (m *udpManager) handlePacket(remote *net.UDPAddr, raw []byte) {
	_, sessionID, err := overlay.UDPPacketID(raw)
	if err != nil {
		return
	}
	m.mu.Lock()
	path := m.pathsByID[sessionID]
	if path == nil {
		m.mu.Unlock()
		return
	}
	key := cloneBytes(path.recvKey)
	peerVirtualIP := path.virtualIP
	m.mu.Unlock()
	packet, err := overlay.DecodeUDPPacket(raw, key)
	if err != nil {
		return
	}
	if !m.acceptPeerNonce(sessionID, packet.Nonce) {
		return
	}
	m.notePeerSeen(sessionID, remote)
	m.handlePeerPacket(sessionID, peerVirtualIP, packet)
}

func (m *udpManager) handlePeerPacket(sessionID, peerVirtualIP string, packet overlay.UDPPacket) {
	switch packet.Type {
	case overlay.UDPTypeProbe:
		m.replyControl(sessionID, overlay.UDPTypeProbeAck, packet.Seq)
	case overlay.UDPTypeProbeAck:
		m.noteControlAck(sessionID, packet.Payload)
	case overlay.UDPTypePing:
		m.replyControl(sessionID, overlay.UDPTypePong, packet.Seq)
	case overlay.UDPTypePong:
		m.noteControlAck(sessionID, packet.Payload)
	case overlay.UDPTypeIP:
		if !m.acceptPeerIPPacket(peerVirtualIP, packet.Payload) {
			return
		}
		if m.cfg.OnPacket != nil {
			m.cfg.OnPacket(packet.Payload)
		}
	}
}

func (m *udpManager) acceptPeerIPPacket(peerVirtualIP string, pkt []byte) bool {
	if peerVirtualIP == "" {
		return false
	}
	_, pkt, err := overlay.UnpackIPPayload(pkt)
	if err != nil {
		return false
	}
	src, dst, err := overlay.IPv4SrcDst(pkt)
	return err == nil && src == peerVirtualIP && dst == m.cfg.VirtualIP
}

func (m *udpManager) registerLoop() {
	ticker := time.NewTicker(udpRegisterInterval)
	defer ticker.Stop()
	for {
		m.sendRegister()
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *udpManager) sendRegister() {
	payload := overlay.UDPRegisterPayload{
		RoomID:             m.cfg.RoomID,
		UserID:             m.cfg.UserID,
		VirtualIP:          m.cfg.VirtualIP,
		LANEndpoints:       m.localLANEndpoints(),
		ReflexiveEndpoints: m.reflexiveEndpoints(),
	}
	m.mu.Lock()
	seq := m.nextSeqLocked()
	m.mu.Unlock()
	raw, err := overlay.EncodeUDPJSON(overlay.UDPTypeRegister, m.cfg.Token, m.cfg.Key, seq, payload)
	if err != nil {
		return
	}
	_, _ = m.conn.WriteToUDP(raw, m.server)
}

func (m *udpManager) probeLoop(virtualIP, sessionID string, generation uint64) {
	deadline := time.Now().Add(udpProbeWindow)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		path := m.pathsByIP[virtualIP]
		if path == nil || path.sessionID != sessionID || path.active || !path.probing || path.probeGeneration != generation {
			m.mu.Unlock()
			return
		}
		endpoints := cloneUDPCandidateEndpoints(path.candidateEndpoints)
		key := cloneBytes(path.sendKey)
		seqs := make([]uint64, len(endpoints))
		now := time.Now()
		for i := range seqs {
			seqs[i] = m.nextSeqLocked()
			m.noteControlSentLocked(sessionID, seqs[i], now)
		}
		m.mu.Unlock()
		for i, endpoint := range endpoints {
			_ = m.sendEncrypted(endpoint.Addr, overlay.UDPTypeProbe, sessionID, key, seqs[i], []byte("probe"))
		}
		timer := time.NewTimer(udpProbeInterval)
		select {
		case <-m.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	m.mu.Lock()
	if path := m.pathsByIP[virtualIP]; path != nil && path.sessionID == sessionID && !path.active && path.probeGeneration == generation {
		now := time.Now()
		path.probing = false
		path.failedProbes++
		path.consecutiveFailures++
		path.lastFailureAt = now
		path.failureReason = "probe timeout"
		path.nextProbeAt = now.Add(udpProbeRetryDelay(path.failedProbes))
		m.recordFailureLocked(now)
		m.emitStatusLocked()
	}
	m.mu.Unlock()
}

func (m *udpManager) keepaliveLoop() {
	ticker := time.NewTicker(udpKeepaliveEvery)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		}
		m.mu.Lock()
		now := time.Now()
		pings := make([]udpPingTarget, 0, len(m.pathsByIP))
		probes := make([]udpProbeTarget, 0)
		for _, path := range m.pathsByIP {
			if udpPathExpired(path, now) {
				resetInactiveUDPPath(path)
				if probe, ok := m.startProbeTargetLocked(path, now); ok {
					probes = append(probes, probe)
				}
				continue
			}
			if path.active {
				pings = append(pings, udpPingTarget{
					endpoint:  path.endpoint,
					sessionID: path.sessionID,
					key:       cloneBytes(path.sendKey),
					seq:       m.nextSeqLocked(),
				})
				m.noteControlSentLocked(path.sessionID, pings[len(pings)-1].seq, now)
				continue
			}
			if probe, ok := m.startProbeTargetLocked(path, now); ok {
				probes = append(probes, probe)
			}
		}
		m.emitStatusLocked()
		m.mu.Unlock()
		for _, item := range probes {
			m.launchProbe(item)
		}
		for _, item := range pings {
			_ = m.sendEncrypted(item.endpoint, overlay.UDPTypePing, item.sessionID, item.key, item.seq, []byte("ping"))
		}
	}
}

func (m *udpManager) replyControl(sessionID string, packetType byte, receivedSeq uint64) {
	m.mu.Lock()
	current := m.pathsByID[sessionID]
	if current == nil {
		m.mu.Unlock()
		return
	}
	endpoint := current.endpoint
	peerSessionID := current.sessionID
	key := cloneBytes(current.sendKey)
	seq := m.nextSeqLocked()
	m.mu.Unlock()
	_ = m.sendEncrypted(endpoint, packetType, peerSessionID, key, seq, udpControlAckPayload(receivedSeq))
}

func (m *udpManager) notePeerSeen(sessionID string, remote *net.UDPAddr) {
	m.mu.Lock()
	defer m.mu.Unlock()
	path := m.pathsByID[sessionID]
	if path == nil {
		return
	}
	if remote != nil {
		cp := *remote
		path.endpoint = &cp
	}
	now := time.Now()
	path.lastSeen = now
	path.lastSuccessAt = path.lastSeen
	if !path.nextProbeAt.IsZero() && now.Before(path.nextProbeAt) && path.failureReason != "" {
		m.emitStatusLocked()
		return
	}
	wasActive := path.active
	if !path.active {
		path.readyAt = path.lastSeen
		path.selected = false
	}
	path.active = true
	path.probing = false
	path.failedProbes = 0
	path.consecutiveFailures = 0
	path.failureReason = ""
	path.nextProbeAt = time.Time{}
	if !wasActive {
		path.highRTTSamples = 0
	}
	m.emitStatusLocked()
}

func (m *udpManager) noteControlSentLocked(sessionID string, seq uint64, sentAt time.Time) {
	if m.controlSent == nil {
		m.controlSent = make(map[uint64]udpControlSent)
	}
	m.controlSent[seq] = udpControlSent{sessionID: sessionID, sentAt: sentAt}
	if len(m.controlSent) <= 512 {
		return
	}
	cutoff := sentAt.Add(-time.Minute)
	for itemSeq, item := range m.controlSent {
		if item.sentAt.Before(cutoff) || len(m.controlSent) > 512 {
			delete(m.controlSent, itemSeq)
		}
	}
}

func (m *udpManager) noteControlAck(sessionID string, payload []byte) {
	seq, ok := udpControlAckSeq(payload)
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sent, ok := m.controlSent[seq]
	if !ok || sent.sessionID != sessionID {
		return
	}
	delete(m.controlSent, seq)
	path := m.pathsByID[sessionID]
	if path == nil {
		return
	}
	rtt := time.Since(sent.sentAt)
	if rtt <= 0 || rtt > time.Minute {
		return
	}
	path.lastRTT = rtt
	path.lastSuccessAt = time.Now()
	if path.active {
		relayPeerRTT := m.relayPeerRTT(path.virtualIP)
		if udpPathDirectWorseThanRelay(path, relayPeerRTT) || (relayPeerRTT <= 0 && rtt > udpDirectMaxHealthyRTT) {
			path.highRTTSamples++
			if path.highRTTSamples >= udpDirectHighRTTLimit {
				m.markPathQualityPoorLocked(path, path.lastSuccessAt, fmt.Sprintf("direct rtt high: %s", rtt.Round(time.Millisecond)))
			}
		} else {
			path.highRTTSamples = 0
		}
	}
	m.emitStatusLocked()
}

func (m *udpManager) markPathQualityPoorLocked(path *udpPeerPath, now time.Time, reason string) {
	if path == nil {
		return
	}
	resetInactiveUDPPath(path)
	path.probing = false
	path.highRTTSamples = 0
	path.lastFailureAt = now
	path.failureReason = reason
	path.nextProbeAt = now.Add(udpDirectQualityCooldown)
}

func (m *udpManager) relayPeerRTT(peerIP string) time.Duration {
	if m.cfg.RelayRTT == nil {
		return 0
	}
	rtt := m.cfg.RelayRTT(peerIP)
	if rtt <= 0 {
		return 0
	}
	return rtt
}

func (m *udpManager) markInactive(sessionID string) {
	var probe udpProbeTarget
	var shouldProbe bool
	m.mu.Lock()
	if path := m.pathsByID[sessionID]; path != nil {
		now := time.Now()
		resetInactiveUDPPath(path)
		path.consecutiveFailures++
		path.lastFailureAt = now
		path.failureReason = "direct path inactive"
		m.recordFailureLocked(now)
		probe, shouldProbe = m.startProbeTargetLocked(path, now)
		m.emitStatusLocked()
	}
	m.mu.Unlock()
	if shouldProbe {
		m.launchProbe(probe)
	}
}

func (m *udpManager) updatePeerPathLocked(virtualIP string, endpoints []udpCandidateEndpoint, sessionID string, key []byte) *udpPeerPath {
	path := m.pathsByIP[virtualIP]
	if path == nil {
		path = &udpPeerPath{virtualIP: virtualIP}
		m.pathsByIP[virtualIP] = path
	}

	sendKey, recvKey := udpDirectionalKeys(key, m.cfg.VirtualIP, virtualIP)
	changed := path.sessionID != sessionID || !udpCandidateSlicesEqual(path.candidateEndpoints, endpoints) || !bytes.Equal(path.pairKey, key)
	if changed {
		if path.sessionID != "" && path.sessionID != sessionID {
			delete(m.pathsByID, path.sessionID)
		}
		path.endpoint = cloneUDPAddr(endpoints[0].Addr)
		path.candidateEndpoints = cloneUDPCandidateEndpoints(endpoints)
		path.sessionID = sessionID
		path.pairKey = cloneBytes(key)
		path.sendKey = sendKey
		path.recvKey = recvKey
		path.replay = overlay.NewUDPReplayCache(0)
		path.active = false
		path.selected = false
		path.probing = false
		path.failedProbes = 0
		path.consecutiveFailures = 0
		path.nextProbeAt = time.Time{}
		path.readyAt = time.Time{}
		path.lastFailureAt = time.Time{}
		path.lastSuccessAt = time.Time{}
		path.lastRTT = 0
		path.highRTTSamples = 0
		path.failureReason = ""
	}
	m.pathsByID[path.sessionID] = path

	return path
}

func (m *udpManager) acceptPeerNonce(sessionID string, nonce [12]byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	path := m.pathsByID[sessionID]
	if path == nil {
		return false
	}
	if path.replay == nil {
		path.replay = overlay.NewUDPReplayCache(0)
	}
	return path.replay.Accept(nonce)
}

func (m *udpManager) startProbeTargetLocked(path *udpPeerPath, now time.Time) (udpProbeTarget, bool) {
	if m.disabled || path == nil || path.active || path.probing || len(path.candidateEndpoints) == 0 || path.sessionID == "" || len(path.sendKey) != 32 || len(path.recvKey) != 32 {
		return udpProbeTarget{}, false
	}
	if !path.nextProbeAt.IsZero() && now.Before(path.nextProbeAt) {
		return udpProbeTarget{}, false
	}
	path.probing = true
	path.lastProbeAt = now
	path.probeGeneration++
	return udpProbeTarget{virtualIP: path.virtualIP, sessionID: path.sessionID, generation: path.probeGeneration}, true
}

func (m *udpManager) launchProbe(probe udpProbeTarget) {
	m.run(func() {
		m.probeLoop(probe.virtualIP, probe.sessionID, probe.generation)
	})
}

func udpPathDirectReady(path *udpPeerPath, now time.Time) bool {
	if path == nil || !path.active || path.lastSeen.IsZero() {
		return false
	}
	return now.Sub(path.lastSeen) <= udpDirectFreshFor
}

func udpPathDirectSelected(path *udpPeerPath, now time.Time) bool {
	return udpPathDirectReady(path, now) && path.selected
}

func udpPathShouldSelectDirect(path *udpPeerPath, relayPeerRTT time.Duration) bool {
	if path == nil || path.lastRTT <= 0 {
		return false
	}
	if relayPeerRTT > 0 {
		return path.lastRTT+udpDirectSwitchMargin < relayPeerRTT
	}
	return path.lastRTT <= udpDirectMaxHealthyRTT
}

func udpPathDirectWorseThanRelay(path *udpPeerPath, relayPeerRTT time.Duration) bool {
	if path == nil || path.lastRTT <= 0 || relayPeerRTT <= 0 {
		return false
	}
	return path.lastRTT > relayPeerRTT+udpDirectSwitchMargin
}

func udpPathNeedsMirror(path *udpPeerPath, now time.Time) bool {
	if path == nil || path.lastSeen.IsZero() {
		return false
	}
	return !path.readyAt.IsZero() && now.Sub(path.readyAt) < udpWarmupDuration
}

func udpPathExpired(path *udpPeerPath, now time.Time) bool {
	if path == nil || !path.active || path.lastSeen.IsZero() {
		return false
	}
	return now.Sub(path.lastSeen) > udpDirectTimeout
}

func resetInactiveUDPPath(path *udpPeerPath) {
	if path == nil {
		return
	}
	path.active = false
	path.selected = false
	path.failedProbes = 0
	path.consecutiveFailures = 0
	path.nextProbeAt = time.Time{}
	path.readyAt = time.Time{}
	path.highRTTSamples = 0
	path.failureReason = ""
}

func udpControlAckPayload(seq uint64) []byte {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, seq)
	return payload
}

func udpControlAckSeq(payload []byte) (uint64, bool) {
	if len(payload) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(payload), true
}

func udpPathDiagnosticState(path *udpPeerPath, now time.Time) string {
	switch {
	case path == nil:
		return ""
	case udpPathDirectSelected(path, now):
		return "selected"
	case path.active:
		return "candidate"
	case path.probing:
		return "probing"
	case !path.nextProbeAt.IsZero() && now.Before(path.nextProbeAt) && path.failureReason != "":
		return "cooldown"
	case path.failedProbes > 0:
		return "failed"
	case len(path.candidateEndpoints) > 0:
		return "new"
	default:
		return "relay"
	}
}

func udpProbeRetryDelay(failures int) time.Duration {
	if failures <= 1 {
		return udpProbeRetryMin
	}
	delay := udpProbeRetryMin
	for i := 1; i < failures; i++ {
		delay *= 2
		if delay >= udpProbeRetryMax {
			return udpProbeRetryMax
		}
	}
	return delay
}

func udpDirectionalKeys(pairKey []byte, localIP, peerIP string) (sendKey, recvKey []byte) {
	return udpDirectionalKey(pairKey, localIP, peerIP), udpDirectionalKey(pairKey, peerIP, localIP)
}

func udpDirectionalKey(pairKey []byte, fromIP, toIP string) []byte {
	mac := hmac.New(sha256.New, pairKey)
	_, _ = mac.Write([]byte("LwyLink UDP directional key v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(fromIP))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(toIP))
	return mac.Sum(nil)
}

func (m *udpManager) sendEncrypted(endpoint *net.UDPAddr, packetType byte, id string, key []byte, seq uint64, payload []byte) error {
	if endpoint == nil {
		return fmt.Errorf("missing udp endpoint")
	}
	raw, err := overlay.EncodeUDPPacket(packetType, id, key, seq, payload)
	if err != nil {
		return err
	}
	_, err = m.conn.WriteToUDP(raw, endpoint)
	return err
}

func (m *udpManager) nextSeqLocked() uint64 {
	m.seq++
	return m.seq
}

func (m *udpManager) emitStatusLocked() {
	if m.cfg.OnStatus == nil {
		return
	}
	status := m.transportStatusLocked(time.Now())
	if m.hasStatus && transportStatusEqual(status, m.lastStatus) {
		return
	}
	m.hasStatus = true
	m.lastStatus = cloneTransportStatus(status)
	go m.cfg.OnStatus(status)
}

func (m *udpManager) transportStatusLocked(now time.Time) TransportStatus {
	if m.disabled {
		return TransportStatus{Mode: TransportModeUDPDisabled, Message: "中继连接", Diagnostics: m.transportDiagnosticsLocked(now)}
	}
	probing := false
	directIPs := make([]string, 0)
	for _, path := range m.pathsByIP {
		if udpPathDirectSelected(path, now) {
			directIPs = append(directIPs, path.virtualIP)
		}
		if path.probing {
			probing = true
		}
	}
	diagnostics := m.transportDiagnosticsLocked(now)
	sort.Strings(directIPs)
	status := TransportStatus{Mode: TransportModeTCPRelay, Message: "中继连接", DirectIPs: directIPs, Diagnostics: diagnostics}
	if len(directIPs) > 0 {
		status.Mode = TransportModeUDPDirect
		status.Message = fmt.Sprintf("P2P 直连 · %d 条隧道", len(directIPs))
	} else if probing {
		status.Mode = TransportModeUDPProbing
		status.Message = "正在探测更优路径"
	}
	return status
}

func (m *udpManager) transportDiagnosticsLocked(now time.Time) []TransportPeerDiagnostic {
	diagnostics := make([]TransportPeerDiagnostic, 0, len(m.pathsByIP))
	for _, path := range m.pathsByIP {
		diagnostics = append(diagnostics, TransportPeerDiagnostic{
			VirtualIP:           path.virtualIP,
			CandidateCount:      len(path.candidateEndpoints),
			CandidateTypes:      udpCandidateTypes(path.candidateEndpoints),
			SelectedType:        udpCandidateTypeForAddr(path.candidateEndpoints, path.endpoint),
			SelectedPath:        udpPathDiagnosticState(path, now),
			LastProbeAt:         path.lastProbeAt,
			LastSuccessAt:       path.lastSuccessAt,
			LastFailureAt:       path.lastFailureAt,
			LastRTT:             path.lastRTT,
			FailedProbes:        path.failedProbes,
			ConsecutiveFailures: path.consecutiveFailures,
			FailureReason:       path.failureReason,
			DisabledReason:      m.disabledReason,
			LastObservedPeerAt:  path.lastSeen,
		})
	}
	sort.Slice(diagnostics, func(i, j int) bool { return diagnostics[i].VirtualIP < diagnostics[j].VirtualIP })
	return diagnostics
}

func cloneTransportStatus(status TransportStatus) TransportStatus {
	status.DirectIPs = append([]string(nil), status.DirectIPs...)
	status.Diagnostics = cloneTransportPeerDiagnostics(status.Diagnostics)
	return status
}

func cloneTransportPeerDiagnostics(items []TransportPeerDiagnostic) []TransportPeerDiagnostic {
	if len(items) == 0 {
		return nil
	}
	out := make([]TransportPeerDiagnostic, 0, len(items))
	for _, item := range items {
		item.CandidateTypes = append([]string(nil), item.CandidateTypes...)
		out = append(out, item)
	}
	return out
}

func transportStatusEqual(a, b TransportStatus) bool {
	return a.Mode == b.Mode && a.Message == b.Message && slices.Equal(a.DirectIPs, b.DirectIPs) && transportDiagnosticsEqual(a.Diagnostics, b.Diagnostics)
}

func transportDiagnosticsEqual(a, b []TransportPeerDiagnostic) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].VirtualIP != b[i].VirtualIP ||
			a[i].CandidateCount != b[i].CandidateCount ||
			!slices.Equal(a[i].CandidateTypes, b[i].CandidateTypes) ||
			a[i].SelectedType != b[i].SelectedType ||
			a[i].SelectedPath != b[i].SelectedPath ||
			!a[i].LastProbeAt.Equal(b[i].LastProbeAt) ||
			!a[i].LastSuccessAt.Equal(b[i].LastSuccessAt) ||
			!a[i].LastFailureAt.Equal(b[i].LastFailureAt) ||
			a[i].LastRTT != b[i].LastRTT ||
			a[i].FailedProbes != b[i].FailedProbes ||
			a[i].ConsecutiveFailures != b[i].ConsecutiveFailures ||
			a[i].FailureReason != b[i].FailureReason ||
			a[i].DisabledReason != b[i].DisabledReason ||
			!a[i].LastObservedPeerAt.Equal(b[i].LastObservedPeerAt) {
			return false
		}
	}
	return true
}

func (m *udpManager) recordFailureLocked(now time.Time) {
	if m.disabled {
		return
	}
	cutoff := now.Add(-udpDisableFailureWindow)
	kept := m.failures[:0]
	for _, item := range m.failures {
		if item.After(cutoff) {
			kept = append(kept, item)
		}
	}
	m.failures = append(kept, now)
	if len(m.failures) < udpDisableFailureLimit {
		return
	}
	m.disabled = true
	m.disabledReason = fmt.Sprintf("udp probing disabled after %d failures in %s", udpDisableFailureLimit, udpDisableFailureWindow)
	m.failures = nil
	for _, path := range m.pathsByIP {
		resetInactiveUDPPath(path)
		path.probing = false
	}
	if m.cfg.OnDisabled != nil {
		go m.cfg.OnDisabled()
	}
}

func cloneBytes(src []byte) []byte {
	return append([]byte(nil), src...)
}
