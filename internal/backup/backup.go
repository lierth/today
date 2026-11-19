// Package backup writes and restores database snapshots.
//
// Two rules shape everything here:
//
//   - A snapshot is made with VACUUM INTO, never by copying files. A copied
//     database can miss the WAL and is exactly the kind of "backup" that looks
//     fine until the day it is needed.
//   - A snapshot that leaves this Mac (iCloud) must not carry secrets that are
//     usable on their own. The session HMAC key and the WeatherAPI key are
//     blanked in every backup, local included, so the rule does not depend on a
//     setting. The password hash (Argon2id) and the device token hashes stay:
//     they cannot be replayed without the plaintext.
package backup

import (
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Keep is how many snapshots each destination retains.
const Keep = 14

// ScrubbedKeys are blanked in every snapshot. Keep this list explicit: a
// wildcard would silently start stripping future keys that need to survive.
var ScrubbedKeys = []string{
	"auth_session_key", // an HMAC key: with it a cookie can be forged
	"weather_api_key",  // plaintext third-party credential
}

// FileName is the snapshot name for a local date: today-20261119.db.
func FileName(prefix string, t time.Time) string {
	return fmt.Sprintf("%s-%s.db", prefix, t.Format("20060102"))
}

// RestoredSecretsNote is what the settings page says after a restore.
const RestoredSecretsNote = "备份里不含登录会话密钥和 WeatherAPI key：已登出的会话需要重新登录，天气 key 需要重新填写。"

// Create writes a scrubbed snapshot of dbPath to destDir and returns its path.
//
// The vacuous case (an empty database) still produces a valid file: VACUUM INTO
// copies the schema, and the scrub then runs against the copy.
func Create(dbPath, destDir, prefix string, now time.Time) (string, error) {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, FileName(prefix, now))
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	if err := vacuumInto(dbPath, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := Scrub(tmp); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := Prune(destDir, 0, prefix); err != nil {
		// Pruning is housekeeping: a failure must not lose the fresh backup.
		return dest, nil
	}
	return dest, nil
}

// Scrub blanks the keys in ScrubbedKeys and vacuums the file so the cleared
// pages are not left in free space.
func Scrub(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, key := range ScrubbedKeys {
		if _, err := db.Exec(`UPDATE settings SET value='' WHERE key=?`, key); err != nil {
			return fmt.Errorf("backup: scrub %s: %w", key, err)
		}
	}
	// VACUUM rewrites the file, so the old page images are gone rather than
	// merely unreferenced.
	if _, err := db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("backup: vacuum after scrub: %w", err)
	}
	return nil
}

// VacuumInto writes a consistent copy; the same statement the migration uses.
func vacuumInto(from, to string) error {
	if _, err := os.Stat(to); err == nil {
		return fmt.Errorf("target exists: %s", to)
	}
	db, err := sql.Open("sqlite", from)
	if err != nil {
		return err
	}
	defer db.Close()
	quoted := "'" + strings.ReplaceAll(to, "'", "''") + "'"
	if _, err := db.Exec("VACUUM INTO " + quoted); err != nil {
		return err
	}
	return nil
}

// Prune keeps the newest `keep` snapshots with the given prefix (and the
// before-restore files out of the count). keep <= 0 means Keep.
// Prune keeps the newest `keep` daily snapshots in dir and at most
// BeforeRestoreKeep safety copies.
//
// Only `today-YYYYMMDD.db` (or the .db.gz twin) counts as a daily snapshot.
// The earlier prefix test matched `today-before-restore-…` too, so fourteen
// safety copies would fill the quota and the next fresh backup would be
// deleted the moment it was written.
func Prune(dir string, keep int, prefix string) error {
	if keep <= 0 {
		keep = Keep
	}
	if prefix == "" {
		prefix = "today"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	daily := make([]string, 0, len(entries))
	safety := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.Contains(name, ".tmp") {
			continue
		}
		switch {
		case DailySnapshotRe.MatchString(name):
			daily = append(daily, name)
		case strings.HasPrefix(name, "today-before-restore-") &&
			(strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".db.gz")):
			safety = append(safety, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(daily)))
	for i, name := range daily {
		if i < keep {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
	sort.Sort(sort.Reverse(sort.StringSlice(safety)))
	for i, name := range safety {
		if i < BeforeRestoreKeep {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
	return nil
}

// BeforeRestoreKeep is how many "before restore" copies a directory keeps.
const BeforeRestoreKeep = 5

// DailySnapshotRe is the only name shape that counts as a daily snapshot.
var DailySnapshotRe = regexp.MustCompile(`^today-[0-9]{8}\.db(\.gz)?$`)

// Snapshot is one restorable backup found on disk.
type Snapshot struct {
	Name    string
	Path    string
	Size    int64
	ModTime time.Time
	Cloud   bool
}

// List returns the snapshots in dir (newest first). gzip files are listed too:
// iCloud copies are compressed.
func List(dir string, cloud bool) ([]Snapshot, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Snapshot, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".db") && !strings.HasSuffix(name, ".db.gz") {
			continue
		}
		if strings.Contains(name, ".tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Snapshot{
			Name: name, Path: filepath.Join(dir, name),
			Size: info.Size(), ModTime: info.ModTime(), Cloud: cloud,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// CloudDocs is iCloud Drive's folder for a given home. It exists only when the
// person turned iCloud Drive on.
func CloudDocs(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs")
}

// CloudDocsForHome is the production lookup: the caller passes the home it
// wants (app.Boot passes the real one once). Nothing in this package calls it
// with os.UserHomeDir directly, so a test cannot inherit the real home by
// accident.
func CloudDocsForHome() (string, string) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", ""
	}
	return home, CloudDocs(home)
}

// CloudDir is <CloudDocs>/Today/backups. An empty CloudDocs yields "".
func CloudDir(cloudDocs string) string {
	if cloudDocs == "" {
		return ""
	}
	return filepath.Join(cloudDocs, "Today", "backups")
}

// CloudAvailable reports whether iCloud Drive is usable right now.
func CloudAvailable(cloudDocs string) bool {
	if cloudDocs == "" {
		return false
	}
	st, err := os.Stat(cloudDocs)
	return err == nil && st.IsDir()
}

// CopyCloud gzips the local snapshot into the iCloud folder.
//
// The write is: gzip into `.tmp`, fsync, close, rename. iCloud's sync agent
// only ever sees a complete file, so a half-written backup cannot become the
// "conflict copy" that a direct write would produce.
func CopyCloud(localPath, cloudDir string, now time.Time) (string, error) {
	if cloudDir == "" {
		return "", fmt.Errorf("iCloud Drive 不可用")
	}
	if err := os.MkdirAll(cloudDir, 0o700); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(filepath.Base(localPath), ".db") + ".db.gz"
	dest := filepath.Join(cloudDir, name)
	tmp := dest + ".tmp"
	in, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		_ = gz.Close()
		_ = out.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := gz.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := Prune(cloudDir, 0, "today"); err != nil {
		return dest, nil
	}
	return dest, nil
}

// RestoreError says the current data was left untouched.
type RestoreError struct {
	Reason string
}

func (e RestoreError) Error() string {
	return e.Reason + "；当前数据未受影响"
}

// Restore replaces the contents of dbPath with the contents of snapshotPath,
// inside one transaction.
//
// Copying rows instead of swapping files is deliberate: SQLite here is a
// single-writer database with live connections, and renaming a database file
// under an open connection leaves readers pointing at an unlinked inode. A
// transaction either commits everything or nothing, which is exactly the
// "a failed restore must not touch current data" rule.
//
// A `.gz` snapshot is decompressed to a temporary file first. Both the
// snapshot and the live database are attached to one connection; every table
// of the snapshot is copied after deleting the live rows.
func Restore(snapshotPath, dbPath, backupsDir string, now time.Time) error {
	if _, err := os.Stat(snapshotPath); err != nil {
		return RestoreError{Reason: "找不到备份文件：" + err.Error()}
	}
	src := snapshotPath
	if strings.HasSuffix(snapshotPath, ".gz") {
		tmp, err := gunzipToTemp(snapshotPath)
		if err != nil {
			return RestoreError{Reason: "解压备份失败：" + err.Error()}
		}
		defer os.Remove(tmp)
		src = tmp
	}
	if err := opensAsSQLite(src); err != nil {
		return RestoreError{Reason: "备份文件不是可用的 SQLite 数据库"}
	}

	// A safety copy first: a restore that goes wrong later still has a way
	// back, and the person can see what the state was.
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		return RestoreError{Reason: "无法写安全副本：" + err.Error()}
	}
	safety := filepath.Join(backupsDir, "today-before-restore-"+now.Format("20060102-150405")+".db")
	if err := vacuumInto(dbPath, safety); err != nil {
		return RestoreError{Reason: "无法写安全副本：" + err.Error()}
	}
	// The safety copy follows the same rule as every other snapshot: no
	// session key, no WeatherAPI key.
	if err := Scrub(safety); err != nil {
		return RestoreError{Reason: "无法清理安全副本：" + err.Error()}
	}
	_ = os.Chmod(safety, 0o600)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return RestoreError{Reason: err.Error()}
	}
	defer db.Close()
	if _, err := db.Exec(`ATTACH DATABASE ? AS snap`, src); err != nil {
		return RestoreError{Reason: "无法读取备份：" + err.Error()}
	}
	defer db.Exec(`DETACH DATABASE snap`)

	tx, err := db.Begin()
	if err != nil {
		return RestoreError{Reason: err.Error()}
	}
	defer tx.Rollback()
	if err := copyAllTables(tx); err != nil {
		return RestoreError{Reason: "恢复失败：" + err.Error()}
	}
	if err := tx.Commit(); err != nil {
		return RestoreError{Reason: "恢复提交失败：" + err.Error()}
	}
	return nil
}

// copyAllTables deletes the live rows and copies the snapshot's rows for every
// table both sides have. A table only the snapshot has is skipped with a note
// in the error path: today the schema is always a superset, so a missing table
// means the snapshot predates it and the migration will fill it in.
func copyAllTables(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT name FROM snap.sqlite_master WHERE type='table'
		AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range tables {
		quoted := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		// A table that exists only in the snapshot cannot be filled into a
		// schema that does not have it; skip it rather than fail the restore.
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM main.sqlite_master
			WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		// Copy by column *name*, never by position: a snapshot from an older
		// version has the columns in a different order (and may lack the
		// newer ones), and `SELECT *` would shift every value one place.
		cols, err := commonColumns(tx, name)
		if err != nil {
			return err
		}
		if len(cols) == 0 {
			continue
		}
		list := strings.Join(cols, ", ")
		if _, err := tx.Exec(`DELETE FROM main.` + quoted); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO main.` + quoted + ` (` + list + `) SELECT ` +
			list + ` FROM snap.` + quoted); err != nil {
			return err
		}
	}
	return nil
}

// commonColumns returns the quoted column names present in both the live table
// and the snapshot's copy, in the live table's order.
func commonColumns(tx *sql.Tx, table string) ([]string, error) {
	names := func(schema string) ([]string, error) {
		rows, err := tx.Query(`SELECT name FROM ` + schema + `.pragma_table_info(` +
			quoteSQL(table) + `)`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			out = append(out, name)
		}
		return out, rows.Err()
	}
	live, err := names("main")
	if err != nil {
		return nil, err
	}
	snap, err := names("snap")
	if err != nil {
		return nil, err
	}
	has := make(map[string]bool, len(snap))
	for _, name := range snap {
		has[name] = true
	}
	out := make([]string, 0, len(live))
	for _, name := range live {
		if has[name] {
			out = append(out, quoteSQL(name))
		}
	}
	return out, nil
}

// quoteSQL quotes one identifier for use in a statement.
func quoteSQL(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// gunzipToTemp decompresses a cloud snapshot into the cache directory.
func gunzipToTemp(src string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	out, err := os.CreateTemp("", "today-restore-*.db")
	if err != nil {
		return "", err
	}
	// A zip bomb inside a "backup" must not fill the disk. 200 MB is far more
	// than any real database this program makes.
	const maxRestoreBytes = 200 << 20
	n, err := io.Copy(out, io.LimitReader(gz, maxRestoreBytes+1))
	if err != nil {
		_ = out.Close()
		_ = os.Remove(out.Name())
		return "", err
	}
	if n > maxRestoreBytes {
		_ = out.Close()
		_ = os.Remove(out.Name())
		return "", fmt.Errorf("备份解压后超过 %d MB，已拒绝", maxRestoreBytes>>20)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}

// opensAsSQLite reads the header and the first page.
func opensAsSQLite(path string) error {
	path = strings.TrimSuffix(path, ".gz")
	// A .gz has to be decompressed to be checked; do that only for the header.
	if strings.HasSuffix(path, ".gz") {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 16)
	if _, err := io.ReadFull(f, head); err != nil {
		return err
	}
	if string(head) != "SQLite format 3\x00" {
		return fmt.Errorf("not a sqlite file")
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var n int
	return db.QueryRow("SELECT COUNT(*) FROM sqlite_master").Scan(&n)
}
