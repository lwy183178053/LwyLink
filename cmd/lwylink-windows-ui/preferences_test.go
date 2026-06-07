package main

import (
	"encoding/json"
	"testing"
)

func TestDefaultClientPreferencesEnableP2P(t *testing.T) {
	if !defaultClientPreferences().P2PEnabled {
		t.Fatalf("P2P should be enabled by default")
	}
}

func TestClientPreferencesFileCanStoreExplicitFalse(t *testing.T) {
	raw := []byte(`{"p2pEnabled":false}`)
	var file clientPreferencesFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if file.P2PEnabled == nil {
		t.Fatalf("p2pEnabled was not present")
	}
	if *file.P2PEnabled {
		t.Fatalf("p2pEnabled=true, want false")
	}
}
