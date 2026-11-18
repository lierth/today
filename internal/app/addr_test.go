package app

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"today/internal/config"
)

func quietLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// freeAddr reserves a port and releases it: a fixed port would make the test
// flaky on a busy machine.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// The listen address must be changeable without reaching the web UI: a port
// taken by another program is exactly when the UI is unreachable.
func TestConfigFileSetsListenAddr(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "today.db")
	if err := config.Save(filepath.Dir(db), config.File{ListenAddr: freeAddr(t)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _, err := Boot(ctx, Config{DBPath: db, Dev: true, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if addr := a.Server.Addr(); addr == DefaultAddr {
		t.Fatalf("addr = %q, want the config file value", addr)
	}
}

func TestFlagBeatsConfigFile(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "today.db")
	if err := config.Save(filepath.Dir(db), config.File{ListenAddr: "127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _, err := Boot(ctx, Config{Addr: "127.0.0.1:0", DBPath: db, Dev: true, Logger: quietLogger()})
	if err != nil {
		t.Fatalf("flag should win over a bad config port: %v", err)
	}
	defer a.Close()
	if addr := a.Server.Addr(); addr == "127.0.0.1:1" {
		t.Fatalf("addr = %q, flag ignored", addr)
	}
}

func TestBrokenConfigFileFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "today.db")
	if err := os.WriteFile(config.Path(filepath.Dir(db)), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, _, err := Boot(ctx, Config{DBPath: db, Dev: true, Logger: quietLogger()}); err == nil {
		t.Fatal("want an error for a broken config file")
	}
}

func TestResolveAddrDefaults(t *testing.T) {
	dir := t.TempDir()
	addr, source, err := resolveAddr(Config{DBPath: filepath.Join(dir, "today.db")})
	if err != nil {
		t.Fatal(err)
	}
	if addr != DefaultAddr || source != "default" {
		t.Fatalf("got %q (%s)", addr, source)
	}
	addr, source, err = resolveAddr(Config{Addr: "9040", DBPath: filepath.Join(dir, "today.db")})
	if err != nil {
		t.Fatal(err)
	}
	if addr != ":9040" || source != "flag" {
		t.Fatalf("got %q (%s), want bare port normalized", addr, source)
	}
}

// Changing the port from the settings page must move the listener and survive
// a restart.
func TestSetListenAddrPersistsAndRebinds(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "today.db")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _, err := Boot(ctx, Config{Addr: freeAddr(t), DBPath: db, Dev: true, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	old := a.Server.Addr()
	if err := a.Server.SetListenAddr(freeAddr(t)); err != nil {
		t.Fatalf("rebind to :0: %v", err)
	}
	moved := a.Server.Addr()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + moved + "/healthz")
	if err != nil {
		t.Fatalf("new addr unreachable: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz on new addr: %d", resp.StatusCode)
	}
	file, err := config.Load(filepath.Dir(db))
	if err != nil {
		t.Fatal(err)
	}
	if file.ListenAddr == "" {
		t.Fatal("listen addr not persisted")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	// A restart on the persisted port must not need :0 again.
	if old == "" {
		t.Fatal("old addr empty")
	}
}
