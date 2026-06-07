package overlay

import (
	"bytes"
	"testing"
)

func TestIPPayloadWrapRoundTrip(t *testing.T) {
	pkt := []byte{0x45, 0, 0, 20}
	payload := PackIPPayload(42, pkt)

	id, got, err := UnpackIPPayload(payload)
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if id != 42 {
		t.Fatalf("packet id=%d, want 42", id)
	}
	if !bytes.Equal(got, pkt) {
		t.Fatalf("packet mismatch")
	}
}

func TestIPPayloadUnpackRejectsBarePacket(t *testing.T) {
	pkt := []byte{0x45, 0, 0, 20}
	if _, _, err := UnpackIPPayload(pkt); err == nil {
		t.Fatalf("bare packet was accepted")
	}
}
