package room

import (
	"strings"
	"testing"
)

func TestNormalizeChatMessage(t *testing.T) {
	got, err := NormalizeChatMessage("  hello  ")
	if err != nil {
		t.Fatalf("NormalizeChatMessage returned error: %v", err)
	}
	if got != "hello" {
		t.Fatalf("message=%q, want hello", got)
	}
	if _, err := NormalizeChatMessage("   "); err == nil {
		t.Fatalf("expected blank chat message to fail")
	}
	if _, err := NormalizeChatMessage(strings.Repeat("你", MaxChatMessageRunes+1)); err == nil {
		t.Fatalf("expected oversized chat message to fail")
	}
}

func TestChatRelayTrafficBytes(t *testing.T) {
	if got, want := ChatRelayTrafficBytes("hello", 3), int64(15); got != want {
		t.Fatalf("ChatRelayTrafficBytes ascii=%d, want %d", got, want)
	}
	if got, want := ChatRelayTrafficBytes("你", 2), int64(6); got != want {
		t.Fatalf("ChatRelayTrafficBytes utf8=%d, want %d", got, want)
	}
	if got, want := ChatRelayTrafficBytes("hi", 0), int64(2); got != want {
		t.Fatalf("ChatRelayTrafficBytes min recipients=%d, want %d", got, want)
	}
}

func TestWebRTCSignalTypesAllowChatDataChannel(t *testing.T) {
	for _, signalType := range []string{"offer", "answer", "ice", "hangup", "chat-offer", "chat-answer", "chat-ice", "chat-hangup"} {
		if !validVoiceSignalType(signalType) {
			t.Fatalf("signal type %q should be valid", signalType)
		}
	}
	if validVoiceSignalType("chat-bad") {
		t.Fatalf("invalid chat signal type was accepted")
	}
}
