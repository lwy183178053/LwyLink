package clientrelease

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/dbutil"
	"github.com/go-sql-driver/mysql"
)

var (
	ErrNotFound        = errors.New("client release not found")
	ErrVersionConflict = errors.New("该平台的版本号已存在")
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, release Release) (Release, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO client_releases
		 (version, platform, notes, mandatory, install_args, file_name, stored_file_name, content_type, size_bytes, sha256)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		release.Version, release.Platform, release.Notes, release.Mandatory, release.InstallArgs,
		release.FileName, release.StoredFileName, release.ContentType, release.SizeBytes, release.SHA256,
	)
	if err != nil {
		if isDuplicateEntry(err) {
			return Release{}, ErrVersionConflict
		}
		return Release{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Release{}, err
	}
	return r.FindByID(ctx, id)
}

func (r *Repository) FindByID(ctx context.Context, id int64) (Release, error) {
	return scanRelease(r.db.QueryRowContext(ctx, releaseColumns()+` FROM client_releases WHERE id = ?`, id))
}

func (r *Repository) FindByStoredFileName(ctx context.Context, storedFileName string) (Release, error) {
	return scanRelease(r.db.QueryRowContext(ctx,
		releaseColumns()+` FROM client_releases WHERE stored_file_name = ?`,
		storedFileName,
	))
}

func (r *Repository) Delete(ctx context.Context, id int64) (Release, error) {
	release, err := r.FindByID(ctx, id)
	if err != nil {
		return Release{}, err
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM client_releases WHERE id = ?`, id)
	if err != nil {
		return Release{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Release{}, err
	}
	if affected == 0 {
		return Release{}, ErrNotFound
	}
	return release, nil
}

func (r *Repository) List(ctx context.Context, filter Filter) ([]Release, error) {
	where, args := releaseWhere(filter)
	limit := dbutil.Limit(filter.Limit, 200, 1000)
	offset := dbutil.Offset(filter.Offset)
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx,
		releaseColumns()+`
		  FROM client_releases`+where+`
		 ORDER BY id DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReleases(rows)
}

func (r *Repository) Count(ctx context.Context) (int, error) {
	var total int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_releases`).Scan(&total)
	return total, err
}

func (r *Repository) DownloadCountSince(ctx context.Context, since time.Time) (int, error) {
	var total int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_download_logs WHERE created_at >= ?`, since).Scan(&total)
	return total, err
}

func (r *Repository) LogDownload(ctx context.Context, item DownloadLog) error {
	if r == nil || r.db == nil {
		return nil
	}
	var userID any
	if item.UserID > 0 {
		userID = item.UserID
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO client_download_logs
		 (user_id, ip, platform, version, file_name, stored_file_name, result)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, item.IP, item.Platform, item.Version, item.FileName, item.StoredFileName, item.Result,
	)
	return err
}

func releaseWhere(filter Filter) (string, []any) {
	var parts []string
	var args []any
	platform := NormalizePlatform(filter.Platform)
	if strings.TrimSpace(filter.Platform) != "" {
		parts = append(parts, `platform = ?`)
		args = append(args, platform)
	}
	query := strings.TrimSpace(filter.Query)
	if query != "" {
		like := "%" + query + "%"
		if id, err := strconv.ParseInt(query, 10, 64); err == nil {
			parts = append(parts, `(id = ? OR version LIKE ? OR platform LIKE ? OR notes LIKE ? OR file_name LIKE ? OR sha256 LIKE ?)`)
			args = append(args, id, like, like, like, like, like)
		} else {
			parts = append(parts, `(version LIKE ? OR platform LIKE ? OR notes LIKE ? OR file_name LIKE ? OR sha256 LIKE ?)`)
			args = append(args, like, like, like, like, like)
		}
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

type releaseScanner interface {
	Scan(dest ...any) error
}

func releaseColumns() string {
	return `SELECT id, version, platform, COALESCE(notes, ''), mandatory, install_args,
	       file_name, stored_file_name, content_type, size_bytes, sha256, created_at, updated_at`
}

func scanRelease(scanner releaseScanner) (Release, error) {
	var r Release
	err := scanner.Scan(
		&r.ID, &r.Version, &r.Platform, &r.Notes, &r.Mandatory, &r.InstallArgs,
		&r.FileName, &r.StoredFileName, &r.ContentType, &r.SizeBytes, &r.SHA256,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Release{}, ErrNotFound
	}
	return r, err
}

func scanReleases(rows *sql.Rows) ([]Release, error) {
	var out []Release
	for rows.Next() {
		release, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, release)
	}
	return out, rows.Err()
}

func isDuplicateEntry(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
