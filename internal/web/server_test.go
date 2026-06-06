package web

import "testing"

func TestParseTrafficGrantBytes(t *testing.T) {
	got, err := parseTrafficGrantBytes("1536")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := int64(1.5 * 1024 * 1024 * 1024)
	if got != want {
		t.Fatalf("bytes=%d, want %d", got, want)
	}
}

func TestParseTrafficGrantBytesRejectsInvalidValue(t *testing.T) {
	if _, err := parseTrafficGrantBytes("0"); err == nil {
		t.Fatalf("expected zero traffic grant to fail")
	}
	if _, err := parseTrafficGrantBytes("1048577"); err == nil {
		t.Fatalf("expected oversized traffic grant to fail")
	}
}
