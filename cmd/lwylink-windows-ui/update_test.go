package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/LwyV-Labs/LwyLink/internal/clientrelease"
)

func TestDownloadPreparedUpdateReportsProgressAndVerifiesFile(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 1024*1024+123)
	sum := sha256.Sum256(payload)
	sha := hex.EncodeToString(sum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization=%q, want bearer token", got)
		}
		w.Header().Set("Content-Length", "1048699")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	t.Setenv("TMP", tempDir)
	t.Setenv("TEMP", tempDir)

	var progress []struct {
		downloaded int64
		total      int64
	}
	path, err := downloadPreparedUpdate(context.Background(), server.URL, "test-token", clientrelease.PublicRelease{
		Version:     "0.2.1",
		Platform:    updatePlatform,
		FileName:    "LwyLink.exe",
		DownloadURL: "/client.exe",
		SizeBytes:   int64(len(payload)),
		SHA256:      sha,
	}, func(downloaded, total int64) {
		progress = append(progress, struct {
			downloaded int64
			total      int64
		}{downloaded: downloaded, total: total})
	})
	if err != nil {
		t.Fatalf("downloadPreparedUpdate returned error: %v", err)
	}
	if err := verifyFileSHA256(path, sha); err != nil {
		t.Fatalf("downloaded file hash mismatch: %v", err)
	}
	if got := filepath.Base(path); got != updatePlatform+"-0.2.1-"+sha[:12]+"-LwyLink.exe" {
		t.Fatalf("downloaded file name=%q", got)
	}
	if len(progress) == 0 {
		t.Fatalf("expected progress callbacks")
	}
	last := progress[len(progress)-1]
	if last.downloaded != int64(len(payload)) || last.total != int64(len(payload)) {
		t.Fatalf("last progress=%+v, want downloaded and total %d", last, len(payload))
	}
}

func TestReplaceExecutableWithRetryCopiesUpdateAndRemovesBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "LwyLink.exe")
	update := filepath.Join(dir, "new.exe")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(update, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := replaceExecutableWithRetry(update, target, 0); err != nil {
		t.Fatalf("replaceExecutableWithRetry returned error: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("target content=%q, want new", got)
	}
	if _, err := os.Stat(target + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed after successful replacement, stat err=%v", err)
	}
}

func TestReplaceExecutableRollsBackWhenUpdateMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "LwyLink.exe")
	missingUpdate := filepath.Join(dir, "missing.exe")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := replaceExecutable(missingUpdate, target); err == nil {
		t.Fatalf("expected replaceExecutable to fail for missing update")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("target was not rolled back, got %q", got)
	}
}
