package overlay

import (
	"net"
	"sync"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/tcpx"
	"github.com/LwyV-Labs/LwyLink/internal/vdhcp"
)

type session struct {
	conn *tcpx.SecureConn

	userID    int64
	username  string
	sessionID string
	roomID    int64
	lease     vdhcp.Lease

	udpToken       string
	udpKey         []byte
	udpAddr        *net.UDPAddr
	udpLAN         []string
	udpReflexive   []string
	udpAnnouncedAt time.Time
	udpReplay      *UDPReplayCache

	relayMu             sync.Mutex
	relayPendingBytes   int64
	relayRemainingBytes int64

	send     chan outboundFrame
	done     chan struct{}
	closeOne sync.Once
}

type outboundFrame struct {
	typ     FrameType
	payload []byte
}

func newSession(conn *tcpx.SecureConn, userID int64, username string, sessionID string, roomID int64, lease vdhcp.Lease, relayRemainingBytes int64) *session {
	return &session{
		conn:                conn,
		userID:              userID,
		username:            username,
		sessionID:           sessionID,
		roomID:              roomID,
		lease:               lease,
		relayRemainingBytes: relayRemainingBytes,
		send:                make(chan outboundFrame, 2048),
		done:                make(chan struct{}),
	}
}

func (s *session) close() {
	s.closeOne.Do(func() {
		close(s.done)
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
}

func (s *session) enqueue(pkt []byte) bool {
	return s.enqueueFrame(FrameIP, pkt)
}

func (s *session) enqueueFrame(typ FrameType, payload []byte) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	frame := outboundFrame{typ: typ, payload: cp}
	select {
	case <-s.done:
		return false
	case s.send <- frame:
		return true
	default:
		// Games prefer fresh packets over unbounded latency. If a player cannot
		// drain the queue, drop instead of blocking the whole room relay.
		return false
	}
}

func (s *session) reserveRelayTraffic(bytes int64) (int64, bool) {
	if s == nil {
		return 0, false
	}
	if bytes <= 0 {
		return 0, true
	}
	s.relayMu.Lock()
	defer s.relayMu.Unlock()
	if s.relayRemainingBytes < bytes {
		return 0, false
	}
	s.relayRemainingBytes -= bytes
	s.relayPendingBytes += bytes
	if s.relayPendingBytes < relayTrafficFlushBytes {
		return 0, true
	}
	pending := s.relayPendingBytes
	s.relayPendingBytes = 0
	return pending, true
}

func (s *session) restoreRelayTraffic(bytes int64) {
	if s == nil || bytes <= 0 {
		return
	}
	s.relayMu.Lock()
	defer s.relayMu.Unlock()
	s.relayPendingBytes += bytes
	s.relayRemainingBytes += bytes
}

func (s *session) takeRelayPending() int64 {
	if s == nil {
		return 0
	}
	s.relayMu.Lock()
	defer s.relayMu.Unlock()
	pending := s.relayPendingBytes
	s.relayPendingBytes = 0
	return pending
}
