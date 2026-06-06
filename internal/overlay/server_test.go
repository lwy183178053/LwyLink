package overlay

import (
	"net"
	"testing"

	"github.com/LwyV-Labs/LwyLink/internal/vdhcp"
)

func TestForwardIPUnicastRoutesOnlyToTarget(t *testing.T) {
	srv, from, peer, other := newForwardTestServer()
	pkt := testIPv4Packet("10.66.1.10", "10.66.1.11")

	srv.forwardIP(from, pkt)

	assertPacket(t, peer, "target peer")
	assertNoPacket(t, from, "sender")
	assertNoPacket(t, other, "other room")
}

func TestForwardIPFansOutDiscoveryPacketsWithinRoom(t *testing.T) {
	tests := []struct {
		name string
		dst  string
	}{
		{name: "limited broadcast", dst: "255.255.255.255"},
		{name: "room subnet broadcast", dst: "10.66.1.255"},
		{name: "multicast", dst: "239.255.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, from, peer, other := newForwardTestServer()
			pkt := testIPv4Packet("10.66.1.10", tt.dst)

			srv.forwardIP(from, pkt)

			assertPacket(t, peer, "room peer")
			assertNoPacket(t, from, "sender")
			assertNoPacket(t, other, "other room")
		})
	}
}

func TestForwardIPDropsForgedSource(t *testing.T) {
	srv, from, peer, _ := newForwardTestServer()
	pkt := testIPv4Packet("10.66.1.99", "255.255.255.255")

	srv.forwardIP(from, pkt)

	assertNoPacket(t, peer, "room peer")
	assertNoPacket(t, from, "sender")
}

func newForwardTestServer() (*Server, *session, *session, *session) {
	srv := NewServer("", 1, "", nil, nil, nil, nil, nil, 1300)
	from := testSession(1, "10.66.1.10")
	peer := testSession(1, "10.66.1.11")
	other := testSession(2, "10.66.2.10")
	srv.byRoomIP[1] = map[string]*session{
		from.lease.IP: from,
		peer.lease.IP: peer,
	}
	srv.byRoomIP[2] = map[string]*session{
		other.lease.IP: other,
	}
	return srv, from, peer, other
}

func testSession(roomID int64, ip string) *session {
	return &session{
		roomID: roomID,
		lease: vdhcp.Lease{
			IP:         ip,
			SubnetMask: "255.255.255.0",
		},
		send: make(chan []byte, 4),
		done: make(chan struct{}),
	}
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

func assertPacket(t *testing.T, sess *session, name string) {
	t.Helper()
	select {
	case pkt := <-sess.send:
		if len(pkt) == 0 {
			t.Fatalf("%s received empty packet", name)
		}
	default:
		t.Fatalf("%s did not receive packet", name)
	}
}

func assertNoPacket(t *testing.T, sess *session, name string) {
	t.Helper()
	select {
	case pkt := <-sess.send:
		t.Fatalf("%s unexpectedly received packet length=%d", name, len(pkt))
	default:
	}
}
