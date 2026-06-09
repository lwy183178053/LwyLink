package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type clientPreferences struct {
	VoiceSettings VoicePreferences `json:"voiceSettings"`
}

type clientPreferencesFile struct {
	VoiceSettings *VoicePreferences `json:"voiceSettings,omitempty"`
}

type VoicePreferences struct {
	InputDeviceID    string  `json:"inputDeviceID"`
	OutputDeviceID   string  `json:"outputDeviceID"`
	MicVolume        float64 `json:"micVolume"`
	OutputVolume     float64 `json:"outputVolume"`
	AutoGainControl  bool    `json:"autoGainControl"`
	NoiseSuppression bool    `json:"noiseSuppression"`
	EchoCancellation bool    `json:"echoCancellation"`
}

func defaultClientPreferences() clientPreferences {
	return clientPreferences{
		VoiceSettings: defaultVoicePreferences(),
	}
}

func defaultVoicePreferences() VoicePreferences {
	return VoicePreferences{
		MicVolume:        1.15,
		OutputVolume:     1,
		AutoGainControl:  true,
		NoiseSuppression: true,
		EchoCancellation: true,
	}
}

func loadClientPreferences() clientPreferences {
	prefs := defaultClientPreferences()
	path, err := preferencesPath()
	if err != nil {
		return prefs
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return prefs
	}
	if err != nil {
		return prefs
	}
	var file clientPreferencesFile
	if err := json.Unmarshal(data, &file); err != nil {
		return prefs
	}
	if file.VoiceSettings != nil {
		prefs.VoiceSettings = normalizeVoicePreferences(*file.VoiceSettings)
	}
	return prefs
}

func saveClientPreferences(prefs clientPreferences) error {
	path, err := preferencesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(clientPreferencesFile{
		VoiceSettings: &prefs.VoiceSettings,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func normalizeVoicePreferences(input VoicePreferences) VoicePreferences {
	defaults := defaultVoicePreferences()
	if input.MicVolume <= 0 || input.MicVolume > 2 {
		input.MicVolume = defaults.MicVolume
	}
	if input.OutputVolume <= 0 || input.OutputVolume > 2 {
		input.OutputVolume = defaults.OutputVolume
	}
	input.AutoGainControl = true
	input.NoiseSuppression = true
	input.EchoCancellation = true
	return input
}

func preferencesPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "LwyLink", "preferences.json"), nil
}
