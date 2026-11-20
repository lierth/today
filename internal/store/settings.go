package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// KnownSettings is the single list of keys the app understands. Add a key
// here before reading or writing it anywhere else.
var KnownSettings = []string{
	"open_at_login",
	"tray_title",
	"tray_mode",
	"timezone",
	"funnel",
	"funnel_host",
	"auth_password_hash",
	"auth_session_key",
	"device_token_hint",
	"firmware_version",
	"firmware_pending_version",
	"update_feed",
	"night_off",
	"night_start",
	"night_end",
	"power_state",
	"funnel_state",
	"funnel_action",
	"funnel_detail",
	"funnel_request",
	"localnet_state",
	"localnet_detail",
	"localnet_adhoc",
	"localnet_request",
	"weather_api_key",
	"weather_city",
	"weather_ip_fallback",
	"sound_volume",
	"sound_keys",
	"sound_alarm",
	"alarm_lead_minutes",
	"auto_carry",
	"proxy_mode",
	"proxy_url",
	"update_auto_check",
	"update_auto_download",
	"update_skip_version",
	"backup_icloud",
	"theme",
	"color_blind",
}

// DefaultSettings returns the values used when a key is absent.
func DefaultSettings() map[string]string {
	return map[string]string{
		"open_at_login":            "false",
		"tray_title":               "true",
		"tray_mode":                "progress",
		"timezone":                 time.Local.String(),
		"funnel":                   "false",
		"funnel_host":              "",
		"auth_password_hash":       "",
		"auth_session_key":         "",
		"device_token_hint":        "",
		"firmware_pending_version": "",
		"night_off":                "true",
		"night_start":              "23:00",
		"night_end":                "07:00",
		"power_state":              "",
		"funnel_state":             "",
		"funnel_action":            "",
		"funnel_detail":            "",
		"funnel_request":           "",
		"localnet_state":           "",
		"localnet_detail":          "",
		"localnet_adhoc":           "false",
		"localnet_request":         "",
		"weather_api_key":          "",
		"weather_city":             "",
		"weather_ip_fallback":      "true",
		"sound_volume":             "60",
		"sound_keys":               "true",
		"sound_alarm":              "true",
		"alarm_lead_minutes":       "0",
		"auto_carry":               "false",
		"proxy_mode":               "on",
		"proxy_url":                "",
		"update_auto_check":        "true",
		"update_auto_download":     "false",
		"update_skip_version":      "",
		"backup_icloud":            "true",
		"theme":                    "system",
		"color_blind":              "false",
	}
}

// deviceCityPrefix scopes a city to one device: weather_city_dev1. The suffix
// is a device id, so the list of known keys cannot name them in advance.
const deviceCityPrefix = "weather_city_"

// IsKnownSetting reports whether the app understands this key. The JSON API
// refuses unknown names so a typo in a front end does not pile up dead rows.
func IsKnownSetting(key string) bool { return knownSetting(key) }

func knownSetting(key string) bool {
	if strings.HasPrefix(key, deviceCityPrefix) && len(key) > len(deviceCityPrefix) {
		return true
	}
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
	now := timeStr(s.Now())
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
