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
	cfg := Config{Addr: "127.0.0.1:0", DBPath: db, Dev: true, Logger: log.New(io.Discard, "", 0)}

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
