package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"today/internal/release"
)

// releaseInfo is one available version. The name avoids colliding with the
// release package, whose version rules this file also uses.
type releaseInfo struct {
	Version string
	URL     string
}

// fetchRelease accepts a GitHub releases/latest document or a small
// {"version":"0.2.0","url":"https://…"} document. It never follows a
// non-HTTP scheme and caps the body so a misconfigured feed cannot stall the
// menu.
func fetchRelease(ctx context.Context, feed string, client *http.Client) (releaseInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return releaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("更新源返回 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Version string `json:"version"`
		URL     string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return releaseInfo{}, fmt.Errorf("更新源格式无效")
	}
	version := strings.TrimSpace(payload.TagName)
	if version == "" {
		version = strings.TrimSpace(payload.Version)
	}
	url := strings.TrimSpace(payload.HTMLURL)
	if url == "" {
		url = strings.TrimSpace(payload.URL)
	}
	if version == "" || url == "" {
		return releaseInfo{}, fmt.Errorf("更新源缺少 version 或 url")
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return releaseInfo{}, fmt.Errorf("更新地址必须是 http(s)")
	}
	return releaseInfo{Version: version, URL: url}, nil
}

// newerVersion compares dotted numeric versions. A dev build never reports an
// update; an unparsable current version does not either.
func newerVersion(latest, current string) bool {
	return release.IsNewer(latest, current, false)
}

func updateItem(h *Host) *mygo.MenuItem {
	h.mu.Lock()
	updating, latest, url := h.updating, h.latest, h.updateURL
	h.mu.Unlock()
	label := fmt.Sprintf("关于 Today %s · 检查更新…", Version)
	if updating {
		label = "关于 Today " + Version + " · 正在检查更新…"
	} else if latest != "" {
		label = "有新版本 " + latest
	}
	item := &mygo.MenuItem{Label: label, Disabled: updating, Subtitle: "Today " + Version}
	item.Click = func(*mygo.MenuItem, *mygo.Window) {
		if url != "" {
			_ = mygo.Shell.OpenExternal(url)
			return
		}
		go h.checkUpdate()
	}
	return item
}
