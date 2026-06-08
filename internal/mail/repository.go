package mail

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Verification struct {
	ID        int64
	Email     string
	Purpose   string
	CodeHash  string
	IP        string
	UsedAt    sql.NullTime
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, v Verification) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO email_verifications (email, purpose, code_hash, ip, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		v.Email, v.Purpose, v.CodeHash, v.IP, v.ExpiresAt,
	)
	return err
}

func (r *Repository) Latest(ctx context.Context, email, purpose string) (Verification, error) {
	return scanVerification(r.db.QueryRowContext(ctx,
		`SELECT id, email, purpose, code_hash, ip, used_at, expires_at, created_at, updated_at
		   FROM email_verifications
		  WHERE email = ? AND purpose = ?
		  ORDER BY id DESC LIMIT 1`,
		email, purpose,
	))
}

func (r *Repository) MarkUsed(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE email_verifications
		    SET used_at = CURRENT_TIMESTAMP
		  WHERE id = ? AND used_at IS NULL`,
		id,
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

func (r *Repository) CountRecentByEmail(ctx context.Context, email, purpose string, since time.Time) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM email_verifications WHERE email = ? AND purpose = ? AND created_at >= ?`,
		email, purpose, since,
	).Scan(&count)
	return count, err
}

func (r *Repository) CountRecentByIP(ctx context.Context, ip, purpose string, since time.Time) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM email_verifications WHERE ip = ? AND purpose = ? AND created_at >= ?`,
		ip, purpose, since,
	).Scan(&count)
	return count, err
}

type verificationScanner interface {
	Scan(dest ...any) error
}

func scanVerification(scanner verificationScanner) (Verification, error) {
	var v Verification
	err := scanner.Scan(
		&v.ID, &v.Email, &v.Purpose, &v.CodeHash, &v.IP, &v.UsedAt, &v.ExpiresAt, &v.CreatedAt, &v.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Verification{}, ErrVerificationNotFound
	}
	return v, err
}
