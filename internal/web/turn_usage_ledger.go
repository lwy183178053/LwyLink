package web

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/dbutil"
)

const (
	turnUsageStatusCharged = "charged"
	turnUsageStatusFailed  = "failed"
)

type turnUsageLedger struct {
	db *sql.DB
}

type turnUsageEvent struct {
	EventKey     string
	UserID       int64
	Bytes        int64
	Source       string
	Status       string
	ErrorMessage string
	RawLine      string
	ReportedAt   time.Time
	RetryCount   int
	NextRetryAt  time.Time
}

type turnUsageFilter struct {
	UserID    int64
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Offset    int
}

type turnUsageDailySummary struct {
	UserID       int64
	Date         string
	ChargedBytes int64
	FailedBytes  int64
	Events       int
	Failures     int
}

type turnUsagePage struct {
	Summaries []turnUsageDailySummary
	Events    []turnUsageEvent
	HasMore   bool
}

func NewTurnUsageLedger(db *sql.DB) *turnUsageLedger {
	if db == nil {
		return nil
	}
	return &turnUsageLedger{db: db}
}

func (l *turnUsageLedger) Record(ctx context.Context, event turnUsageEvent) error {
	if l == nil || l.db == nil || event.UserID <= 0 || event.Bytes <= 0 {
		return nil
	}
	event = normalizeTurnUsageEvent(event)
	_, err := l.db.ExecContext(ctx,
		`INSERT INTO turn_usage_events
		 (event_key, user_id, source, bytes, status, error_message, raw_line, reported_at, retry_count, next_retry_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   error_message = CASE WHEN status = ? THEN error_message ELSE VALUES(error_message) END,
		   reported_at = CASE WHEN status = ? THEN reported_at ELSE VALUES(reported_at) END,
		   retry_count = CASE
		     WHEN status = ? THEN retry_count
		     WHEN VALUES(status) = ? THEN retry_count
		     ELSE retry_count + 1
		   END,
		   next_retry_at = CASE
		     WHEN status = ? THEN NULL
		     WHEN VALUES(status) = ? THEN NULL
		     ELSE VALUES(next_retry_at)
		   END,
		   status = CASE WHEN status = ? THEN status ELSE VALUES(status) END`,
		nullableString(event.EventKey), event.UserID, event.Source, event.Bytes, event.Status, nullableString(event.ErrorMessage),
		nullableString(event.RawLine), event.ReportedAt, event.RetryCount, nullableTime(event.NextRetryAt),
		turnUsageStatusCharged, turnUsageStatusCharged, turnUsageStatusCharged, turnUsageStatusCharged, turnUsageStatusCharged, turnUsageStatusCharged,
		turnUsageStatusCharged,
	)
	return err
}

func normalizeTurnUsageEvent(event turnUsageEvent) turnUsageEvent {
	source := strings.TrimSpace(event.Source)
	if source == "" {
		source = "coturn"
	}
	status := strings.TrimSpace(event.Status)
	if status == "" {
		status = turnUsageStatusCharged
	}
	reportedAt := event.ReportedAt
	if reportedAt.IsZero() {
		reportedAt = time.Now()
	}
	event.Source = source
	event.Status = status
	event.ReportedAt = reportedAt
	event.EventKey = strings.TrimSpace(event.EventKey)
	if event.EventKey == "" && source == "coturn" && strings.TrimSpace(event.RawLine) != "" {
		event.EventKey = turnUsageEventKey(event.RawLine)
	}
	if status == turnUsageStatusFailed && event.NextRetryAt.IsZero() {
		event.NextRetryAt = turnUsageNextRetryAt(time.Now(), event.RetryCount)
	}
	if status == turnUsageStatusCharged {
		event.ErrorMessage = ""
		event.NextRetryAt = time.Time{}
	}
	return event
}

func (l *turnUsageLedger) FindByEventKey(ctx context.Context, eventKey string) (turnUsageEvent, bool, error) {
	if l == nil || l.db == nil || strings.TrimSpace(eventKey) == "" {
		return turnUsageEvent{}, false, nil
	}
	event, err := scanTurnUsageEvent(l.db.QueryRowContext(ctx,
		`SELECT COALESCE(event_key, ''), user_id, source, bytes, status, COALESCE(error_message, ''), COALESCE(raw_line, ''), reported_at, retry_count, next_retry_at
		   FROM turn_usage_events
		  WHERE event_key = ?`,
		strings.TrimSpace(eventKey),
	))
	if err != nil {
		if err == sql.ErrNoRows {
			return turnUsageEvent{}, false, nil
		}
		return turnUsageEvent{}, false, err
	}
	return event, true, nil
}

func (l *turnUsageLedger) PendingRetries(ctx context.Context, now time.Time, limit int) ([]turnUsageEvent, error) {
	if l == nil || l.db == nil {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	limit = dbutil.Limit(limit, 50, 200)
	rows, err := l.db.QueryContext(ctx,
		`SELECT COALESCE(event_key, ''), user_id, source, bytes, status, COALESCE(error_message, ''), COALESCE(raw_line, ''), reported_at, retry_count, next_retry_at
		   FROM turn_usage_events
		  WHERE status = ? AND event_key IS NOT NULL AND event_key <> '' AND (next_retry_at IS NULL OR next_retry_at <= ?)
		  ORDER BY reported_at ASC, id ASC
		  LIMIT ?`,
		turnUsageStatusFailed, now, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []turnUsageEvent
	for rows.Next() {
		event, err := scanTurnUsageEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (l *turnUsageLedger) List(ctx context.Context, filter turnUsageFilter) (turnUsagePage, error) {
	if l == nil || l.db == nil {
		return turnUsagePage{}, nil
	}
	where, args := turnUsageWhere(filter)
	limit := dbutil.Limit(filter.Limit, 100, 500)
	offset := dbutil.Offset(filter.Offset)

	summaryRows, err := l.db.QueryContext(ctx,
		`SELECT user_id, DATE_FORMAT(reported_at, '%Y-%m-%d') AS day,
		        COALESCE(SUM(CASE WHEN status = ? THEN bytes ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = ? THEN bytes ELSE 0 END), 0),
		        COUNT(*),
		        COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		   FROM turn_usage_events`+where+`
		  GROUP BY user_id, day
		  ORDER BY day DESC, user_id DESC
		  LIMIT 200`,
		append([]any{turnUsageStatusCharged, turnUsageStatusFailed, turnUsageStatusFailed}, args...)...,
	)
	if err != nil {
		return turnUsagePage{}, err
	}
	defer summaryRows.Close()

	var page turnUsagePage
	for summaryRows.Next() {
		var item turnUsageDailySummary
		if err := summaryRows.Scan(&item.UserID, &item.Date, &item.ChargedBytes, &item.FailedBytes, &item.Events, &item.Failures); err != nil {
			return turnUsagePage{}, err
		}
		page.Summaries = append(page.Summaries, item)
	}
	if err := summaryRows.Err(); err != nil {
		return turnUsagePage{}, err
	}

	eventRows, err := l.db.QueryContext(ctx,
		`SELECT COALESCE(event_key, ''), user_id, source, bytes, status, COALESCE(error_message, ''), COALESCE(raw_line, ''), reported_at, retry_count, next_retry_at
		   FROM turn_usage_events`+where+`
		  ORDER BY reported_at DESC, id DESC
		  LIMIT ? OFFSET ?`,
		append(args, limit+1, offset)...,
	)
	if err != nil {
		return turnUsagePage{}, err
	}
	defer eventRows.Close()
	for eventRows.Next() {
		item, err := scanTurnUsageEvent(eventRows)
		if err != nil {
			return turnUsagePage{}, err
		}
		page.Events = append(page.Events, item)
	}
	if err := eventRows.Err(); err != nil {
		return turnUsagePage{}, err
	}
	if len(page.Events) > limit {
		page.HasMore = true
		page.Events = page.Events[:limit]
	}
	return page, nil
}

func turnUsageWhere(filter turnUsageFilter) (string, []any) {
	var parts []string
	var args []any
	if filter.UserID > 0 {
		parts = append(parts, "user_id = ?")
		args = append(args, filter.UserID)
	}
	if !filter.StartTime.IsZero() {
		parts = append(parts, "reported_at >= ?")
		args = append(args, filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		parts = append(parts, "reported_at < ?")
		args = append(args, filter.EndTime)
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

type turnUsageEventScanner interface {
	Scan(dest ...any) error
}

func scanTurnUsageEvent(scanner turnUsageEventScanner) (turnUsageEvent, error) {
	var item turnUsageEvent
	var nextRetry sql.NullTime
	err := scanner.Scan(
		&item.EventKey, &item.UserID, &item.Source, &item.Bytes, &item.Status, &item.ErrorMessage, &item.RawLine,
		&item.ReportedAt, &item.RetryCount, &nextRetry,
	)
	if nextRetry.Valid {
		item.NextRetryAt = nextRetry.Time
	}
	return item, err
}

func turnUsageEventKey(rawLine string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawLine)))
	return "coturn:" + hex.EncodeToString(sum[:])
}

func turnUsageNextRetryAt(now time.Time, retryCount int) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	if retryCount < 0 {
		retryCount = 0
	}
	delay := time.Minute
	for i := 0; i < retryCount && delay < time.Hour; i++ {
		delay *= 2
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	return now.Add(delay)
}
