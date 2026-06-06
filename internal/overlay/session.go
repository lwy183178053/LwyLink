package overlay

import (
	"net"
	"sync"

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

	udpToken string
	udpKey   []byte
	udpAddr  *net.UDPAddr

	send     chan []byte
	done     chan struct{}
	closeOne sync.Once
}

func newSession(conn *tcpx.SecureConn, userID int64, username string, sessionID string, roomID int64, lease vdhcp.Lease) *session {
	return &session{
		conn:      conn,
		userID:    userID,
		username:  username,
		sessionID: sessionID,
		roomID:    roomID,
		lease:     lease,
		send:      make(chan []byte, 2048),
		done:      make(chan struct{}),
	}
}

func (s *session) close() {
	s.closeOne.Do(func() {
		close(s.done)
		_ = s.conn.Close()
	})
}

func (s *session) enqueue(pkt []byte) {
	cp := make([]byte, len(pkt))
	copy(cp, pkt)
	select {
	case <-s.done:
	case s.send <- cp:
	default:
		// Games prefer fresh packets over unbounded latency. If a player cannot
		// drain the queue, drop instead of blocking the whole room relay.
	}
}
