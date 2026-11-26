package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"today/internal/release"
)

// Update is the one in-flight update: what the menu shows, what a key press
// acts on, and where the verified bundle lives.
type Update struct {
	State    release.State
	Version  string
	Notes    string
	URL      string // release page
	Asset    string // direct zip URL
	Progress int
	Err      string
	Ready    string // staged .app path once verified
}

// updateState is the host's copy plus the fields the machine does not need.
type updater struct {
	State       release.State
	CheckResult string
	Version     string
	Notes       string
	PageURL     string
	AssetURL    string
	Asset       string
	Firmware    string
	FirmwareURL string
	Progress    int
	Err         string
	Staged      string // extraction directory
	Ready       string // verified .app inside Staged
	Dir         string // ~/Library/Caches/Today/updates/<version>
}

// updateCacheRoot is the parent of every staged download.
func updateCacheRoot() string {
	if cache, err := os.UserCacheDir(); err == nil && cache != "" {
		return cache
	}
	return os.TempDir()
}

// updateDir is where downloads stage. One version at a time; a new check
// removes the previous version's directory. The version has already passed
// release.StrictTag, and UpdateDirFor re-checks containment, so a crafted tag
// can never point this at the home directory.
func updateDir(version string) (string, error) {
	return release.UpdateDirFor(updateCacheRoot(), version)
}

// ghRelease is the subset of the GitHub releases/latest document the updater
// reads.
type ghRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// fetchReleaseInfo reads a GitHub releases/latest document (or the older
// {"version","url"} shape) and picks the arm64 zip, the firmware image and the
// notes. It never installs anything; the caller decides.
func fetchReleaseInfo(ctx context.Context, client *http.Client, feed string) (*releaseInfoFull, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Today")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("更新源返回 HTTP %d", resp.StatusCode)
	}
	var gh ghRelease
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &gh); err != nil || gh.TagName == "" {
		// The small custom shape: "version" + "url".
		var small struct {
			Version string `json:"version"`
			URL     string `json:"url"`
		}
		if err2 := json.Unmarshal(body, &small); err2 != nil {
			return nil, fmt.Errorf("更新源格式无效")
		}
		if small.Version == "" || small.URL == "" {
			return nil, fmt.Errorf("更新源缺少 version 或 url")
		}
		if !release.StrictTag(small.Version) {
			return nil, fmt.Errorf("更新源的版本号格式不合法：%q", small.Version)
		}
		if !strings.HasPrefix(small.URL, "https://") && !strings.HasPrefix(small.URL, "http://") {
			return nil, fmt.Errorf("更新地址必须是 http(s)")
		}
		return &releaseInfoFull{Version: small.Version, PageURL: small.URL}, nil
	}
	version := strings.TrimSpace(gh.TagName)
	if version == "" {
		return nil, fmt.Errorf("更新源缺少 tag_name")
	}
	if !release.StrictTag(version) {
		// A tag is used in a URL and in a directory name; anything that is
		// not v1.2.3 is refused before it can reach either.
		return nil, fmt.Errorf("更新源的版本号格式不合法：%q", version)
	}
	out := &releaseInfoFull{Version: version, PageURL: gh.HTMLURL, Notes: gh.Body}
	var names []string
	for _, a := range gh.Assets {
		names = append(names, a.Name)
		switch {
		case a.Name == release.FirmwareName(version):
			// Only the application image: the -full.bin would corrupt the
			// board if written to the OTA partition.
			out.FirmwareName, out.FirmwareURL = a.Name, a.URL
		case a.Name == release.AssetFor([]string{a.Name}, version):
			out.AssetName, out.AssetURL = a.Name, a.URL
		}
	}
	if out.AssetURL == "" {
		// Fall back to the asset picker so a name like Today-0.4.0.zip works.
		if pick := release.AssetFor(names, version); pick != "" {
			for _, a := range gh.Assets {
				if a.Name == pick {
					out.AssetName, out.AssetURL = a.Name, a.URL
				}
			}
		}
	}
	out.AssetsBase = releaseBase(gh.HTMLURL)
	return out, nil
}

// releaseInfoFull is the full release description the updater needs. The name
// keeps it distinct from releaseInfo in update.go, which only feeds the menu.
type releaseInfoFull struct {
	Version      string
	PageURL      string
	Notes        string
	AssetName    string
	AssetURL     string
	AssetsBase   string // https://github.com/o/r/releases/download/v0.4.0
	FirmwareName string
	FirmwareURL  string
}

// releaseBase turns .../releases/tag/v0.4.0 into the download directory, where
// SHA256SUMS lives alongside the assets.
func releaseBase(pageURL string) string {
	if pageURL == "" {
		return ""
	}
	if i := strings.Index(pageURL, "/releases/tag/"); i >= 0 {
		return pageURL[:i] + "/releases/download/" + pageURL[i+len("/releases/tag/"):]
	}
	return ""
}

// RefreshState moves the machine and copies the result into the host under the
// lock, so the tray always renders a consistent snapshot.
func (h *Host) applyUpdateEvent(ev release.Event) release.Transition {
	h.mu.Lock()
	cur := h.updater.State
	tr := release.Next(cur, ev)
	h.updater.State = tr.State
	switch ev.Kind {
	case release.EventNone:
		if tr.State == release.StateIdle {
			h.updater.CheckResult = tr.Note
		}
	case release.EventAvailable:
		h.updater.Version = ev.Version
	case release.EventFail:
		h.updater.Err = ev.Reason
	case release.EventProgress:
		h.updater.Progress = ev.Progress
	case release.EventReset:
		if tr.State == release.StateIdle {
			h.updater.Version, h.updater.Err, h.updater.Progress = "", "", 0
			h.updater.Staged, h.updater.Ready = "", ""
		}
	}
	h.mu.Unlock()
	h.refreshMenu()
	return tr
}

// beginUpdate is the whole click-through: check, find the asset, download,
// verify, stage. It runs in a goroutine; every failure lands in the machine
// with the reason kept for the menu and the log.
func (h *Host) beginUpdate() {
	if res := h.applyUpdateEvent(release.Event{Kind: release.EventCheck}); res.State != release.StateChecking {
		return
	}
	go func() {
		settings := h.setting("update_feed")
		if settings == "" {
			h.applyUpdateEvent(release.Event{Kind: release.EventFail, Reason: "还没有配置更新源"})
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		data, err := fetchReleaseInfo(ctx, h.srv.OutboundAPIClient(), settings)
		if err != nil {
			h.applyUpdateEvent(release.Event{Kind: release.EventFail, Reason: err.Error()})
			return
		}
		if !release.IsNewer(data.Version, Version, false) {
			h.applyUpdateEvent(release.Event{Kind: release.EventNone})
			h.notify("Today 已是最新版本", "当前 "+Version+"。")
			return
		}
		if skip := h.setting("update_skip_version"); skip != "" &&
			release.CompareVersions(skip, data.Version) >= 0 {
			// The person said "skip this one": stay quiet until a newer
			// release appears, not just until the next check.
			h.applyUpdateEvent(release.Event{Kind: release.EventNone})
			return
		}
		h.mu.Lock()
		h.updater.Version = data.Version
		h.updater.Notes = clipNotes(data.Notes)
		h.updater.PageURL = data.PageURL
		h.updater.AssetURL = data.AssetURL
		h.updater.Asset = data.AssetName
		h.updater.Firmware, h.updater.FirmwareURL = data.FirmwareName, data.FirmwareURL
		h.mu.Unlock()
		h.applyUpdateEvent(release.Event{Kind: release.EventAvailable, Version: data.Version})
		h.notify("Today "+data.Version+" 可用", "菜单栏里可以下载并安装。")

		// With update_auto_download on, and only when the person is away and
		// the Mac is on mains power, keep going without a click.
		if h.setting("update_auto_download") == "true" && idleNow() {
			h.downloadUpdate()
		}
	}()
}

func clipNotes(body string) string {
	body = strings.TrimSpace(body)
	r := []rune(body)
	if len(r) > 1000 {
		return string(r[:1000]) + "…"
	}
	return body
}

// idleNow reports whether a background download may start.
func idleNow() bool {
	idle, have := release.ReadIdle()
	return release.IdleEligible(idle, have, release.ReadAC(), release.IdleFor)
}

// downloadUpdate fetches the zip and the firmware, verifies both against the
// signed manifest, and leaves them staged. Nothing is replaced here.
func (h *Host) downloadUpdate() {
	h.mu.Lock()
	up := h.updater
	h.mu.Unlock()
	if up.AssetURL == "" || up.Version == "" {
		h.applyUpdateEvent(release.Event{Kind: release.EventFail, Reason: "没有可下载的版本"})
		return
	}
	if tr := h.applyUpdateEvent(release.Event{Kind: release.EventDownload}); tr.State != release.StateDownloading {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		// fail records the reason and never deletes anything: a bad network
		// must keep the partial file so the next attempt resumes. Only a
		// verification failure wipes the download (see verifyFailed).
		fail := func(err error) {
			h.applyUpdateEvent(release.Event{Kind: release.EventFail, Reason: err.Error()})
			h.notify("更新失败", err.Error())
		}
		verifyFailed := func(err error) {
			// A file that does not match the signed manifest is not worth
			// keeping; a network error is.
			_ = release.RemoveUpdateDir(updateCacheRoot(), up.Version)
			fail(err)
		}
		pub, err := release.LoadPublicKey(builtinPublicKey)
		if err != nil {
			fail(err)
			return
		}
		dir, err := updateDir(up.Version)
		if err != nil {
			fail(err)
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail(err)
			return
		}
		base := releaseBase(up.PageURL)
		if base == "" {
			base = up.AssetURL
			if i := strings.LastIndex(base, "/"); i > 0 {
				base = base[:i]
			}
		}
		sums, err := release.FetchSignedSums(ctx, h.srv.OutboundClient(), base, pub)
		if err != nil {
			fail(err)
			return
		}
		asset := filepath.Join(dir, up.Asset)
		lastAt := time.Now()
		_, err = release.Download(ctx, h.srv.OutboundClient(), up.AssetURL, asset, func(written, total int64) {
			if total <= 0 || time.Since(lastAt) < 300*time.Millisecond {
				return
			}
			lastAt = time.Now()
			pct := int(written * 100 / total)
			h.applyUpdateEvent(release.Event{Kind: release.EventProgress, Progress: pct})
		})
		if err != nil {
			fail(err)
			return
		}
		if tr := h.applyUpdateEvent(release.Event{Kind: release.EventDownloaded}); tr.State != release.StateVerifying {
			return
		}
		if err := release.VerifyArchive(asset, sums); err != nil {
			verifyFailed(err)
			return
		}
		// The firmware image is optional in an older release; when present it
		// must verify too, and it goes into the same OTA slot the settings
		// page uses, without being pushed to a board.
		if up.FirmwareURL != "" {
			fwPath := filepath.Join(dir, up.FirmwareName())
			if _, err := release.Download(ctx, h.srv.OutboundClient(), up.FirmwareURL, fwPath, nil); err == nil {
				if err := release.VerifyArchive(fwPath, sums); err != nil {
					verifyFailed(err)
					return
				}
				// Staged, not pushed: the settings page offers it and the
				// person decides. Writing firmware_version here would make
				// every board upgrade on its next poll.
				h.stageFirmware(fwPath, up.Version)
			}
		}
		extract := filepath.Join(dir, "stage")
		_ = os.RemoveAll(extract)
		if err := os.MkdirAll(extract, 0o755); err != nil {
			fail(err)
			return
		}
		// ditto preserves the bundle's metadata and signatures; unzip does not.
		if out, err := exec.Command("ditto", "-x", "-k", asset, extract).CombinedOutput(); err != nil {
			// A verified zip that will not unzip is a broken release, not a
			// network problem; starting over is the right move.
			verifyFailed(fmt.Errorf("解压失败：%s", strings.TrimSpace(string(out))))
			return
		}
		newApp, err := release.BundleInside(extract)
		if err != nil {
			fail(err)
			return
		}
		plist, err := os.ReadFile(filepath.Join(newApp, "Contents", "Info.plist"))
		if err != nil {
			fail(err)
			return
		}
		id, gotVersion, _ := release.VersionFromPlist(plist)
		if id != expectedBundleID() {
			fail(fmt.Errorf("bundle id 是 %q，不是 %q", id, expectedBundleID()))
			return
		}
		if gotVersion != up.Version && "v"+gotVersion != up.Version {
			fail(fmt.Errorf("新包版本是 %q，期望 %q", gotVersion, up.Version))
			return
		}
		if err := release.VerifyDesignatedRequirement(newApp, currentAppPath()); err != nil {
			verifyFailed(err)
			return
		}
		h.mu.Lock()
		h.updater.Staged = extract
		h.updater.Ready = newApp
		h.mu.Unlock()
		h.applyUpdateEvent(release.Event{Kind: release.EventVerified})
		h.notify("更新已就绪", "重启 Today 即可完成更新到 "+up.Version+"。")
	}()
}

// FirmwareName is the release asset name for the board image, empty when the
// release carries none.
func (u updater) FirmwareName() string { return u.Firmware }

// stageFirmware copies a verified firmware image into the server's OTA slot.
func (h *Host) stageFirmware(path, version string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if err := h.srv.StageFirmware(io.Reader(f), version); err != nil {
		h.log.Printf("host: stage firmware %s: %v", version, err)
	}
}

// errNoReady is returned by installUpdate when nothing verified waits.
var errNoReady = errors.New("host: no verified update")

// installUpdate writes and starts the replacement script, then asks the app to
// quit. On any error the staged files are removed.
func (h *Host) installUpdate() error {
	h.mu.Lock()
	up := h.updater
	h.mu.Unlock()
	if up.State != release.StateReady || up.Ready == "" {
		return errNoReady
	}
	app := currentAppPath()
	if app == "" {
		return errors.New("host: cannot tell where the app lives")
	}
	// A read-only app directory (running from the dmg, or a managed Mac)
	// must not trigger an admin prompt: offer the file instead.
	if err := writableDir(filepath.Dir(app)); err != nil {
		_ = mygo.Shell.OpenExternal(filepath.Dir(filepath.Join(up.Staged, "stage")))
		return fmt.Errorf("程序目录不可写：%s。已打开下载位置，请手动拖进「应用程序」", filepath.Dir(app))
	}
	// The staged bundle was verified when it arrived, but the cache is on
	// disk and anything could have replaced it since. Re-run the signature
	// check right before the substitution: a swapped app never gets installed.
	if err := h.verifyApp(up.Ready, app); err != nil {
		return err
	}
	if tr := h.applyUpdateEvent(release.Event{Kind: release.EventInstall}); tr.State != release.StateInstalling {
		return errNoReady
	}
	script := filepath.Join(up.Staged, "install.sh")
	plan := release.InstallPlan{
		CurrentApp:    app,
		NewApp:        up.Ready,
		OldApp:        app + ".old",
		Script:        script,
		PID:           os.Getpid(),
		StartedMarker: release.StartedMarkerPath(),
	}
	// A stale marker from an earlier run would let the script believe this
	// launch worked before it even started.
	_ = os.Remove(plan.StartedMarker)
	if err := os.WriteFile(script, []byte(release.InstallScript(plan, 30)), 0o755); err != nil {
		h.applyUpdateEvent(release.Event{Kind: release.EventFail, Reason: err.Error()})
		return err
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		h.applyUpdateEvent(release.Event{Kind: release.EventFail, Reason: err.Error()})
		return err
	}
	_ = cmd.Process.Release()
	// The script waits for this PID, so quit only after it is running.
	go func() {
		time.Sleep(300 * time.Millisecond)
		h.quitApp()
	}()
	return nil
}

// quitApp is mygo.App.Quit in production and a stub in tests, so installUpdate
// can be exercised without ending the test process.
func (h *Host) quitApp() {
	if h.quit != nil {
		h.quit()
		return
	}
	mygo.App.Quit()
}

// verifyApp is the signature check; a test can replace it to isolate the
// installer from codesign.
func (h *Host) verifyApp(newApp, currentApp string) error {
	if h.verify != nil {
		return h.verify(newApp, currentApp)
	}
	return release.VerifyDesignatedRequirement(newApp, currentApp)
}

// writableDir proves the installer can create and remove a file there.
func writableDir(dir string) error {
	f, err := os.CreateTemp(dir, ".today-write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// builtinPublicKey is replaced at build time:
//
//	-ldflags "-X today/internal/host.builtinPublicKey=..."
var builtinPublicKey = ""

// expectedBundleID is injected from mygo.json at build time; the literal is
// the repository value.
var expectedBundleIDValue = "com.lierth.today"

func expectedBundleID() string { return expectedBundleIDValue }

// currentAppPath is the bundle this process runs from: .../Today.app/Contents/MacOS/todayd
func currentAppPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, _ = filepath.EvalSymlinks(exe)
	for {
		dir := filepath.Dir(exe)
		if dir == exe || dir == "/" {
			return ""
		}
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
		exe = dir
	}
}

// updateMenuItem keeps actions compact; detailed state is in the tooltip and settings.
func (h *Host) updateMenuItem() *mygo.MenuItem {
	h.mu.Lock()
	up := h.updater
	h.mu.Unlock()
	label := "检查更新…"
	tip, badge := "", ""
	disabled := false
	var click func(*mygo.MenuItem, *mygo.Window)
	switch up.State {
	case release.StateChecking:
		label = "正在检查更新…"
		disabled = true
	case release.StateAvailable:
		label = "下载更新…"
		tip = strings.TrimSpace(up.Version + "\n" + clipNotes(up.Notes))
		click = func(*mygo.MenuItem, *mygo.Window) { go h.downloadUpdate() }
	case release.StateDownloading:
		label = "正在下载更新…"
		badge = fmt.Sprintf("%d%%", up.Progress)
		tip = up.Version
		disabled = true
	case release.StateVerifying:
		label = "正在校验更新…"
		disabled = true
	case release.StateReady:
		label = "重启并更新"
		tip = up.Version
		click = func(*mygo.MenuItem, *mygo.Window) {
			if err := h.installUpdate(); err != nil {
				h.notify("无法自动更新", err.Error())
			}
		}
	case release.StateInstalling:
		label = "正在安装更新…"
		disabled = true
	case release.StateFailed:
		label = "重试更新…"
		tip = up.Err
		click = func(*mygo.MenuItem, *mygo.Window) {
			h.applyUpdateEvent(release.Event{Kind: release.EventReset})
			go h.beginUpdate()
		}
	default:
		click = func(*mygo.MenuItem, *mygo.Window) { go h.beginUpdate() }
	}
	if click == nil && !disabled {
		click = func(*mygo.MenuItem, *mygo.Window) { go h.beginUpdate() }
	}
	item := &mygo.MenuItem{Label: label, ToolTip: tip, Badge: badge, Disabled: disabled}
	item.Click = click
	return item
}

// ReleaseRepo is injected at build time:
//
//	-ldflags -X today/internal/host.ReleaseRepo=owner/repo
var ReleaseRepo = ""

// defaultUpdateFeed is the GitHub endpoint a build with a known repository
// points at. Empty means "the person must fill the setting", which is what a
// source build gets.
func defaultUpdateFeed() string {
	if ReleaseRepo == "" {
		return ""
	}
	return "https://api.github.com/repos/" + ReleaseRepo + "/releases/latest"
}

// ensureUpdateFeed fills the setting once, on the first launch of a build that
// knows its repository. It never overwrites a value the person set.
func (h *Host) ensureUpdateFeed() {
	feed := h.setting("update_feed")
	if feed != "" {
		return
	}
	if def := defaultUpdateFeed(); def != "" {
		if _, err := h.srv.UpdateSettings(context.Background(), map[string]string{"update_feed": def}); err != nil {
			h.log.Printf("host: default update feed: %v", err)
		}
	}
}

// maybeInstallOnQuit starts the replacement when the person quits with a
// verified update waiting. It never blocks: the script waits for this process
// to exit, and close() runs right after.
func (h *Host) maybeInstallOnQuit() {
	h.mu.Lock()
	state := h.updater.State
	h.mu.Unlock()
	if state != release.StateReady {
		return
	}
	if err := h.installUpdate(); err != nil {
		h.log.Printf("host: install on quit: %v", err)
	}
}

// signalStarted writes the marker the installer script waits for, then removes
// a leftover Today.app.old once this version has been running long enough to
// prove it starts. Called from ready().
func (h *Host) signalStarted() {
	if err := release.WriteStartedMarker(); err != nil {
		h.log.Printf("host: startup marker: %v", err)
	}
	h.maybeCleanOldBundle()
}

// maybeCleanOldBundle removes a leftover Today.app.old once the new version has
// been running long enough to prove it starts. Called from ready().
func (h *Host) maybeCleanOldBundle() {
	app := currentAppPath()
	if app == "" {
		return
	}
	if err := release.CleanupOldBundle(app, 30*time.Second); err != nil {
		h.log.Printf("host: remove old bundle: %v", err)
	}
}

// updateLoop is the 5-minute-then-6-hour cadence.
func (h *Host) updateLoop() {
	h.ensureUpdateFeed()
	select {
	case <-h.done:
		return
	case <-time.After(release.FirstCheckAfter):
	}
	if h.setting("update_feed") != "" && h.setting("update_auto_check") != "false" {
		h.beginUpdate()
	}
	t := time.NewTicker(release.CheckInterval)
	defer t.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-t.C:
			if h.setting("update_feed") != "" && h.setting("update_auto_check") != "false" {
				h.beginUpdate()
			}
		}
	}
}

// updateStatus is what the settings card shows.
func (h *Host) updateStatus() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch h.updater.State {
	case release.StateIdle, "":
		return h.updater.CheckResult
	case release.StateChecking:
		return "正在检查更新…"
	case release.StateAvailable:
		return "发现新版本 " + h.updater.Version
	case release.StateDownloading:
		return fmt.Sprintf("正在下载 %s… %d%%", h.updater.Version, h.updater.Progress)
	case release.StateReady:
		return "更新 " + h.updater.Version + " 已就绪，重启即可"
	case release.StateFailed:
		return "上次更新失败：" + h.updater.Err
	}
	return string(h.updater.State)
}

var _ = strconv.Itoa
