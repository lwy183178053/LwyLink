package client

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/overlay"
)

const (
	TransportModeTCPRelay    = "tcp-relay"
	TransportModeUDPProbing  = "udp-probing"
	TransportModeUDPDirect   = "udp-direct"
	TransportModeUDPDisabled = "udp-disabled"

	udpRegisterInterval = 2 * time.Second
	udpProbeInterval    = 500 * time.Millisecond
	udpProbeWindow      = 6 * time.Second
	udpProbeRetryAfter  = 30 * time.Second
	udpProbeMaxFailures = 3
	udpKeepaliveEvery   = 2 * time.Second
	udpDirectTimeout    = 9 * time.Second
)

type TransportStatus struct {
	Mode        string
	Message     string
	DirectPeers int
}

type udpManagerConfig struct {
	ServerAddr string
	Token      string
	Key        []byte
	RoomID     int64
	UserID     int64
	VirtualIP  string
	OnPacket   func([]byte)
	OnStatus   func(TransportStatus)
}

type udpManager struct {
	cfg    udpManagerConfig
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
}

type udpPeerPath struct {
	virtualIP          string
	endpoint           *net.UDPAddr
	candidateEndpoints []*net.UDPAddr
	sessionID          string
	key                []byte

	active   bool
	probing  bool
	lastSeen time.Time

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
	m := &udpManager{
		cfg:       cfg,
		conn:      conn,
		server:    server,
		pathsByIP: make(map[string]*udpPeerPath),
		pathsByID: make(map[string]*udpPeerPath),
	}
	m.emitStatusLocked()
	go m.readLoop(ctx)
	go m.registerLoop(ctx)
	go m.keepaliveLoop(ctx)
	return m, nil
}

func (m *udpManager) Close() error {
	m.closeOnce.Do(func() {
		m.closeErr = m.conn.Close()
	})
	return m.closeErr
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
	var probe *udpProbeTarget
	m.mu.Lock()
	path, shouldProbe, generation := m.updatePeerPathLocked(candidate.VirtualIP, endpoints, candidate.SessionID, key, now)
	if shouldProbe {
		probe = &udpProbeTarget{virtualIP: path.virtualIP, sessionID: path.sessionID, generation: generation}
	}
	m.emitStatusLocked()
	m.mu.Unlock()
	if probe != nil {
		go m.probeLoop(probe.virtualIP, probe.sessionID, probe.generation)
	}
}

func (m *udpManager) SendToPeer(dstIP string, pkt []byte) bool {
	var probe *udpProbeTarget
	m.mu.Lock()
	path := m.pathsByIP[dstIP]
	if path == nil || !path.active || time.Since(path.lastSeen) > udpDirectTimeout {
		if path != nil && path.active {
			path.active = false
			path.failedProbes = 0
			path.nextProbeAt = time.Time{}
			if generation, ok := m.startProbeLocked(path, time.Now()); ok {
				probe = &udpProbeTarget{virtualIP: path.virtualIP, sessionID: path.sessionID, generation: generation}
			}
			m.emitStatusLocked()
		}
		m.mu.Unlock()
		if probe != nil {
			go m.probeLoop(probe.virtualIP, probe.sessionID, probe.generation)
		}
		return false
	}
	endpoint := path.endpoint
	sessionID := path.sessionID
	key := cloneBytes(path.key)
	seq := m.nextSeqLocked()
	m.mu.Unlock()
	if err := m.sendEncrypted(endpoint, overlay.UDPTypeIP, sessionID, key, seq, pkt); err != nil {
		m.markInactive(sessionID)
		return false
	}
	return true
}

func (m *udpManager) readLoop(ctx context.Context) {
	buf := make([]byte, 64*1024)
	for {
		_ = m.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, remote, err := m.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
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
	key := cloneBytes(path.key)
	m.mu.Unlock()
	packet, err := overlay.DecodeUDPPacket(raw, key)
	if err != nil {
		return
	}
	m.notePeerSeen(sessionID, remote)
	switch packet.Type {
	case overlay.UDPTypeProbe:
		m.replyControl(sessionID, overlay.UDPTypeProbeAck)
	case overlay.UDPTypeProbeAck:
	case overlay.UDPTypePing:
		m.replyControl(sessionID, overlay.UDPTypePong)
	case overlay.UDPTypePong:
	case overlay.UDPTypeIP:
		if m.cfg.OnPacket != nil {
			m.cfg.OnPacket(packet.Payload)
		}
	}
}

func (m *udpManager) registerLoop(ctx context.Context) {
	ticker := time.NewTicker(udpRegisterInterval)
	defer ticker.Stop()
	for {
		m.sendRegister()
		select {
		case <-ctx.Done():
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
		key := cloneBytes(path.key)
		seqs := make([]uint64, len(endpoints))
		for i := range seqs {
			seqs[i] = m.nextSeqLocked()
		}
		m.mu.Unlock()
		for i, endpoint := range endpoints {
			_ = m.sendEncrypted(endpoint, overlay.UDPTypeProbe, sessionID, key, seqs[i], []byte("probe"))
		}
		time.Sleep(udpProbeInterval)
	}
	m.mu.Lock()
	if path := m.pathsByIP[virtualIP]; path != nil && path.sessionID == sessionID && !path.active && path.probeGeneration == generation {
		path.probing = false
		path.failedProbes++
		if path.failedProbes < udpProbeMaxFailures {
			path.nextProbeAt = time.Now().Add(udpProbeRetryAfter)
		} else {
			path.nextProbeAt = time.Time{}
		}
		m.emitStatusLocked()
	}
	m.mu.Unlock()
}

func (m *udpManager) keepaliveLoop(ctx context.Context) {
	ticker := time.NewTicker(udpKeepaliveEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		m.mu.Lock()
		now := time.Now()
		pings := make([]udpPingTarget, 0, len(m.pathsByIP))
		probes := make([]udpProbeTarget, 0)
		for _, path := range m.pathsByIP {
			if path.active && now.Sub(path.lastSeen) > udpDirectTimeout {
				path.active = false
				path.failedProbes = 0
				path.nextProbeAt = time.Time{}
				if generation, ok := m.startProbeLocked(path, now); ok {
					probes = append(probes, udpProbeTarget{virtualIP: path.virtualIP, sessionID: path.sessionID, generation: generation})
				}
				continue
			}
			if path.active {
				pings = append(pings, udpPingTarget{
					endpoint:  path.endpoint,
					sessionID: path.sessionID,
					key:       cloneBytes(path.key),
					seq:       m.nextSeqLocked(),
				})
			}
		}
		m.emitStatusLocked()
		m.mu.Unlock()
		for _, item := range probes {
			go m.probeLoop(item.virtualIP, item.sessionID, item.generation)
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
	key := cloneBytes(current.key)
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
	path.active = true
	path.probing = false
	path.failedProbes = 0
	path.nextProbeAt = time.Time{}
	m.emitStatusLocked()
}

func (m *udpManager) markInactive(sessionID string) {
	var probe *udpProbeTarget
	m.mu.Lock()
	if path := m.pathsByID[sessionID]; path != nil {
		path.active = false
		path.failedProbes = 0
		path.nextProbeAt = time.Time{}
		if generation, ok := m.startProbeLocked(path, time.Now()); ok {
			probe = &udpProbeTarget{virtualIP: path.virtualIP, sessionID: path.sessionID, generation: generation}
		}
		m.emitStatusLocked()
	}
	m.mu.Unlock()
	if probe != nil {
		go m.probeLoop(probe.virtualIP, probe.sessionID, probe.generation)
	}
}

func (m *udpManager) updatePeerPathLocked(virtualIP string, endpoints []*net.UDPAddr, sessionID string, key []byte, now time.Time) (*udpPeerPath, bool, uint64) {
	path := m.pathsByIP[virtualIP]
	if path == nil {
		path = &udpPeerPath{virtualIP: virtualIP}
		m.pathsByIP[virtualIP] = path
	}

	changed := path.sessionID != sessionID || !udpAddrSlicesEqual(path.candidateEndpoints, endpoints) || !bytes.Equal(path.key, key)
	if changed {
		if path.sessionID != "" && path.sessionID != sessionID {
			delete(m.pathsByID, path.sessionID)
		}
		path.endpoint = cloneUDPAddr(endpoints[0])
		path.candidateEndpoints = cloneUDPAddrs(endpoints)
		path.sessionID = sessionID
		path.key = cloneBytes(key)
		path.active = false
		path.probing = false
		path.failedProbes = 0
		path.nextProbeAt = time.Time{}
	}
	m.pathsByID[path.sessionID] = path

	generation, shouldProbe := m.startProbeLocked(path, now)
	return path, shouldProbe, generation
}

func (m *udpManager) startProbeLocked(path *udpPeerPath, now time.Time) (uint64, bool) {
	if path == nil || path.active || path.probing || len(path.candidateEndpoints) == 0 || path.sessionID == "" || len(path.key) != 32 {
		return 0, false
	}
	if path.failedProbes >= udpProbeMaxFailures {
		return 0, false
	}
	if !path.nextProbeAt.IsZero() && now.Before(path.nextProbeAt) {
		return 0, false
	}
	path.probing = true
	path.probeGeneration++
	return path.probeGeneration, true
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
	active := 0
	probing := false
	for _, path := range m.pathsByIP {
		if path.active {
			active++
		}
		if path.probing {
			probing = true
		}
	}
	status := TransportStatus{Mode: TransportModeTCPRelay, Message: "稳定连接", DirectPeers: active}
	if active > 0 {
		status.Mode = TransportModeUDPDirect
		status.Message = fmt.Sprintf("智能加速已开启 · %d 人", active)
	} else if probing {
		status.Mode = TransportModeUDPProbing
		status.Message = "正在优化连接"
	}
	if m.hasStatus && status == m.lastStatus {
		return
	}
	m.hasStatus = true
	m.lastStatus = status
	go m.cfg.OnStatus(status)
}

func cloneBytes(src []byte) []byte {
	return append([]byte(nil), src...)
}
