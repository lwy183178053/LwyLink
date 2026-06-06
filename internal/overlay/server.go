package overlay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/account"
	"github.com/LwyV-Labs/LwyLink/internal/room"
	"github.com/LwyV-Labs/LwyLink/internal/tcpx"
	"github.com/LwyV-Labs/LwyLink/internal/vdhcp"
)

type PresenceStore interface {
	SetOnline(ctx context.Context, roomID, userID int64, ip string)
	SetOffline(ctx context.Context, roomID, userID int64)
}

type Server struct {
	addr       string
	nodeID     int64
	privateKey string

	accounts *account.Service
	rooms    *room.Service
	leases   *vdhcp.Manager
	presence PresenceStore

	defaultDNS []string
	defaultMTU int

	mu       sync.RWMutex
	byConn   map[*tcpx.SecureConn]*session
	byRoomIP map[int64]map[string]*session
}

func NewServer(addr string, nodeID int64, privateKey string, accounts *account.Service, rooms *room.Service, leases *vdhcp.Manager, presence PresenceStore, defaultDNS []string, defaultMTU int) *Server {
	return &Server{
		addr:       addr,
		nodeID:     nodeID,
		privateKey: privateKey,
		accounts:   accounts,
		rooms:      rooms,
		leases:     leases,
		presence:   presence,
		defaultDNS: append([]string(nil), defaultDNS...),
		defaultMTU: defaultMTU,
		byConn:     make(map[*tcpx.SecureConn]*session),
		byRoomIP:   make(map[int64]map[string]*session),
	}
}

// ListenAndServe starts the tcpx listener. Clients authenticate at the overlay
// protocol layer after the X25519/AES-GCM channel is established.
func (s *Server) ListenAndServe(ctx context.Context) error {
	privateKeyHex, err := tcpx.NormalizePrivateKeyHex(s.privateKey)
	if err != nil {
		return err
	}

	server, err := tcpx.NewServer(tcpx.ServerConfig{
		Addr:                s.addr,
		StaticPrivateKeyHex: privateKeyHex,
		BaseConfig: tcpx.BaseConfig{
			ReadTimeout:       18 * time.Second,
			WriteTimeout:      8 * time.Second,
			HeartbeatBase:     5 * time.Second,
			HeartbeatJitter:   time.Second,
			KeyRotateInterval: 45 * time.Second,
			OldKeyGrace:       30 * time.Second,
		},
		OnDisconnect: s.onDisconnect,
		OnMessage:    s.onMessage,
	})
	if err != nil {
		return err
	}

	go s.watchRoomEvents(ctx)
	log.Printf("encrypted overlay listening on %s", s.addr)
	return server.ListenAndServe(ctx)
}

func (s *Server) onMessage(conn *tcpx.SecureConn, raw []byte) {
	typ, payload, err := UnpackFrame(raw)
	if err != nil {
		_ = conn.Close()
		return
	}

	if sess := s.bySecureConn(conn); sess != nil {
		s.handleSessionFrame(sess, typ, payload)
		return
	}

	// Before a session is registered, the only valid message is auth. This keeps
	// unauthenticated clients from sending IP packets or consuming vDHCP leases.
	if typ != FrameAuth {
		_ = s.writeJSON(conn, FrameError, map[string]string{"error": "first frame must be auth"})
		_ = conn.Close()
		return
	}
	var req AuthRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		_ = s.writeJSON(conn, FrameError, map[string]string{"error": "bad auth payload"})
		_ = conn.Close()
		return
	}

	// The room public key is checked against Redis here, not just against MySQL,
	// so deleted or closed rooms stop accepting joins and overlay connects.
	user, claims, err := s.accounts.AuthenticateTokenClaims(context.Background(), req.Token)
	if err != nil {
		_ = s.writeJSON(conn, FrameError, map[string]string{"error": "invalid token"})
		_ = conn.Close()
		return
	}
	roomInfo, err := s.rooms.EnsureMemberByRoomKey(context.Background(), s.nodeID, req.RoomID, user.ID, req.RoomPublicKey)
	if err != nil {
		_ = s.writeJSON(conn, FrameError, map[string]string{"error": err.Error()})
		_ = conn.Close()
		return
	}
	lease, err := s.leases.Acquire(roomInfo.ID, user.ID, roomInfo.NetworkCIDR, s.defaultDNS, s.defaultMTU)
	if err != nil {
		_ = s.writeJSON(conn, FrameError, map[string]string{"error": err.Error()})
		_ = conn.Close()
		return
	}

	// Register first, then send DHCP. If the client starts sending packets
	// immediately after DHCP, the server can already route by room/IP.
	sess := newSession(conn, user.ID, user.Username, claims.SessionID, roomInfo.ID, lease)
	old := s.register(sess)
	if old != nil {
		old.close()
	}
	go s.writeLoop(sess)
	go s.refreshOnlineLoop(context.Background(), sess)

	if err := s.writeJSON(conn, FrameDHCP, DHCPResponse{
		RequestID:    req.RequestID,
		IP:           lease.IP,
		SubnetMask:   lease.SubnetMask,
		Gateway:      lease.Gateway,
		DNS:          lease.DNS,
		MTU:          lease.MTU,
		LeaseSeconds: lease.LeaseSeconds,
	}); err != nil {
		sess.close()
		return
	}

	log.Printf("overlay joined room=%d user=%s ip=%s", sess.roomID, sess.username, sess.lease.IP)
}

func (s *Server) handleSessionFrame(sess *session, typ FrameType, payload []byte) {
	switch typ {
	case FrameIP:
		s.forwardIP(sess, payload)
	case FramePing:
		_ = sess.conn.Write(PackFrame(FramePing, nil))
	default:
		_ = s.writeJSON(sess.conn, FrameError, map[string]string{"error": fmt.Sprintf("unsupported frame %d", typ)})
		sess.close()
	}
}

func (s *Server) writeLoop(sess *session) {
	for {
		select {
		case <-sess.done:
			return
		case pkt := <-sess.send:
			if err := sess.conn.Write(PackFrame(FrameIP, pkt)); err != nil {
				sess.close()
				return
			}
		}
	}
}

func (s *Server) onDisconnect(conn *tcpx.SecureConn, err error) {
	if conn == nil {
		if err != nil && !errors.Is(err, io.EOF) {
			log.Printf("overlay handshake failed: %v", err)
		}
		return
	}
	sess := s.bySecureConn(conn)
	if sess != nil {
		s.unregister(context.Background(), sess)
	}
}

func (s *Server) forwardIP(from *session, pkt []byte) {
	src, dst, err := ipv4SrcDst(pkt)
	if err != nil {
		return
	}
	// Security boundary: a client may only send packets from its leased source
	// IP. Discovery packets are still allowed, but they are copied only to other
	// live sessions in the same room.
	if src != from.lease.IP {
		return
	}
	if isRoomFanoutDestination(dst, from.lease.IP, from.lease.SubnetMask) {
		s.fanoutRoom(from, pkt)
		return
	}
	target := s.findByIP(from.roomID, dst)
	if target == nil || target == from {
		return
	}
	target.enqueue(pkt)
}

func (s *Server) fanoutRoom(from *session, pkt []byte) {
	targets := s.roomPeers(from)
	for _, target := range targets {
		target.enqueue(pkt)
	}
}

func (s *Server) register(sess *session) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byConn[sess.conn] = sess
	roomMap := s.byRoomIP[sess.roomID]
	if roomMap == nil {
		roomMap = make(map[string]*session)
		s.byRoomIP[sess.roomID] = roomMap
	}
	old := roomMap[sess.lease.IP]
	roomMap[sess.lease.IP] = sess
	// A reconnect for the same user/IP replaces the old live session so packets
	// do not split across two TCP connections.
	if s.presence != nil {
		s.presence.SetOnline(context.Background(), sess.roomID, sess.userID, sess.lease.IP)
	}
	if s.rooms != nil {
		s.rooms.NotifyPresenceChanged(context.Background(), sess.roomID, sess.userID)
	}
	return old
}

// refreshOnlineLoop keeps Redis presence records alive. The data is advisory:
// routing uses the in-memory session maps, while Redis is for admin/visibility.
func (s *Server) refreshOnlineLoop(ctx context.Context, sess *session) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sess.done:
			return
		case <-ticker.C:
			if s.accounts != nil {
				if err := s.accounts.EnsureActive(ctx, sess.userID); err != nil {
					log.Printf("overlay closing inactive account room=%d user=%s ip=%s: %v", sess.roomID, sess.username, sess.lease.IP, err)
					sess.close()
					return
				}
				if err := s.accounts.EnsureSession(ctx, sess.userID, sess.sessionID); err != nil {
					log.Printf("overlay closing stale login session room=%d user=%s ip=%s: %v", sess.roomID, sess.username, sess.lease.IP, err)
					sess.close()
					return
				}
			}
			if s.rooms != nil {
				if _, err := s.rooms.EnsureMember(ctx, sess.roomID, sess.userID); err != nil {
					log.Printf("overlay closing non-member room=%d user=%s ip=%s: %v", sess.roomID, sess.username, sess.lease.IP, err)
					sess.close()
					return
				}
			}
			if s.presence != nil {
				s.presence.SetOnline(ctx, sess.roomID, sess.userID, sess.lease.IP)
			}
		}
	}
}

func (s *Server) unregister(ctx context.Context, sess *session) {
	sess.close()
	s.mu.Lock()
	removedConn := false
	removedRouting := false
	if current := s.byConn[sess.conn]; current == sess {
		delete(s.byConn, sess.conn)
		removedConn = true
	}
	roomMap := s.byRoomIP[sess.roomID]
	if roomMap != nil && roomMap[sess.lease.IP] == sess {
		delete(roomMap, sess.lease.IP)
		removedRouting = true
	}
	s.mu.Unlock()
	if removedRouting && s.presence != nil {
		s.presence.SetOffline(ctx, sess.roomID, sess.userID)
	}
	if removedRouting && s.rooms != nil {
		s.rooms.NotifyPresenceChanged(ctx, sess.roomID, sess.userID)
	}
	if removedConn || removedRouting {
		log.Printf("overlay left room=%d user=%s ip=%s", sess.roomID, sess.username, sess.lease.IP)
	}
}

func (s *Server) bySecureConn(conn *tcpx.SecureConn) *session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byConn[conn]
}

func (s *Server) findByIP(roomID int64, ip string) *session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	roomMap := s.byRoomIP[roomID]
	if roomMap == nil {
		return nil
	}
	return roomMap[ip]
}

func (s *Server) roomPeers(from *session) []*session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	roomMap := s.byRoomIP[from.roomID]
	if roomMap == nil {
		return nil
	}
	peers := make([]*session, 0, len(roomMap))
	for _, peer := range roomMap {
		if peer != nil && peer != from {
			peers = append(peers, peer)
		}
	}
	return peers
}

func (s *Server) watchRoomEvents(ctx context.Context) {
	if s.rooms == nil {
		return
	}
	for ctx.Err() == nil {
		events, cleanup, err := s.rooms.SubscribeAllEvents(ctx)
		if err != nil {
			log.Printf("overlay room event subscribe failed: %v", err)
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}
		for event := range events {
			s.applyRoomEvent(ctx, event)
		}
		if cleanup != nil {
			cleanup()
		}
	}
}

func (s *Server) applyRoomEvent(ctx context.Context, event room.RoomEvent) {
	if event.RoomID <= 0 {
		return
	}
	switch event.Type {
	case room.RoomEventChatMessage:
		return
	case room.RoomEventDeleted, room.RoomEventClosed:
		s.closeRoomSessions(ctx, event.RoomID, 0, event.Type)
	case room.RoomEventMemberKicked, room.RoomEventMemberLeft:
		if event.TargetUserID > 0 {
			s.closeRoomSessions(ctx, event.RoomID, event.TargetUserID, event.Type)
			return
		}
		s.revalidateRoomSessions(ctx, event.RoomID, event.Type)
	default:
		s.revalidateRoomSessions(ctx, event.RoomID, event.Type)
	}
}

func (s *Server) revalidateRoomSessions(ctx context.Context, roomID int64, reason string) {
	for _, sess := range s.sessionsForRoom(roomID) {
		if _, err := s.rooms.EnsureMember(ctx, sess.roomID, sess.userID); err != nil {
			log.Printf("overlay closing revalidated session room=%d user=%s ip=%s reason=%s err=%v", sess.roomID, sess.username, sess.lease.IP, reason, err)
			s.unregister(ctx, sess)
		}
	}
}

func (s *Server) closeRoomSessions(ctx context.Context, roomID, userID int64, reason string) {
	for _, sess := range s.sessionsForRoom(roomID) {
		if userID > 0 && sess.userID != userID {
			continue
		}
		log.Printf("overlay closing session room=%d user=%s ip=%s reason=%s", sess.roomID, sess.username, sess.lease.IP, reason)
		s.unregister(ctx, sess)
	}
}

func (s *Server) sessionsForRoom(roomID int64) []*session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	roomMap := s.byRoomIP[roomID]
	if len(roomMap) == 0 {
		return nil
	}
	out := make([]*session, 0, len(roomMap))
	for _, sess := range roomMap {
		if sess != nil {
			out = append(out, sess)
		}
	}
	return out
}

func (s *Server) writeJSON(conn *tcpx.SecureConn, typ FrameType, v any) error {
	raw, err := PackJSON(typ, v)
	if err != nil {
		return err
	}
	return conn.Write(raw)
}
