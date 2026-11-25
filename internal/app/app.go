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
	// CloudDocs overrides the iCloud Drive folder. Empty + SkipCloud=false
	// means "look it up at boot"; tests set SkipCloud so they can never write
	// into the person's real iCloud.
	CloudDocs string
	SkipCloud bool
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
	// logCloser closes logs/today.log; nil when the log goes to stderr only.
	logCloser func()
}

// DefaultDBPath lives in paths.go: the database is ~/.today/today.db, so
// removing the app (even with a cleaner that takes Application Support) cannot
// take the data with it.

// Boot opens the store, ensures auth and starts serving. The returned password
// is non-empty only on first boot, when one was generated.
func Boot(ctx context.Context, cfg Config) (*App, string, error) {
	toStderr := cfg.Logger == nil
	var logCloser func()
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	// The flag as given, before PathsFor replaces it with the resolved path:
	// cloudDocsFor needs to know whether this boot was pointed elsewhere.
	override := cfg.DBPath
	paths, pathNote := PathsFor(cfg.DBPath, cfg.Logger)
	// The app bundle has no terminal: keep a copy of the log in the data root
	// so a failure after a restart is still readable.
	if toStderr || cfg.Logger.Writer() == os.Stderr {
		if lg, closeLog, err := teeLog(paths.Logs); err == nil {
			cfg.Logger = lg
			// Closed in App.Close, not on return: Boot hands the App back and
			// keeps logging for the whole process lifetime.
			logCloser = closeLog
		}
	}
	if pathNote != "" {
		cfg.Logger.Printf("today: data: %s", pathNote)
	}
	cfg.DBPath = paths.DB
	addr, source, err := resolveAddr(cfg)
	if err != nil {
		return nil, "", err
	}
	cfg.Addr = addr
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, "", err
	}
	srv, err := server.New(st, server.Config{
		Addr: cfg.Addr, DataDir: filepath.Dir(cfg.DBPath), Dev: cfg.Dev,
		Logger: cfg.Logger, FallbackPort: cfg.FallbackPort,
		Paths: server.Paths{
			Root: paths.Root, DB: paths.DB, Backups: paths.Backups,
			Firmware: paths.Firmware, DataNote: pathNote,
			// The shell is the one place that may look at the real home.
			CloudDocs: cloudDocsFor(cfg, override),
		},
	})
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
	return &App{Store: st, Server: srv, logCloser: logCloser}, pw, nil
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
		if a.logCloser != nil {
			a.logCloser()
			a.logCloser = nil
		}
		a.closeErr = err
	})
	return a.closeErr
}
