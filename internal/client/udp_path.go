package client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"log"
	"net"
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

	udpRegisterInterval     = 2 * time.Second
	udpProbeInterval        = 500 * time.Millisecond
	udpProbeWindow          = 6 * time.Second
	udpProbeRetryMin        = 5 * time.Second
	udpProbeRetryMax        = 30 * time.Second
	udpKeepaliveEvery       = time.Second
	udpWarmupDuration       = 3 * time.Second
	udpDirectFreshFor       = 8 * time.Second
	udpDirectTimeout        = 20 * time.Second
	udpDisableFailureWindow = 45 * time.Second
	udpDisableFailureLimit  = 3
)

type udpSendDecision int

const (
	udpSendRelay udpSendDecision = iota
	udpSendMirror
	udpSendDirect
)

type TransportStatus struct {
	Mode      string
	Message   string
	DirectIPs []string
}

type udpManagerConfig struct {
	ServerAddr string
	Token      string
	Key        []byte
	RoomID     int64
	UserID     int64
	VirtualIP  string
	OnPacket   func([]byte)
	OnVoice    func([]byte)
	OnStatus   func(TransportStatus)
	OnDisabled func()
}

type udpManager struct {
	cfg    udpManagerConfig
	ctx    context.Context
	cancel context.CancelFunc
	conn   *net.UDPConn
	server *net.UDPAddr

	mu         sync.Mutex
	seq        uint64
	pathsByIP  map[string]*udpPeerPath
	pathsByID  map[string]*udpPeerPath
	closeOnce  sync.Once
	closeErr   error
	hasStatus  bool
	lastStatus TransportStatus
	disabled   bool
	failures   []time.Time
	lifecycle  sync.Mutex
	closed     bool
	wg         sync.WaitGroup
}

type udpPeerPath struct {
	virtualIP          string
	endpoint           *net.UDPAddr
	candidateEndpoints []*net.UDPAddr
	sessionID          string
	pairKey            []byte
	sendKey            []byte
	recvKey            []byte
	replay             *overlay.UDPReplayCache

	active   bool
	probing  bool
	lastSeen time.Time
	readyAt  time.Time

	failedProbes    int
	nextProbeAt     time.Time
	probeGeneration uint64
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
		cfg:       cfg,
		ctx:       managerCtx,
		cancel:    cancel,
		conn:      conn,
		server:    server,
		pathsByIP: make(map[string]*udpPeerPath),
		pathsByID: make(map[string]*udpPeerPath),
	}
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
		m.replyControl(sessionID, overlay.UDPTypeProbeAck)
	case overlay.UDPTypeProbeAck:
	case overlay.UDPTypePing:
		m.replyControl(sessionID, overlay.UDPTypePong)
	case overlay.UDPTypePong:
	case overlay.UDPTypeIP:
		if !m.acceptPeerIPPacket(peerVirtualIP, packet.Payload) {
			return
		}
		if m.cfg.OnPacket != nil {
			m.cfg.OnPacket(packet.Payload)
		}
	case overlay.UDPTypeVoice:
		if m.cfg.OnVoice != nil {
			m.cfg.OnVoice(packet.Payload)
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
		RoomID:       m.cfg.RoomID,
		UserID:       m.cfg.UserID,
		VirtualIP:    m.cfg.VirtualIP,
		LANEndpoints: m.localLANEndpoints(),
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
		endpoints := cloneUDPAddrs(path.candidateEndpoints)
		key := cloneBytes(path.sendKey)
		seqs := make([]uint64, len(endpoints))
		for i := range seqs {
			seqs[i] = m.nextSeqLocked()
		}
		m.mu.Unlock()
		for i, endpoint := range endpoints {
			_ = m.sendEncrypted(endpoint, overlay.UDPTypeProbe, sessionID, key, seqs[i], []byte("probe"))
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
		path.probing = false
		path.failedProbes++
		path.nextProbeAt = time.Now().Add(udpProbeRetryDelay(path.failedProbes))
		m.recordFailureLocked(time.Now())
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

func (m *udpManager) replyControl(sessionID string, packetType byte) {
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
	_ = m.sendEncrypted(endpoint, packetType, peerSessionID, key, seq, []byte("ok"))
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
	path.lastSeen = time.Now()
	if !path.active {
		path.readyAt = path.lastSeen
	}
	path.active = true
	path.probing = false
	path.failedProbes = 0
	path.nextProbeAt = time.Time{}
	m.emitStatusLocked()
}

func (m *udpManager) markInactive(sessionID string) {
	var probe udpProbeTarget
	var shouldProbe bool
	m.mu.Lock()
	if path := m.pathsByID[sessionID]; path != nil {
		now := time.Now()
		resetInactiveUDPPath(path)
		m.recordFailureLocked(now)
		probe, shouldProbe = m.startProbeTargetLocked(path, now)
		m.emitStatusLocked()
	}
	m.mu.Unlock()
	if shouldProbe {
		m.launchProbe(probe)
	}
}

func (m *udpManager) updatePeerPathLocked(virtualIP string, endpoints []*net.UDPAddr, sessionID string, key []byte) *udpPeerPath {
	path := m.pathsByIP[virtualIP]
	if path == nil {
		path = &udpPeerPath{virtualIP: virtualIP}
		m.pathsByIP[virtualIP] = path
	}

	sendKey, recvKey := udpDirectionalKeys(key, m.cfg.VirtualIP, virtualIP)
	changed := path.sessionID != sessionID || !udpAddrSlicesEqual(path.candidateEndpoints, endpoints) || !bytes.Equal(path.pairKey, key)
	if changed {
		if path.sessionID != "" && path.sessionID != sessionID {
			delete(m.pathsByID, path.sessionID)
		}
		path.endpoint = cloneUDPAddr(endpoints[0])
		path.candidateEndpoints = cloneUDPAddrs(endpoints)
		path.sessionID = sessionID
		path.pairKey = cloneBytes(key)
		path.sendKey = sendKey
		path.recvKey = recvKey
		path.replay = overlay.NewUDPReplayCache(0)
		path.active = false
		path.probing = false
		path.failedProbes = 0
		path.nextProbeAt = time.Time{}
		path.readyAt = time.Time{}
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
	path.failedProbes = 0
	path.nextProbeAt = time.Time{}
	path.readyAt = time.Time{}
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
	if m.disabled {
		status := TransportStatus{Mode: TransportModeUDPDisabled, Message: "UDP不稳定，已使用稳定连接"}
		if m.hasStatus && transportStatusEqual(status, m.lastStatus) {
			return
		}
		m.hasStatus = true
		m.lastStatus = cloneTransportStatus(status)
		go m.cfg.OnStatus(status)
		return
	}
	probing := false
	directIPs := make([]string, 0)
	now := time.Now()
	for _, path := range m.pathsByIP {
		if udpPathDirectReady(path, now) {
			directIPs = append(directIPs, path.virtualIP)
		}
		if path.probing {
			probing = true
		}
	}
	sort.Strings(directIPs)
	status := TransportStatus{Mode: TransportModeTCPRelay, Message: "稳定连接", DirectIPs: directIPs}
	if len(directIPs) > 0 {
		status.Mode = TransportModeUDPDirect
		status.Message = fmt.Sprintf("智能加速已开启 · %d 条隧道", len(directIPs))
	} else if probing {
		status.Mode = TransportModeUDPProbing
		status.Message = "正在优化连接"
	}
	if m.hasStatus && transportStatusEqual(status, m.lastStatus) {
		return
	}
	m.hasStatus = true
	m.lastStatus = cloneTransportStatus(status)
	go m.cfg.OnStatus(status)
}

func cloneTransportStatus(status TransportStatus) TransportStatus {
	status.DirectIPs = append([]string(nil), status.DirectIPs...)
	return status
}

func transportStatusEqual(a, b TransportStatus) bool {
	if a.Mode != b.Mode || a.Message != b.Message || len(a.DirectIPs) != len(b.DirectIPs) {
		return false
	}
	for i := range a.DirectIPs {
		if a.DirectIPs[i] != b.DirectIPs[i] {
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
