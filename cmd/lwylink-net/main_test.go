package main

import "testing"

func TestParseConnectFlagsDefaultsToPublicAPI(t *testing.T) {
	t.Setenv("LWYLINK_API", "")

	cfg, err := parseConnectFlags(nil)
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if cfg.apiBaseURL != defaultAPIBaseURL {
		t.Fatalf("apiBaseURL=%q, want %q", cfg.apiBaseURL, defaultAPIBaseURL)
	}
}
