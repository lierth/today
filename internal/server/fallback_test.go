package server

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"today/internal/store"
)

// The GUI must come up even when the configured port is taken: without a
// running app there is no settings page to fix the port from.
func TestStartFallsBackToNearbyPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	addr := busy.Addr().String()

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "today.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	srv, err := New(st, Config{Addr: addr, DataDir: dir, Dev: true, FallbackPort: true, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		sctx, scancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer scancel()
		_ = srv.Shutdown(sctx)
	}()
	got := srv.Addr()
	if got == addr {
		t.Fatalf("still on the busy address %s", got)
	}
	if srv.BindNote() == "" {
		t.Fatal("BindNote empty after fallback")
	}
	resp, err := http.Get("http://" + got + "/healthz")
	if err != nil {
		t.Fatalf("fallback listener unreachable: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
}

func TestStartRefusesToFallBackWhenNotAllowed(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "today.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	srv, err := New(st, Config{Addr: busy.Addr().String(), DataDir: dir, Dev: true, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err == nil {
		t.Fatal("want a bind error for the CLI path")
	}
}

func TestNextFreeAddr(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	addr := busy.Addr().String()
	got, err := nextFreeAddr(addr)
	if err != nil {
		t.Fatalf("nextFreeAddr: %v", err)
	}
	if got == addr {
		t.Fatalf("returned the busy address %s", got)
	}
	ln, err := net.Listen("tcp", got)
	if err != nil {
		t.Fatalf("suggested %s is not free: %v", got, err)
	}
	_ = ln.Close()
	// Port 0 means "any port": there is nothing to step away from.
	if got, err := nextFreeAddr("127.0.0.1:0"); err != nil || got == "" {
		t.Fatalf("nextFreeAddr(:0) = %q, %v", got, err)
	}
}
