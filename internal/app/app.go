// Package app is the composition root: it opens the store, builds the HTTP
// server and starts listening. Host and CLI both boot through it.
package app

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"today/internal/config"
	"today/internal/server"
	"today/internal/store"
)

// DefaultAddr is only a starting point: the port is configurable through
// --addr and through config.json next to the database, because 8787 is a
// popular port and the web page that would fix a conflict is unreachable
// when the bind fails.
const DefaultAddr = "0.0.0.0:8787"

// Config is the subset of flags both entry points accept. An empty Addr means
// "read config.json, then fall back to DefaultAddr".
type Config struct {
	Addr   string
	DBPath string
	Dev    bool
	Logger *log.Logger
	// FallbackPort lets the GUI keep running when the configured port is
	// taken; the CLI keeps the hard failure.
	FallbackPort bool
}

// resolveAddr picks the listen address and reports where it came from, so the
// log line explains why the app bound the port it did.
func resolveAddr(cfg Config) (string, string, error) {
	if cfg.Addr != "" {
		return server.NormalizeAddr(cfg.Addr), "flag", nil
	}
	file, err := config.Load(filepath.Dir(cfg.DBPath))
	if err != nil {
		return "", "", err
	}
	if file.ListenAddr != "" {
		addr := server.NormalizeAddr(file.ListenAddr)
		if err := server.ValidateAddr(addr); err != nil {
			return "", "", err
		}
		return addr, config.FileName, nil
	}
	return DefaultAddr, "default", nil
}

// App owns the process-wide resources.
type App struct {
	Store  *store.Store
	Server *server.Server

	closeOnce sync.Once
	closeErr  error
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
	if cfg.DBPath == "" {
		cfg.DBPath = DefaultDBPath()
	}
	addr, source, err := resolveAddr(cfg)
	if err != nil {
		return nil, "", err
	}
	cfg.Addr = addr
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, "", err
	}
	if _, err := st.EnsureDevice(ctx, "dev1", "开发设备"); err != nil {
		_ = st.Close()
		return nil, "", err
	}
	srv, err := server.New(st, server.Config{Addr: cfg.Addr, DataDir: filepath.Dir(cfg.DBPath), Dev: cfg.Dev, Logger: cfg.Logger, FallbackPort: cfg.FallbackPort})
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
	cfg.Logger.Printf("today: listening on %s (%s, db %s)", srv.Addr(), source, cfg.DBPath)
	return &App{Store: st, Server: srv}, pw, nil
}

// Close drains the listener and the database. The quit hook and the deferred
// call in main both run it, so it is safe to call more than once.
func (a *App) Close() error {
	a.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := a.Server.Shutdown(ctx)
		if cerr := a.Store.Close(); err == nil {
			err = cerr
		}
		a.closeErr = err
	})
	return a.closeErr
}
