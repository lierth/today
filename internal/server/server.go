// Package server exposes the Today HTTP API, the HTMX pages and the device
// frame endpoint. It depends on store and frame; it never imports host.
package server

import (
	"context"
	"errors"
	"html/template"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"today/internal/store"
	"today/web"
)

// Config configures the HTTP listener.
type Config struct {
	Addr    string
	DataDir string
	Dev     bool
	Logger  *log.Logger
}

// Server is the long-lived HTTP service.
type Server struct {
	st   *store.Store
	cfg  Config
	log  *log.Logger
	mux  *http.ServeMux
	http *http.Server
	ln   net.Listener

	startedAt time.Time
	frameSeq  atomic.Uint64

	settingsMu sync.Mutex
	onSettings func(old, new map[string]string)

	sessionMu  sync.RWMutex
	sessionKey []byte

	limiter *loginLimiter
	tmpl    *template.Template
	sleep   func(time.Duration)
}

// New wires the router. Call Start to listen.
func New(st *store.Store, cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	s := &Server{
		st:        st,
		cfg:       cfg,
		log:       cfg.Logger,
		mux:       http.NewServeMux(),
		startedAt: time.Now(),
		limiter:   newLoginLimiter(),
		sleep:     time.Sleep,
	}
	tmpl, err := template.New("today").Funcs(templateFuncs()).ParseFS(web.Templates(), "*.html")
	if err != nil {
		return nil, err
	}
	s.tmpl = tmpl
	s.routes()
	s.http = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s, nil
}

// Handler exposes the router for tests.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /panel", s.handlePanel)
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /login", s.handleLoginPost)

	s.mux.HandleFunc("GET /v1/devices/{id}/frame", s.handleFrame)
	s.mux.HandleFunc("GET /v1/devices/{id}/firmware", s.handleFirmware)
	s.mux.HandleFunc("POST /v1/devices/{id}/acks", s.handleAcks)

	s.mux.HandleFunc("GET /app", s.requireSession(s.handleApp))
	s.mux.HandleFunc("GET /items", s.requireSession(s.handleItemsPage))
	s.mux.HandleFunc("GET /history", s.requireSession(s.handleHistory))
	s.mux.HandleFunc("GET /items/{id}", s.requireSession(s.handleItemPage))
	s.mux.HandleFunc("POST /items/{id}", s.requireSession(s.handleItemPatch))
	s.mux.HandleFunc("POST /items/{id}/delete", s.requireSession(s.handleItemDeletePost))
	s.mux.HandleFunc("GET /settings", s.requireSession(s.handleSettingsPage))
	s.mux.HandleFunc("GET /", s.handleRoot)

	s.mux.HandleFunc("GET /api/items", s.requireSession(s.handleItemsList))
	s.mux.HandleFunc("POST /api/items", s.requireSession(s.handleItemCreate))
	s.mux.HandleFunc("PATCH /api/items/{id}", s.requireSession(s.handleItemPatch))
	s.mux.HandleFunc("DELETE /api/items/{id}", s.requireSession(s.handleItemDelete))
	s.mux.HandleFunc("GET /api/settings", s.requireSession(s.handleSettingsGet))
	s.mux.HandleFunc("POST /api/settings", s.requireSession(s.handleSettingsPost))
	s.mux.HandleFunc("GET /api/devices/{id}/preview.png", s.requireSession(s.handlePreview))

	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(web.Static())))
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/app", http.StatusFound)
}

// Start listens on cfg.Addr and serves in the background.
func (s *Server) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.ln = ln
	go func() {
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Printf("server: serve: %v", err)
		}
	}()
	go s.tick(ctx)
	return nil
}

// Addr is the actual listen address (useful with :0 in tests).
func (s *Server) Addr() string {
	if s.ln == nil {
		return s.cfg.Addr
	}
	return s.ln.Addr().String()
}

// Shutdown stops the listener gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

func (s *Server) tick(ctx context.Context) {
	s.maintain(ctx)
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.maintain(ctx)
		}
	}
}

func (s *Server) maintain(ctx context.Context) {
	loc, err := s.Location(ctx)
	if err != nil {
		s.log.Printf("server: timezone: %v", err)
		return
	}
	now := time.Now().In(loc)
	n, err := s.st.Materialize(ctx, loc, now, 31)
	if err != nil {
		s.log.Printf("server: materialize: %v", err)
	} else if n > 0 {
		s.log.Printf("server: materialized %d recurring items", n)
	}
	if n, err := s.st.PruneOps(ctx, now.AddDate(0, 0, -30)); err == nil && n > 0 {
		s.log.Printf("server: pruned %d op records", n)
	}
	if n, err := s.st.PruneAuthFails(ctx, now.AddDate(0, 0, -1)); err == nil && n > 0 {
		s.log.Printf("server: pruned %d auth failures", n)
	}
}

// Location resolves the configured timezone, falling back to the host zone.
func (s *Server) Location(ctx context.Context) (*time.Location, error) {
	name, err := s.st.Setting(ctx, "timezone")
	if err != nil {
		return time.Local, err
	}
	if name == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local, nil
	}
	return loc, nil
}

// Today returns the local calendar date used by pages and frames.
func (s *Server) Today(ctx context.Context) (time.Time, *time.Location, error) {
	loc, err := s.Location(ctx)
	if err != nil {
		return time.Time{}, loc, err
	}
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc), loc, nil
}

// Settings returns the merged settings map for the host.
func (s *Server) Settings(ctx context.Context) (map[string]string, error) {
	return s.st.Settings(ctx)
}

// OnSettingsChanged registers the host hook. Only one host exists.
func (s *Server) OnSettingsChanged(fn func(old, new map[string]string)) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.onSettings = fn
}

// UpdateSettings persists a diff and notifies the host once.
func (s *Server) UpdateSettings(ctx context.Context, diff map[string]string) (map[string]string, error) {
	if len(diff) == 0 {
		return s.st.Settings(ctx)
	}
	old, err := s.st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.st.PutSettings(ctx, diff); err != nil {
		return nil, err
	}
	next, err := s.st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	s.settingsMu.Lock()
	fn := s.onSettings
	s.settingsMu.Unlock()
	if fn != nil {
		fn(old, next)
	}
	return next, nil
}

// TodaySummary powers the menu bar title.
func (s *Server) TodaySummary(ctx context.Context) (total, done int, err error) {
	day, _, err := s.Today(ctx)
	if err != nil {
		return 0, 0, err
	}
	return s.st.TodaySummary(ctx, day.Format(store.DateLayout))
}

// Stats powers the settings/diagnostics block.
func (s *Server) Stats(ctx context.Context) (store.Stats, error) {
	day, _, err := s.Today(ctx)
	if err != nil {
		return store.Stats{}, err
	}
	return s.st.Stats(ctx, day.Format(store.DateLayout))
}

// Store exposes the store to the host (devices page uses it too).
func (s *Server) Store() *store.Store { return s.st }
