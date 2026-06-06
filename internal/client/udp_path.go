package client

import (
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
	virtualIP string
	endpoint  *net.UDPAddr
	sessionID string
	key       []byte

	active   bool
	probing  bool
	lastSeen time.Time
}

type udpPingTarget struct {
	endpoint  *net.UDPAddr
	sessionID string
	key       []byte
	seq       uint64
}

type udpProbeTarget struct {
	virtualIP string
	sessionID string
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
	if candidate.VirtualIP == "" || candidate.Endpoint == "" || candidate.SessionID == "" || candidate.Key == "" {
		return
	}
	key, err := overlay.DecodeUDPKey(candidate.Key)
	if err != nil {
		return
	}
	endpoint, err := net.ResolveUDPAddr("udp", candidate.Endpoint)
	if err != nil {
		return
	}
	m.mu.Lock()
	path := m.pathsByIP[candidate.VirtualIP]
	if path == nil {
		path = &udpPeerPath{virtualIP: candidate.VirtualIP}
		m.pathsByIP[candidate.VirtualIP] = path
	}
	if path.sessionID != "" && path.sessionID != candidate.SessionID {
		delete(m.pathsByID, path.sessionID)
	}
	path.endpoint = endpoint
	path.sessionID = candidate.SessionID
	path.key = key
	path.probing = !path.active
	m.pathsByID[path.sessionID] = path
	m.emitStatusLocked()
	m.mu.Unlock()
	go m.probeLoop(candidate.VirtualIP, candidate.SessionID)
}

func (m *udpManager) SendToPeer(dstIP string, pkt []byte) bool {
	m.mu.Lock()
	path := m.pathsByIP[dstIP]
	if path == nil || !path.active || time.Since(path.lastSeen) > udpDirectTimeout {
		if path != nil && path.active {
			path.active = false
			path.probing = true
			m.emitStatusLocked()
		}
		m.mu.Unlock()
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
		RoomID:    m.cfg.RoomID,
		UserID:    m.cfg.UserID,
		VirtualIP: m.cfg.VirtualIP,
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

func (m *udpManager) probeLoop(virtualIP, sessionID string) {
	deadline := time.Now().Add(udpProbeWindow)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		path := m.pathsByIP[virtualIP]
		if path == nil || path.sessionID != sessionID || path.active {
			m.mu.Unlock()
			return
		}
		endpoint := path.endpoint
		key := cloneBytes(path.key)
		seq := m.nextSeqLocked()
		m.mu.Unlock()
		_ = m.sendEncrypted(endpoint, overlay.UDPTypeProbe, sessionID, key, seq, []byte("probe"))
		time.Sleep(udpProbeInterval)
	}
	m.mu.Lock()
	if path := m.pathsByIP[virtualIP]; path != nil && path.sessionID == sessionID && !path.active {
		path.probing = false
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
				path.probing = true
				probes = append(probes, udpProbeTarget{virtualIP: path.virtualIP, sessionID: path.sessionID})
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
			go m.probeLoop(item.virtualIP, item.sessionID)
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
	m.emitStatusLocked()
}

func (m *udpManager) markInactive(sessionID string) {
	m.mu.Lock()
	var virtualIP string
	if path := m.pathsByID[sessionID]; path != nil {
		path.active = false
		path.probing = true
		virtualIP = path.virtualIP
		m.emitStatusLocked()
	}
	m.mu.Unlock()
	if virtualIP != "" {
		go m.probeLoop(virtualIP, sessionID)
	}
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
