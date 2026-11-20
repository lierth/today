package datahome

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// makeDBWithWAL writes a small database and keeps a second connection open with
// wal_autocheckpoint=0, so the committed row really lives in the -wal file when
// the migration runs. The caller closes the returned connection afterwards.
//
// A test that merely closes its own connection proves nothing: modernc's driver
// checkpoints on the last close, and the -wal file is then empty or gone.
func makeDBWithWAL(t *testing.T, path string) *sql.DB {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	setup, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		setup.Close()
		t.Fatal(err)
	}
	if _, err := setup.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, text TEXT)`); err != nil {
		setup.Close()
		t.Fatal(err)
	}
	if err := setup.Close(); err != nil {
		t.Fatal(err)
	}

	// The long-lived connection: no autocheckpoint, one committed row.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO notes (text) VALUES ('hello')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

// mustMakeDB is makeDBWithWAL for callers that do not need the connection; it
// closes it, which checkpoints — fine for the cases that only need a database
// to exist.
func mustMakeDB(t *testing.T, path string) {
	t.Helper()
	db := makeDBWithWAL(t, path)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func readNote(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var text string
	if err := db.QueryRow(`SELECT text FROM notes LIMIT 1`).Scan(&text); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return text
}

// Only an old database: it is copied, config.json comes along, the note file
// appears, and the old database is still there.
func TestMigrateOnlyOld(t *testing.T) {
	root := t.TempDir()
	legacy := t.TempDir()
	mustMakeDB(t, DBPath(legacy))
	if err := os.WriteFile(ConfigPath(legacy), []byte(`{"listen_addr":":9911"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	res := Migrator{Root: root, Legacy: legacy}.Run()
	if !res.Migrated || res.Failed || res.Conflict {
		t.Fatalf("result: %+v", res)
	}
	if got := readNote(t, DBPath(root)); got != "hello" {
		t.Fatalf("migrated row = %q", got)
	}
	if b, err := os.ReadFile(ConfigPath(root)); err != nil || string(b) != `{"listen_addr":":9911"}` {
		t.Fatalf("config: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(legacy, "MOVED_TO_~.today.txt")); err != nil {
		t.Fatal("no MOVED_TO note in the old directory")
	}
	if _, err := os.Stat(DBPath(legacy)); err != nil {
		t.Fatal("old database was removed")
	}
	// File modes: the new root and the copy are private.
	if st, _ := os.Stat(root); st.Mode().Perm() != 0o700 {
		t.Fatalf("root mode %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(DBPath(root)); st.Mode().Perm() != 0o600 {
		t.Fatalf("db mode %v", st.Mode().Perm())
	}
}

// Both sides have a database: the new one wins, nothing is touched, and the
// result says so.
func TestMigrateConflict(t *testing.T) {
	root, legacy := t.TempDir(), t.TempDir()
	mustMakeDB(t, DBPath(root))
	mustMakeDB(t, DBPath(legacy))
	res := Migrator{Root: root, Legacy: legacy}.Run()
	if !res.Conflict || res.Migrated || res.Failed {
		t.Fatalf("result: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(legacy, "MOVED_TO_~.today.txt")); err == nil {
		t.Fatal("conflict wrote the moved note")
	}
}

// A migration failure keeps the old path in use and leaves no partial file.
func TestMigrateFailureFallsBack(t *testing.T) {
	root, legacy := t.TempDir(), t.TempDir()
	mustMakeDB(t, DBPath(legacy))
	res := Migrator{
		Root: root, Legacy: legacy,
		VacuumInto: func(string, string) error { return os.ErrPermission },
	}.Run()
	if !res.Failed || res.UsedLegacyDB != DBPath(legacy) {
		t.Fatalf("result: %+v", res)
	}
	if _, err := os.Stat(DBPath(root)); !os.IsNotExist(err) {
		t.Fatal("a partial database was left in the new root")
	}
}

// Nothing anywhere: no migration, no error.
func TestMigrateNothing(t *testing.T) {
	res := Migrator{Root: t.TempDir(), Legacy: t.TempDir()}.Run()
	if res.Migrated || res.Failed || res.Conflict || res.Reason != "" {
		t.Fatalf("result: %+v", res)
	}
}

// Only the new database: nothing to do and the old root is not consulted.
func TestMigrateOnlyNew(t *testing.T) {
	root, legacy := t.TempDir(), t.TempDir()
	mustMakeDB(t, DBPath(root))
	res := Migrator{Root: root, Legacy: legacy}.Run()
	if res.Migrated || res.Failed || res.Conflict {
		t.Fatalf("result: %+v", res)
	}
}

// The layout is the one the plan documents, and Ensure creates it with the
// right modes.
func TestPathsAndEnsure(t *testing.T) {
	root := t.TempDir()
	p := Resolve(root, "")
	if p.DB != filepath.Join(root, "today.db") ||
		p.Backups != filepath.Join(root, "backups") ||
		p.Logs != filepath.Join(root, "logs") ||
		p.Firmware != filepath.Join(root, "firmware") {
		t.Fatalf("layout: %+v", p)
	}
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.Backups, p.Logs, p.Firmware} {
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			t.Fatalf("%s missing", dir)
		}
		if st.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode %v", dir, st.Mode().Perm())
		}
	}
	// --db moves the whole layout to the database's directory: its backups
	// belong beside it, not in the default root.
	other := filepath.Join(root, "elsewhere.db")
	if p2 := Resolve(root, other); p2.DB != other || p2.Root != root ||
		p2.Backups != p.Backups {
		t.Fatalf("override in the same directory: %+v", p2)
	}
	sub := filepath.Join(root, "profiles", "test")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	moved := Resolve(root, filepath.Join(sub, "today.db"))
	if moved.Root != sub || moved.Backups != filepath.Join(sub, "backups") ||
		moved.Logs != filepath.Join(sub, "logs") {
		t.Fatalf("override elsewhere: %+v", moved)
	}
	// The default root itself is never touched by such a run.
	if moved.Backups == p.Backups {
		t.Fatalf("override still wrote into the default root: %+v", moved)
	}
}

// TODAY_HOME moves the root; an unset variable falls back to ~/.today.
func TestRootEnv(t *testing.T) {
	t.Setenv(EnvOverride, "/tmp/somewhere")
	if got := Root(); got != "/tmp/somewhere" {
		t.Fatalf("Root = %q", got)
	}
	t.Setenv(EnvOverride, "")
	home, _ := os.UserHomeDir()
	if got := Root(); got != filepath.Join(home, ".today") {
		t.Fatalf("Root = %q", got)
	}
}

// The migration must carry a row that is still only in the -wal file. The
// source connection stays open and autocheckpoint is off, and the test asserts
// the -wal file exists before the copy.
func TestMigrateCarriesUncheckpointedWAL(t *testing.T) {
	root := t.TempDir()
	legacy := t.TempDir()
	db := makeDBWithWAL(t, DBPath(legacy))
	defer db.Close()

	// Second row, still uncheckpointed.
	if _, err := db.Exec(`INSERT INTO notes (text) VALUES ('from the wal')`); err != nil {
		t.Fatal(err)
	}
	wal := DBPath(legacy) + "-wal"
	st, err := os.Stat(wal)
	if err != nil || st.Size() == 0 {
		t.Fatalf("the test setup left no WAL to migrate: %v", err)
	}

	res := Migrator{Root: root, Legacy: legacy}.Run()
	if !res.Migrated {
		t.Fatalf("result: %+v", res)
	}
	out, err := sql.Open("sqlite", DBPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	rows, err := out.Query(`SELECT text FROM notes ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	if len(got) != 2 || got[1] != "from the wal" {
		t.Fatalf("migrated rows: %v", got)
	}
}
