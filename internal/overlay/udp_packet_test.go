package overlay

import (
	"bytes"
	"testing"
)

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
	if bytes.Equal(packet.Nonce[:], make([]byte, len(packet.Nonce))) {
		t.Fatalf("nonce was all zero")
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

func TestUDPPacketUsesFreshNonceForSameSeq(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	firstRaw, err := EncodeUDPPacket(UDPTypeIP, "pair-1", key, 7, []byte("payload"))
	if err != nil {
		t.Fatalf("encode first: %v", err)
	}
	secondRaw, err := EncodeUDPPacket(UDPTypeIP, "pair-1", key, 7, []byte("payload"))
	if err != nil {
		t.Fatalf("encode second: %v", err)
	}
	first, err := DecodeUDPPacket(firstRaw, key)
	if err != nil {
		t.Fatalf("decode first: %v", err)
	}
	second, err := DecodeUDPPacket(secondRaw, key)
	if err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if bytes.Equal(first.Nonce[:], second.Nonce[:]) {
		t.Fatalf("same key/id/seq reused nonce %x", first.Nonce)
	}
}

func TestUDPReplayCacheRejectsDuplicateNonce(t *testing.T) {
	cache := NewUDPReplayCache(2)
	var first [12]byte
	first[11] = 1
	var second [12]byte
	second[11] = 2
	var third [12]byte
	third[11] = 3

	if !cache.Accept(first) {
		t.Fatalf("first nonce rejected")
	}
	if cache.Accept(first) {
		t.Fatalf("duplicate nonce accepted")
	}
	if !cache.Accept(second) || !cache.Accept(third) {
		t.Fatalf("new nonce rejected")
	}
	if !cache.Accept(first) {
		t.Fatalf("oldest nonce should be evicted after cache limit")
	}
}
