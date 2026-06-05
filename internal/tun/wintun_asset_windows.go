//go:build windows

package tun

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed assets/wintun-amd64.dll
var embeddedWintunDLL []byte

var embeddedWintunHash = sha256.Sum256(embeddedWintunDLL)

func EnsureWintunDLL() error {
	if runtime.GOARCH != "amd64" {
		return fmt.Errorf("embedded wintun.dll is only available for windows/amd64")
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	dllPath := filepath.Join(filepath.Dir(exePath), "wintun.dll")
	if existing, err := os.ReadFile(dllPath); err == nil {
		existingHash := sha256.Sum256(existing)
		if bytes.Equal(existingHash[:], embeddedWintunHash[:]) {
			return nil
		}
	}
	if err := os.WriteFile(dllPath, embeddedWintunDLL, 0644); err != nil {
		return fmt.Errorf("release wintun.dll: %w", err)
	}
	return nil
}
