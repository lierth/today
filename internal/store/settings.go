package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// KnownSettings is the single list of keys the app understands. Add a key
// here before reading or writing it anywhere else.
var KnownSettings = []string{
	"open_at_login",
	"tray_title",
	"timezone",
	"funnel",
	"funnel_host",
	"auth_password_hash",
	"auth_session_key",
	"device_token_hint",
	"firmware_version",
	"update_feed",
	"night_off",
	"night_start",
	"night_end",
	"power_state",
	"funnel_state",
	"funnel_action",
	"funnel_detail",
	"funnel_request",
}

// DefaultSettings returns the values used when a key is absent.
func DefaultSettings() map[string]string {
	return map[string]string{
		"open_at_login":      "false",
		"tray_title":         "true",
		"timezone":           time.Local.String(),
		"funnel":             "false",
		"funnel_host":        "",
		"auth_password_hash": "",
		"auth_session_key":   "",
		"device_token_hint":  "",
		"night_off":          "true",
		"night_start":        "23:00",
		"night_end":          "07:00",
		"power_state":        "",
		"funnel_state":       "",
		"funnel_action":      "",
		"funnel_detail":      "",
		"funnel_request":     "",
	}
}

func knownSetting(key string) bool {
	for _, k := range KnownSettings {
		if k == key {
			return true
		}
	}
	return false
}

// Settings returns defaults overlaid with stored values.
func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	out := DefaultSettings()
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("store: settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("store: settings scan: %w", err)
		}
		if knownSetting(k) {
			out[k] = v
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: settings: %w", err)
	}
	return out, nil
}

// Setting reads one value, falling back to the default.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultSettings()[key], nil
	}
	if err != nil {
		return "", fmt.Errorf("store: setting %s: %w", key, err)
	}
	return v, nil
}

// PutSettings upserts the given keys atomically.
func (s *Store) PutSettings(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: settings tx: %w", err)
	}
	defer tx.Rollback()
	now := timeStr(time.Now())
	for k, v := range values {
		if !knownSetting(k) {
			return fmt.Errorf("store: unknown setting %q", k)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at)
			VALUES (?,?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
			k, v, now); err != nil {
			return fmt.Errorf("store: put setting %s: %w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: settings commit: %w", err)
	}
	return nil
}

// BoolSetting is a small parsing helper for "true"/"1".
func BoolSetting(m map[string]string, key string) bool {
	v := m[key]
	return v == "true" || v == "1" || v == "on"
}
