package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestBootAndRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := filepath.Join(t.TempDir(), "today.db")
	cfg := Config{Addr: "127.0.0.1:0", DBPath: db, Dev: true,
		Logger: log.New(io.Discard, "", 0), SkipCloud: true}

	a, pw, err := Boot(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) != 12 {
		t.Fatalf("first-run password: %q", pw)
	}
	base := "http://" + a.Server.Addr()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(base + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("healthz: %d %q", resp.StatusCode, body)
	}
	if _, err := a.Store.Device(ctx, "dev1"); err != nil {
		t.Fatalf("default device missing: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	a2, pw2, err := Boot(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	if pw2 != "" {
		t.Fatalf("password regenerated on restart: %q", pw2)
	}
	if v, _ := a2.Server.Settings(ctx); v["tray_title"] != "true" {
		t.Fatalf("default settings missing: %v", v)
	}
}

func TestDefaultPaths(t *testing.T) {
	if DefaultAddr != "0.0.0.0:8787" {
		t.Fatalf("default addr: %q", DefaultAddr)
	}
	if got := DefaultDBPath(); got == "" || filepath.Base(got) != "today.db" {
		t.Fatalf("default db path: %q", got)
	}
}

// A restart must not race the first process's ticker: Start's maintain runs
// immediately, and if Close leaves it running it queries the closed store and
// the next Boot fails on journal_mode with "database is locked". This is the
// flake that took 26 clean runs to notice; with the ticker owned by the
// server and waited for in Shutdown it is deterministic.
func TestRestartWaitsForTicker(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := Config{Addr: "127.0.0.1:0", DBPath: filepath.Join(dir, "today.db"),
		Dev: true, Logger: log.New(io.Discard, "", 0), SkipCloud: true}
	for i := 0; i < 5; i++ {
		a, _, err := Boot(ctx, cfg)
		if err != nil {
			t.Fatalf("boot %d: %v", i, err)
		}
		if err := a.Close(); err != nil {
			t.Fatalf("close %d: %v", i, err)
		}
	}
}

// A --db run keeps everything beside that database. It used to keep its
// backups in ~/.today, so a smoke test with --db wrote its rows into the
// person's real backup folder and from there into iCloud.
func TestDbOverrideKeepsTheLayoutTogether(t *testing.T) {
	dir := t.TempDir()
	paths, note := PathsFor(filepath.Join(dir, "today.db"), log.New(io.Discard, "", 0))
	if paths.Root != dir || paths.Backups != filepath.Join(dir, "backups") {
		t.Fatalf("layout: %+v (note %q)", paths, note)
	}
	if got := cloudDocsFor(Config{}, filepath.Join(dir, "today.db")); got != "" {
		t.Fatalf("a --db run would copy into %q", got)
	}
	// An explicit folder still wins, so the shell can pass one in tests.
	if got := cloudDocsFor(Config{CloudDocs: "/tmp/x"}, "elsewhere.db"); got != "/tmp/x" {
		t.Fatalf("explicit cloud folder lost: %q", got)
	}
	// A plain boot with SkipCloud asks nobody either.
	if got := cloudDocsFor(Config{SkipCloud: true}, ""); got != "" {
		t.Fatalf("SkipCloud was ignored: %q", got)
	}
}
