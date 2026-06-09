package main

import (
	"encoding/json"
	"reflect"
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

func TestVoicePreferencesHideManualSilenceThreshold(t *testing.T) {
	if _, ok := reflect.TypeOf(VoicePreferences{}).FieldByName("SilenceThreshold"); ok {
		t.Fatalf("manual silence threshold should not be exposed in voice preferences")
	}
	if got := defaultVoicePreferences().MicVolume; got != 1.15 {
		t.Fatalf("default mic volume=%v, want 1.15", got)
	}
}
