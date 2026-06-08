package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, username, passwordHash, displayName string) (User, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, display_name, role) VALUES (?, ?, ?, 'player')`,
		username, passwordHash, displayName,
	)
	if err != nil {
		if isDuplicateEntry(err) {
			return User{}, ErrUsernameUsed
		}
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return r.FindByID(ctx, id)
}

func (r *Repository) FindByUsername(ctx context.Context, username string) (User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		userColumns()+` FROM users WHERE username = ?`,
		username,
	))
}

func (r *Repository) FindByID(ctx context.Context, id int64) (User, error) {
	return scanUser(r.db.QueryRowContext(ctx,
		userColumns()+` FROM users WHERE id = ?`,
		id,
	))
}

func (r *Repository) UpdatePassword(ctx context.Context, userID int64, passwordHash string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (r *Repository) List(ctx context.Context, filter UserFilter) ([]User, error) {
	where, args := userWhere(filter)
	limit := normalizeLimit(filter.Limit, 500, 1000)
	offset := normalizeOffset(filter.Offset)
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx,
		userColumns()+`
		 FROM users`+where+`
		 ORDER BY id DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type Stats struct {
	Total    int
	Active   int
	Disabled int
}

func (r *Repository) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN disabled = FALSE THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN disabled = TRUE THEN 1 ELSE 0 END), 0)
		   FROM users`,
	).Scan(&stats.Total, &stats.Active, &stats.Disabled)
	return stats, err
}

func (r *Repository) SetDisabled(ctx context.Context, userID int64, disabled bool) error {
	res, err := r.db.ExecContext(ctx, `UPDATE users SET disabled = ? WHERE id = ?`, disabled, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (r *Repository) SetMembership(ctx context.Context, userID int64, membership string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE users SET membership = ? WHERE id = ?`, normalizeMembership(membership), userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (r *Repository) AddRelayTraffic(ctx context.Context, userID, bytes int64) error {
	if bytes <= 0 {
		return fmt.Errorf("traffic bytes must be positive")
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE users
		    SET relay_traffic_extra_bytes = relay_traffic_extra_bytes + ?
		  WHERE id = ?`,
		bytes, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (r *Repository) ConsumeRelayTraffic(ctx context.Context, userID, bytes int64) error {
	if bytes <= 0 {
		return nil
	}
	period := currentRelayTrafficPeriod()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var u User
	err = tx.QueryRowContext(ctx,
		`SELECT id, membership, disabled, relay_traffic_used_bytes, relay_traffic_extra_bytes, relay_traffic_period
		   FROM users
		  WHERE id = ?
		  FOR UPDATE`,
		userID,
	).Scan(&u.ID, &u.Membership, &u.Disabled, &u.RelayTrafficUsedBytes, &u.RelayTrafficExtraBytes, &u.RelayTrafficPeriod)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if u.Disabled {
		return ErrDisabled
	}
	u.Membership = normalizeMembership(u.Membership)
	if u.RelayTrafficPeriod != period {
		u.RelayTrafficUsedBytes = 0
		u.RelayTrafficPeriod = period
	}
	baseRemaining := maxInt64(0, u.RelayTrafficBaseQuotaBytes()-maxInt64(0, u.RelayTrafficUsedBytes))
	extraCost := maxInt64(0, bytes-baseRemaining)
	if extraCost > maxInt64(0, u.RelayTrafficExtraBytes) {
		return ErrRelayTrafficExceeded
	}
	nextUsed := maxInt64(0, u.RelayTrafficUsedBytes) + bytes
	nextExtra := maxInt64(0, u.RelayTrafficExtraBytes) - extraCost

	if _, err := tx.ExecContext(ctx,
		`UPDATE users
		    SET relay_traffic_used_bytes = ?,
		        relay_traffic_extra_bytes = ?,
		        relay_traffic_period = ?
		  WHERE id = ?`,
		nextUsed, nextExtra, period, userID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) Delete(ctx context.Context, userID int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func userWhere(filter UserFilter) (string, []any) {
	var parts []string
	var args []any
	query := strings.TrimSpace(filter.Query)
	if query != "" {
		like := "%" + query + "%"
		if id, err := strconv.ParseInt(query, 10, 64); err == nil {
			parts = append(parts, `(id = ? OR username LIKE ? OR display_name LIKE ? OR role LIKE ? OR membership LIKE ?)`)
			args = append(args, id, like, like, like, like)
		} else {
			parts = append(parts, `(username LIKE ? OR display_name LIKE ? OR role LIKE ? OR membership LIKE ?)`)
			args = append(args, like, like, like, like)
		}
	}
	switch strings.ToLower(strings.TrimSpace(filter.Status)) {
	case "active":
		parts = append(parts, `disabled = FALSE`)
	case "disabled":
		parts = append(parts, `disabled = TRUE`)
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func normalizeLimit(v, fallback, max int) int {
	if v <= 0 {
		return fallback
	}
	return min(v, max)
}

func normalizeOffset(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

type userScanner interface {
	Scan(dest ...any) error
}

func userColumns() string {
	return `SELECT id, username, password_hash, display_name, role, membership, disabled,
	       CASE WHEN relay_traffic_period = ` + relayTrafficPeriodSQL() + ` THEN relay_traffic_used_bytes ELSE 0 END,
	       relay_traffic_extra_bytes,
	       CASE WHEN relay_traffic_period = ` + relayTrafficPeriodSQL() + ` THEN relay_traffic_period ELSE ` + relayTrafficPeriodSQL() + ` END,
	       created_at, updated_at`
}

func scanUser(scanner userScanner) (User, error) {
	var u User
	err := scanner.Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.Membership, &u.Disabled,
		&u.RelayTrafficUsedBytes, &u.RelayTrafficExtraBytes, &u.RelayTrafficPeriod,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.Membership = normalizeMembership(u.Membership)
	return u, err
}

func currentRelayTrafficPeriod() string {
	return time.Now().Format("2006-01")
}

func relayTrafficPeriodSQL() string {
	return `DATE_FORMAT(CURRENT_DATE(), '%Y-%m')`
}

func isDuplicateEntry(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
