package room

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("room not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, name string, ownerUserID, nodeID int64, joinCode, roomPublicKey, networkCIDR string, maxMembers int) (Room, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, err
	}
	defer tx.Rollback()

	var nodeValue any
	if nodeID > 0 {
		nodeValue = nodeID
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO rooms (name, owner_user_id, node_id, join_code, room_public_key, network_cidr, max_members, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		name, ownerUserID, nodeValue, joinCode, roomPublicKey, networkCIDR, maxMembers, StatusOpen,
	)
	if err != nil {
		return Room{}, err
	}
	roomID, err := res.LastInsertId()
	if err != nil {
		return Room{}, err
	}
	// The owner is inserted as a member in the same transaction so a newly
	// created room is immediately connectable by its creator.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO room_members (room_id, user_id) VALUES (?, ?)`,
		roomID, ownerUserID,
	); err != nil {
		return Room{}, err
	}
	if err := tx.Commit(); err != nil {
		return Room{}, err
	}
	return r.FindByID(ctx, roomID)
}

func (r *Repository) UserHasMembership(ctx context.Context, userID int64) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx,
		`SELECT 1 FROM room_members WHERE user_id = ? LIMIT 1`,
		userID,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r *Repository) UserMembership(ctx context.Context, userID int64) (string, error) {
	var membership string
	err := r.db.QueryRowContext(ctx, `SELECT membership FROM users WHERE id = ?`, userID).Scan(&membership)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("user not found")
	}
	return membership, err
}

func (r *Repository) FindForUser(ctx context.Context, userID int64) (Room, bool, error) {
	room, err := scanRoom(r.db.QueryRowContext(ctx,
		roomColumns()+`
		FROM rooms r
		 JOIN room_members m ON m.room_id = r.id
		 LEFT JOIN server_nodes n ON n.id = r.node_id
		 WHERE m.user_id = ?
		 ORDER BY r.id DESC LIMIT 1`,
		userID,
	))
	if errors.Is(err, ErrNotFound) {
		return Room{}, false, nil
	}
	return room, err == nil, err
}

func (r *Repository) FindByID(ctx context.Context, id int64) (Room, error) {
	return scanRoom(r.db.QueryRowContext(ctx,
		roomSelect()+` WHERE r.id = ?`,
		id,
	))
}

func (r *Repository) AddMemberWithinLimit(ctx context.Context, roomID, userID int64, maxMembers int) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var roomExists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM rooms WHERE id = ? FOR UPDATE`, roomID).Scan(&roomExists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}

	var one int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM room_members WHERE room_id = ? AND user_id = ? LIMIT 1`,
		roomID, userID,
	).Scan(&one)
	if err == nil {
		return true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	var existingRoomID int64
	err = tx.QueryRowContext(ctx,
		`SELECT room_id FROM room_members WHERE user_id = ? LIMIT 1`,
		userID,
	).Scan(&existingRoomID)
	if err == nil {
		return false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	var memberCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM room_members WHERE room_id = ?`, roomID).Scan(&memberCount); err != nil {
		return false, err
	}
	if memberCount >= maxMembers {
		return false, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO room_members (room_id, user_id) VALUES (?, ?)`,
		roomID, userID,
	); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *Repository) IsMember(ctx context.Context, roomID, userID int64) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx,
		`SELECT 1 FROM room_members WHERE room_id = ? AND user_id = ? LIMIT 1`,
		roomID, userID,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r *Repository) ListForUser(ctx context.Context, userID int64) ([]Room, error) {
	rows, err := r.db.QueryContext(ctx,
		roomColumns()+`
		FROM rooms r
		 JOIN room_members m ON m.room_id = r.id
		 LEFT JOIN server_nodes n ON n.id = r.node_id
		 WHERE m.user_id = ?
		 ORDER BY r.id DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRooms(rows)
}

func (r *Repository) ListMembers(ctx context.Context, roomID int64) ([]MemberView, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT rm.room_id, u.id, u.username, u.display_name,
		        CASE WHEN r.owner_user_id = u.id THEN ? ELSE ? END AS role,
		        rm.created_at
		   FROM room_members rm
		   JOIN users u ON u.id = rm.user_id
		   JOIN rooms r ON r.id = rm.room_id
		  WHERE rm.room_id = ?
		  ORDER BY CASE WHEN r.owner_user_id = u.id THEN 0 ELSE 1 END, rm.created_at ASC`,
		MemberRoleOwner, MemberRoleMember, roomID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MemberView
	for rows.Next() {
		var m MemberView
		if err := rows.Scan(&m.RoomID, &m.UserID, &m.Username, &m.DisplayName, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repository) List(ctx context.Context, filter Filter) ([]Room, error) {
	where, args := roomWhere(filter)
	limit := normalizeRoomLimit(filter.Limit, 500, 1000)
	offset := normalizeRoomOffset(filter.Offset)
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx,
		roomSelect()+where+` ORDER BY r.id DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRooms(rows)
}

type Stats struct {
	Total  int
	Open   int
	Closed int
}

func (r *Repository) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		   FROM rooms`,
		StatusOpen, StatusClosed,
	).Scan(&stats.Total, &stats.Open, &stats.Closed)
	return stats, err
}

func (r *Repository) ListOwnedByOwner(ctx context.Context, ownerUserID int64) ([]Room, error) {
	rows, err := r.db.QueryContext(ctx,
		roomSelect()+` WHERE r.owner_user_id = ? ORDER BY r.id DESC`,
		ownerUserID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRooms(rows)
}

func (r *Repository) ListOpen(ctx context.Context) ([]Room, error) {
	rows, err := r.db.QueryContext(ctx,
		roomSelect()+` WHERE r.status = ? ORDER BY r.id DESC`,
		StatusOpen,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRooms(rows)
}

func (r *Repository) ListOpenOlderThan(ctx context.Context, grace time.Duration) ([]Room, error) {
	seconds := int64(grace.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	// Compare with MySQL's own clock to avoid Go/MySQL timezone drift on TIMESTAMP columns.
	rows, err := r.db.QueryContext(ctx,
		roomSelect()+` WHERE r.status = ? AND r.updated_at <= DATE_SUB(CURRENT_TIMESTAMP, INTERVAL ? SECOND) ORDER BY r.id ASC`,
		StatusOpen, seconds,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRooms(rows)
}

func (r *Repository) NetworkExists(ctx context.Context, cidr string) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM rooms WHERE network_cidr = ? LIMIT 1`, cidr).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r *Repository) Delete(ctx context.Context, roomID int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM rooms WHERE id = ?`, roomID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func (r *Repository) RemoveMember(ctx context.Context, roomID, userID int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM room_members WHERE room_id = ? AND user_id = ?`, roomID, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func (r *Repository) SetOwner(ctx context.Context, roomID, ownerUserID int64) error {
	res, err := r.db.ExecContext(ctx, `UPDATE rooms SET owner_user_id = ? WHERE id = ?`, ownerUserID, roomID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func (r *Repository) SetStatus(ctx context.Context, roomID int64, status string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE rooms SET status = ? WHERE id = ?`, status, roomID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

type roomScanner interface {
	Scan(dest ...any) error
}

func roomSelect() string {
	return roomColumns() + `
	       FROM rooms r
	       LEFT JOIN server_nodes n ON n.id = r.node_id`
}

func roomColumns() string {
	return `SELECT r.id, r.name, r.owner_user_id, COALESCE(r.node_id, 0),
	       r.room_public_key, r.network_cidr, r.status,
	       r.max_members, (SELECT COUNT(*) FROM room_members rm WHERE rm.room_id = r.id),
	       COALESCE(n.name, ''), COALESCE(n.public_overlay_addr, ''), COALESCE(n.public_udp_addr, ''), COALESCE(n.overlay_public_key, ''),
	       r.created_at, r.updated_at`
}

func scanRoom(scanner roomScanner) (Room, error) {
	var r Room
	err := scanner.Scan(
		&r.ID, &r.Name, &r.OwnerUserID, &r.NodeID,
		&r.RoomPublicKey, &r.NetworkCIDR, &r.Status,
		&r.MaxMembers, &r.MemberCount,
		&r.NodeName, &r.NodeOverlayAddr, &r.NodeUDPAddr, &r.NodePublicKey,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Room{}, ErrNotFound
	}
	return r, err
}

func scanRooms(rows *sql.Rows) ([]Room, error) {
	var out []Room
	for rows.Next() {
		r, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func roomWhere(filter Filter) (string, []any) {
	var parts []string
	var args []any
	query := strings.TrimSpace(filter.Query)
	if query != "" {
		like := "%" + query + "%"
		if id, err := strconv.ParseInt(query, 10, 64); err == nil {
			parts = append(parts, `(r.id = ? OR r.owner_user_id = ? OR r.name LIKE ? OR r.room_public_key LIKE ? OR r.network_cidr LIKE ? OR n.name LIKE ?)`)
			args = append(args, id, id, like, like, like, like)
		} else {
			parts = append(parts, `(r.name LIKE ? OR r.room_public_key LIKE ? OR r.network_cidr LIKE ? OR n.name LIKE ?)`)
			args = append(args, like, like, like, like)
		}
	}
	switch strings.ToLower(strings.TrimSpace(filter.Status)) {
	case StatusOpen:
		parts = append(parts, `r.status = ?`)
		args = append(args, StatusOpen)
	case StatusClosed:
		parts = append(parts, `r.status = ?`)
		args = append(args, StatusClosed)
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func normalizeRoomLimit(v, fallback, max int) int {
	if v <= 0 {
		return fallback
	}
	return min(v, max)
}

func normalizeRoomOffset(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
