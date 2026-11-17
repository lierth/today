// Package host is the macOS menu-bar shell. It renders a tray icon, a native
// menu and the Tailscale funnel state; all business data comes from server.
package host

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
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
	base := "http://" + webAddr(addr)
	open := func(raw string) { go openWhenReady(base, deepLinkTarget(raw, base)) }
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
func openWhenReady(base, target string) {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	for i := 0; i < 25; i++ {
		resp, err := client.Get(base + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = mygo.Shell.OpenExternal(target)
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
	funnelOK bool
	fails    int
	tailnet  string

	wake      chan struct{}
	firstPass string

	updating  bool
	latest    string
	updateURL string
}

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
	h.iconNormal = drawIcon(false)
	h.iconError = drawIcon(true)
	settings, err := h.srv.Settings(context.Background())
	if err != nil {
		h.log.Printf("host: settings: %v", err)
		settings = map[string]string{}
	}
	h.setSettings(settings)
	h.applyLoginItem(settings["open_at_login"] == "true")

	mygo.Power.OnResume(func() {
		h.srv.ForceFullRefresh()
		h.kick()
		if h.funnelEnabled() && !h.dev {
			go h.probeFunnel()
		}
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
		mygo.Clipboard.WriteText(h.firstPass)
		if mygo.NotificationsSupported() {
			mygo.NewNotification(mygo.NotificationOptions{
				ID:    "today-first-run",
				Title: "Today 已就绪",
				Body:  "首次启动口令已复制到剪贴板：" + h.firstPass,
			}).Show()
		}
		h.open("/login")
		h.firstPass = ""
	}
	h.srv.OnSettingsChanged(func(old, new map[string]string) {
		if old["funnel"] != new["funnel"] {
			if new["funnel"] == "true" {
				go h.startFunnel()
			} else {
				go h.stopFunnel()
			}
		}
		h.kick()
	})
	if settings["funnel"] == "true" && !h.dev {
		go h.startFunnel()
	}
	go h.loop()
}

func (h *Host) kick() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *Host) loop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	probe := time.NewTicker(60 * time.Second)
	defer probe.Stop()
	for {
		select {
		case <-ticker.C:
			h.refresh()
		case <-probe.C:
			if h.funnelEnabled() && !h.dev {
				go h.probeFunnel()
			}
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
func (h *Host) title() string {
	if h.setting("tray_title") == "false" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done, total, err := h.srv.TodaySummary(ctx)
	if err != nil {
		return "!"
	}
	text := fmt.Sprintf("%2d/%2d", done, total)
	return strings.ReplaceAll(text, " ", "\u2007")
}

func (h *Host) iconForState() []byte {
	h.mu.Lock()
	ok, tailnet := h.funnelOK, h.tailnet
	h.mu.Unlock()
	if h.dev || !h.funnelEnabled() || ok || tailnet == "missing" {
		return h.iconNormal
	}
	return h.iconError
}

// togglePanel shows a small read-only WebView beside the tray icon. The URL
// carries a short-lived panel token; the window has no session cookie and
// cannot mutate anything.
func (h *Host) togglePanel() {
	if h.panel == nil {
		panel := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Today",
			Width:           360,
			Height:          460,
			Panel:           true,
			Frameless:       true,
			DisableResize:   true,
			Hidden:          true,
			CornerRadius:    12,
			BackgroundColor: "#f7f4ed",
		})
		panel.OnBlur(func() { panel.Hide() })
		panel.OnClosed(func() { h.panel = nil })
		h.panel = panel
	}
	if h.panel.IsVisible() {
		h.panel.Hide()
		return
	}
	token, err := h.srv.PanelToken()
	if err != nil {
		h.log.Printf("host: panel token: %v", err)
		return
	}
	url := "http://" + webAddr(h.srv.Addr()) + "/panel?t=" + url.QueryEscape(token)
	if err := h.panel.Page().LoadURL(url); err != nil {
		h.log.Printf("host: panel load: %v", err)
		return
	}
	if b := h.tray.Bounds(); b.Width > 0 {
		x := b.X + b.Width - 360
		if x < 8 {
			x = 8
		}
		h.panel.SetPosition(x, b.Y+b.Height+6)
	}
	h.panel.Show()
	h.panel.Focus()
}

func (h *Host) open(path string) {
	url := "http://" + webAddr(h.srv.Addr()) + path
	if err := mygo.Shell.OpenExternal(url); err != nil {
		h.log.Printf("host: open %s: %v", url, err)
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
	rel, err := fetchRelease(context.Background(), feed, nil)
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

func (h *Host) buildMenu() *mygo.Menu {
	funnelHost := h.setting("funnel_host")
	funnelOn := h.funnelEnabled()
	trayTitle := h.setting("tray_title") != "false"
	login := h.setting("open_at_login") == "true"
	done, total, _ := h.srv.TodaySummary(context.Background())

	statusLabel := "服务：运行中"
	h.mu.Lock()
	ok, tailnet := h.funnelOK, h.tailnet
	h.mu.Unlock()
	funnelLabel := "外网：关闭"
	switch {
	case h.dev:
		funnelLabel = "外网：开发模式不接管"
	case tailnet == "missing":
		funnelLabel = "外网：未安装 Tailscale"
	case funnelOn && ok:
		funnelLabel = "外网：已开启"
	case funnelOn:
		funnelLabel = "外网：正在连接"
	}

	items := []*mygo.MenuItem{
		{Label: "打开今日页面", Submenu: []*mygo.MenuItem{
			{Label: "在浏览器打开", Click: func(*mygo.MenuItem, *mygo.Window) { h.open("/app") }},
			{Label: "快速面板（只读）", Click: func(*mygo.MenuItem, *mygo.Window) { h.togglePanel() }},
		}},
		{Label: "管理事项", Click: func(*mygo.MenuItem, *mygo.Window) { h.open("/items") }},
		{Label: "设置…", Click: func(*mygo.MenuItem, *mygo.Window) { h.open("/settings") }},
		mygo.Separator(),
		{
			ID: "funnel", Label: "外网访问", Type: mygo.MenuItemCheckbox, Checked: funnelOn,
			KeepOpen: true, Disabled: h.dev,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				on := item.Checked
				if _, err := h.srv.UpdateSettings(context.Background(), map[string]string{"funnel": boolString(on)}); err != nil {
					h.log.Printf("host: funnel setting: %v", err)
					return
				}
				h.kick()
			},
		},
		{
			Label: "外网地址", Disabled: funnelHost == "",
			Submenu: []*mygo.MenuItem{
				{Label: "复制地址", Badge: funnelHost, Disabled: funnelHost == "", Click: func(*mygo.MenuItem, *mygo.Window) {
					mygo.Clipboard.WriteText(funnelHost)
				}},
				{Label: "在浏览器打开", Disabled: funnelHost == "", Click: func(*mygo.MenuItem, *mygo.Window) {
					_ = mygo.Shell.OpenExternal(funnelHost)
				}},
			},
		},
		mygo.Separator(),
		{
			ID: "autostart", Label: "开机自启", Type: mygo.MenuItemCheckbox, Checked: login, KeepOpen: true,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				if err := mygo.App.SetOpenAtLogin(item.Checked); err != nil {
					h.log.Printf("host: open at login: %v", err)
				}
				if _, err := h.srv.UpdateSettings(context.Background(), map[string]string{"open_at_login": boolString(item.Checked)}); err != nil {
					h.log.Printf("host: autostart setting: %v", err)
				}
				h.kick()
			},
		},
		{
			ID: "title", Label: "状态栏显示进度", Type: mygo.MenuItemCheckbox, Checked: trayTitle, KeepOpen: true,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				if _, err := h.srv.UpdateSettings(context.Background(), map[string]string{"tray_title": boolString(item.Checked)}); err != nil {
					h.log.Printf("host: title setting: %v", err)
				}
				h.kick()
			},
		},
		{Label: "复制局域网地址", Click: func(*mygo.MenuItem, *mygo.Window) {
			mygo.Clipboard.WriteText("http://" + webAddr(h.srv.Addr()) + "/app")
		}},
		mygo.Separator(),
		{
			Label: "服务状态",
			Submenu: []*mygo.MenuItem{
				{Label: statusLabel, Disabled: true},
				{Label: fmt.Sprintf("今日：%d/%d", done, total), Disabled: true},
				{Label: funnelLabel, Disabled: true},
				{Label: "监听 " + h.srv.Addr(), Disabled: true},
			},
		},
		updateItem(h),
		mygo.Separator(),
		{Role: mygo.RoleQuit, Label: "退出 Today"},
	}
	return mygo.NewMenu(items)
}

func drawIcon(alert bool) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	ink := color.NRGBA{A: 255}
	if alert {
		draw.Draw(img, image.Rect(4, 4, 28, 28), image.NewUniform(ink), image.Point{}, draw.Src)
		// Punch an exclamation mark out of the filled badge.
		for y := 10; y < 20; y++ {
			for x := 14; x < 18; x++ {
				img.SetNRGBA(x, y, color.NRGBA{A: 0})
			}
		}
		for y := 23; y < 26; y++ {
			for x := 14; x < 18; x++ {
				img.SetNRGBA(x, y, color.NRGBA{A: 0})
			}
		}
	} else {
		for _, r := range []image.Rectangle{
			image.Rect(6, 7, 26, 10),
			image.Rect(6, 14, 20, 17),
			image.Rect(6, 21, 23, 24),
		} {
			draw.Draw(img, r, image.NewUniform(ink), image.Point{}, draw.Src)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func (h *Host) startFunnel() {
	if h.dev {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	out, err := h.runTailscale(ctx, "funnel", "--bg", "8787")
	if err != nil {
		h.log.Printf("host: funnel on: %v: %s", err, strings.TrimSpace(out))
		h.mu.Lock()
		h.funnelOK = false
		h.mu.Unlock()
		h.kick()
		return
	}
	h.probeFunnel()
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
	if _, err := h.srv.UpdateSettings(context.Background(), map[string]string{"funnel_host": ""}); err != nil {
		h.log.Printf("host: clear funnel host: %v", err)
	}
	h.kick()
}

func (h *Host) probeFunnel() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := h.runTailscale(ctx, "funnel", "status")
	url := ""
	if m := funnelURL.FindString(out); m != "" {
		url = m
	}
	h.mu.Lock()
	prevOK, prevFails := h.funnelOK, h.fails
	if err != nil || url == "" {
		h.fails++
		h.funnelOK = false
	} else {
		h.fails = 0
		h.funnelOK = true
	}
	ok, fails := h.funnelOK, h.fails
	h.mu.Unlock()
	if h.tray != nil {
		h.tray.SetIcon(h.iconForState(), true)
	}
	if prevOK != ok || (url != "" && url != h.setting("funnel_host")) || (err != nil && prevFails < 3 && fails >= 3) {
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
