package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var diagnosticMu sync.Mutex

func diagnosticLogf(format string, args ...any) {
	path := diagnosticLogPath()
	if path == "" {
		return
	}
	diagnosticMu.Lock()
	defer diagnosticMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "%s pid=%d %s\n", time.Now().Format(time.RFC3339Nano), os.Getpid(), fmt.Sprintf(format, args...))
}

func diagnosticWriteState(state AppState) {
	path := diagnosticStatePath()
	if path == "" {
		return
	}
	status := state.Update.Status
	latest := state.Update.LatestVersion
	mandatory := state.Update.Mandatory
	progress := state.Update.Progress
	downloaded := state.Update.DownloadedBytes
	size := state.Update.SizeBytes
	hasUser := state.User != nil

	diagnosticMu.Lock()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		diagnosticMu.Unlock()
		return
	}
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		diagnosticMu.Unlock()
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0644); err != nil {
		diagnosticMu.Unlock()
		return
	}
	_ = os.Rename(tmp, path)
	diagnosticMu.Unlock()
	diagnosticLogf("state update_status=%s latest=%s mandatory=%t progress=%d downloaded=%d size=%d user=%t",
		status,
		latest,
		mandatory,
		progress,
		downloaded,
		size,
		hasUser,
	)
}

func diagnosticLogPath() string {
	return firstEnvPath("LWYLINK_DIAGNOSTIC_LOG", "LWYLINK_UPGRADE_TEST_LOG")
}

func diagnosticStatePath() string {
	return firstEnvPath("LWYLINK_DIAGNOSTIC_STATE", "LWYLINK_UPGRADE_TEST_STATE")
}

func firstEnvPath(names ...string) string {
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value != "" {
			return value
		}
	}
	return ""
}
