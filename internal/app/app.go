// Package app is the composition root: it opens the store, builds the HTTP
// server and starts listening. Host and CLI both boot through it.
package app

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"today/internal/server"
	"today/internal/store"
)

// DefaultAddr keeps every consumer on one listener; a second bind on
// 127.0.0.1 would make Tailscale funnel and device tokens disagree.
const DefaultAddr = "0.0.0.0:8787"

// Config is the subset of flags both entry points accept.
type Config struct {
	Addr   string
	DBPath string
	Dev    bool
	Logger *log.Logger
}

// App owns the process-wide resources.
type App struct {
	Store  *store.Store
	Server *server.Server
}

// DefaultDBPath is ~/Library/Application Support/Today/today.db on macOS and
// the user config dir elsewhere.
func DefaultDBPath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "Today", "today.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".today", "today.db")
}

// Boot opens the store, ensures auth and starts serving. The returned password
// is non-empty only on first boot, when one was generated.
func Boot(ctx context.Context, cfg Config) (*App, string, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	if cfg.Addr == "" {
		cfg.Addr = DefaultAddr
	}
	if cfg.DBPath == "" {
		cfg.DBPath = DefaultDBPath()
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, "", err
	}
	if _, err := st.EnsureDevice(ctx, "dev1", "开发设备"); err != nil {
		_ = st.Close()
		return nil, "", err
	}
	srv, err := server.New(st, server.Config{Addr: cfg.Addr, DataDir: filepath.Dir(cfg.DBPath), Dev: cfg.Dev, Logger: cfg.Logger})
	if err != nil {
		_ = st.Close()
		return nil, "", err
	}
	pw, err := srv.EnsureAuth(ctx)
	if err != nil {
		_ = st.Close()
		return nil, "", err
	}
	if err := srv.Start(ctx); err != nil {
		_ = st.Close()
		return nil, "", err
	}
	cfg.Logger.Printf("today: listening on %s (db %s)", srv.Addr(), cfg.DBPath)
	return &App{Store: st, Server: srv}, pw, nil
}

// Close drains the listener and the database.
func (a *App) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := a.Server.Shutdown(ctx)
	if cerr := a.Store.Close(); err == nil {
		err = cerr
	}
	return err
}
