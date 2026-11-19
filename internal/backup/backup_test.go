package backup

import (
	"compress/gzip"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// liveDB builds the shape the real database has where it matters here: a
// settings table with the secret and the harmless keys.
func liveDB(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT)`,
		`INSERT INTO settings (key, value) VALUES
			('auth_session_key', 'super-secret-hmac'),
			('weather_api_key', 'wkey-123456789'),
			('auth_password_hash', '$argon2id$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA'),
			('tray_mode', 'progress')`,
		`INSERT INTO items (id, title) VALUES ('i1', '写周报')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func settings(t *testing.T, path string) map[string]string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		out[k] = v
	}
	return out
}

// The snapshot has the data, not the secrets, and the live database keeps its
// own copy of both.
func TestCreateScrubsSecrets(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "today.db")
	liveDB(t, live)
	now := time.Date(2026, 11, 19, 0, 5, 0, 0, time.Local)

	path, err := Create(live, filepath.Join(dir, "backups"), "today", now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "today-20261119.db" {
		t.Fatalf("name %q", filepath.Base(path))
	}
	copy := settings(t, path)
	if copy["auth_session_key"] != "" || copy["weather_api_key"] != "" {
		t.Fatalf("secrets survived: %v", copy)
	}
	if copy["auth_password_hash"] == "" || copy["tray_mode"] != "progress" {
		t.Fatalf("harmless keys lost: %v", copy)
	}
	// Items came along.
	db, _ := sql.Open("sqlite", path)
	defer db.Close()
	var title string
	if err := db.QueryRow(`SELECT title FROM items WHERE id='i1'`).Scan(&title); err != nil || title != "写周报" {
		t.Fatalf("row: %q %v", title, err)
	}
	// The live database is untouched.
	if got := settings(t, live); got["auth_session_key"] != "super-secret-hmac" {
		t.Fatalf("live db was scrubbed: %v", got)
	}
	// The snapshot is private.
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode %v", st.Mode().Perm())
	}
}

// Pruning keeps the newest `Keep`-style count and leaves other prefixes be.
func TestPrune(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 20; i++ {
		name := FileName("today", time.Date(2026, 11, i, 0, 5, 0, 0, time.Local))
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// More safety copies than the quota: they must not eat the daily slots.
	var keepFiles []string
	for i := 1; i <= 9; i++ {
		name := "today-before-restore-202611" + string(rune('0'+i)) + "-101500.db"
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		keepFiles = append(keepFiles, full)
	}
	if err := Prune(dir, 14, "today"); err != nil {
		t.Fatal(err)
	}
	snaps, err := List(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	daily, safety := 0, 0
	for _, s := range snaps {
		if DailySnapshotRe.MatchString(s.Name) {
			daily++
		} else if strings.HasPrefix(s.Name, "today-before-restore-") {
			safety++
		}
	}
	if daily != 14 {
		t.Fatalf("daily snapshots kept: %d, want 14", daily)
	}
	if safety != BeforeRestoreKeep {
		t.Fatalf("safety copies kept: %d, want %d", safety, BeforeRestoreKeep)
	}
	// Newest kept, oldest gone (the 20 files were 11-01 .. 11-20).
	if _, err := os.Stat(filepath.Join(dir, FileName("today", time.Date(2026, 11, 1, 0, 5, 0, 0, time.Local)))); !os.IsNotExist(err) {
		t.Fatal("oldest snapshot kept")
	}
	if _, err := os.Stat(filepath.Join(dir, FileName("today", time.Date(2026, 11, 20, 0, 5, 0, 0, time.Local)))); err != nil {
		t.Fatal("newest snapshot missing")
	}
	// The newest safety copies survive; the oldest are gone.
	if _, err := os.Stat(keepFiles[len(keepFiles)-1]); err != nil {
		t.Fatal("newest safety copy pruned")
	}
	if _, err := os.Stat(keepFiles[0]); !os.IsNotExist(err) {
		t.Fatal("oldest safety copy kept")
	}
}

// A fresh backup is never pruned by safety copies: the exact bug the earlier
// prefix rule had.
func TestPruneKeepsTheFreshBackup(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= BeforeRestoreKeep+3; i++ {
		name := "today-before-restore-202611" + string(rune('0'+i)) + "-101500.db"
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	live := filepath.Join(t.TempDir(), "today.db")
	liveDB(t, live)
	fresh, err := Create(live, dir, "today", time.Date(2026, 11, 19, 0, 5, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh backup was pruned: %v", err)
	}
}

// The iCloud copy is a gzip whose content is the snapshot, written atomically,
// and it keeps its own 14.
func TestCopyCloud(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "today.db")
	liveDB(t, live)
	local, err := Create(live, filepath.Join(dir, "backups"), "today", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cloud := filepath.Join(dir, "cloud", "Today", "backups")
	dest, err := CopyCloud(local, cloud, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dest, ".db.gz") {
		t.Fatalf("cloud name %q", dest)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "roundtrip.db")
	w, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(w, gz); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if got := settings(t, out); got["auth_session_key"] != "" {
		t.Fatalf("cloud copy carries secrets: %v", got)
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
}

// CloudAvailable follows the directory, and an empty path is never available.
func TestCloudAvailable(t *testing.T) {
	dir := t.TempDir()
	if CloudAvailable("") {
		t.Fatal("empty path reported available")
	}
	// "dir" itself exists (t.TempDir), so use a child that does not.
	if CloudAvailable(filepath.Join(dir, "nope")) {
		t.Fatal("missing directory reported available")
	}
	if err := os.MkdirAll(filepath.Join(dir, "iCloud"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !CloudAvailable(filepath.Join(dir, "iCloud")) {
		t.Fatal("existing directory reported unavailable")
	}
	if got := CloudDir(""); got != "" {
		t.Fatalf("CloudDir(\"\") = %q", got)
	}
	if got := CloudDir("/x"); got != "/x/Today/backups" {
		t.Fatalf("CloudDir = %q", got)
	}
}

// A restore replaces the database and leaves a before-restore copy. A failed
// restore leaves the live database byte-identical.
func TestRestore(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "today.db")
	liveDB(t, live)
	backups := filepath.Join(dir, "backups")
	snap, err := Create(live, backups, "today", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// Change the live database after the snapshot.
	db, _ := sql.Open("sqlite", live)
	if _, err := db.Exec(`INSERT INTO items (id, title) VALUES ('i2', '后加的')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	if err := Restore(snap, live, backups, time.Now()); err != nil {
		t.Fatal(err)
	}
	db, _ = sql.Open("sqlite", live)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM items WHERE id='i2'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if n != 0 {
		t.Fatal("restore kept the later row")
	}
	// The safety copy uses the documented name, with a timestamp.
	var safety int
	for _, name := range listNames(backups) {
		if strings.HasPrefix(name, "today-before-restore-") && strings.HasSuffix(name, ".db") {
			safety++
		}
	}
	if safety != 1 {
		t.Fatalf("safety copies: %v", listNames(backups))
	}
	// A garbage snapshot is refused and the live data survives.
	garbage := filepath.Join(dir, "garbage.db")
	if err := os.WriteFile(garbage, []byte("not a database at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A failed restore must leave every row where it was (the file's bytes
	// may move when SQLite writes the WAL; the data is what matters).
	if err := Restore(garbage, live, backups, time.Now()); err == nil {
		t.Fatal("garbage accepted")
	}
	db, _ = sql.Open("sqlite", live)
	var title string
	if err := db.QueryRow(`SELECT title FROM items WHERE id='i1'`).Scan(&title); err != nil || title != "写周报" {
		t.Fatalf("data after the failed restore: %q %v", title, err)
	}
	_ = db.Close()
}

// itemSchema is the live items table's SQL, for a readable failure message.
func itemSchema(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sqlText string
	_ = db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='items'`).Scan(&sqlText)
	return sqlText
}

func listNames(dir string) []string {
	entries, _ := os.ReadDir(dir)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// List reads names and sizes, newest first, and skips temp files.
func TestList(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"today-20261118.db", "today-20261119.db.gz", "today-20261120.db.tmp", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("xx"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := List(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "today-20261119.db.gz" || got[1].Name != "today-20261118.db" {
		t.Fatalf("list: %+v", got)
	}
	if !got[0].Cloud || got[0].Size != 2 {
		t.Fatalf("metadata: %+v", got[0])
	}
	if snaps, err := List(filepath.Join(dir, "missing"), false); err != nil || len(snaps) != 0 {
		t.Fatalf("missing dir: %v %v", snaps, err)
	}
}

// A snapshot from an older schema (fewer columns, different order) must restore
// by name: an INSERT…SELECT * would shift every value.
func TestRestoreOldSchemaByColumnName(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "today.db")
	liveDB(t, live)

	// The "old" snapshot: items has (title, id) in that order and no settings
	// rows the new code knows, plus an extra table the new schema lacks.
	old := filepath.Join(dir, "old.db")
	odb, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE items (title TEXT, id TEXT)`,
		`CREATE TABLE legacy_only (k TEXT)`,
		`INSERT INTO items (title, id) VALUES ('旧版的事项', 'old-1')`,
	} {
		if _, err := odb.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	_ = odb.Close()

	if err := Restore(old, live, filepath.Join(dir, "backups"), time.Now()); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", live)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id, title string
	if err := db.QueryRow(`SELECT id, title FROM items`).Scan(&id, &title); err != nil {
		t.Fatal(err)
	}
	if id != "old-1" || title != "旧版的事项" {
		t.Fatalf("columns restored by position: id=%q title=%q", id, title)
	}
	// The snapshot had no settings table; the live one is emptied (the
	// snapshot is the new truth), and the schema still works.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&n); err != nil {
		t.Fatalf("settings table broken after restore: %v", err)
	}
}

// A restore that fails half way leaves every row where it was, and the safety
// copy exists (named the way the implementation names it).
func TestRestoreFailureKeepsDataAndSafetyCopy(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "today.db")
	// A live schema where title is NOT NULL, so a NULL in the snapshot makes
	// the INSERT fail inside the transaction: the "restore dies half way" case.
	{
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", live)
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range []string{
			`PRAGMA journal_mode=WAL`,
			`CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT NOT NULL)`,
			`INSERT INTO items VALUES ('i1', '写周报')`,
			`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '')`,
			`INSERT INTO settings VALUES ('auth_session_key', 'super-secret-hmac')`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				db.Close()
				t.Fatal(err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	backups := filepath.Join(dir, "backups")

	// The snapshot has a NULL title; the live table refuses it.
	bad := filepath.Join(dir, "bad.db")
	bdb, err := sql.Open("sqlite", bad)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE items (id TEXT, title TEXT)`,
		`INSERT INTO items VALUES ('broken-1', NULL)`,
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '')`,
	} {
		if _, err := bdb.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	_ = bdb.Close()

	now := time.Now()
	err = Restore(bad, live, backups, now)
	if err == nil {
		t.Fatalf("a snapshot violating the live schema restored (live schema: %v)", itemSchema(t, live))
	}
	// The live data is untouched.
	db, err := sql.Open("sqlite", live)
	if err != nil {
		t.Fatal(err)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM items WHERE id='i1'`).Scan(&title); err != nil || title != "写周报" {
		t.Fatalf("live row after the failed restore: %q %v", title, err)
	}
	_ = db.Close()
	// The safety copy exists under the documented name.
	want := filepath.Join(backups, "today-before-restore-"+now.Format("20060102-150405")+".db")
	if _, err := os.Stat(want); err != nil {
		names, _ := os.ReadDir(backups)
		var got []string
		for _, n := range names {
			got = append(got, n.Name())
		}
		t.Fatalf("no safety copy at %q; directory has %v", filepath.Base(want), got)
	}
	// And the safety copy carries no secrets either.
	if got := settings(t, want); got["auth_session_key"] != "" {
		t.Fatalf("safety copy carries the session key: %v", got)
	}
}
