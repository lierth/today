package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"today/internal/store"
)

// This file holds the small rules the JSON API, the panel bindings and the
// device frames share: how a device reads to a person, how a date is written,
// which settings a client may see. Most of it used to sit in pages.go beside
// the HTML handlers; when the React app replaced those pages, the parts with
// another caller moved here and the rest went with the templates.

type CarriedItem struct {
	store.Item
	DayLabel string
}

// carryRow is the name the templates use.

var weekdayZH = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

var deviceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func deviceOnline(d store.Device) bool {
	return d.LastSeenAt != nil && time.Since(*d.LastSeenAt) <= OnlineWindow
}

func deviceBatteryLabel(d store.Device) string {
	if p := batteryPercent(d.BatteryMV); p >= 0 {
		return strconv.Itoa(p) + "%"
	}
	return "—"
}

func deviceClimate(d store.Device) string {
	if d.TempC == 0 && d.Humidity == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f° %.0f%%", d.TempC, d.Humidity)
}

func deviceSince(d store.Device) string {
	if d.LastSeenAt == nil {
		return "从未连接"
	}
	return fmtTime(d.LastSeenAt)
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.Format("01-02 15:04")
}

// The password hash and the session key never leave the process. They used to
// be removed by the HTML settings handler; now that every client sees this
// map, the stripping belongs here, where no caller can forget it.
func shownSettings(st map[string]string) map[string]string {
	out := make(map[string]string, len(st))
	for k, v := range st {
		if k == "auth_password_hash" || k == "auth_session_key" {
			continue
		}
		if k == "weather_api_key" {
			out[k] = maskKey(v)
			continue
		}
		if k == "proxy_url" {
			out[k] = redactURL(v)
			continue
		}
		out[k] = v
	}
	tz := out["timezone"]
	if tz == "" || tz == "Local" {
		if name := systemTimezone(); name != "" {
			out["timezone"] = name
		}
	}
	return out
}

// liveDevices lists the ids whose preview should be offered on the today page.

func liveDevices(devices []store.Device) []string {
	out := make([]string, 0, len(devices))
	for _, d := range devices {
		if d.RevokedAt == nil {
			out = append(out, d.ID)
		}
	}
	return out
}

// setupCommands is the serial provisioning block shown after a token is made.
// The Wi-Fi line is a placeholder: the password is not stored anywhere.

func previewDevice(devices []store.Device) string {
	for _, d := range devices {
		if d.RevokedAt == nil {
			return d.ID
		}
	}
	return ""
}

func setupCommands(base, token string) []string {
	if token == "" {
		return nil
	}
	return []string{
		"wifi <SSID> <password>",
		"server " + base,
		"device dev1",
		"token " + token,
	}
}

// previewDevice picks the device whose frame the pages link to: the first
// live one, so a revoked or single dev1 does not leave a dead link behind.

func validateProxyURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("代理地址无效：需要 host:port")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return errors.New("代理协议只能是 http、https 或 socks5")
	}
	if u.Port() == "" {
		return errors.New("代理地址需要带端口，例如 127.0.0.1:6152")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func rruleNumber(rule, key string, def int) int {
	for _, part := range strings.Split(rule, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok || !strings.EqualFold(k, key) {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func rruleByDay(rule string) []string {
	for _, part := range strings.Split(rule, ";") {
		k, v, ok := strings.Cut(part, "=")
		if ok && strings.EqualFold(k, "BYDAY") {
			out := []string{}
			for _, tok := range strings.Split(v, ",") {
				if tok = strings.TrimSpace(tok); tok != "" {
					out = append(out, strings.ToUpper(tok))
				}
			}
			return out
		}
	}
	return nil
}

func chineseDay(date string, loc *time.Location) string {
	t, err := time.ParseInLocation(store.DateLayout, date, loc)
	if err != nil {
		return date
	}
	return t.Format("1月2日")
}

// handleReorder writes one drag order. The body is either repeated `item`
// fields in the new order or a JSON array of ids.
