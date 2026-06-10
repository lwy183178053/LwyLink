package node

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("node not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, n Node) (Node, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO server_nodes
		 (name, public_overlay_addr, public_udp_addr, health_addr, overlay_public_key, max_rooms, enabled, status, last_error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Name, n.PublicOverlayAddr, n.PublicUDPAddr, n.HealthAddr, n.OverlayPublicKey, n.MaxRooms, n.Enabled, n.Status,
		n.LastError,
	)
	if err != nil {
		return Node{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Node{}, err
	}
	return r.FindByID(ctx, id)
}

func (r *Repository) Update(ctx context.Context, n Node) (Node, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE server_nodes
		 SET name = ?, public_overlay_addr = ?, public_udp_addr = ?, health_addr = ?, overlay_public_key = ?,
		     max_rooms = ?, enabled = ?
		 WHERE id = ?`,
		n.Name, n.PublicOverlayAddr, n.PublicUDPAddr, n.HealthAddr, n.OverlayPublicKey,
		n.MaxRooms, n.Enabled, n.ID,
	)
	if err != nil {
		return Node{}, err
	}
	if _, err := res.RowsAffected(); err != nil {
		return Node{}, err
	}
	return r.FindByID(ctx, n.ID)
}

func (r *Repository) FindByID(ctx context.Context, id int64) (Node, error) {
	return scanNode(r.db.QueryRowContext(ctx, nodeSelect()+` WHERE n.id = ? GROUP BY n.id`, id))
}

func (r *Repository) FindByPublicKey(ctx context.Context, publicKey string) (Node, error) {
	return scanNode(r.db.QueryRowContext(ctx, nodeSelect()+` WHERE n.overlay_public_key = ? GROUP BY n.id`, publicKey))
}

func (r *Repository) List(ctx context.Context) ([]Node, error) {
	rows, err := r.db.QueryContext(ctx, nodeSelect()+` GROUP BY n.id ORDER BY n.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

type Stats struct {
	Total  int
	Online int
	Paused int
}

func (r *Repository) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		   FROM server_nodes`,
		StatusOnline, StatusPaused,
	).Scan(&stats.Total, &stats.Online, &stats.Paused)
	return stats, err
}

func (r *Repository) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	status := StatusUnknown
	if !enabled {
		status = StatusDisabled
	}
	res, err := r.db.ExecContext(ctx, `UPDATE server_nodes SET enabled = ?, status = ? WHERE id = ?`, enabled, status, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return r.errIfMissing(ctx, id)
	}
	return nil
}

func (r *Repository) SetStatus(ctx context.Context, id int64, status, lastError string) error {
	var (
		res sql.Result
		err error
	)
	if status == StatusOnline {
		res, err = r.db.ExecContext(ctx,
			`UPDATE server_nodes SET status = ?, last_seen_at = CURRENT_TIMESTAMP, last_error = ? WHERE id = ?`,
			status, lastError, id,
		)
	} else {
		res, err = r.db.ExecContext(ctx,
			`UPDATE server_nodes SET status = ?, last_error = ? WHERE id = ?`,
			status, lastError, id,
		)
	}
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return r.errIfMissing(ctx, id)
	}
	return nil
}

func (r *Repository) errIfMissing(ctx context.Context, id int64) error {
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM server_nodes WHERE id = ? LIMIT 1`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func nodeSelect() string {
	return `SELECT n.id, n.name, n.public_overlay_addr, COALESCE(n.public_udp_addr, ''), n.health_addr, n.overlay_public_key,
	       n.max_rooms, COUNT(r.id) AS current_rooms, n.enabled, n.status,
	       n.last_seen_at, n.last_error, n.created_at, n.updated_at
	       FROM server_nodes n
	       LEFT JOIN rooms r ON r.node_id = n.id AND r.status = 'open'`
}

type nodeScanner interface {
	Scan(dest ...any) error
}

func scanNode(scanner nodeScanner) (Node, error) {
	var n Node
	var lastSeen sql.NullTime
	var lastError sql.NullString
	err := scanner.Scan(
		&n.ID, &n.Name, &n.PublicOverlayAddr, &n.PublicUDPAddr, &n.HealthAddr, &n.OverlayPublicKey,
		&n.MaxRooms, &n.CurrentRooms, &n.Enabled, &n.Status,
		&lastSeen, &lastError, &n.CreatedAt, &n.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	if err != nil {
		return Node{}, err
	}
	if lastSeen.Valid {
		n.LastSeenAt = &lastSeen.Time
	}
	if lastError.Valid {
		n.LastError = lastError.String
	}
	return n, nil
}

func scanNodes(rows *sql.Rows) ([]Node, error) {
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
