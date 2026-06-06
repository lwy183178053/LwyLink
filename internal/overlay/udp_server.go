package overlay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"slices"
	"time"
)

const (
	udpReadBufferSize       = 64 * 1024
	udpPeerAnnounceInterval = 10 * time.Second
)

func (s *Server) prepareUDP(sess *session) error {
	token, err := randomHex(18)
	if err != nil {
		return err
	}
	key, err := randomBytes(32)
	if err != nil {
		return err
	}
	sess.udpToken = token
	sess.udpKey = key
	return nil
}

func (s *Server) listenAndServeUDP(ctx context.Context) {
	if s.addr == "" {
		return
	}
	addr, err := net.ResolveUDPAddr("udp", s.addr)
	if err != nil {
		log.Printf("udp overlay resolve failed: %v", err)
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("udp overlay disabled: %v", err)
		return
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	log.Printf("udp overlay rendezvous listening on %s", conn.LocalAddr())

	buf := make([]byte, udpReadBufferSize)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("udp overlay read failed: %v", err)
			continue
		}
		raw := make([]byte, n)
		copy(raw, buf[:n])
		s.handleUDPPacket(remote, raw)
	}
}

func (s *Server) handleUDPPacket(remote *net.UDPAddr, raw []byte) {
	packetType, token, err := UDPPacketID(raw)
	if err != nil || packetType != UDPTypeRegister {
		return
	}
	sess := s.sessionByUDPToken(token)
	if sess == nil {
		return
	}
	var payload UDPRegisterPayload
	if _, err := DecodeUDPJSON(raw, sess.udpKey, &payload); err != nil {
		return
	}
	if payload.RoomID != sess.roomID || payload.UserID != sess.userID || payload.VirtualIP != sess.lease.IP {
		return
	}
	lanEndpoints := NormalizeUDPLANEndpoints(payload.LANEndpoints)

	changed, shouldAnnounce := s.setSessionUDPAddr(sess, remote, lanEndpoints, time.Now())
	if changed {
		log.Printf("udp overlay registered room=%d user=%s ip=%s endpoint=%s", sess.roomID, sess.username, sess.lease.IP, remote)
	}
	if shouldAnnounce {
		s.announceUDPPeers(sess)
	}
}

func (s *Server) sessionByUDPToken(token string) *session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byUDP[token]
}

func (s *Server) setSessionUDPAddr(sess *session, remote *net.UDPAddr, lanEndpoints []string, now time.Time) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.byUDP[sess.udpToken]; current != sess {
		return false, false
	}
	changed := sess.udpAddr == nil || sess.udpAddr.String() != remote.String() || !slices.Equal(sess.udpLAN, lanEndpoints)
	if changed {
		cp := *remote
		sess.udpAddr = &cp
		sess.udpLAN = append([]string(nil), lanEndpoints...)
	}
	shouldAnnounce := changed || sess.udpAnnouncedAt.IsZero() || now.Sub(sess.udpAnnouncedAt) >= udpPeerAnnounceInterval
	if shouldAnnounce {
		sess.udpAnnouncedAt = now
	}
	return changed, shouldAnnounce
}

func (s *Server) announceUDPPeers(sess *session) {
	announcements := s.buildUDPAnnouncements(sess)
	for _, item := range announcements {
		_ = s.writeJSON(item.to.conn, FrameUDPControl, UDPControl{
			Type: UDPControlPeer,
			Peer: &UDPPeerCandidate{
				VirtualIP:    item.peer.lease.IP,
				Endpoint:     item.peer.udpAddr.String(),
				LANEndpoints: append([]string(nil), item.peer.udpLAN...),
				SessionID:    item.pair.SessionID,
				Key:          EncodeUDPKey(item.pair.Key),
			},
		})
	}
}

type udpAnnouncement struct {
	to   *session
	peer *session
	pair udpPair
}

func (s *Server) buildUDPAnnouncements(sess *session) []udpAnnouncement {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess.udpAddr == nil || s.byUDP[sess.udpToken] != sess {
		return nil
	}
	roomMap := s.byRoomIP[sess.roomID]
	if roomMap == nil {
		return nil
	}
	out := make([]udpAnnouncement, 0, len(roomMap)*2)
	for _, peer := range roomMap {
		if peer == nil || peer == sess || peer.udpAddr == nil {
			continue
		}
		pair := s.udpPairLocked(sess, peer)
		out = append(out,
			udpAnnouncement{to: sess, peer: peer, pair: pair},
			udpAnnouncement{to: peer, peer: sess, pair: pair},
		)
	}
	return out
}

func (s *Server) udpPairLocked(a, b *session) udpPair {
	key := newUDPPairKey(a.roomID, a.lease.IP, b.lease.IP)
	if existing, ok := s.udpPairs[key]; ok {
		return existing
	}
	sessionID, err := randomHex(16)
	if err != nil {
		sessionID = hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	pairKey, err := randomBytes(32)
	if err != nil {
		pairKey = []byte("0123456789abcdef0123456789abcdef")
	}
	pair := udpPair{SessionID: sessionID, Key: pairKey}
	s.udpPairs[key] = pair
	return pair
}

func newUDPPairKey(roomID int64, leftIP, rightIP string) udpPairKey {
	if rightIP < leftIP {
		leftIP, rightIP = rightIP, leftIP
	}
	return udpPairKey{roomID: roomID, leftIP: leftIP, rightIP: rightIP}
}

func (s *Server) deleteUDPPairsLocked(sess *session) {
	if sess == nil || len(s.udpPairs) == 0 {
		return
	}
	for key := range s.udpPairs {
		if key.roomID == sess.roomID && (key.leftIP == sess.lease.IP || key.rightIP == sess.lease.IP) {
			delete(s.udpPairs, key)
		}
	}
}

func randomHex(size int) (string, error) {
	b, err := randomBytes(size)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomBytes(size int) ([]byte, error) {
	b := make([]byte, size)
	_, err := rand.Read(b)
	return b, err
}
