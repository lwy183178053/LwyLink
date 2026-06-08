package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const savedLoginVersion = 2

type savedLogin struct {
	APIBaseURL string
	Email      string
	Password   string
}

type savedLoginFile struct {
	Version           int    `json:"version"`
	APIBaseURL        string `json:"apiBaseURL"`
	Email             string `json:"email"`
	ProtectedPassword string `json:"protectedPassword"`
}

func normalizeSavedAPIBaseURL(value string) string {
	apiBaseURL := strings.TrimRight(strings.TrimSpace(value), "/")
	if apiBaseURL == "" {
		return defaultAPIBaseURL
	}
	return apiBaseURL
}

func loadSavedLogin() (savedLogin, bool, error) {
	path, err := savedLoginPath()
	if err != nil {
		return savedLogin{}, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return savedLogin{}, false, nil
	}
	if err != nil {
		return savedLogin{}, false, err
	}
	var file savedLoginFile
	if err := json.Unmarshal(data, &file); err != nil {
		return savedLogin{}, false, err
	}
	if file.Version != savedLoginVersion || strings.TrimSpace(file.Email) == "" || file.ProtectedPassword == "" {
		return savedLogin{}, false, fmt.Errorf("saved login file is invalid")
	}
	protected, err := base64.StdEncoding.DecodeString(file.ProtectedPassword)
	if err != nil {
		return savedLogin{}, false, err
	}
	plain, err := unprotectCredential(protected)
	if err != nil {
		return savedLogin{}, false, err
	}
	return savedLogin{
		APIBaseURL: normalizeSavedAPIBaseURL(file.APIBaseURL),
		Email:      strings.TrimSpace(file.Email),
		Password:   string(plain),
	}, true, nil
}

func saveSavedLogin(login savedLogin) error {
	login.APIBaseURL = normalizeSavedAPIBaseURL(login.APIBaseURL)
	login.Email = strings.TrimSpace(login.Email)
	if login.Email == "" || login.Password == "" {
		return clearSavedLogin()
	}
	protected, err := protectCredential([]byte(login.Password))
	if err != nil {
		return err
	}
	path, err := savedLoginPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(savedLoginFile{
		Version:           savedLoginVersion,
		APIBaseURL:        login.APIBaseURL,
		Email:             login.Email,
		ProtectedPassword: base64.StdEncoding.EncodeToString(protected),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func clearSavedLogin() error {
	path, err := savedLoginPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func savedLoginPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "LwyLink", "credentials.json"), nil
}
