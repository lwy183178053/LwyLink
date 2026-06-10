package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/config"
)

func TestParseCoturnUsageLine(t *testing.T) {
	line := `session 001: usage: realm=<lwylink.xyz>, username=<1700:u42:lwylink>, rp=3, rb=120, sp=4, sb=240`
	userID, bytes, ok := parseCoturnUsageLine(line)
	if !ok {
		t.Fatalf("usage line was not parsed")
	}
	if userID != 42 || bytes != 360 {
		t.Fatalf("usage user=%d bytes=%d, want 42/360", userID, bytes)
	}
}

func TestCoturnUsageCollectorChargesNewUsageOnce(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "coturn.log")
	statePath := filepath.Join(dir, "coturn.offset")
	if err := os.WriteFile(logPath, []byte("old line\n"), 0600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	var charged []int64
	collector := newCoturnUsageCollector(config.TURNCredentialConfig{
		UsageLogPath:   logPath,
		UsageStatePath: statePath,
	}, func(_ context.Context, event turnUsageEvent) error {
		if event.UserID != 7 {
			t.Fatalf("charged user=%d, want 7", event.UserID)
		}
		charged = append(charged, event.Bytes)
		return nil
	})
	if err := collector.initOffset(); err != nil {
		t.Fatalf("init offset: %v", err)
	}
	appendLog(t, logPath, `session 002: usage: realm=<lwylink.xyz>, username=<2000:u7:lwylink>, rp=1, rb=100, sp=1, sb=50`+"\n")
	if err := collector.processOnce(context.Background()); err != nil {
		t.Fatalf("process once: %v", err)
	}
	if err := collector.processOnce(context.Background()); err != nil {
		t.Fatalf("process twice: %v", err)
	}
	if len(charged) != 1 || charged[0] != 150 {
		t.Fatalf("charged=%v, want one 150-byte charge", charged)
	}
}

func TestCoturnUsageCollectorStatsChargeFailures(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "coturn.log")
	statePath := filepath.Join(dir, "coturn.offset")
	if err := os.WriteFile(logPath, nil, 0600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	collector := newCoturnUsageCollector(config.TURNCredentialConfig{
		UsageLogPath:   logPath,
		UsageStatePath: statePath,
	}, func(context.Context, turnUsageEvent) error {
		return errors.New("quota exceeded")
	})
	if err := collector.initOffset(); err != nil {
		t.Fatalf("init offset: %v", err)
	}
	appendLog(t, logPath, `session 003: usage: realm=<lwylink.xyz>, username=<2000:u9:lwylink>, rb=10, sb=5`+"\n")
	if err := collector.processOnce(context.Background()); err != nil {
		t.Fatalf("process once: %v", err)
	}
	stats := collector.Stats(time.Now())
	if stats.ProcessedLines != 1 || stats.ChargeFailures != 1 {
		t.Fatalf("bad coturn stats: %+v", stats)
	}
	if stats.LastUsageAt.IsZero() || stats.LastLogModAt.IsZero() || stats.LogLagSeconds < 0 {
		t.Fatalf("missing coturn timestamps: %+v", stats)
	}
}

func TestCoturnUsageCollectorSkipsAlreadyChargedLedgerEvent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "coturn.log")
	statePath := filepath.Join(dir, "coturn.offset")
	line := `session 004: usage: realm=<lwylink.xyz>, username=<2000:u10:lwylink>, rb=20, sb=5`
	if err := os.WriteFile(logPath, []byte(line+"\n"), 0600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	ledger := newMemoryTurnLedger()
	var charged int
	collector := newCoturnUsageCollector(config.TURNCredentialConfig{
		UsageLogPath:   logPath,
		UsageStatePath: statePath,
	}, func(context.Context, turnUsageEvent) error {
		charged++
		return nil
	})
	collector.SetLedger(ledger)
	collector.offset = 0
	collector.ready = true
	if err := collector.processOnce(context.Background()); err != nil {
		t.Fatalf("process once: %v", err)
	}
	collector.offset = 0
	if err := collector.processOnce(context.Background()); err != nil {
		t.Fatalf("process replay: %v", err)
	}
	if charged != 1 {
		t.Fatalf("charged=%d, want one idempotent charge", charged)
	}
}

func TestCoturnUsageCollectorRetriesFailedLedgerEvent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "coturn.log")
	statePath := filepath.Join(dir, "coturn.offset")
	line := `session 005: usage: realm=<lwylink.xyz>, username=<2000:u11:lwylink>, rb=30, sb=5`
	if err := os.WriteFile(logPath, []byte(line+"\n"), 0600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	ledger := newMemoryTurnLedger()
	attempts := 0
	collector := newCoturnUsageCollector(config.TURNCredentialConfig{
		UsageLogPath:   logPath,
		UsageStatePath: statePath,
	}, func(context.Context, turnUsageEvent) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary quota backend failure")
		}
		return nil
	})
	collector.SetLedger(ledger)
	collector.offset = 0
	collector.ready = true
	if err := collector.processOnce(context.Background()); err != nil {
		t.Fatalf("process once: %v", err)
	}
	key := turnUsageEventKey(line)
	failed, ok, err := ledger.FindByEventKey(context.Background(), key)
	if err != nil || !ok {
		t.Fatalf("missing failed event ok=%v err=%v", ok, err)
	}
	if failed.Status != turnUsageStatusFailed {
		t.Fatalf("status=%s, want failed", failed.Status)
	}
	failed.NextRetryAt = time.Now().Add(-time.Second)
	ledger.events[key] = failed
	if err := collector.processOnce(context.Background()); err != nil {
		t.Fatalf("process retry: %v", err)
	}
	charged, ok, err := ledger.FindByEventKey(context.Background(), key)
	if err != nil || !ok {
		t.Fatalf("missing charged event ok=%v err=%v", ok, err)
	}
	if charged.Status != turnUsageStatusCharged {
		t.Fatalf("status=%s, want charged", charged.Status)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d, want initial failure plus retry", attempts)
	}
}

func TestCoturnUsageCollectorStatsLogMissing(t *testing.T) {
	dir := t.TempDir()
	collector := newCoturnUsageCollector(config.TURNCredentialConfig{
		UsageLogPath:   filepath.Join(dir, "missing.log"),
		UsageStatePath: filepath.Join(dir, "coturn.offset"),
	}, func(context.Context, turnUsageEvent) error {
		return nil
	})
	if err := collector.processOnce(context.Background()); err != nil {
		t.Fatalf("process missing log: %v", err)
	}
	stats := collector.Stats(time.Now())
	if !stats.Enabled || stats.LogAvailable {
		t.Fatalf("bad log availability stats: %+v", stats)
	}
	if stats.LastPollAt.IsZero() || stats.LastProcessError == "" {
		t.Fatalf("missing log did not record poll/error stats: %+v", stats)
	}
}

func TestTurnUsageWhereUsesReportedAtRange(t *testing.T) {
	start := time.Date(2026, 6, 10, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 1)
	where, args := turnUsageWhere(turnUsageFilter{
		UserID:    42,
		StartTime: start,
		EndTime:   end,
	})
	for _, want := range []string{"user_id = ?", "reported_at >= ?", "reported_at < ?"} {
		if !strings.Contains(where, want) {
			t.Fatalf("where %q missing %q", where, want)
		}
	}
	if strings.Contains(where, "DATE(") {
		t.Fatalf("where should not wrap reported_at in DATE(): %q", where)
	}
	if len(args) != 3 {
		t.Fatalf("args=%v, want 3 args", args)
	}
}

func TestTurnUsageAuthoritativeFallsBackAfterMissingLogPoll(t *testing.T) {
	collector := &coturnUsageCollector{}
	srv := &Server{turnUsage: collector}
	if !srv.turnUsageAuthoritative() {
		t.Fatalf("collector should be authoritative before first poll to avoid duplicate client charges")
	}

	collector.lastPollUnix.Store(time.Now().Unix())
	collector.logAvailable.Store(false)
	if srv.turnUsageAuthoritative() {
		t.Fatalf("collector should stop being authoritative after a poll proves the log is unavailable")
	}

	collector.logAvailable.Store(true)
	if !srv.turnUsageAuthoritative() {
		t.Fatalf("collector should be authoritative when the usage log is available")
	}
}

func appendLog(t *testing.T, path, line string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open log append: %v", err)
	}
	defer file.Close()
	if _, err := file.WriteString(line); err != nil {
		t.Fatalf("append log: %v", err)
	}
}

type memoryTurnLedger struct {
	events map[string]turnUsageEvent
}

func newMemoryTurnLedger() *memoryTurnLedger {
	return &memoryTurnLedger{events: make(map[string]turnUsageEvent)}
}

func (m *memoryTurnLedger) Record(_ context.Context, event turnUsageEvent) error {
	event = normalizeTurnUsageEvent(event)
	existing, ok := m.events[event.EventKey]
	if ok && existing.Status == turnUsageStatusCharged {
		return nil
	}
	if ok && event.Status == turnUsageStatusFailed {
		event.RetryCount = existing.RetryCount + 1
	}
	m.events[event.EventKey] = event
	return nil
}

func (m *memoryTurnLedger) FindByEventKey(_ context.Context, eventKey string) (turnUsageEvent, bool, error) {
	event, ok := m.events[eventKey]
	return event, ok, nil
}

func (m *memoryTurnLedger) PendingRetries(_ context.Context, now time.Time, limit int) ([]turnUsageEvent, error) {
	var out []turnUsageEvent
	for _, event := range m.events {
		if event.Status == turnUsageStatusFailed && (event.NextRetryAt.IsZero() || !event.NextRetryAt.After(now)) {
			out = append(out, event)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
