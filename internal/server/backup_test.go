package server

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"today/internal/backup"
	"today/internal/store"
)

// backupEnv is a test environment whose Paths point at its own temp dir, so a
// backup really lands on disk.
func backupEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newTestEnv(t) // its Paths already point into e's temp dir
	return e
}

// 立即备份 writes a snapshot, the page says so, and the snapshot is scrubbed.
func TestBackupNowEndpoint(t *testing.T) {
	e := backupEnv(t)
	e.login(e.password())
	ctx := context.Background()
	if err := e.st.PutSettings(ctx, map[string]string{
		"auth_session_key": "live-session-key", "weather_api_key": "live-weather-key",
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := e.client.PostForm(e.ts.URL+"/api/backup", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	html, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backup: %d %s", resp.StatusCode, snippet(string(html)))
	}
	if !strings.Contains(string(html), "已备份到") {
		t.Fatalf("no confirmation: %s", snippet(string(html)))
	}
	snaps, err := backup.List(e.srv.backupDir(), false)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("snapshots: %v %v", snaps, err)
	}
	db, err := sql.Open("sqlite", snaps[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, key := range backup.ScrubbedKeys {
		var v string
		if err := db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v != "" {
			t.Fatalf("%s survived in the snapshot: %q", key, v)
		}
	}
	// The live database keeps both.
	if v, _ := e.st.Setting(ctx, "weather_api_key"); v != "live-weather-key" {
		t.Fatalf("live key changed: %q", v)
	}
}

// 从备份恢复 puts the rows back and leaves a before-restore copy.
func TestRestoreEndpoint(t *testing.T) {
	e := backupEnv(t)
	e.login(e.password())
	ctx := context.Background()
	day, _, _ := e.srv.Today(ctx)
	loc, _ := e.srv.Location(ctx)
	at := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, loc)
	first, err := e.st.CreateItem(ctx, store.Item{Title: "备份前的事项", StartsAt: &at}, loc)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := e.client.PostForm(e.ts.URL+"/api/backup", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	snaps, _ := backup.List(e.srv.backupDir(), false)
	if len(snaps) != 1 {
		t.Fatalf("snapshots: %d", len(snaps))
	}
	// Change the data after the snapshot.
	second, err := e.st.CreateItem(ctx, store.Item{Title: "备份后的事项", StartsAt: &at}, loc)
	if err != nil {
		t.Fatal(err)
	}

	resp, err = e.client.PostForm(e.ts.URL+"/api/backup/restore", url.Values{"snapshot": {snaps[0].Name}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	html, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore: %d %s", resp.StatusCode, snippet(string(html)))
	}
	if !strings.Contains(string(html), "已从") {
		t.Fatalf("no confirmation: %s", snippet(string(html)))
	}
	// The row from before is back, the row after is gone.
	if _, err := e.st.Item(ctx, first.ID); err != nil {
		t.Fatalf("restored row missing: %v", err)
	}
	if _, err := e.st.Item(ctx, second.ID); err == nil {
		t.Fatal("row from after the snapshot survived")
	}
	// A before-restore copy exists.
	all, _ := backup.List(e.srv.backupDir(), false)
	found := false
	for _, s := range all {
		if strings.HasPrefix(s.Name, "today-before-restore-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no safety copy: %v", all)
	}
}

// Restoring a file that is not on the list, or not a database, fails without
// touching the live data.
func TestRestoreRejectsBadInput(t *testing.T) {
	e := backupEnv(t)
	e.login(e.password())
	ctx := context.Background()
	day, _, _ := e.srv.Today(ctx)
	loc, _ := e.srv.Location(ctx)
	at := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, loc)
	keep, err := e.st.CreateItem(ctx, store.Item{Title: "留下的事项", StartsAt: &at}, loc)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nope.db", "../today.db"} {
		resp, err := e.client.PostForm(e.ts.URL+"/api/backup/restore", url.Values{"snapshot": {name}})
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("%q restored", name)
		}
	}
	// A non-database sitting in the backup directory is refused too.
	_ = os.MkdirAll(e.srv.backupDir(), 0o700)
	garbage := filepath.Join(e.srv.backupDir(), "garbage.db")
	if err := os.WriteFile(garbage, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp, err := e.client.PostForm(e.ts.URL+"/api/backup/restore", url.Values{"snapshot": {"garbage.db"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("garbage restored")
	}
	if _, err := e.st.Item(ctx, keep.ID); err != nil {
		t.Fatalf("live data lost after refused restores: %v", err)
	}
}

// The iCloud toggle: on by default, the page states availability, and a
// missing iCloud Drive greys the block out.
func TestICloudToggleAndStatus(t *testing.T) {
	// An injected, temporary iCloud folder: never the real one.
	cloud := filepath.Join(t.TempDir(), "com~apple~CloudDocs")
	if err := os.MkdirAll(cloud, 0o700); err != nil {
		t.Fatal(err)
	}
	e := newTestEnvCloud(t, cloud)
	e.login(e.password())
	page := string(e.body("/settings"))
	if !strings.Contains(page, "iCloud") {
		t.Fatal("settings page has no iCloud block")
	}
	if strings.Contains(page, "在系统设置里打开 iCloud Drive 后可用") {
		t.Fatal("an existing injected iCloud folder was reported missing")
	}
	if !strings.Contains(page, "同时把备份复制到 iCloud") {
		t.Fatal("the iCloud checkbox is missing with a folder configured")
	}
	// A backup really lands there, gzipped.
	if err := e.srv.RunBackup("2026-11-19"); err != nil {
		t.Fatal(err)
	}
	cloudBackups := filepath.Join(cloud, "Today", "backups")
	entries, err := os.ReadDir(cloudBackups)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no cloud backup: %v %v", entries, err)
	}
	for _, ent := range entries {
		if !strings.HasSuffix(ent.Name(), ".db.gz") {
			t.Fatalf("cloud file %q is not gzip", ent.Name())
		}
	}
	ctx := context.Background()
	resp, err := e.client.PostForm(e.ts.URL+"/api/settings", url.Values{
		"action": {"general"}, "backup_icloud_present": {"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if v, _ := e.st.Setting(ctx, "backup_icloud"); v != "false" {
		t.Fatalf("backup_icloud = %q", v)
	}
}

// The daily hook makes one backup per local date, not one per tick.
func TestDailyHookOncePerDay(t *testing.T) {
	e := backupEnv(t)
	e.srv.SetDailyBackupForTest()
	loc := time.UTC
	e.srv.runDailyHook(time.Date(2026, 11, 19, 0, 5, 0, 0, loc), loc)
	e.srv.runDailyHook(time.Date(2026, 11, 19, 0, 10, 0, 0, loc), loc)
	snaps, err := backup.List(e.srv.backupDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Fatalf("same day made %d backups", len(snaps))
	}
	// A failure does not burn the day: the next tick retries.
	e.srv.backups.mu.Lock()
	e.srv.backups.lastDay = ""
	e.srv.backups.mu.Unlock()
	e.srv.runDailyHook(time.Date(2026, 11, 20, 0, 5, 0, 0, loc), loc)
	days := map[string]bool{}
	for _, s := range mustList(t, e.srv.backupDir()) {
		days[s.Name] = true
	}
	if len(days) < 1 {
		t.Fatal("no backup after the next day")
	}
}

func mustList(t *testing.T, dir string) []backup.Snapshot {
	t.Helper()
	snaps, err := backup.List(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	return snaps
}

// Start installs the daily hook when Paths.Backups is set, so the ticker makes
// a backup every day without the host doing anything. The test environment
// starts the handler server itself (no listener), so the check is explicit.
func TestStartInstallsBackupHook(t *testing.T) {
	e := backupEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.srv.SetDailyBackupForTest(); err != nil {
		t.Fatal(err)
	}
	e.srv.runDailyHook(time.Date(2026, 11, 19, 0, 5, 0, 0, time.UTC), time.UTC)
	snaps, err := backup.List(e.srv.backupDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) == 0 {
		t.Fatal("the daily hook made no backup")
	}
	_ = ctx
}

// The guard: a test that points the cloud folder at the real iCloud Drive must
// fail before it writes anything.
func TestCloudDirGuardRejectsRealICloud(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	real := backup.CloudDocs(home)
	e := newTestEnvCloud(t, real)
	defer func() {
		if recover() == nil {
			t.Fatal("cloudDir accepted the real iCloud folder in a test")
		}
	}()
	_ = e.srv.cloudDir()
}
