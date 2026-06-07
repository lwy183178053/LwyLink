package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type clientPreferences struct {
	P2PEnabled bool `json:"p2pEnabled"`
}

type clientPreferencesFile struct {
	P2PEnabled *bool `json:"p2pEnabled,omitempty"`
}

func defaultClientPreferences() clientPreferences {
	return clientPreferences{P2PEnabled: true}
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
	if err := json.Unmarshal(data, &file); err == nil && file.P2PEnabled != nil {
		prefs.P2PEnabled = *file.P2PEnabled
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
		P2PEnabled: &prefs.P2PEnabled,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func preferencesPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "LwyLink", "preferences.json"), nil
}
