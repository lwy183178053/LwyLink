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
			username VARCHAR(254) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL,
			display_name VARCHAR(64) NOT NULL,
			role VARCHAR(32) NOT NULL DEFAULT 'player',
			membership VARCHAR(16) NOT NULL DEFAULT 'free',
			disabled BOOLEAN NOT NULL DEFAULT FALSE,
			relay_traffic_used_bytes BIGINT NOT NULL DEFAULT 0,
			relay_traffic_extra_bytes BIGINT NOT NULL DEFAULT 0,
			relay_traffic_period VARCHAR(7) NOT NULL DEFAULT '',
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
			max_members INT NOT NULL DEFAULT 4,
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
			public_udp_addr VARCHAR(255) NOT NULL DEFAULT '',
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
			mandatory BOOLEAN NOT NULL DEFAULT TRUE,
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
		`CREATE TABLE IF NOT EXISTS client_download_logs (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			user_id BIGINT NULL,
			ip VARCHAR(64) NOT NULL DEFAULT '',
			platform VARCHAR(32) NOT NULL DEFAULT '',
			version VARCHAR(32) NOT NULL DEFAULT '',
			file_name VARCHAR(255) NOT NULL DEFAULT '',
			stored_file_name VARCHAR(255) NOT NULL DEFAULT '',
			result VARCHAR(32) NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_client_download_logs_user_created (user_id, created_at),
			INDEX idx_client_download_logs_ip_created (ip, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			setting_key VARCHAR(64) NOT NULL PRIMARY KEY,
			setting_value TEXT NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS email_verifications (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			email VARCHAR(254) NOT NULL,
			purpose VARCHAR(32) NOT NULL,
			code_hash VARCHAR(255) NOT NULL,
			ip VARCHAR(64) NOT NULL DEFAULT '',
			used_at TIMESTAMP NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_email_verifications_email_purpose_created (email, purpose, created_at),
			INDEX idx_email_verifications_ip_created (ip, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS turn_usage_events (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			event_key VARCHAR(96) NULL,
			user_id BIGINT NOT NULL,
			source VARCHAR(32) NOT NULL DEFAULT 'coturn',
			bytes BIGINT NOT NULL,
			status VARCHAR(32) NOT NULL,
			error_message TEXT NULL,
			raw_line TEXT NULL,
			reported_at TIMESTAMP NOT NULL,
			retry_count INT NOT NULL DEFAULT 0,
			next_retry_at TIMESTAMP NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE INDEX uniq_turn_usage_events_event_key (event_key),
			INDEX idx_turn_usage_events_user_reported (user_id, reported_at),
			INDEX idx_turn_usage_events_reported (reported_at),
			INDEX idx_turn_usage_events_retry (status, next_retry_at),
			INDEX idx_turn_usage_events_status (status, reported_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`INSERT IGNORE INTO app_settings (setting_key, setting_value)
		 VALUES ('announcement', '官方 QQ 群：715516422')`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if err := ensureColumn(ctx, db, "users", "membership", "VARCHAR(16) NOT NULL DEFAULT 'free' AFTER role"); err != nil {
		return fmt.Errorf("migrate users.membership: %w", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE users MODIFY COLUMN username VARCHAR(254) NOT NULL`); err != nil {
		return fmt.Errorf("migrate users.username: %w", err)
	}
	if err := ensureColumn(ctx, db, "users", "relay_traffic_used_bytes", "BIGINT NOT NULL DEFAULT 0 AFTER disabled"); err != nil {
		return fmt.Errorf("migrate users.relay_traffic_used_bytes: %w", err)
	}
	if err := ensureColumn(ctx, db, "users", "relay_traffic_extra_bytes", "BIGINT NOT NULL DEFAULT 0 AFTER relay_traffic_used_bytes"); err != nil {
		return fmt.Errorf("migrate users.relay_traffic_extra_bytes: %w", err)
	}
	if err := ensureColumn(ctx, db, "users", "relay_traffic_period", "VARCHAR(7) NOT NULL DEFAULT '' AFTER relay_traffic_extra_bytes"); err != nil {
		return fmt.Errorf("migrate users.relay_traffic_period: %w", err)
	}
	if err := ensureColumn(ctx, db, "server_nodes", "public_udp_addr", "VARCHAR(255) NOT NULL DEFAULT '' AFTER public_overlay_addr"); err != nil {
		return fmt.Errorf("migrate server_nodes.public_udp_addr: %w", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE client_releases MODIFY COLUMN mandatory BOOLEAN NOT NULL DEFAULT TRUE`); err != nil {
		return fmt.Errorf("migrate client_releases.mandatory: %w", err)
	}
	if err := ensureColumn(ctx, db, "turn_usage_events", "event_key", "VARCHAR(96) NULL AFTER id"); err != nil {
		return fmt.Errorf("migrate turn_usage_events.event_key: %w", err)
	}
	if err := ensureColumn(ctx, db, "turn_usage_events", "retry_count", "INT NOT NULL DEFAULT 0 AFTER reported_at"); err != nil {
		return fmt.Errorf("migrate turn_usage_events.retry_count: %w", err)
	}
	if err := ensureColumn(ctx, db, "turn_usage_events", "next_retry_at", "TIMESTAMP NULL AFTER retry_count"); err != nil {
		return fmt.Errorf("migrate turn_usage_events.next_retry_at: %w", err)
	}
	if err := ensureColumn(ctx, db, "turn_usage_events", "updated_at", "TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP AFTER created_at"); err != nil {
		return fmt.Errorf("migrate turn_usage_events.updated_at: %w", err)
	}
	if err := ensureIndex(ctx, db, "turn_usage_events", "uniq_turn_usage_events_event_key", "UNIQUE INDEX `uniq_turn_usage_events_event_key` (`event_key`)"); err != nil {
		return fmt.Errorf("migrate turn_usage_events.event_key index: %w", err)
	}
	if err := ensureIndex(ctx, db, "turn_usage_events", "idx_turn_usage_events_retry", "INDEX `idx_turn_usage_events_retry` (`status`, `next_retry_at`)"); err != nil {
		return fmt.Errorf("migrate turn_usage_events.retry index: %w", err)
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

func ensureIndex(ctx context.Context, db *sql.DB, table, indexName, definition string) error {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM information_schema.STATISTICS
		  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?`,
		table, indexName,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE `%s` ADD %s", table, definition))
	return err
}
