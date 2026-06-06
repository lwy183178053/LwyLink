package overlay

import "testing"

func TestUDPPacketRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	raw, err := EncodeUDPPacket(UDPTypeIP, "pair-1", key, 7, []byte("payload"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	packetType, id, err := UDPPacketID(raw)
	if err != nil {
		t.Fatalf("packet id: %v", err)
	}
	if packetType != UDPTypeIP || id != "pair-1" {
		t.Fatalf("unexpected header type=%d id=%q", packetType, id)
	}
	packet, err := DecodeUDPPacket(raw, key)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if packet.Type != UDPTypeIP || packet.ID != "pair-1" || packet.Seq != 7 || string(packet.Payload) != "payload" {
		t.Fatalf("unexpected packet: %+v", packet)
	}
}

func TestUDPPacketRejectsWrongKey(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	wrong := []byte("abcdef0123456789abcdef0123456789")
	raw, err := EncodeUDPPacket(UDPTypeIP, "pair-1", key, 7, []byte("payload"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := DecodeUDPPacket(raw, wrong); err == nil {
		t.Fatalf("expected decode with wrong key to fail")
	}
}
