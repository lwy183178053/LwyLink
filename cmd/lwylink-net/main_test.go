package main

import "testing"

func TestParseConnectFlagsDefaultsToPublicAPIAndP2P(t *testing.T) {
	t.Setenv("LWYLINK_API", "")
	t.Setenv("LWYLINK_P2P", "")

	cfg, err := parseConnectFlags(nil)
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if cfg.apiBaseURL != defaultAPIBaseURL {
		t.Fatalf("apiBaseURL=%q, want %q", cfg.apiBaseURL, defaultAPIBaseURL)
	}
	if !cfg.p2p {
		t.Fatalf("p2p should default to enabled")
	}
}

func TestParseConnectFlagsCanDisableP2P(t *testing.T) {
	cfg, err := parseConnectFlags([]string{"--p2p=false"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if cfg.p2p {
		t.Fatalf("p2p flag did not disable direct transport")
	}
}
