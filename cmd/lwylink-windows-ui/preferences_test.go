package main

import (
	"reflect"
	"testing"
)

func TestVoicePreferencesHideManualSilenceThreshold(t *testing.T) {
	if _, ok := reflect.TypeOf(VoicePreferences{}).FieldByName("SilenceThreshold"); ok {
		t.Fatalf("manual silence threshold should not be exposed in voice preferences")
	}
	if got := defaultVoicePreferences().MicVolume; got != 1.15 {
		t.Fatalf("default mic volume=%v, want 1.15", got)
	}
}
