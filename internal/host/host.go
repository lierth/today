// Package host is the macOS menu-bar shell. It renders a tray icon, a native
// menu and the Tailscale funnel state; all business data comes from server.
package host

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"today/internal/keychain"
	"today/internal/server"
)

// Version is surfaced in the menu; release builds overwrite it.
var Version = "dev"

// AppVersion prefers an explicit build version, then the packaged bundle's
// Info.plist version. It is safe to call before Run.
func AppVersion() string {
	if Version != "dev" {
		return Version
	}
	if v := strings.TrimSpace(mygo.App.Version()); v != "" {
		return v
	}
	return Version
}

// Claim takes the single-instance lock and routes today:// links to the local
// server. Call it before starting the server: a second launch exits quietly
// after handing its command line to the first instance.
func Claim(addr string) bool {
	open := func(raw string) { go openWhenReady(addr, raw) }
	mygo.App.SetName("Today")
	mygo.App.OnOpenURL(open)
	mygo.App.OnSecondInstance(func(args []string, _ string) {
		for _, arg := range args {
			if strings.HasPrefix(arg, "today://") {
				open(arg)
				return
			}
		}
		open("")
	})
	return mygo.App.RequestSingleInstanceLock()
}

// deepLinkTarget maps a today:// URL to the page that serves it.
func deepLinkTarget(raw, base string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return base + "/app"
	}
	if !strings.HasPrefix(raw, "today://") {
		if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
			return raw
		}
		return base + "/app"
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(raw, "today://"), "/")
	switch {
	case rest == "" || rest == "app":
		return base + "/app"
	case rest == "settings":
		return base + "/settings"
	case rest == "items" || rest == "item":
		return base + "/items"
	case strings.HasPrefix(rest, "item/"):
		id := strings.TrimPrefix(rest, "item/")
		if id == "" || strings.ContainsAny(id, "/?#") {
			return base + "/items"
		}
		return base + "/items/" + id
	default:
		return base + "/app"
	}
}

// openWhenReady waits for the local server, then hands the URL to the browser.
// It resolves which loopback address really serves Today first, so a stranger
// squatting on 127.0.0.1:8787 never receives the click.
func openWhenReady(addr, raw string) {
	base := waitForBase(addr)
	_ = mygo.Shell.OpenExternal(deepLinkTarget(raw, base))
}

// waitForBase polls every candidate loopback address until Today answers, and
// returns the URL prefix to use. When nothing answers it still returns the
// plain loopback URL so the click has a destination.
func waitForBase(addr string) string {
	for i := 0; i < 25; i++ {
		if base, ok := probeBase(addr, ""); ok {
			return base
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "http://" + webAddr(addr)
}

// probeBase finds the loopback URL that actually reaches our server. Binding a
// wildcard port and a loopback port can both succeed, and the kernel then sends
// loopback traffic to the more specific listener — so a stranger holding
// 127.0.0.1:8787 would answer the tray's links. Today marks every /healthz
// response with its instance id; only the address that echoes the expected
// marker (any marker when want is empty) is used.
func probeBase(addr, want string) (string, bool) {
	port := portOf(addr)
	if port == "" {
		return "http://" + webAddr(addr), false
	}
	client := &http.Client{Timeout: 400 * time.Millisecond}
	for _, host := range []string{"127.0.0.1", "::1"} {
		base := "http://" + net.JoinHostPort(host, port)
		resp, err := client.Get(base + "/healthz")
		if err != nil {
			continue
		}
		id := resp.Header.Get("X-Today-Instance")
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || id == "" {
			continue
		}
		if want != "" && id != want {
			continue
		}
		return base, true
	}
	return "http://" + webAddr(addr), false
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" || port == "0" {
		return ""
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return port
}

var funnelURL = regexp.MustCompile(`https://[A-Za-z0-9.-]+\.ts\.net`)

// Host owns the tray and the background reconciliation loop.
type Host struct {
	srv *server.Server
	dev bool
	log *log.Logger

	tray  *mygo.Tray
	menu  *mygo.Menu
	panel *mygo.Window

	iconNormal []byte
	iconError  []byte

	mu       sync.Mutex
	settings map[string]string
	devices  []server.HostDevice // last fetched, for the alert dot and the menu
	funnelOK bool
	fails    int
	tailnet  string
	stranger string
	base     string

	localNet   LocalNetworkState
	localNetAt time.Time

	funnel       FunnelVerdict
	funnelTries  int
	funnelNextAt time.Time
	funnelBusy   bool

	wake      chan struct{}
	firstPass string

	updating  bool
	latest    string
	updateURL string
	updater   updater
	// quit and verify are hooks for the update tests: nil means the real
	// mygo.App.Quit and release.VerifyDesignatedRequirement.
	quit   func()
	verify func(newApp, currentApp string) error

	release  func()
	shutdown func() error
	done     chan struct{}
	stopOnce sync.Once
}

// SetShutdown registers the process-wide cleanup that runs when the app is
// about to quit: the listener drains and the database closes, so the WAL is
// checkpointed instead of being replayed on the next start.
func (h *Host) SetShutdown(fn func() error) { h.shutdown = fn }

// New builds a host around an already started server.
func New(srv *server.Server, dev bool, logger *log.Logger) *Host {
	if logger == nil {
		logger = log.Default()
	}
	return &Host{
		srv:     srv,
		dev:     dev,
		log:     logger,
		wake:    make(chan struct{}, 1),
		fails:   3,
		tailnet: "unknown",
		done:    make(chan struct{}),
	}
}

// SetFirstRunPassword hands the one-time password to the shell: on first
// boot it is copied to the clipboard and announced once.
func (h *Host) SetFirstRunPassword(pw string) { h.firstPass = pw }

// Run blocks in the native event loop until the app quits.
func (h *Host) Run() error {
	Version = AppVersion()
	mygo.App.SetName("Today")
	mygo.App.SetVersion(Version)
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.WhenReady(h.ready)
	return mygo.App.Run()
}

func (h *Host) ready() {
	h.iconNormal = trayIcon(trayPNG, false)
	h.iconError = trayIcon(trayPNG, true)
	settings, err := h.srv.Settings(context.Background())
	if err != nil {
		h.log.Printf("host: settings: %v", err)
		settings = map[string]string{}
	}
	h.setSettings(settings)
	h.applyLoginItem(settings["open_at_login"] == "true")
	if base := h.baseURL(); base != "http://"+webAddr(h.srv.Addr()) {
		h.log.Printf("host: %s is served by another process; links use %s", webAddr(h.srv.Addr()), base)
	}

	// A port change invalidates every cached link and leaves Tailscale
	// forwarding the old port, so re-run the funnel on the new one.
	h.srv.OnAddrChanged(func(addr string) {
		h.mu.Lock()
		h.base = ""
		h.stranger = ""
		h.mu.Unlock()
		h.log.Printf("host: listening moved to %s", addr)
		if h.funnelEnabled() && !h.dev {
			go func() {
				h.stopFunnel()
				h.startFunnel()
			}()
		}
		h.kick()
	})

	// The tray panel is the only window this app ever opens. Closing it
	// must not quit the shell, which is what "last window closed" means
	// without a listener.
	mygo.App.OnWindowAllClosed(func() {})

	// The shell has no visible window, so App Nap would throttle the
	// listener that the panel and the boards depend on. KeepRunning holds
	// an idle-sleep-allowed activity: the Mac still sleeps when idle.
	// KeepAwake would keep the whole machine up, which it must not.
	h.release = mygo.Power.KeepRunning("serving the Today panel and boards")

	// Session end cannot be canceled, so the cleanup has to finish
	// synchronously inside the hook before the process goes away.
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		reason := "quit"
		if e.SessionEnding {
			reason = "session end"
		}
		// Quitting is also when a verified update gets installed, if one is
		// waiting: the script waits for this PID, so starting it here is safe.
		h.maybeInstallOnQuit()
		h.close(reason)
	})

	mygo.Power.OnResume(func() {
		h.srv.ForceFullRefresh()
		h.kick()
		if h.funnelEnabled() && !h.dev {
			go h.probeFunnel()
			h.scheduleFunnelRetry()
		}
	})

	// Nothing to do on suspend: the store is in WAL mode and every write is
	// already durable. The line is here so a wake that loses frames can be
	// told apart from one that never slept.
	mygo.Power.OnSuspend(func() {
		h.log.Printf("host: system sleeping; next wake forces a full repaint")
	})

	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:              "today",
		Icon:            h.iconNormal,
		IconIsTemplate:  true,
		Title:           h.title(),
		MonospacedTitle: true,
		ToolTip:         "Today",
		Menu:            h.buildMenu(),
	})
	if err != nil {
		h.log.Printf("host: tray: %v", err)
		mygo.App.Quit()
		return
	}
	h.tray = tray
	if h.firstPass != "" {
		pw := h.firstPass
		h.firstPass = ""
		// The plaintext lives in the keychain from here on; the database only
		// keeps its hash. The notification deliberately carries no password:
		// notification history survives on the lock screen and in Notification
		// Centre, which is exactly where a login secret must not be.
		if err := keychain.Set(context.Background(), pw); err != nil {
			h.log.Printf("host: %v", err)
		}
		mygo.Clipboard.WriteText(pw)
		h.scheduleClipboardClear(pw)
		if mygo.NotificationsSupported() {
			mygo.NewNotification(mygo.NotificationOptions{
				ID:    "today-first-run",
				Title: "Today 已就绪",
				Body:  "首次启动口令已复制到剪贴板，60 秒后自动清除；之后可从托盘菜单再复制。",
			}).Show()
		}
		// The person is at this Mac: skip the password form entirely.
		h.openLocal("/app")
	}
	h.srv.OnSettingsChanged(func(old, new map[string]string) {
		if old["funnel"] != new["funnel"] {
			if new["funnel"] == "true" {
				go h.continueFunnel()
			} else {
				go h.stopFunnel()
			}
		}
		if old["funnel_request"] != new["funnel_request"] && new["funnel"] == "true" {
			go h.continueFunnel()
		}
		if old["localnet_request"] != new["localnet_request"] {
			h.openLocalNetworkSettings()
		}
		h.kick()
	})
	if settings["funnel"] == "true" && !h.dev {
		go h.startFunnel()
	}
	// A password reset from the web page rotates the hash; mirror the new
	// plaintext into the keychain in the same breath, or the copy button would
	// go stale until the next launch.
	h.srv.SetPasswordHook(func(pw string) {
		if err := keychain.Set(context.Background(), pw); err != nil {
			h.log.Printf("host: %v", err)
		}
	})

	// A leftover Today.app.old from the previous update is removed once this
	// version has been running for a while (the check is cheap and idempotent).
	go h.signalStarted()
	// Ask for local network access on first launch instead of the first time a
	// board times out: the alert belongs to the person's first minute with the
	// app, and macOS only asks when the app dials a LAN address (TN3179).
	go h.probeLocalNetwork(true)
	go h.loop()
	go h.updateLoop()
}

// probeLocalNetwork dials our own LAN address. With `announce` the result goes
// to the log and (once) to the settings page; the recurring probe stays quiet.
func (h *Host) probeLocalNetwork(announce bool) {
	if h.dev {
		// A command-line process is not subject to local network privacy.
		h.setLocalNetwork(LocalNetworkOK, time.Time{})
		return
	}
	timeout := time.Second
	if announce {
		// The first probe waits for the alert: macOS holds the connection
		// until the person answers, and a short timeout would record "unknown"
		// while the dialog is still on screen.
		timeout = 2 * time.Minute
	}
	state, target := TriggerLocalNetwork(h.srv.Port(), timeout)
	if state == LocalNetworkUnknown && !announce {
		// A transient timeout (sleep, Wi-Fi reassociating) must not replace a
		// known-good answer with a warning.
		h.mu.Lock()
		prev := h.localNet
		h.mu.Unlock()
		if prev == LocalNetworkOK {
			return
		}
	}
	h.setLocalNetwork(state, time.Now())
	if announce {
		h.log.Printf("host: local network access: %s (%s)", state, target)
	}
}

func (h *Host) setLocalNetwork(s LocalNetworkState, at time.Time) {
	h.mu.Lock()
	prev := h.localNet
	h.localNet = s
	if !at.IsZero() {
		h.localNetAt = at
	}
	h.mu.Unlock()
	if prev == s {
		return
	}
	_, err := h.srv.UpdateSettings(context.Background(), map[string]string{
		"localnet_state":  string(s),
		"localnet_detail": LocalNetworkExplanation(s, LocalNetworkTarget()),
		"localnet_adhoc":  boolString(AdhocSigned()),
	})
	if err != nil {
		h.log.Printf("host: local network state: %v", err)
	}
}

func (h *Host) localNetworkState() LocalNetworkState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.localNet
}

// RequestLocalNetwork is the one path behind the menu item and the settings
// button: trigger again, and if the answer is still no, open the pane where
// the switch lives. macOS cannot show the alert twice.
func (h *Host) RequestLocalNetwork() {
	if h.dev {
		return
	}
	state, _ := TriggerLocalNetwork(h.srv.Port(), time.Second)
	h.setLocalNetwork(state, time.Now())
	if state == LocalNetworkOK {
		h.notify("本地网络已允许", "手机和显示屏现在可以连入。")
		h.kick()
		return
	}
	_ = mygo.Shell.OpenExternal(localNetworkURL)
	h.kick()
}

// openLocalNetworkSettings is what the settings page's button pokes through.
func (h *Host) openLocalNetworkSettings() {
	go func() {
		state, _ := TriggerLocalNetwork(h.srv.Port(), time.Second)
		h.setLocalNetwork(state, time.Now())
		if state != LocalNetworkOK {
			_ = mygo.Shell.OpenExternal(localNetworkURL)
		}
		h.kick()
	}()
}

// cachedBase is the last verified loopback prefix. Menu building must never
// block on the network, so it reads the value the probe loop refreshed.
func (h *Host) cachedBase() string {
	h.mu.Lock()
	base := h.base
	h.mu.Unlock()
	if base != "" {
		return base
	}
	return "http://" + webAddr(h.srv.Addr())
}

// noteStranger records whether some other program answers our port on
// loopback, so the menu can say so instead of silently sending users to it.
func (h *Host) noteStranger() {
	hp := h.strangerOn()
	base, ok := probeBase(h.srv.Addr(), h.srv.InstanceID())
	h.mu.Lock()
	changed := h.stranger != hp
	h.stranger = hp
	if ok {
		h.base = base
	} else if h.base == "" {
		h.base = "http://" + webAddr(h.srv.Addr())
	}
	h.mu.Unlock()
	if changed {
		if hp != "" {
			h.log.Printf("host: %s answers but is not Today; tray links avoid it", hp)
		}
		h.kick()
	}
}

func (h *Host) kick() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *Host) loop() {
	// The health probe is deliberately slow: it only feeds the status menu and
	// the funnel guard, and both tolerate a stale answer.
	go func() {
		for {
			select {
			case <-h.done:
				return
			case <-time.After(30 * time.Second):
			}
			h.noteStranger()
		}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	probe := time.NewTicker(60 * time.Second)
	defer probe.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.refresh()
		case <-probe.C:
			if h.funnelEnabled() && !h.dev {
				go h.probeFunnel()
			}
			go h.probeLocalNetwork(false)
			h.refresh()
		case <-h.wake:
			h.refresh()
		}
	}
}

func (h *Host) refresh() {
	settings, err := h.srv.Settings(context.Background())
	if err != nil {
		h.log.Printf("host: refresh settings: %v", err)
		return
	}
	changed := h.setSettings(settings)
	if changed {
		h.applyLoginItem(settings["open_at_login"] == "true")
	}
	// Cache the device list the menu will show; the alert dot and the menu
	// then agree without a second database read.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if devices, err := h.srv.Devices(ctx); err == nil {
		h.mu.Lock()
		h.devices = devices
		h.mu.Unlock()
	}
	cancel()
	if h.tray != nil {
		h.tray.SetTitle(h.title())
		h.tray.SetIcon(h.iconForState(), true)
	}
}

func (h *Host) setSettings(next map[string]string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	changed := false
	for _, k := range []string{"tray_title", "open_at_login", "funnel", "funnel_host"} {
		if h.settings == nil || h.settings[k] != next[k] {
			changed = true
		}
	}
	h.settings = next
	if changed && h.tray != nil {
		h.refreshMenu()
	}
	return changed
}

func (h *Host) setting(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.settings[key]
}

func (h *Host) funnelEnabled() bool { return h.setting("funnel") == "true" }

func (h *Host) funnelVerdict() FunnelVerdict {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.funnel
}

func (h *Host) setFunnelVerdict(v FunnelVerdict) {
	h.mu.Lock()
	if h.funnel == v {
		h.mu.Unlock()
		return
	}
	h.funnel = v
	h.mu.Unlock()
	go func() {
		_, err := h.srv.UpdateSettings(context.Background(), map[string]string{
			"funnel_state":  string(v.State),
			"funnel_action": v.ActionURL,
			"funnel_detail": v.Detail,
		})
		if err != nil {
			h.log.Printf("host: funnel state: %v", err)
		}
	}()
}

// scheduleClipboardClear empties the clipboard after a minute, but only if it
// still holds what we put there. A person who copied a URL in between keeps it.
func (h *Host) scheduleClipboardClear(copied string) {
	go func() {
		select {
		case <-h.done:
			return
		case <-time.After(60 * time.Second):
		}
		if clipboardClearDue(mygo.Clipboard.ReadText(), copied) {
			mygo.Clipboard.WriteText("")
		}
	}()
}

// CopyPassword is the tray action: copy, schedule the wipe, and say so
// without repeating the secret.
func (h *Host) CopyPassword() {
	pw, err := CopyablePassword(context.Background(), h.srv)
	if err != nil {
		h.log.Printf("host: copy password: %v", err)
		return
	}
	mygo.Clipboard.WriteText(pw)
	h.scheduleClipboardClear(pw)
	if mygo.NotificationsSupported() {
		mygo.NewNotification(mygo.NotificationOptions{
			ID:    "today-copy-password",
			Title: "登录口令已复制",
			Body:  "60 秒后自动清除剪贴板；口令在钥匙串里，随时可从菜单再复制。",
		}).Show()
	}
}

// openLocal opens a page through a one-shot link instead of a password prompt.
// Falling back to /login keeps the menu working when signing fails.
func (h *Host) openLocal(path string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	link, err := h.srv.LocalLoginToken(ctx, path, time.Now())
	if err != nil {
		h.log.Printf("host: local link: %v", err)
		h.open(path)
		return
	}
	if err := mygo.Shell.OpenExternal(h.baseURL() + link); err != nil {
		h.log.Printf("host: open %s: %v", path, err)
	}
}

func (h *Host) passwordUsable() bool {
	_, err := CopyablePassword(context.Background(), h.srv)
	return err == nil
}

// loginLabel spells out why the copy item is grey: a stale keychain copy is
// repaired from the settings page, and the label says which page.
func loginLabel(usable bool) string {
	if usable {
		return "复制登录口令"
	}
	return "复制登录口令（不可用：口令已在别处修改，到设置页重设一次即可）"
}

func (h *Host) applyLoginItem(on bool) {
	if mygo.App.OpenAtLogin() == on {
		return
	}
	if err := mygo.App.SetOpenAtLogin(on); err != nil {
		h.log.Printf("host: open at login: %v", err)
	}
}

// title is "done/total" padded with figure spaces so the menu bar does not
// jitter between one- and two-digit counts.
// trayMode resolves the menu-bar title style. tray_mode is the current key;
// the old boolean tray_title is still read so an existing database keeps its
// choice after the upgrade.
func (h *Host) trayMode() string {
	if v := h.setting("tray_mode"); v != "" {
		return v
	}
	if h.setting("tray_title") == "false" {
		return "icon"
	}
	return "progress"
}

// nextItem is the first item that is neither done nor overdue: "10:00 站会".
// It returns "" when everything is done (the caller shows ✓) or the list is
// empty.
func (h *Host) nextItem(items []server.HostItem) string {
	now := time.Now()
	var best *server.HostItem
	for i := range items {
		it := items[i]
		if it.Done || it.At == nil {
			continue
		}
		if it.At.Before(now) {
			continue
		}
		if best == nil || it.At.Before(*best.At) {
			best = &items[i]
		}
	}
	if best == nil {
		return ""
	}
	label := best.Title
	if r := []rune(label); len(r) > 8 {
		label = string(r[:8]) + "…"
	}
	return best.At.Format("15:04") + " " + label
}

func (h *Host) title() string {
	mode := h.trayMode()
	if mode == "icon" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if mode == "next" {
		items, err := h.srv.TodayItems(ctx)
		if err != nil {
			return "!"
		}
		if next := h.nextItem(items); next != "" {
			return next
		}
		// Nothing timed is still ahead: show ✓ only when everything is done,
		// otherwise the count, so an all-day-only list is not a lie.
		done, total := 0, 0
		for _, it := range items {
			total++
			if it.Done {
				done++
			}
		}
		if total > 0 && done == total {
			return "✓"
		}
		return strings.ReplaceAll(fmt.Sprintf("%2d/%2d", done, total), " ", "\u2007")
	}
	done, total, err := h.srv.TodaySummary(ctx)
	if err != nil {
		return "!"
	}
	text := fmt.Sprintf("%2d/%2d", done, total)
	return strings.ReplaceAll(text, " ", "\u2007")
}

// iconForState chooses the menu-bar glyph. The alert dot appears for the
// three states the person can act on: a board missing for a while, the local
// network permission refused, and the funnel switched on but not connected.
func (h *Host) iconForState() []byte {
	h.mu.Lock()
	ok := h.funnelOK
	state := h.funnel.State
	localNet := h.localNet
	h.mu.Unlock()
	alert := false
	if !h.dev {
		if !ok && h.funnelEnabled() && state != FunnelMissing && state != "" {
			alert = true
		}
		if LocalNetworkNeedsAttention(localNet) {
			alert = true
		}
		if h.staleDevices() {
			alert = true
		}
	}
	if alert {
		return h.iconError
	}
	return h.iconNormal
}

// staleDeviceAfter is how long a board may miss its one-minute poll before the
// menu bar says something is wrong. Three misses is the same window the menu
// and the settings card call "online".
const staleDeviceAfter = server.OnlineWindow

// staleDevices reports whether any live board has been missing longer than
// staleDeviceAfter. It reads the cached device list the refresh loop already
// fetches, so the 2 s tick does not hit the database.
func (h *Host) staleDevices() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, d := range h.devices {
		if d.Online {
			continue
		}
		if d.LastSeen == nil {
			return true
		}
		if time.Since(*d.LastSeen) > staleDeviceAfter {
			return true
		}
	}
	return false
}

// baseURL returns the loopback prefix that demonstrably reaches this process.
// The probe runs per call (a cached answer could go stale if the port holder
// changes) but a previous good value is kept as the fallback.
func (h *Host) baseURL() string {
	want := h.srv.InstanceID()
	if base, ok := probeBase(h.srv.Addr(), want); ok {
		h.mu.Lock()
		h.base = base
		h.mu.Unlock()
		return base
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.base != "" {
		return h.base
	}
	return "http://" + webAddr(h.srv.Addr())
}

// strangerOn reports a loopback address that answers our port but is not this
// process. It matters twice: links must avoid it, and the funnel — which
// proxies to 127.0.0.1 — would deliver traffic to the stranger instead.
func (h *Host) strangerOn() string {
	port := portOf(h.srv.Addr())
	if port == "" {
		return ""
	}
	want := h.srv.InstanceID()
	client := &http.Client{Timeout: 300 * time.Millisecond}
	for _, host := range []string{"127.0.0.1", "::1"} {
		hp := net.JoinHostPort(host, port)
		resp, err := client.Get("http://" + hp + "/healthz")
		if err != nil {
			continue
		}
		id := resp.Header.Get("X-Today-Instance")
		_ = resp.Body.Close()
		if id != want {
			return hp
		}
	}
	return ""
}

// togglePanel shows a small read-only flyout under the tray icon. The URL
// carries a short-lived panel token; the flyout has no session cookie and
// cannot mutate anything. It is rebuilt on each open because the token expires
// and a popover closes itself on the first click outside it.
func (h *Host) togglePanel() {
	if h.panel != nil {
		h.panel.Close()
		h.panel = nil
		return
	}
	if h.tray == nil {
		return
	}
	token, err := h.srv.PanelToken()
	if err != nil {
		h.log.Printf("host: panel token: %v", err)
		return
	}
	panel := mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      h.tray,
		Placement: mygo.PlacementBottom,
		Gap:       4,
		Width:     360,
		Height:    460,
		MinWidth:  320,
		MaxWidth:  420,
		Focusable: true,
		Shadow:    true,
		Popover:   true,
		URL:       h.baseURL() + "/panel?t=" + url.QueryEscape(token),
	})
	// The callback fires after a slow close animation; only clear the field
	// when it still points at this flyout, or a fast reopen would be lost.
	panel.OnClosed(func() {
		if h.panel == panel {
			h.panel = nil
		}
	})
	h.panel = panel
}

func (h *Host) open(path string) {
	target := h.baseURL() + path
	if err := mygo.Shell.OpenExternal(target); err != nil {
		h.log.Printf("host: open %s: %v", target, err)
	}
}

func webAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

// lanAddr returns the address other devices on the network can use. It picks
// the first private IPv4 interface address, and falls back to the loopback
// prefix when the machine is offline.
func (h *Host) lanAddr() string {
	port := portOf(h.srv.Addr())
	addrs, err := net.InterfaceAddrs()
	if err == nil && port != "" {
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || !ip4.IsPrivate() {
				continue
			}
			return "http://" + net.JoinHostPort(ip4.String(), port)
		}
	}
	return h.baseURL()
}

func (h *Host) refreshMenu() {
	if h.tray == nil || h.menu == nil {
		return
	}
	h.menu = h.buildMenu()
	h.tray.SetMenu(h.menu)
}

// checkUpdate asks the configured feed once. The menu label doubles as the
// result: it becomes clickable only when a newer release exists.
func (h *Host) checkUpdate() {
	h.mu.Lock()
	if h.updating {
		h.mu.Unlock()
		return
	}
	h.updating = true
	h.mu.Unlock()
	h.refreshMenu()

	feed := h.setting("update_feed")
	if feed == "" {
		h.finishUpdate("", "")
		h.notify("Today 更新", "还没有配置更新源，先在设置里填写。")
		return
	}
	// The update feed is an internet request: it goes through the proxy the
	// person configured, the same client WeatherAPI uses.
	rel, err := fetchRelease(context.Background(), feed, h.srv.OutboundAPIClient())
	if err != nil {
		h.finishUpdate("", "")
		h.notify("检查更新失败", err.Error())
		return
	}
	if newerVersion(rel.Version, Version) {
		h.finishUpdate(rel.Version, rel.URL)
		h.notify("Today 有新版本 "+rel.Version, "点菜单栏的“有新版本”打开下载页。")
		return
	}
	h.finishUpdate("", "")
	h.notify("Today 已是最新版本", "当前 "+Version+"。")
}

func (h *Host) finishUpdate(latest, url string) {
	h.mu.Lock()
	h.updating = false
	h.latest = latest
	h.updateURL = url
	h.mu.Unlock()
	h.refreshMenu()
}

func (h *Host) notify(title, body string) {
	if !mygo.NotificationsSupported() {
		return
	}
	mygo.NewNotification(mygo.NotificationOptions{
		ID:    "today-update",
		Title: title,
		Body:  body,
	}).Show()
}

// close releases the App Nap activity and runs the process cleanup once. The
// quit hook fires on a normal quit and again when the OS ends the session, so
// the work has to be idempotent; it also has to be synchronous, because the
// process may go away as soon as the hook returns.
func (h *Host) close(reason string) {
	h.mu.Lock()
	release, shutdown := h.release, h.shutdown
	h.release, h.shutdown = nil, nil
	h.mu.Unlock()
	if release == nil && shutdown == nil {
		return
	}
	h.stopOnce.Do(func() { close(h.done) })
	// Speaking to Tailscale would outlive the process: the funnel is a
	// system service that keeps running, and resetting it on quit would
	// take the public entry point down with the menu bar.
	if release != nil {
		release()
	}
	if shutdown != nil {
		if err := shutdown(); err != nil {
			h.log.Printf("host: %s: cleanup: %v", reason, err)
			return
		}
		h.log.Printf("host: %s: server stopped", reason)
	}
}

// buildMenu renders the tray menu. It is rebuilt on every open and every 60 s,
// so the checkboxes and the device subtitles stay current.
func (h *Host) buildMenu() *mygo.Menu {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	items, _ := h.srv.TodayItems(ctx)
	devices, _ := h.srv.Devices(ctx)
	settings := map[string]string{}
	if st, err := h.srv.Settings(ctx); err == nil {
		settings = st
	}

	funnelHost := settings["funnel_host"]
	funnelOn := h.funnelEnabled()
	login := settings["open_at_login"] == "true"
	trayMode := h.trayMode()
	done, total, _ := h.srv.TodaySummary(ctx)

	statusLabel := "服务：运行中"
	if note := h.srv.BindNote(); note != "" {
		statusLabel = "服务：原端口被占用，已改用 " + h.srv.Port()
	}
	h.mu.Lock()
	stranger := h.stranger
	localNet := h.localNet
	h.mu.Unlock()
	if stranger != "" {
		statusLabel = "服务：端口被占用（" + stranger + "）"
	}
	localNetWarn := !h.dev && LocalNetworkNeedsAttention(localNet)
	localNetHide := localNet == LocalNetworkOK
	passwordOK := h.passwordUsable()
	verdict := h.funnelVerdict()

	// Header: date, then lunar + weather as the subtitle.
	now := time.Now()
	lunar := lunarLabel(now)
	subtitle := lunar
	if h.dev {
		subtitle += " · 开发模式"
	}
	if w := h.weatherLine(ctx); w != "" {
		subtitle += " · " + w
	}
	header := &mygo.MenuItem{
		Label:    "今天 " + now.Format("1月2日") + " " + weekdayZH[now.Weekday()],
		Subtitle: subtitle,
		Disabled: true,
	}

	// Item checkboxes: at most eight, plus a link to the full page.
	itemMenu := make([]*mygo.MenuItem, 0, 9)
	const maxTrayItems = 8
	for i, it := range items {
		if i >= maxTrayItems {
			break
		}
		label := it.Title
		if it.At != nil && !it.AllDay {
			label = it.At.Format("15:04") + " " + label
		}
		id := it.ID
		doneNow := it.Done
		itemMenu = append(itemMenu, &mygo.MenuItem{
			Label: label, Type: mygo.MenuItemCheckbox, Checked: doneNow, KeepOpen: true,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				// The menu toggles Checked before Click runs; the store is the
				// authority and a failure re-syncs the next rebuild.
				if err := h.srv.SetItemDone(context.Background(), id, item.Checked); err != nil {
					h.log.Printf("host: toggle item: %v", err)
				}
				h.kick()
			},
		})
	}
	if len(items) > maxTrayItems {
		itemMenu = append(itemMenu, &mygo.MenuItem{
			Label: fmt.Sprintf("还有 %d 项…", len(items)-maxTrayItems),
			Click: func(*mygo.MenuItem, *mygo.Window) { go h.openLocal("/app") },
		})
	}
	if len(itemMenu) == 0 {
		itemMenu = append(itemMenu, &mygo.MenuItem{Label: "今天没有事项", Disabled: true})
	}

	// Devices submenu: one row per board, with preview / refresh / test tone.
	deviceItems := make([]*mygo.MenuItem, 0, len(devices)*4)
	for _, d := range devices {
		deviceItems = append(deviceItems, &mygo.MenuItem{
			Label:    deviceLabel(d),
			Subtitle: deviceSubtitle(d),
			Submenu: []*mygo.MenuItem{
				{Label: "预览", Click: func(*mygo.MenuItem, *mygo.Window) {
					go h.openLocal("/settings#devices")
				}},
				{Label: "立即刷新", Click: func(*mygo.MenuItem, *mygo.Window) {
					h.srv.ForceRefresh(d.ID)
					h.notify("已请求刷新", deviceLabel(d)+" 会在 1 分钟内重画。")
					h.kick()
				}},
				{Label: "试听提示音", Click: func(*mygo.MenuItem, *mygo.Window) {
					h.srv.RequestPreview(d.ID)
					h.notify("试听已排队", d.ID+" 会在 1 分钟内响一声。")
					h.kick()
				}},
			},
		})
	}
	if len(deviceItems) == 0 {
		deviceItems = append(deviceItems, &mygo.MenuItem{Label: "还没有设备", Disabled: true})
	}

	funnelLabel := "外网访问"
	funnelSub := "关闭"
	switch {
	case h.dev:
		funnelSub = "开发模式不接管"
	case verdict.State == FunnelMissing:
		funnelSub = "未安装/不支持的 Tailscale（点击安装）"
	case verdict.State == FunnelLoggedOut:
		funnelSub = "Tailscale 未登录（点击授权）"
	case verdict.State == FunnelNeedsSetup:
		funnelSub = "需要开启 HTTPS/Funnel（点击继续）"
	case funnelOn && verdict.State == FunnelOn:
		funnelSub = "已开启"
	case funnelOn:
		funnelSub = "正在连接"
	}

	menu := []*mygo.MenuItem{
		header,
		mygo.Separator(),
	}
	menu = append(menu, itemMenu...)
	menu = append(menu,
		&mygo.MenuItem{Label: "添加事项…", Accelerator: "CmdOrCtrl+N",
			Click: func(*mygo.MenuItem, *mygo.Window) { go h.openLocal("/app?focus=add") }},
		mygo.Separator(),
		&mygo.MenuItem{Label: "打开今日页面", Accelerator: "CmdOrCtrl+O",
			Click: func(*mygo.MenuItem, *mygo.Window) { go h.openLocal("/app") }},
		&mygo.MenuItem{Label: "快速面板",
			Click: func(*mygo.MenuItem, *mygo.Window) { h.togglePanel() }},
		&mygo.MenuItem{Label: "管理事项",
			Click: func(*mygo.MenuItem, *mygo.Window) { go h.openLocal("/items") }},
		mygo.Separator(),
		&mygo.MenuItem{Label: "设备", Submenu: deviceItems},
		&mygo.MenuItem{ID: "funnel", Label: funnelLabel, Subtitle: funnelSub,
			Type: mygo.MenuItemCheckbox, Checked: funnelOn, KeepOpen: true, Disabled: h.dev,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				if _, err := h.srv.UpdateSettings(context.Background(),
					map[string]string{"funnel": boolString(item.Checked)}); err != nil {
					h.log.Printf("host: funnel setting: %v", err)
					return
				}
				h.kick()
			}},
		&mygo.MenuItem{Label: "外网：继续设置", Disabled: h.dev || verdict.ActionURL == "",
			Click: func(*mygo.MenuItem, *mygo.Window) { go h.continueFunnel() }},
		&mygo.MenuItem{Label: "外网地址", Disabled: funnelHost == "",
			Submenu: []*mygo.MenuItem{
				{Label: "复制地址", Badge: funnelHost, Disabled: funnelHost == "",
					Click: func(*mygo.MenuItem, *mygo.Window) { mygo.Clipboard.WriteText(funnelHost) }},
				{Label: "在浏览器打开", Disabled: funnelHost == "",
					Click: func(*mygo.MenuItem, *mygo.Window) { _ = mygo.Shell.OpenExternal(funnelHost) }},
			}},
		&mygo.MenuItem{Label: "⚠ 设备无法连入：允许本地网络…",
			Disabled: !localNetWarn, Hidden: localNetHide,
			Click: func(*mygo.MenuItem, *mygo.Window) { go h.RequestLocalNetwork() }},
		mygo.Separator(),
		&mygo.MenuItem{Label: loginLabel(passwordOK), Disabled: !passwordOK,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				if passwordOK {
					h.CopyPassword()
				}
			}},
		&mygo.MenuItem{Label: "复制局域网地址",
			Click: func(*mygo.MenuItem, *mygo.Window) { mygo.Clipboard.WriteText(h.lanAddr() + "/app") }},
		&mygo.MenuItem{Label: "设置…", Accelerator: "CmdOrCtrl+,",
			Click: func(*mygo.MenuItem, *mygo.Window) { go h.openLocal("/settings") }},
		&mygo.MenuItem{Label: "偏好", Submenu: []*mygo.MenuItem{
			{ID: "autostart", Label: "开机自启", Type: mygo.MenuItemCheckbox, Checked: login,
				KeepOpen: true, Click: func(item *mygo.MenuItem, _ *mygo.Window) {
					if err := mygo.App.SetOpenAtLogin(item.Checked); err != nil {
						h.log.Printf("host: open at login: %v", err)
					}
					if _, err := h.srv.UpdateSettings(context.Background(),
						map[string]string{"open_at_login": boolString(item.Checked)}); err != nil {
						h.log.Printf("host: autostart setting: %v", err)
					}
					h.kick()
				}},
			{Label: "菜单栏显示", Submenu: trayModeItems(h, trayMode)},
		}},
		h.updateMenuItem(),
		&mygo.MenuItem{Label: "服务状态", Submenu: []*mygo.MenuItem{
			{Label: statusLabel, Disabled: true},
			{Label: fmt.Sprintf("今日：%d/%d", done, total), Disabled: true},
			{Label: funnelSub, Disabled: true},
			{Label: "监听 " + h.srv.Addr(), Disabled: true},
			{Label: "内网链接 " + h.cachedBase(), Disabled: true},
		}},
		mygo.Separator(),
		&mygo.MenuItem{Role: mygo.RoleQuit, Label: "退出 Today"},
	)
	return mygo.NewMenu(menu)
}

// trayModeItems is the three-way "菜单栏显示" picker. Choosing writes
// tray_mode, so the choice survives and the old boolean is no longer consulted.
func trayModeItems(h *Host, current string) []*mygo.MenuItem {
	modes := []struct{ value, label string }{
		{"progress", "进度 2/6"},
		{"next", "下一项"},
		{"icon", "仅图标"},
	}
	out := make([]*mygo.MenuItem, 0, len(modes))
	for _, m := range modes {
		value := m.value
		out = append(out, &mygo.MenuItem{
			Label: m.label, Type: mygo.MenuItemRadio, Checked: current == value, KeepOpen: true,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				if _, err := h.srv.UpdateSettings(context.Background(),
					map[string]string{"tray_mode": value}); err != nil {
					h.log.Printf("host: tray mode: %v", err)
				}
				h.kick()
			},
		})
	}
	return out
}

func deviceLabel(d server.HostDevice) string {
	if d.Name != "" {
		return d.Name
	}
	return d.ID
}

// deviceSubtitle is the one line under a device in the menu:
// "在线 · 76% · 23° 45%" or "离线 2 小时".
func deviceSubtitle(d server.HostDevice) string {
	if !d.Online {
		if d.LastSeen == nil {
			return "从未连接"
		}
		return "离线 " + humanSince(time.Since(*d.LastSeen))
	}
	if d.LastSeen != nil && time.Since(*d.LastSeen) > staleDeviceAfter {
		// The online flag is recomputed by server.Devices; this branch only
		// fires when the cached row was built before the window ran out.
		return "离线 " + humanSince(time.Since(*d.LastSeen))
	}
	parts := []string{"在线"}
	if d.BatteryP >= 0 {
		parts = append(parts, fmt.Sprintf("%d%%", d.BatteryP))
	}
	if d.TempC != 0 || d.Humidity != 0 {
		parts = append(parts, fmt.Sprintf("%.0f° %.0f%%", d.TempC, d.Humidity))
	}
	return strings.Join(parts, " · ")
}

// humanSince is the short Chinese form the menu uses.
func humanSince(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 天", int(d.Hours()/24))
	}
}

// weatherLine is the short forecast the header subtitle carries, read from the
// same cache the frame uses (no network here).
func (h *Host) weatherLine(ctx context.Context) string {
	return h.srv.WeatherLine(ctx)
}

var weekdayZH = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// trayIcon returns the menu-bar image the tray shows: the embedded artwork
// when it decodes, otherwise the hand-drawn fallback. `alert` adds the small
// solid dot at the top-right, which is the only "needs attention" mark.
func trayIcon(base []byte, alert bool) []byte {
	if img, err := png.Decode(bytes.NewReader(base)); err == nil {
		if !alert {
			var b bytes.Buffer
			if err := png.Encode(&b, img); err == nil {
				return b.Bytes()
			}
		}
		// The artwork is 18x18; the dot sits in the corner, outside the
		// rounded frame, so it never covers the tick.
		bounds := img.Bounds()
		out := image.NewNRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				out.Set(x, y, img.At(x, y))
			}
		}
		ink := color.NRGBA{A: 255}
		cx, cy := float64(bounds.Max.X)-3, 2.5
		for y := bounds.Min.Y; y < 5; y++ {
			for x := bounds.Max.X - 5; x < bounds.Max.X; x++ {
				dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
				if dx*dx+dy*dy <= 5.5 {
					out.SetNRGBA(x, y, ink)
				}
			}
		}
		var b bytes.Buffer
		if err := png.Encode(&b, out); err == nil {
			return b.Bytes()
		}
	}
	return drawTrayGlyph(alert)
}

func drawTrayGlyph(alert bool) []byte {
	const size = 18
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	ink := color.NRGBA{A: 255}
	clear := color.NRGBA{}

	// Rounded frame from (2.5,3.5) to (15.5,14.5), radius 2.5, stroke 1.6.
	// Drawn as a 1-px box plus corners approximated with two pixels each,
	// which is what survives at 18 px.
	for x := 4; x <= 14; x++ {
		img.SetNRGBA(x, 3, ink)
		img.SetNRGBA(x, 4, ink)
		img.SetNRGBA(x, 14, ink)
		img.SetNRGBA(x, 15, ink)
	}
	for y := 5; y <= 13; y++ {
		img.SetNRGBA(2, y, ink)
		img.SetNRGBA(3, y, ink)
		img.SetNRGBA(14, y, ink)
		img.SetNRGBA(15, y, ink)
	}
	// Round the corners: drop the outermost pair on each diagonal.
	for _, p := range [][2]int{{2, 5}, {2, 13}, {15, 5}, {15, 13}, {3, 3}, {14, 3}, {3, 15}, {14, 15}} {
		img.SetNRGBA(p[0], p[1], clear)
	}

	// Tick inside the screen: (6,9) -> (8,11) -> (12,6.5), 2 px wide.
	for _, p := range [][2]int{{6, 9}, {7, 9}, {7, 10}, {8, 10}, {8, 11}, {9, 10}, {9, 9}, {10, 8}, {10, 7}, {11, 7}, {11, 6}, {12, 6}} {
		img.SetNRGBA(p[0], p[1], ink)
	}

	if alert {
		// 5 px dot at the top-right, sitting outside the frame like a badge.
		for y := 0; y < 5; y++ {
			for x := 13; x < 18; x++ {
				dx, dy := float64(x)-15.0, float64(y)-2.0
				if dx*dx+dy*dy <= 6.25 {
					img.SetNRGBA(x, y, ink)
				}
			}
		}
	}

	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// collectFunnel gathers the two command outputs the state machine needs.
func (h *Host) collectFunnel() FunnelInput {
	bin, err := tailscaleBinary()
	in := FunnelInput{BinaryFound: err == nil, BinaryPath: bin}
	if err != nil {
		return in
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, bin, "status", "--json").Output(); err == nil {
		in.StatusJSON = out
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	out, err := exec.CommandContext(ctx2, bin, "funnel", "status").CombinedOutput()
	in.FunnelOutput = string(out)
	in.FunnelErr = err != nil
	return in
}

// startFunnel only runs `funnel --bg` when the state machine says the setup
// is complete. Repeated --bg calls after a missing login/capability are what
// burn the Let's Encrypt certificate rate limit.
func (h *Host) startFunnel() {
	if h.dev {
		return
	}
	if hp := h.strangerOn(); hp != "" {
		h.log.Printf("host: funnel refused: %s answers but is not Today", hp)
		h.mu.Lock()
		h.funnelOK = false
		h.stranger = hp
		h.mu.Unlock()
		h.kick()
		return
	}
	in := h.collectFunnel()
	verdict := DiagnoseFunnel(in)
	if verdict.State != FunnelConnecting && verdict.State != FunnelOn {
		h.setFunnelVerdict(verdict)
		h.mu.Lock()
		h.funnelOK = false
		h.mu.Unlock()
		h.kick()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	bin, err := tailscaleBinary()
	if err != nil {
		h.setFunnelVerdict(FunnelVerdict{State: FunnelMissing, ActionURL: funnelDownloadURL(), Detail: "未找到 Tailscale CLI"})
		return
	}
	out, err := exec.CommandContext(ctx, bin, "funnel", "--bg", h.port()).CombinedOutput()
	if err != nil {
		h.log.Printf("host: funnel on: %v: %s", err, strings.TrimSpace(string(out)))
		in.FunnelOutput = string(out)
		in.FunnelErr = true
		h.setFunnelVerdict(DiagnoseFunnel(in))
		h.mu.Lock()
		h.funnelOK = false
		h.mu.Unlock()
		h.kick()
		return
	}
	h.probeFunnel()
}

// continueFunnel is the single click path shared by the tray and the settings
// page: it opens whatever URL the current failure needs, or starts the retry
// when the tailnet is ready.
func (h *Host) continueFunnel() {
	if h.dev {
		return
	}
	in := h.collectFunnel()
	verdict := DiagnoseFunnel(in)
	h.setFunnelVerdict(verdict)
	switch verdict.State {
	case FunnelMissing:
		if verdict.ActionURL != "" {
			_ = mygo.Shell.OpenExternal(verdict.ActionURL)
		}
	case FunnelLoggedOut:
		go h.beginLogin(verdict.ActionURL)
	case FunnelNeedsSetup:
		if verdict.ActionURL != "" {
			_ = mygo.Shell.OpenExternal(verdict.ActionURL)
		}
	case FunnelOn:
		if verdict.ActionURL != "" {
			_ = mygo.Shell.OpenExternal(verdict.ActionURL)
		}
	default:
		go h.startFunnel()
	}
	h.kick()
}

// beginLogin runs `tailscale up` off the main loop, then opens the auth URL
// from its output or from the status JSON it leaves behind.
func (h *Host) beginLogin(fallback string) {
	bin, err := tailscaleBinary()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, bin, "up").CombinedOutput()
	if m := loginURL.FindString(string(out)); m != "" {
		_ = mygo.Shell.OpenExternal(m)
		return
	}
	in := h.collectFunnel()
	if v := DiagnoseFunnel(in); v.ActionURL != "" {
		_ = mygo.Shell.OpenExternal(v.ActionURL)
		return
	}
	if fallback != "" {
		_ = mygo.Shell.OpenExternal(fallback)
	}
}

// port is the port Tailscale should forward, taken from the live listener so a
// custom port needs no second place to change.
func (h *Host) port() string {
	if p := h.srv.Port(); p != "" {
		return p
	}
	return "8787"
}

func (h *Host) stopFunnel() {
	if h.dev {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if out, err := h.runTailscale(ctx, "funnel", "reset"); err != nil {
		h.log.Printf("host: funnel reset: %v: %s", err, strings.TrimSpace(out))
	}
	h.mu.Lock()
	h.funnelOK = false
	h.mu.Unlock()
	h.setFunnelVerdict(FunnelVerdict{})
	if _, err := h.srv.UpdateSettings(context.Background(), map[string]string{"funnel_host": ""}); err != nil {
		h.log.Printf("host: clear funnel host: %v", err)
	}
	h.kick()
}

func (h *Host) probeFunnel() {
	in := h.collectFunnel()
	verdict := DiagnoseFunnel(in)
	h.setFunnelVerdict(verdict)
	url := ""
	if verdict.State == FunnelOn {
		url = funnelStatusURL.FindString(in.FunnelOutput)
		if url == "" {
			url = verdict.ActionURL
		}
	}
	h.mu.Lock()
	prevOK := h.funnelOK
	if verdict.State == FunnelOn && url != "" {
		h.funnelOK = true
		h.fails = 0
		h.funnelTries = 0
		h.funnelNextAt = time.Time{}
	} else {
		h.funnelOK = false
		h.fails++
	}
	ok := h.funnelOK
	h.mu.Unlock()
	if h.tray != nil {
		h.tray.SetIcon(h.iconForState(), true)
	}
	changed := prevOK != ok
	if url != "" && url != h.setting("funnel_host") {
		changed = true
	}
	if changed {
		diff := map[string]string{}
		if url != "" {
			diff["funnel_host"] = url
		}
		if len(diff) > 0 {
			if _, err := h.srv.UpdateSettings(context.Background(), diff); err != nil {
				h.log.Printf("host: funnel host: %v", err)
			}
		}
		h.kick()
	}
}

// scheduleFunnelRetry implements 1 -> 2 -> 5 min backoff for a box that is
// checked on but not connected yet. `funnel reset` is never called here.
func (h *Host) scheduleFunnelRetry() {
	if h.dev || !h.funnelEnabled() {
		return
	}
	h.mu.Lock()
	v := h.funnel
	busy := h.funnelBusy
	h.mu.Unlock()
	if busy || v.State == FunnelOn || v.State == FunnelMissing || v.State == FunnelLoggedOut || v.State == FunnelNeedsSetup {
		return
	}
	now := time.Now()
	if !h.funnelNextAt.IsZero() && now.Before(h.funnelNextAt) {
		return
	}
	h.mu.Lock()
	h.funnelBusy = true
	tries := h.funnelTries
	h.funnelTries++
	delay := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute}
	step := tries
	if step >= len(delay) {
		step = len(delay) - 1
	}
	h.funnelNextAt = now.Add(delay[step])
	h.mu.Unlock()
	go func() {
		defer func() {
			h.mu.Lock()
			h.funnelBusy = false
			h.mu.Unlock()
		}()
		h.startFunnel()
	}()
}

func (h *Host) runTailscale(ctx context.Context, args ...string) (string, error) {
	bin, err := tailscaleBinary()
	if err != nil {
		h.mu.Lock()
		h.tailnet = "missing"
		h.mu.Unlock()
		return "", err
	}
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", err
	}
	h.mu.Lock()
	if h.tailnet != "ok" {
		h.tailnet = "ok"
	}
	h.mu.Unlock()
	return string(out), err
}

func tailscaleBinary() (string, error) {
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p, nil
	}
	for _, p := range []string{
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		"/opt/homebrew/bin/tailscale",
		"/usr/local/bin/tailscale",
	} {
		if _, err := exec.LookPath(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("tailscale not found")
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
