package overlay

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/vdhcp"
)

func TestForwardIPUnicastRoutesOnlyToTarget(t *testing.T) {
	srv, from, peer, other := newForwardTestServer()
	pkt := testIPv4Packet("172.16.1.10", "172.16.1.11")

	srv.forwardIP(from, PackIPPayload(1, pkt))

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
		{name: "room subnet broadcast", dst: "172.16.1.255"},
		{name: "multicast", dst: "239.255.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, from, peer, other := newForwardTestServer()
			pkt := testIPv4Packet("172.16.1.10", tt.dst)

			srv.forwardIP(from, PackIPPayload(1, pkt))

			assertPacket(t, peer, "room peer")
			assertNoPacket(t, from, "sender")
			assertNoPacket(t, other, "other room")
		})
	}
}

func TestForwardIPDropsForgedSource(t *testing.T) {
	srv, from, peer, _ := newForwardTestServer()
	pkt := testIPv4Packet("172.16.1.99", "255.255.255.255")

	srv.forwardIP(from, PackIPPayload(1, pkt))

	assertNoPacket(t, peer, "room peer")
	assertNoPacket(t, from, "sender")
}

func TestForwardRelayProbeRoutesRequestAndReply(t *testing.T) {
	srv, from, peer, _ := newForwardTestServer()
	request, err := json.Marshal(RelayProbePayload{
		Seq:    7,
		FromIP: from.lease.IP,
		ToIP:   peer.lease.IP,
	})
	if err != nil {
		t.Fatal(err)
	}

	srv.forwardRelayProbe(from, request)

	reqFrame := assertFrame(t, peer, "request target")
	assertRelayProbeControl(t, reqFrame, "request")
	assertNoPacket(t, from, "request sender")

	reply, err := json.Marshal(RelayProbePayload{
		Seq:    7,
		FromIP: peer.lease.IP,
		ToIP:   from.lease.IP,
		Reply:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.forwardRelayProbe(peer, reply)

	replyFrame := assertFrame(t, from, "reply target")
	assertRelayProbeControl(t, replyFrame, "reply")
}

func TestForwardRelayProbeRejectsForgedSource(t *testing.T) {
	srv, from, peer, _ := newForwardTestServer()
	request, err := json.Marshal(RelayProbePayload{
		Seq:    7,
		FromIP: "172.16.1.99",
		ToIP:   peer.lease.IP,
	})
	if err != nil {
		t.Fatal(err)
	}

	srv.forwardRelayProbe(from, request)

	assertNoPacket(t, peer, "target peer")
}

func TestDeleteUDPPairsLockedKeepsUnrelatedPairs(t *testing.T) {
	srv := NewServer("", 1, "", nil, nil, nil, nil, nil, 1300)
	sess := testSession(1, "172.16.1.10")
	deleteKey := newUDPPairKey(1, "172.16.1.10", "172.16.1.11")
	keepSameRoomKey := newUDPPairKey(1, "172.16.1.12", "172.16.1.13")
	keepOtherRoomKey := newUDPPairKey(2, "172.16.1.10", "172.16.1.11")
	srv.udpPairs[deleteKey] = udpPair{SessionID: "delete"}
	srv.udpPairs[keepSameRoomKey] = udpPair{SessionID: "same-room"}
	srv.udpPairs[keepOtherRoomKey] = udpPair{SessionID: "other-room"}

	srv.deleteUDPPairsLocked(sess)

	if _, ok := srv.udpPairs[deleteKey]; ok {
		t.Fatalf("deleted session pair remained")
	}
	if _, ok := srv.udpPairs[keepSameRoomKey]; !ok {
		t.Fatalf("unrelated same-room pair was deleted")
	}
	if _, ok := srv.udpPairs[keepOtherRoomKey]; !ok {
		t.Fatalf("other-room pair was deleted")
	}
}

func TestNormalizeUDPLANEndpointsFiltersUnsafeValues(t *testing.T) {
	got := NormalizeUDPLANEndpoints([]string{
		"192.168.1.20:40000",
		"127.0.0.1:40000",
		"203.0.113.9:40000",
		"10.0.0.5:40000",
		"192.168.1.20:40000",
		"bad",
	})
	want := []string{"10.0.0.5:40000", "192.168.1.20:40000"}
	if !slices.Equal(got, want) {
		t.Fatalf("lan endpoints=%v, want %v", got, want)
	}
}

func TestNormalizeUDPReflexiveEndpointsFiltersPrivateValues(t *testing.T) {
	got := NormalizeUDPReflexiveEndpoints([]string{
		"198.51.100.9:40000",
		"192.168.1.20:40000",
		"127.0.0.1:40000",
		"203.0.113.9:40000",
		"198.51.100.9:40000",
		"bad",
	})
	want := []string{"198.51.100.9:40000", "203.0.113.9:40000"}
	if !slices.Equal(got, want) {
		t.Fatalf("reflexive endpoints=%v, want %v", got, want)
	}
}

func TestBuildSTUNBindingResponse(t *testing.T) {
	request := make([]byte, 20)
	binary.BigEndian.PutUint16(request[0:2], stunBindingRequest)
	binary.BigEndian.PutUint32(request[4:8], stunMagicCookie)
	copy(request[8:20], []byte("abcdefghijkl"))
	remote := testUDPAddr(t, "198.51.100.9:40000")

	response, ok := buildSTUNBindingResponse(request, remote)
	if !ok {
		t.Fatalf("stun response was not built")
	}
	if binary.BigEndian.Uint16(response[0:2]) != stunBindingSuccess {
		t.Fatalf("bad stun response type")
	}
	if !slices.Equal(response[8:20], request[8:20]) {
		t.Fatalf("stun transaction id was not echoed")
	}
	port := binary.BigEndian.Uint16(response[26:28]) ^ uint16(stunMagicCookie>>16)
	if port != 40000 {
		t.Fatalf("stun port=%d, want 40000", port)
	}
}

func TestBuildUDPAnnouncementsIncludesPeerLANEndpoints(t *testing.T) {
	srv, from, peer, _ := newForwardTestServer()
	from.udpToken = "from-token"
	from.udpAddr = testUDPAddr(t, "203.0.113.10:50001")
	from.udpLAN = []string{"192.168.1.10:50001"}
	peer.udpToken = "peer-token"
	peer.udpAddr = testUDPAddr(t, "203.0.113.10:50002")
	peer.udpLAN = []string{"192.168.1.11:50002"}
	peer.udpReflexive = []string{"198.51.100.11:50002"}
	srv.byUDP[from.udpToken] = from
	srv.byUDP[peer.udpToken] = peer

	announcements := srv.buildUDPAnnouncements(from)
	if len(announcements) != 2 {
		t.Fatalf("announcements=%d, want 2", len(announcements))
	}
	for _, item := range announcements {
		if item.to == from && !slices.Equal(item.peer.udpLAN, peer.udpLAN) {
			t.Fatalf("from announcement peer lan=%v, want %v", item.peer.udpLAN, peer.udpLAN)
		}
		if item.to == peer && !slices.Equal(item.peer.udpLAN, from.udpLAN) {
			t.Fatalf("peer announcement peer lan=%v, want %v", item.peer.udpLAN, from.udpLAN)
		}
	}
}

func TestSetSessionUDPAddrPeriodicallyReannouncesUnchangedEndpoint(t *testing.T) {
	srv, sess, _, _ := newForwardTestServer()
	sess.udpToken = "token"
	srv.byUDP[sess.udpToken] = sess
	endpoint := testUDPAddr(t, "203.0.113.10:50001")
	now := time.Unix(100, 0)

	changed, announce := srv.setSessionUDPAddr(sess, endpoint, []string{"192.168.1.10:50001"}, []string{"198.51.100.10:50001"}, now)
	if !changed || !announce {
		t.Fatalf("first registration changed=%v announce=%v, want true true", changed, announce)
	}

	changed, announce = srv.setSessionUDPAddr(sess, endpoint, []string{"192.168.1.10:50001"}, []string{"198.51.100.10:50001"}, now.Add(time.Second))
	if changed || announce {
		t.Fatalf("early repeat changed=%v announce=%v, want false false", changed, announce)
	}

	changed, announce = srv.setSessionUDPAddr(sess, endpoint, []string{"192.168.1.10:50001"}, []string{"198.51.100.10:50001"}, now.Add(udpPeerAnnounceInterval+time.Second))
	if changed || !announce {
		t.Fatalf("interval repeat changed=%v announce=%v, want false true", changed, announce)
	}
}

func TestHandleUDPPacketRejectsReplay(t *testing.T) {
	srv, sess, _, _ := newForwardTestServer()
	sess.userID = 10
	sess.udpToken = "token"
	sess.udpKey = []byte("0123456789abcdef0123456789abcdef")
	srv.byUDP[sess.udpToken] = sess
	payload := UDPRegisterPayload{
		RoomID:    sess.roomID,
		UserID:    sess.userID,
		VirtualIP: sess.lease.IP,
	}
	raw, err := EncodeUDPJSON(UDPTypeRegister, sess.udpToken, sess.udpKey, 1, payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	firstRemote := testUDPAddr(t, "203.0.113.10:50001")
	replayRemote := testUDPAddr(t, "203.0.113.10:50002")

	srv.handleUDPPacket(firstRemote, raw)
	if sess.udpAddr == nil || sess.udpAddr.String() != firstRemote.String() {
		t.Fatalf("first registration endpoint=%v, want %s", sess.udpAddr, firstRemote)
	}
	srv.handleUDPPacket(replayRemote, raw)
	if sess.udpAddr == nil || sess.udpAddr.String() != firstRemote.String() {
		t.Fatalf("replay changed endpoint to %v", sess.udpAddr)
	}
}

func TestSessionRelayTrafficReserveUsesLocalRemaining(t *testing.T) {
	sess := testSession(1, "172.16.1.10")
	sess.relayRemainingBytes = relayTrafficFlushBytes + 10

	pending, ok := sess.reserveRelayTraffic(10)
	if !ok || pending != 0 {
		t.Fatalf("first reserve pending=%d ok=%v, want 0 true", pending, ok)
	}
	if sess.relayRemainingBytes != relayTrafficFlushBytes {
		t.Fatalf("remaining=%d, want %d", sess.relayRemainingBytes, relayTrafficFlushBytes)
	}

	pending, ok = sess.reserveRelayTraffic(relayTrafficFlushBytes)
	if !ok || pending != relayTrafficFlushBytes+10 {
		t.Fatalf("threshold reserve pending=%d ok=%v, want %d true", pending, ok, relayTrafficFlushBytes+10)
	}
	if _, ok := sess.reserveRelayTraffic(1); ok {
		t.Fatalf("reserve succeeded after local quota was exhausted")
	}
}

func TestSessionCloseAllowsNilConn(t *testing.T) {
	sess := testSession(1, "172.16.1.10")
	sess.close()
	select {
	case <-sess.done:
	default:
		t.Fatalf("session was not closed")
	}
}

func TestSessionEnqueueDropsWhenQueueIsFull(t *testing.T) {
	sess := testSession(1, "172.16.1.10")
	pkt := PackIPPayload(1, testIPv4Packet("172.16.1.10", "172.16.1.11"))
	for i := 0; i < cap(sess.send); i++ {
		if !sess.enqueue(pkt) {
			t.Fatalf("enqueue %d failed before queue was full", i)
		}
	}
	if sess.enqueue(pkt) {
		t.Fatalf("enqueue succeeded after queue was full")
	}
	if got := len(sess.send); got != cap(sess.send) {
		t.Fatalf("queue length=%d, want full capacity %d", got, cap(sess.send))
	}
}

func TestSessionEnqueueDropsAfterClose(t *testing.T) {
	sess := testSession(1, "172.16.1.10")
	sess.close()
	pkt := PackIPPayload(1, testIPv4Packet("172.16.1.10", "172.16.1.11"))
	if sess.enqueue(pkt) {
		t.Fatalf("enqueue succeeded after close")
	}
}

func newForwardTestServer() (*Server, *session, *session, *session) {
	srv := NewServer("", 1, "", nil, nil, nil, nil, nil, 1300)
	from := testSession(1, "172.16.1.10")
	peer := testSession(1, "172.16.1.11")
	other := testSession(2, "172.16.2.10")
	srv.byRoomIP[1] = map[string]*session{
		from.lease.IP: from,
		peer.lease.IP: peer,
	}
	srv.byRoomIP[2] = map[string]*session{
		other.lease.IP: other,
	}
	return srv, from, peer, other
}

func testUDPAddr(t *testing.T, value string) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", value)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func testSession(roomID int64, ip string) *session {
	return &session{
		roomID: roomID,
		lease: vdhcp.Lease{
			IP:         ip,
			SubnetMask: "255.255.255.0",
		},
		send: make(chan outboundFrame, 4),
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
	frame := assertFrame(t, sess, name)
	if frame.typ != FrameIP {
		t.Fatalf("%s received frame type=%d, want FrameIP", name, frame.typ)
	}
	if len(frame.payload) == 0 {
		t.Fatalf("%s received empty payload", name)
	}
	if _, pkt, err := UnpackIPPayload(frame.payload); err != nil || len(pkt) == 0 {
		t.Fatalf("%s received bad payload: len=%d err=%v", name, len(frame.payload), err)
	}
}

func assertFrame(t *testing.T, sess *session, name string) outboundFrame {
	t.Helper()
	select {
	case frame := <-sess.send:
		return frame
	default:
		t.Fatalf("%s did not receive frame", name)
		return outboundFrame{}
	}
}

func assertRelayProbeControl(t *testing.T, frame outboundFrame, name string) {
	t.Helper()
	if frame.typ != FrameUDPControl {
		t.Fatalf("%s frame type=%d, want FrameUDPControl", name, frame.typ)
	}
	var control UDPControl
	if err := json.Unmarshal(frame.payload, &control); err != nil {
		t.Fatalf("%s bad control payload: %v", name, err)
	}
	if control.Type != UDPControlRelayProbe || control.RelayProbe == nil {
		t.Fatalf("%s control=%+v, want relay probe", name, control)
	}
}

func assertNoPacket(t *testing.T, sess *session, name string) {
	t.Helper()
	select {
	case frame := <-sess.send:
		t.Fatalf("%s unexpectedly received frame type=%d length=%d", name, frame.typ, len(frame.payload))
	default:
	}
}
