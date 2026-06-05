package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			username VARCHAR(64) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL,
			display_name VARCHAR(64) NOT NULL,
			role VARCHAR(32) NOT NULL DEFAULT 'player',
			membership VARCHAR(16) NOT NULL DEFAULT 'free',
			disabled BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS rooms (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(80) NOT NULL,
			owner_user_id BIGINT NOT NULL,
			node_id BIGINT NULL,
			join_code VARCHAR(16) NOT NULL UNIQUE,
			room_public_key VARCHAR(128) NOT NULL UNIQUE,
			network_cidr VARCHAR(32) NOT NULL UNIQUE,
			max_members INT NOT NULL DEFAULT 8,
			status VARCHAR(16) NOT NULL DEFAULT 'open',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_rooms_owner (owner_user_id),
			INDEX idx_rooms_node (node_id),
			CONSTRAINT fk_rooms_owner FOREIGN KEY (owner_user_id) REFERENCES users(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS server_nodes (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(80) NOT NULL UNIQUE,
			public_overlay_addr VARCHAR(255) NOT NULL,
			health_addr VARCHAR(255) NOT NULL DEFAULT '',
			overlay_public_key VARCHAR(128) NOT NULL,
			max_rooms INT NOT NULL DEFAULT 100,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			status VARCHAR(16) NOT NULL DEFAULT 'unknown',
			last_seen_at TIMESTAMP NULL,
			last_error TEXT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_server_nodes_status (enabled, status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS room_members (
			room_id BIGINT NOT NULL,
			user_id BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (room_id, user_id),
			UNIQUE INDEX uniq_room_members_user (user_id),
			CONSTRAINT fk_room_members_room FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
			CONSTRAINT fk_room_members_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS client_releases (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			version VARCHAR(32) NOT NULL,
			platform VARCHAR(32) NOT NULL,
			notes TEXT NULL,
			mandatory BOOLEAN NOT NULL DEFAULT FALSE,
			install_args VARCHAR(255) NOT NULL DEFAULT '',
			file_name VARCHAR(255) NOT NULL,
			stored_file_name VARCHAR(255) NOT NULL UNIQUE,
			content_type VARCHAR(128) NOT NULL DEFAULT '',
			size_bytes BIGINT NOT NULL,
			sha256 CHAR(64) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE INDEX uniq_client_releases_platform_version (platform, version),
			INDEX idx_client_releases_platform_created (platform, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if err := ensureColumn(ctx, db, "users", "membership", "VARCHAR(16) NOT NULL DEFAULT 'free' AFTER role"); err != nil {
		return fmt.Errorf("migrate users.membership: %w", err)
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM information_schema.COLUMNS
		  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
		table, column,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", table, column, definition))
	return err
}
