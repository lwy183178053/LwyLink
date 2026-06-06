package account

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

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
	return `SELECT id, username, password_hash, display_name, role, membership, disabled, created_at, updated_at`
}

func scanUser(scanner userScanner) (User, error) {
	var u User
	err := scanner.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.Membership, &u.Disabled, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.Membership = normalizeMembership(u.Membership)
	return u, err
}

func isDuplicateEntry(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
