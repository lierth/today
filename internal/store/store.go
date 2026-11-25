// Package store is the single source of truth for Today. It wraps SQLite
// (pure-Go modernc driver, WAL) and contains no HTTP or rendering logic.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist or is soft-deleted.
var ErrNotFound = errors.New("store: not found")

// Store owns the database handle. A single connection serializes writes,
// which is all one Mac mini and one user ever need.
type Store struct {
	db   *sql.DB
	path string
	// now is the clock every derived date and timestamp reads. It exists so a
	// test can pin "today": a literal date in a fixture plus a live clock is a
	// time bomb (the row stops being "today" the day after it was written).
	now func() time.Time
}

// Now is the store clock; it never returns nil.
func (s *Store) Now() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// SetNow fixes the store clock. Tests use it; production leaves the default.
func (s *Store) SetNow(fn func() time.Time) {
	if fn == nil {
		fn = time.Now
	}
	s.now = fn
}

// Open creates the database file and schema if needed.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store: create dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx := context.Background()
	// busy_timeout comes first: journal_mode needs an exclusive moment, and
	// without a wait window a second opener fails at once with "database is
	// locked" instead of waiting for the first connection.
	for _, pragma := range []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", pragma, err)
		}
	}
	s := &Store{db: db, path: path, now: time.Now}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Path is the database file location.
func (s *Store) Path() string { return s.path }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS items (
			id          TEXT PRIMARY KEY,
			title       TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			all_day     INTEGER NOT NULL DEFAULT 0,
			starts_at   TEXT,
			due_at      TEXT,
			date_local  TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'open',
			pinned      INTEGER NOT NULL DEFAULT 0,
			rrule       TEXT NOT NULL DEFAULT '',
			series_id   TEXT,
			sort_key    INTEGER NOT NULL DEFAULT 0,
			carried_from TEXT NOT NULL DEFAULT '',
			manual_rank INTEGER NOT NULL DEFAULT 0,
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL,
			deleted_at  TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_items_date ON items(date_local, status) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_items_series ON items(series_id, date_local) WHERE series_id IS NOT NULL AND deleted_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS devices (
			id           TEXT PRIMARY KEY,
			name         TEXT NOT NULL DEFAULT '',
			token_hash   TEXT NOT NULL DEFAULT '',
			panel_w      INTEGER NOT NULL DEFAULT 0,
			panel_h      INTEGER NOT NULL DEFAULT 0,
			row_h        INTEGER NOT NULL DEFAULT 0,
			font_size    INTEGER NOT NULL DEFAULT 0,
			buttons_json TEXT NOT NULL DEFAULT '',
			tz           TEXT NOT NULL DEFAULT '',
			battery_mv   INTEGER NOT NULL DEFAULT 0,
			temp_c       REAL NOT NULL DEFAULT 0,
			humidity     REAL NOT NULL DEFAULT 0,
			last_seen_at TEXT,
			revoked_at   TEXT,
			created_at   TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ops (
			op_id      TEXT PRIMARY KEY,
			device_id  TEXT NOT NULL,
			item_id    TEXT NOT NULL DEFAULT '',
			status     TEXT NOT NULL DEFAULT '',
			applied_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ops_applied ON ops(applied_at)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS auth_fail (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ip TEXT NOT NULL,
			at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_fail_ip ON auth_fail(ip, at)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("store: schema version: %w", err)
	}
	if version < 1 {
		// A deleted recurring occurrence must stay deleted: the unique index
		// has to cover tombstones too, otherwise Materialize recreates it.
		for _, stmt := range []string{
			`DROP INDEX IF EXISTS idx_items_series_date`,
			`DELETE FROM items WHERE series_id IS NOT NULL AND deleted_at IS NOT NULL
				AND EXISTS (SELECT 1 FROM items live
					WHERE live.series_id = items.series_id AND live.date_local = items.date_local
					AND live.deleted_at IS NULL AND live.rowid <> items.rowid)`,
			`DELETE FROM items WHERE series_id IS NOT NULL AND deleted_at IS NOT NULL
				AND rowid NOT IN (SELECT MIN(rowid) FROM items
					WHERE series_id IS NOT NULL AND deleted_at IS NOT NULL
					GROUP BY series_id, date_local)`,
			`CREATE UNIQUE INDEX idx_items_series_date ON items(series_id, date_local)
				WHERE series_id IS NOT NULL`,
			`PRAGMA user_version = 1`,
		} {
			if _, err := s.db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("store: migrate series index: %w", err)
			}
		}
	}
	if version < 2 {
		// Device climate arrives with the acks payload. ALTER TABLE cannot say
		// IF NOT EXISTS, so the duplicate-column error is expected on a fresh
		// database that already has them from the CREATE above.
		for _, stmt := range []string{
			`ALTER TABLE devices ADD COLUMN temp_c REAL NOT NULL DEFAULT 0`,
			`ALTER TABLE devices ADD COLUMN humidity REAL NOT NULL DEFAULT 0`,
			`PRAGMA user_version = 2`,
		} {
			if _, err := s.db.ExecContext(ctx, stmt); err != nil &&
				!strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("store: migrate device climate: %w", err)
			}
		}
	}
	if version < 3 {
		// A carried item remembers the day it slipped from, so the page can say
		// "from 11月12日" and the device can draw the ↻ mark.
		for _, stmt := range []string{
			`ALTER TABLE items ADD COLUMN carried_from TEXT NOT NULL DEFAULT ''`,
			`PRAGMA user_version = 3`,
		} {
			if _, err := s.db.ExecContext(ctx, stmt); err != nil &&
				!strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("store: migrate carried_from: %w", err)
			}
		}
	}
	if version < 4 {
		// A manual drag order is a separate axis from the clock: sort_key is
		// the start time, manual_rank is the position a person chose. 0 means
		// "never dragged" and falls back to the time ordering.
		for _, stmt := range []string{
			`ALTER TABLE items ADD COLUMN manual_rank INTEGER NOT NULL DEFAULT 0`,
			`PRAGMA user_version = 4`,
		} {
			if _, err := s.db.ExecContext(ctx, stmt); err != nil &&
				!strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("store: migrate manual_rank: %w", err)
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS categories (id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE UNIQUE); CREATE TABLE IF NOT EXISTS series_exclusions (series_id TEXT NOT NULL, date_local TEXT NOT NULL, PRIMARY KEY(series_id,date_local))`); err != nil {
		return err
	}
	if version < 5 {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE items ADD COLUMN category_id TEXT REFERENCES categories(id) ON DELETE SET NULL`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("store: migrate category: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, `PRAGMA user_version = 5`); err != nil {
			return err
		}
	}

	return nil
}

// timeLayout keeps sub-second precision and the original offset.
const timeLayout = time.RFC3339Nano

func timeStr(t time.Time) string { return t.Format(timeLayout) }

func nullTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return timeStr(*t)
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return nil
	}
	return &t
}

func scanTime(s sql.NullString) *time.Time {
	if !s.Valid {
		return nil
	}
	return parseTime(s.String)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
