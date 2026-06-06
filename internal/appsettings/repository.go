package appsettings

import (
	"context"
	"database/sql"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT setting_value FROM app_settings WHERE setting_key = ?`, key).Scan(&value)
	return value, err
}

func (r *Repository) Set(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO app_settings (setting_key, setting_value)
		 VALUES (?, ?)
		 ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value)`,
		key, value,
	)
	return err
}
