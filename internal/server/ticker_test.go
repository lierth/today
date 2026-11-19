package server

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"today/internal/store"
)

// Shutdown must wait for the ticker. The ticker's first maintain runs as soon
// as the listener is up and it queries the store; closing the app before it
// returns is what made TestBootAndRestart fail with "database is locked" once
// in 27 runs.
//
// The test blocks the ticker inside the daily hook, so the wait is
// deterministic: Shutdown cannot return until the hook is released.
func TestShutdownWaitsForTicker(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "today.db"))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(st, Config{
		Addr: "127.0.0.1:0", DataDir: dir, Dev: true,
		Logger: log.New(io.Discard, "", 0),
		Paths:  Paths{Root: dir, DB: filepath.Join(dir, "today.db")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutSettings(context.Background(), map[string]string{"timezone": "Asia/Shanghai"}); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	srv.SetDailyHook(func(string) error {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		return nil
	})
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatalf("the ticker never ran its first maintain (addr %s, note %q)", srv.Addr(), srv.BindNote())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Shutdown(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("Shutdown returned (%v) while the ticker was still running", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return after the ticker finished")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}
