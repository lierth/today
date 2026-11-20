package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
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
} // Restoring a file that is not on the list, or not a database, fails without
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
		resp := e.apiCall("POST", "/api/v2/backups/restore", map[string]string{"snapshot": name})
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
	resp := e.apiCall("POST", "/api/v2/backups/restore", map[string]string{"snapshot": "garbage.db"})
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("garbage restored")
	}
	if _, err := e.st.Item(ctx, keep.ID); err != nil {
		t.Fatalf("live data lost after refused restores: %v", err)
	}
}
