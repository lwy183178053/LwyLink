package web

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/config"
)

type turnTrafficChargeFunc func(context.Context, turnUsageEvent) error

type turnUsageLedgerStore interface {
	Record(context.Context, turnUsageEvent) error
	FindByEventKey(context.Context, string) (turnUsageEvent, bool, error)
	PendingRetries(context.Context, time.Time, int) ([]turnUsageEvent, error)
}

type coturnUsageCollector struct {
	logPath   string
	statePath string
	pollEvery time.Duration
	charge    turnTrafficChargeFunc
	ledger    turnUsageLedgerStore

	mu               sync.Mutex
	offset           int64
	ready            bool
	processedLines   atomic.Int64
	chargeFailures   atomic.Int64
	lastPollUnix     atomic.Int64
	lastUsageUnix    atomic.Int64
	lastLogModUnix   atomic.Int64
	logAvailable     atomic.Bool
	lastProcessError atomic.Value
}

func newCoturnUsageCollector(cfg config.TURNCredentialConfig, charge turnTrafficChargeFunc) *coturnUsageCollector {
	logPath := strings.TrimSpace(cfg.UsageLogPath)
	if logPath == "" || charge == nil {
		return nil
	}
	pollSeconds := cfg.UsagePollIntervalSeconds
	if pollSeconds <= 0 {
		pollSeconds = 10
	}
	statePath := strings.TrimSpace(cfg.UsageStatePath)
	if statePath == "" {
		statePath = logPath + ".lwylink.offset"
	}
	return &coturnUsageCollector{
		logPath:   logPath,
		statePath: statePath,
		pollEvery: time.Duration(pollSeconds) * time.Second,
		charge:    charge,
	}
}

func (c *coturnUsageCollector) SetLedger(ledger turnUsageLedgerStore) {
	if c != nil {
		c.ledger = ledger
	}
}

func (c *coturnUsageCollector) Start(ctx context.Context) {
	if c == nil {
		return
	}
	go c.loop(ctx)
}

func (c *coturnUsageCollector) loop(ctx context.Context) {
	if err := c.initOffset(); err != nil {
		log.Printf("coturn usage init failed path=%s: %v", c.logPath, err)
	}
	ticker := time.NewTicker(c.pollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.processOnce(ctx); err != nil {
				c.lastProcessError.Store(err.Error())
				log.Printf("coturn usage process failed path=%s: %v", c.logPath, err)
			}
		}
	}
}

func (c *coturnUsageCollector) initOffset() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ready {
		return nil
	}
	if data, err := os.ReadFile(c.statePath); err == nil {
		offset, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if parseErr == nil && offset >= 0 {
			c.offset = offset
			c.ready = true
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	info, err := os.Stat(c.logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.ready = true
			return nil
		}
		return err
	}
	c.offset = info.Size()
	c.ready = true
	return c.persistOffsetLocked()
}

func (c *coturnUsageCollector) processOnce(ctx context.Context) error {
	if err := c.initOffset(); err != nil {
		return err
	}
	if err := c.retryPending(ctx); err != nil {
		log.Printf("coturn usage retry failed path=%s: %v", c.logPath, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastPollUnix.Store(time.Now().Unix())
	file, err := os.Open(c.logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.logAvailable.Store(false)
			c.lastLogModUnix.Store(0)
			c.lastProcessError.Store("coturn usage log not found")
			return nil
		}
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}
	c.logAvailable.Store(true)
	c.lastProcessError.Store("")
	c.lastLogModUnix.Store(info.ModTime().Unix())
	if info.Size() < c.offset {
		c.offset = 0
	}
	if _, err := file.Seek(c.offset, io.SeekStart); err != nil {
		return err
	}

	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" && strings.HasSuffix(line, "\n") {
			c.offset += int64(len(line))
			c.processLine(ctx, strings.TrimRight(line, "\r\n"))
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return c.persistOffsetLocked()
}

func (c *coturnUsageCollector) processLine(ctx context.Context, line string) {
	userID, bytes, ok := parseCoturnUsageLine(line)
	if !ok || userID <= 0 || bytes <= 0 {
		return
	}
	c.processedLines.Add(1)
	c.lastUsageUnix.Store(time.Now().Unix())
	event := turnUsageEvent{
		EventKey:   turnUsageEventKey(line),
		UserID:     userID,
		Bytes:      bytes,
		Source:     "coturn",
		Status:     turnUsageStatusCharged,
		RawLine:    line,
		ReportedAt: time.Now(),
	}
	c.chargeAndRecord(ctx, event)
}

func (c *coturnUsageCollector) retryPending(ctx context.Context) error {
	if c.ledger == nil {
		return nil
	}
	pending, err := c.ledger.PendingRetries(ctx, time.Now(), 50)
	if err != nil {
		return err
	}
	for _, event := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		event.ReportedAt = time.Now()
		c.chargeAndRecord(ctx, event)
	}
	return nil
}

func (c *coturnUsageCollector) chargeAndRecord(ctx context.Context, event turnUsageEvent) {
	if c.ledger != nil && strings.TrimSpace(event.EventKey) != "" {
		existing, ok, err := c.ledger.FindByEventKey(ctx, event.EventKey)
		if err != nil {
			log.Printf("coturn usage ledger lookup failed user=%d bytes=%d: %v", event.UserID, event.Bytes, err)
		} else if ok {
			if existing.Status == turnUsageStatusCharged {
				return
			}
			event.RetryCount = existing.RetryCount
		}
	}
	if err := c.charge(ctx, event); err != nil {
		event.Status = turnUsageStatusFailed
		event.ErrorMessage = err.Error()
		c.chargeFailures.Add(1)
		log.Printf("coturn usage charge failed user=%d bytes=%d: %v", event.UserID, event.Bytes, err)
	} else {
		event.Status = turnUsageStatusCharged
		event.ErrorMessage = ""
		event.NextRetryAt = time.Time{}
	}
	if c.ledger != nil {
		if err := c.ledger.Record(ctx, event); err != nil {
			log.Printf("coturn usage ledger record failed user=%d bytes=%d: %v", event.UserID, event.Bytes, err)
		}
	}
}

func (c *coturnUsageCollector) persistOffsetLocked() error {
	if c.statePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.statePath), 0700); err != nil {
		return err
	}
	return os.WriteFile(c.statePath, []byte(strconv.FormatInt(c.offset, 10)+"\n"), 0600)
}

var (
	coturnUsernameRE = regexp.MustCompile(`\busername=<([^>]*)>|\busername=([^,\s]+)`)
	coturnRBRE       = regexp.MustCompile(`\brb=([0-9]+)\b`)
	coturnSBRE       = regexp.MustCompile(`\bsb=([0-9]+)\b`)
)

func parseCoturnUsageLine(line string) (int64, int64, bool) {
	if !strings.Contains(line, "usage:") {
		return 0, 0, false
	}
	usernameMatch := coturnUsernameRE.FindStringSubmatch(line)
	if len(usernameMatch) == 0 {
		return 0, 0, false
	}
	username := usernameMatch[1]
	if username == "" {
		username = usernameMatch[2]
	}
	userID, ok := turnUsernameUserID(username)
	if !ok {
		return 0, 0, false
	}
	rb, okRB := coturnInt(coturnRBRE, line)
	sb, okSB := coturnInt(coturnSBRE, line)
	if !okRB && !okSB {
		return 0, 0, false
	}
	return userID, rb + sb, true
}

func coturnInt(re *regexp.Regexp, line string) (int64, bool) {
	match := re.FindStringSubmatch(line)
	if len(match) < 2 {
		return 0, false
	}
	value, err := strconv.ParseInt(match[1], 10, 64)
	return value, err == nil
}

func turnUsernameUserID(username string) (int64, bool) {
	for _, part := range strings.Split(username, ":") {
		part = strings.TrimSpace(part)
		if len(part) < 2 || part[0] != 'u' {
			continue
		}
		userID, err := strconv.ParseInt(part[1:], 10, 64)
		if err == nil && userID > 0 {
			return userID, true
		}
	}
	return 0, false
}

type coturnUsageStats struct {
	Enabled          bool
	LogAvailable     bool
	ProcessedLines   int64
	ChargeFailures   int64
	LastPollAt       time.Time
	LastUsageAt      time.Time
	LastLogModAt     time.Time
	LogLagSeconds    float64
	LastProcessError string
}

func (c *coturnUsageCollector) Stats(now time.Time) coturnUsageStats {
	if c == nil {
		return coturnUsageStats{}
	}
	stats := coturnUsageStats{
		Enabled:        true,
		LogAvailable:   c.logAvailable.Load(),
		ProcessedLines: c.processedLines.Load(),
		ChargeFailures: c.chargeFailures.Load(),
	}
	if value := c.lastPollUnix.Load(); value > 0 {
		stats.LastPollAt = time.Unix(value, 0)
	}
	if value := c.lastUsageUnix.Load(); value > 0 {
		stats.LastUsageAt = time.Unix(value, 0)
	}
	if value := c.lastLogModUnix.Load(); value > 0 {
		stats.LastLogModAt = time.Unix(value, 0)
		stats.LogLagSeconds = now.Sub(stats.LastLogModAt).Seconds()
	}
	if value, ok := c.lastProcessError.Load().(string); ok {
		stats.LastProcessError = value
	}
	return stats
}
