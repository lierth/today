// Package datahome owns where Today keeps its data and how an old installation
// moves to the new place.
//
// The rule is one sentence: the database lives in ~/.today (0700), never in
// Application Support and never inside the .app, so uninstalling the app with
// AppCleaner and friends cannot take the data with it.
package datahome

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// EnvOverride is the environment variable that moves the root (tests, a second
// profile, a user with a different layout).
const EnvOverride = "TODAY_HOME"

// Root is the data directory: ~/.today.
func Root() string {
	if v := strings.TrimSpace(os.Getenv(EnvOverride)); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".today"
	}
	return filepath.Join(home, ".today")
}

// LegacyRoot is where the app used to keep things.
func LegacyRoot() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "Today")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Today")
}

// DBPath is <root>/today.db.
func DBPath(root string) string { return filepath.Join(root, "today.db") }

// ConfigPath is <root>/config.json.
func ConfigPath(root string) string { return filepath.Join(root, "config.json") }

// BackupsDir is <root>/backups.
func BackupsDir(root string) string { return filepath.Join(root, "backups") }

// LogsDir is <root>/logs.
func LogsDir(root string) string { return filepath.Join(root, "logs") }

// FirmwareDir is <root>/firmware. The server's OTA slot lives here instead of a
// single firmware.bin next to the database, so the directory layout above is
// real rather than aspirational; the file name and the X-Firmware-Version
// mechanism are unchanged.
func FirmwareDir(root string) string { return filepath.Join(root, "firmware") }

// Paths is the resolved layout for one process.
type Paths struct {
	Root     string
	DB       string
	Config   string
	Backups  string
	Logs     string
	Firmware string
}

// Resolve builds the layout. `dbOverride` is the --db flag: when it is set,
// the database's own directory becomes the root.
//
// The root follows the database on purpose. Everything the root holds
// (backups, firmware, logs) describes that database; keeping ~/.today while
// reading another file made a `--db /tmp/test.db` run write its snapshots into
// the person's real backup folder. A person who wants a database in another
// place is asking for a separate set of backups beside it.
func Resolve(root, dbOverride string) Paths {
	if dbOverride != "" {
		root = filepath.Dir(dbOverride)
		if root == "" || root == "." {
			root = "."
		}
	} else if root == "" {
		root = Root()
	}
	p := Paths{
		Root:     root,
		DB:       DBPath(root),
		Config:   ConfigPath(root),
		Backups:  BackupsDir(root),
		Logs:     LogsDir(root),
		Firmware: FirmwareDir(root),
	}
	if dbOverride != "" {
		p.DB = dbOverride
	}
	return p
}

// Ensure makes the directories with the permissions the plan asks for: the
// root is 0700, everything under it 0700 as well (they hold the database and
// its backups).
func (p Paths) Ensure() error {
	for _, dir := range []string{p.Root, p.Backups, p.Logs, p.Firmware} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// MigrateResult says what happened, for the log, the notification and the
// settings status line.
type MigrateResult struct {
	Migrated     bool   // data was copied from the old root
	Reason       string // a sentence for the person
	Conflict     bool   // both roots had data: the old one is untouched
	Failed       bool   // the migration did not complete; the old root is in use
	UsedLegacyDB string // non-empty when the caller should keep using this DB
}

// Migrator is the migration with its dependencies as fields, so a test can
// drive every branch without a real home directory.
type Migrator struct {
	Root   string
	Legacy string
	// VacuumInto copies a SQLite database consistently; the default runs it
	// against `from` with `to` as the target.
	VacuumInto func(from, to string) error
	// Logf receives one line per step. Nil discards.
	Logf func(format string, args ...any)
}

func (m Migrator) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}

func (m Migrator) paths() (newDB, oldDB, newCfg, oldCfg string) {
	return DBPath(m.Root), DBPath(m.Legacy), ConfigPath(m.Root), ConfigPath(m.Legacy)
}

func exists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// Run is the whole one-shot migration.
func (m Migrator) Run() MigrateResult {
	newDB, oldDB, newCfg, oldCfg := m.paths()
	haveNew, haveOld := exists(newDB), exists(oldDB)
	switch {
	case haveNew && haveOld:
		m.logf("datahome: both %s and %s have a database; keeping the new one and leaving the old alone", newDB, oldDB)
		return MigrateResult{
			Reason:   "检测到旧目录里还有数据；当前使用 ~/.today，旧数据未动。",
			Conflict: true,
		}
	case haveNew:
		return MigrateResult{Reason: ""}
	case !haveOld:
		return MigrateResult{Reason: ""}
	}

	m.logf("datahome: migrating %s -> %s", oldDB, newDB)
	if err := os.MkdirAll(m.Root, 0o700); err != nil {
		return m.failed(err)
	}
	// MkdirAll leaves an existing directory's mode alone; a temp dir from the
	// test (or a root made by hand) must become private too.
	_ = os.Chmod(m.Root, 0o700)
	// The copy must be consistent: VACUUM INTO reads through the WAL, a file
	// copy would drop anything not yet checkpointed.
	vacuum := m.VacuumInto
	if vacuum == nil {
		vacuum = VacuumInto
	}
	// The copy goes to a temporary name first. VACUUM INTO writes the final
	// path directly, so a power cut mid-copy would leave a half database that
	// the next start reads as "have new" and uses.
	tmpDB := newDB + ".tmp"
	_ = os.Remove(tmpDB)
	if err := vacuum(oldDB, tmpDB); err != nil {
		_ = os.Remove(tmpDB)
		return m.failed(err)
	}
	if err := fsyncFile(tmpDB); err != nil {
		_ = os.Remove(tmpDB)
		return m.failed(err)
	}
	if err := os.Chmod(tmpDB, 0o600); err != nil {
		_ = os.Remove(tmpDB)
		return m.failed(err)
	}
	if err := os.Rename(tmpDB, newDB); err != nil {
		_ = os.Remove(tmpDB)
		return m.failed(err)
	}
	// Make the rename itself durable before the note claims success.
	if err := fsyncDir(m.Root); err != nil {
		m.logf("datahome: fsync %s: %v", m.Root, err)
	}
	if exists(oldCfg) {
		if err := copyFile(oldCfg, newCfg, 0o600); err != nil {
			// config.json only carries the port; a missing one falls back to
			// the default, so this is a warning and not a failure.
			m.logf("datahome: config copy failed: %v", err)
		}
	}
	// Tell the next person who looks at the old directory where the data went.
	note := filepath.Join(m.Legacy, "MOVED_TO_~.today.txt")
	_ = os.WriteFile(note, []byte(`Today's data now lives in ~/.today.

The database was copied here on first launch after the move; this directory is
kept for one release as a fallback and is no longer read.

To go back: quit Today, move ~/.today/today.db aside and restore the old copy
from this directory.
`), 0o644)
	return MigrateResult{
		Migrated: true,
		Reason:   "数据已从旧目录迁到 ~/.today（旧目录保留一份作为回退）。",
	}
}

func (m Migrator) failed(err error) MigrateResult {
	m.logf("datahome: migration failed: %v; continuing on the old path", err)
	return MigrateResult{
		Failed:       true,
		Reason:       "迁移失败，继续使用旧目录：" + err.Error(),
		UsedLegacyDB: DBPath(m.Legacy),
	}
}

// VacuumInto writes a consistent copy of the database at `from` to `to`.
func VacuumInto(from, to string) error {
	if _, err := os.Stat(to); err == nil {
		return fmt.Errorf("目标已存在：%s", to)
	}
	db, err := sql.Open("sqlite", from)
	if err != nil {
		return err
	}
	defer db.Close()
	// The statement takes no placeholders in some builds; quoting the path is
	// the portable way (the path never comes from the network).
	quoted := "'" + strings.ReplaceAll(to, "'", "''") + "'"
	if _, err := db.Exec("VACUUM INTO " + quoted); err != nil {
		return err
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// ErrNoDatabase is returned by Open when there is nothing to open.
var ErrNoDatabase = errors.New("datahome: no database")

// fsyncFile flushes a file's contents to disk.
func fsyncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// fsyncDir flushes a directory entry so a rename survives a power cut.
func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
