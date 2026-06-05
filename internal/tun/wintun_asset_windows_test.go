//go:build windows

package tun

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureWintunDLL(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("embedded wintun.dll is only available for windows/amd64")
	}
	exePath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dllPath := filepath.Join(filepath.Dir(exePath), "wintun.dll")
	_ = os.Remove(dllPath)
	if err := EnsureWintunDLL(); err != nil {
		t.Fatal(err)
	}
	released, err := os.ReadFile(dllPath)
	if err != nil {
		t.Fatal(err)
	}
	releasedHash := sha256.Sum256(released)
	if !bytes.Equal(releasedHash[:], embeddedWintunHash[:]) {
		t.Fatal("released wintun.dll hash mismatch")
	}
}
