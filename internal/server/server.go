// Package server exposes the Today HTTP API, the HTMX pages and the device
// frame endpoint. It depends on store and frame; it never imports host.
package server

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"today/internal/config"
	"today/internal/store"
	"today/internal/weather"
	"today/web"
)

// Config configures the HTTP listener.
type Config struct {
	Addr    string
	DataDir string
	Dev     bool
	Logger  *log.Logger
	// FallbackPort picks a free port when Addr is taken. The menu-bar app
	// wants that (an app that never starts has no settings page); the CLI
	// leaves it off so a wrong port fails loudly in the terminal.
	FallbackPort bool
	// Paths is the resolved data layout (~/.today): backups, firmware, and
	// the note the migration left for the settings page.
	Paths Paths
}

// Paths mirrors datahome.Paths for the parts the server uses. It is a plain
// struct so the server package does not import the data layout package.
type Paths struct {
	Root     string
	DB       string
	Backups  string
	Firmware string
	// CloudDocs is the resolved iCloud Drive folder, or "" for "no iCloud".
	// It is injected rather than discovered here on purpose: a test that calls
	// backup code must never be able to write into the person's real iCloud,
	// and app.Boot is the only place that looks the folder up.
	CloudDocs string
	// DataNote explains a failed or conflicting migration; empty when normal.
	DataNote string
}

// Server is the long-lived HTTP service.
type Server struct {
	st   *store.Store
	cfg  Config
	log  *log.Logger
	mux  *http.ServeMux
	http *http.Server

	listenMu sync.Mutex
	ln       net.Listener
	addr     string
	onAddr   func(string)
	bindNote string

	// now is the server's clock. It defaults to the store's, so a test that
	// pins the store clock also pins "today" for pages, frames and backups;
	// two clocks would be a fresh time bomb in every hand-written date.
	now func() time.Time

	startedAt  time.Time
	frameSeq   atomic.Uint64
	instanceID string

	settingsMu sync.Mutex
	onSettings func(old, new map[string]string)

	// onPassword runs after SetPassword committed the new hash. The host uses
	// it to keep its keychain copy in step; the server stays platform-neutral.
	onPassword func(pw string)

	sessionMu  sync.RWMutex
	sessionKey []byte

	localTokens localTokenStore
	limiter     *loginLimiter
	tmpl        *template.Template
	sleep       func(time.Duration)

	weather *weather.Client
	// outbound is the one client for requests that leave this machine. It
	// follows the proxy settings; local traffic uses its own clients.
	outbound *http.Client
	preview  previewSet

	refreshMu   sync.Mutex
	refreshAll  bool
	refreshDone map[string]bool
	refreshOne  map[string]bool // one-off per device, set by ForceRefresh

	carryMu   sync.Mutex
	lastCarry string // local date the auto-carry last ran

	backups backupState

	// The ticker owns a context derived from the caller's, and Shutdown
	// cancels it and waits for tick to return. Without that, the first
	// maintain keeps querying the database after the app closed it, and the
	// next process (or the next Boot in a test) sees "database is locked".
	tickMu     sync.Mutex
	tickCancel context.CancelFunc
	tickDone   chan struct{}
}

// New wires the router. Call Start to listen.
func New(st *store.Store, cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	s := &Server{
		st:          st,
		cfg:         cfg,
		refreshAll:  true,
		refreshDone: map[string]bool{},
		log:         cfg.Logger,
		mux:         http.NewServeMux(),
		weather:     weather.New(),
		// Outbound HTTP (WeatherAPI now, the update feed later) goes through
		// the configured proxy; loopback and LAN traffic never does.
		outbound:   newOutboundClient(st, cfg.Logger),
		startedAt:  time.Now(),
		addr:       cfg.Addr,
		instanceID: b64(randomBytes(9)),
		limiter:    newLoginLimiter(),
		sleep:      time.Sleep,
		now:        st.Now,
	}
	tmpl, err := template.New("today").Funcs(templateFuncs()).ParseFS(web.Templates(), "*.html")
	if err != nil {
		return nil, err
	}
	s.tmpl = tmpl
	// WeatherAPI is the one request a frame makes to the internet; it must
	// honour the proxy setting like every other outbound call.
	s.weather.HTTP = s.outbound
	s.routes()
	s.http = &http.Server{
		Handler:           s.withOriginCheck(s.mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s, nil
}

// Handler exposes the router for tests.
func (s *Server) Handler() http.Handler { return s.withOriginCheck(s.mux) }

// withOriginCheck rejects a state-changing request that a browser made from
// another origin. See checkWriteOrigin for the rule.
func (s *Server) withOriginCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !checkWriteOrigin(r) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
			s.renderStatus(w, r, http.StatusForbidden, "login.html",
				pageData{Title: "登录", Error: "来自其他网站的请求已被拒绝。"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /panel", s.handlePanel)
	s.mux.HandleFunc("GET /local", s.handleLocalLogin)
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
	s.mux.HandleFunc("POST /api/items/{id}/restore", s.requireSession(s.handleRestore))
	s.mux.HandleFunc("POST /api/items/{id}/status/{status}", s.requireSession(s.handleStatusUndo))
	s.mux.HandleFunc("POST /api/items/carry", s.requireSession(s.handleCarry))
	s.mux.HandleFunc("POST /api/items/uncarry", s.requireSession(s.handleUncarry))
	s.mux.HandleFunc("POST /api/items/reorder", s.requireSession(s.handleReorder))
	s.mux.HandleFunc("POST /api/items/move", s.requireSession(s.handleReorderGroup))
	s.mux.HandleFunc("POST /api/items/parse", s.requireSession(s.handleParsePreview))
	s.mux.HandleFunc("POST /api/backup", s.requireSession(s.handleBackupNow))
	s.mux.HandleFunc("POST /api/backup/restore", s.requireSession(s.handleBackupRestore))
	s.mux.HandleFunc("GET /search", s.requireSession(s.handleSearch))
	s.mux.HandleFunc("GET /api/settings", s.requireSession(s.handleSettingsGet))
	s.mux.HandleFunc("POST /api/settings", s.requireSession(s.handleSettingsPost))
	s.mux.HandleFunc("GET /api/devices/{id}/preview.png", s.requireSession(s.handlePreview))

	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(web.Static())))
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Today-Instance", s.instanceID)
	_, _ = w.Write([]byte("ok"))
}

// InstanceID identifies this process. The shell probes candidate loopback
// addresses and keeps the one that answers with a matching id, so a foreign
// listener squatting on 127.0.0.1:8787 cannot hijack the tray's links.
func (s *Server) InstanceID() string { return s.instanceID }

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/app", http.StatusFound)
}

// Start listens on the configured address and serves in the background. When
// the port is taken by another program it falls back to a free port on the
// same interface instead of exiting: an app that never starts cannot even show
// the settings page that fixes the port. BindNote reports what happened.
func (s *Server) Start(ctx context.Context) error {
	s.ForceFullRefresh()
	// Backups run on the same five-minute ticker as materialisation; installing
	// the hook here means both entry points (the app and todayd) get it.
	s.installBackupHook()
	if err := s.listen(); err != nil {
		if !s.cfg.FallbackPort {
			return err
		}
		fallback, ferr := nextFreeAddr(s.Addr())
		if ferr != nil || fallback == "" {
			return err
		}
		s.listenMu.Lock()
		s.addr = fallback
		s.bindNote = err.Error()
		s.listenMu.Unlock()
		s.log.Printf("server: %v; trying %s instead (fix the port in %s or with --addr)", err, s.Addr(), config.FileName)
		if err := s.listen(); err != nil {
			return err
		}
	}
	tickCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.tickMu.Lock()
	prev := s.tickCancel
	s.tickCancel, s.tickDone = cancel, done
	s.tickMu.Unlock()
	if prev != nil {
		prev()
	}
	go func() {
		defer close(done)
		s.tick(tickCtx)
	}()
	return nil
}

// nextFreeAddr walks the ports just above the busy one on the same interface.
// A deterministic neighbour beats a random port: the firmware stores the
// server URL, and a random port would change on every launch.
func nextFreeAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(NormalizeAddr(addr))
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return "", err
	}
	if n <= 0 {
		// Port 0 already means "any free port"; the kernel picked one, so
		// rebinding to 0 is the fix and the caller supplies its own label.
		return net.JoinHostPort(host, "0"), nil
	}
	for i := 1; i <= 20 && n+i <= 65535; i++ {
		cand := net.JoinHostPort(host, strconv.Itoa(n+i))
		ln, err := net.Listen("tcp", cand)
		if err != nil {
			continue
		}
		_ = ln.Close()
		return cand, nil
	}
	return "", errors.New("no free port nearby")
}

// BindNote is empty unless the configured port was busy at boot.
func (s *Server) BindNote() string {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	return s.bindNote
}

func (s *Server) listen() error {
	s.listenMu.Lock()
	addr := s.addr
	s.listenMu.Unlock()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listenMu.Lock()
	if s.ln != nil {
		_ = s.ln.Close()
	}
	s.ln = ln
	s.listenMu.Unlock()
	go s.serve(ln)
	return nil
}

func (s *Server) serve(ln net.Listener) {
	if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		s.log.Printf("server: serve: %v", err)
	}
}

// Addr is the actual listen address (useful with :0 in tests).
func (s *Server) Addr() string {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	if s.ln == nil {
		return s.addr
	}
	return s.ln.Addr().String()
}

// OnAddrChanged registers one callback fired after a successful rebind. The
// shell uses it to drop cached links and move the funnel to the new port.
func (s *Server) OnAddrChanged(fn func(addr string)) {
	s.listenMu.Lock()
	s.onAddr = fn
	s.listenMu.Unlock()
}

// Rebind moves the listener to addr. Existing requests finish on the old
// listener; new connections go to the new one.
func (s *Server) Rebind(addr string) error {
	if err := ValidateAddr(addr); err != nil {
		return err
	}
	if sameListen(addr, s.Addr()) {
		return nil
	}
	// Probe first: binding is how we learn the port is free, and finding out
	// after closing the old listener would leave the app unreachable.
	probe, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	_ = probe.Close()

	s.listenMu.Lock()
	old := s.addr
	s.addr = addr
	s.listenMu.Unlock()
	if err := s.listen(); err != nil {
		s.listenMu.Lock()
		s.addr = old
		s.listenMu.Unlock()
		return err
	}
	s.listenMu.Lock()
	fn := s.onAddr
	s.listenMu.Unlock()
	if fn != nil {
		fn(s.Addr())
	}
	return nil
}

// sameListen reports whether two spellings would serve the same socket, so
// that saving the settings page without touching the port does not bounce the
// listener. Wildcard spellings (":8787", "0.0.0.0:8787", "[::]:8787") match
// each other; 127.0.0.1:8787 does not.
func sameListen(a, b string) bool {
	ah, ap, errA := net.SplitHostPort(NormalizeAddr(a))
	bh, bp, errB := net.SplitHostPort(NormalizeAddr(b))
	if errA != nil || errB != nil {
		return a == b
	}
	if ap != bp {
		return false
	}
	return wildcardHost(ah) == wildcardHost(bh)
}

func wildcardHost(h string) string {
	switch h {
	case "", "0.0.0.0", "::", "[::]":
		return "*"
	}
	return h
}

// ExpandAddr fills a bare port with the host currently in use, so typing "8892"
// in the settings page keeps serving on the same interface instead of
// silently widening the listener to every interface.
func ExpandAddr(current, input string) string {
	input = strings.TrimSpace(input)
	if input == "" || strings.Contains(input, ":") {
		return NormalizeAddr(input)
	}
	host, _, err := net.SplitHostPort(NormalizeAddr(current))
	if err != nil {
		return NormalizeAddr(input)
	}
	if host == "" {
		return NormalizeAddr(input)
	}
	return net.JoinHostPort(host, input)
}

// SetListenAddr rebinds and records the address in config.json, so the choice
// survives a restart even when the old port is taken by another program.
func (s *Server) SetListenAddr(addr string) error {
	addr = ExpandAddr(s.Addr(), addr)
	if err := ValidateAddr(addr); err != nil {
		return err
	}
	if err := s.Rebind(addr); err != nil {
		return err
	}
	file, err := config.Load(s.cfg.DataDir)
	if err != nil {
		return err
	}
	file.ListenAddr = addr
	return config.Save(s.cfg.DataDir, file)
}

// Port is the port currently served, without the host part. An unstarted
// server (or one bound to :0) reports "" because there is no real port yet.
func (s *Server) Port() string {
	_, port, err := net.SplitHostPort(s.Addr())
	if err != nil || port == "" || port == "0" {
		return ""
	}
	return port
}

// ListenLabel is what the settings page shows in the port field: the bare port
// when there is one, otherwise the whole address.
func (s *Server) ListenLabel() string {
	if p := s.Port(); p != "" {
		return p
	}
	return s.Addr()
}

// ValidateAddr accepts "port", ":port", "host:port" and "[v6]:port".
func ValidateAddr(addr string) error {
	if strings.TrimSpace(addr) == "" {
		return errors.New("监听地址不能为空")
	}
	addr = NormalizeAddr(addr)
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("监听地址要写成 8787 或 0.0.0.0:8787：%s", addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("端口无效：%s", port)
	}
	return nil
}

// NormalizeAddr turns a bare port into a wildcard listen address so the
// settings field can accept "8788" and mean the same as ":8788".
func NormalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return addr
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return ":" + strings.TrimPrefix(addr, ":")
	}
	return addr
}

// Shutdown stops the ticker and the listener gracefully. The ticker is waited
// for: it owns the database connection, and returning before its current
// maintain finishes would close the store underneath it.
func (s *Server) Shutdown(ctx context.Context) error {
	s.tickMu.Lock()
	cancel, done := s.tickCancel, s.tickDone
	s.tickCancel, s.tickDone = nil, nil
	s.tickMu.Unlock()
	if cancel != nil {
		cancel()
	}
	var tickErr error
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			tickErr = ctx.Err()
		}
	}
	if s.http == nil {
		return tickErr
	}
	if err := s.http.Shutdown(ctx); err != nil {
		return err
	}
	return tickErr
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
	now := s.clock().In(loc)
	n, err := s.st.Materialize(ctx, loc, now, 31)
	if err != nil {
		s.log.Printf("server: materialize: %v", err)
	} else if n > 0 {
		s.log.Printf("server: materialized %d recurring items", n)
	}
	s.autoCarry(ctx, loc, now)
	s.runDailyHook(now, loc)
	if n, err := s.st.PruneOps(ctx, now.AddDate(0, 0, -30)); err == nil && n > 0 {
		s.log.Printf("server: pruned %d op records", n)
	}
	if n, err := s.st.PruneAuthFails(ctx, now.AddDate(0, 0, -1)); err == nil && n > 0 {
		s.log.Printf("server: pruned %d auth failures", n)
	}
}

// autoCarry is the 00:05 rule: with `auto_carry` on, everything in the
// unfinished window moves to today. It runs at most once per local day, so the
// five-minute ticker does not move rows the person just carried by hand.
func (s *Server) autoCarry(ctx context.Context, loc *time.Location, now time.Time) {
	settings, err := s.st.Settings(ctx)
	if err != nil || !store.BoolSetting(settings, "auto_carry") {
		return
	}
	today := now.Format(store.DateLayout)
	// The 00:00-00:05 tick belongs to the previous day's sleep; the maintain
	// loop runs again within five minutes. This check must come *before* the
	// day is marked, or the early return would burn the whole day and the
	// carry would never run.
	if now.Hour() == 0 && now.Minute() < 5 {
		return
	}
	s.carryMu.Lock()
	if s.lastCarry == today {
		s.carryMu.Unlock()
		return
	}
	s.carryMu.Unlock()
	n, err := s.st.AutoCarry(ctx, today, loc)
	if err != nil {
		// Not marked as done: a transient failure retries on the next tick.
		s.log.Printf("server: auto carry: %v", err)
		return
	}
	s.carryMu.Lock()
	s.lastCarry = today
	s.carryMu.Unlock()
	if n > 0 {
		s.log.Printf("server: auto carried %d items to %s", n, today)
		s.ForceFullRefresh()
	}
}

// Clock exposes the server clock to tests that build dates by hand.
func (s *Server) Clock() time.Time { return s.clock() }

// clock is the server's wall clock, shared with the store.
func (s *Server) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
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
	now := s.clock().In(loc)
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
