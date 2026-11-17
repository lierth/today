package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Device is a panel that polls frames.
type Device struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenHash   string     `json:"-"`
	PanelW      int        `json:"panel_w"`
	PanelH      int        `json:"panel_h"`
	RowH        int        `json:"row_h"`
	FontSize    int        `json:"font_size"`
	ButtonsJSON string     `json:"buttons_json,omitempty"`
	TZ          string     `json:"tz,omitempty"`
	BatteryMV   int        `json:"battery_mv,omitempty"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Panel geometry falls back to the only panel that exists today.
func (d Device) Panel() (w, h, rowH, font int) {
	w, h, rowH, font = d.PanelW, d.PanelH, d.RowH, d.FontSize
	if w == 0 {
		w = 400
	}
	if h == 0 {
		h = 300
	}
	if rowH == 0 {
		rowH = 24
	}
	if font == 0 {
		font = 18
	}
	return
}

const deviceColumns = `id, name, token_hash, panel_w, panel_h, row_h, font_size,
	buttons_json, tz, battery_mv, last_seen_at, revoked_at, created_at`

func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var (
		d          Device
		last, rev  sql.NullString
		createdAt  string
	)
	if err := row.Scan(&d.ID, &d.Name, &d.TokenHash, &d.PanelW, &d.PanelH, &d.RowH,
		&d.FontSize, &d.ButtonsJSON, &d.TZ, &d.BatteryMV, &last, &rev, &createdAt); err != nil {
		return Device{}, err
	}
	d.LastSeenAt = scanTime(last)
	d.RevokedAt = scanTime(rev)
	if t := parseTime(createdAt); t != nil {
		d.CreatedAt = *t
	}
	return d, nil
}

// Devices lists all devices, newest first.
func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deviceColumns+` FROM devices ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: devices: %w", err)
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan device: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: devices: %w", err)
	}
	return out, nil
}

// Device loads one device by id even when revoked (auth needs to say 404).
func (s *Store) Device(ctx context.Context, id string) (Device, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+deviceColumns+` FROM devices WHERE id = ?`, id)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("store: device: %w", err)
	}
	return d, nil
}

// CreateDevice inserts a device row with a token hash (possibly empty).
func (s *Store) CreateDevice(ctx context.Context, id, name, tokenHash string) (Device, error) {
	now := time.Now()
	d := Device{ID: id, Name: name, TokenHash: tokenHash, CreatedAt: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices (`+deviceColumns+`)
		VALUES (?,?,?,0,0,0,0,'','',0,NULL,NULL,?)`,
		d.ID, d.Name, d.TokenHash, timeStr(now))
	if err != nil {
		return Device{}, fmt.Errorf("store: create device: %w", err)
	}
	return d, nil
}

// EnsureDevice creates the row only when it is missing.
func (s *Store) EnsureDevice(ctx context.Context, id, name string) (Device, error) {
	d, err := s.Device(ctx, id)
	if err == nil {
		return d, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Device{}, err
	}
	return s.CreateDevice(ctx, id, name, "")
}

// SetDeviceToken replaces the token hash (rotation invalidates the old one).
func (s *Store) SetDeviceToken(ctx context.Context, id, hash string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET token_hash=?, revoked_at=NULL WHERE id = ?`, hash, id)
	if err != nil {
		return fmt.Errorf("store: set device token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchDevice records a successful frame fetch.
func (s *Store) TouchDevice(ctx context.Context, id string, batteryMV int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET last_seen_at=?, battery_mv=CASE WHEN ? > 0 THEN ? ELSE battery_mv END
		WHERE id = ?`, timeStr(time.Now()), batteryMV, batteryMV, id)
	if err != nil {
		return fmt.Errorf("store: touch device: %w", err)
	}
	return nil
}

// RevokeDevice invalidates a device immediately.
func (s *Store) RevokeDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET revoked_at=? WHERE id = ? AND revoked_at IS NULL`,
		timeStr(time.Now()), id)
	if err != nil {
		return fmt.Errorf("store: revoke device: %w", err)
	}
	return nil
}
