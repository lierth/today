package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"today/internal/backup"
	"today/internal/store"
)

// This file is the narrow slice of the server the menu bar app drives: things
// the pages do with a form post, but the app needs as a call. They all sit on
// the same rules layer the HTML pages and /api/v2 use, so the two cannot drift.

// TokenResult is one freshly minted device token plus the four lines a person
// pastes into the board's serial console. It is only available right after
// generating: the database keeps a hash, never the token.
type TokenResult struct {
	Token    string   `json:"token"`
	Hint     string   `json:"hint"`
	Commands []string `json:"commands"`
}

// GenerateDeviceToken replaces a board's token. The old one stops working on
// its next request, which is the point of the button.
func (s *Server) GenerateDeviceToken(ctx context.Context, id string) (TokenResult, error) {
	if !deviceIDPattern.MatchString(id) {
		return TokenResult{}, fmt.Errorf("设备 ID 无效")
	}
	token := newDeviceToken()
	if err := s.st.SetDeviceToken(ctx, id, tokenHash(token)); err != nil {
		return TokenResult{}, err
	}
	hint := token
	if len(hint) > 4 {
		hint = "…" + hint[len(hint)-4:]
	}
	if _, err := s.UpdateSettings(ctx, map[string]string{"device_token_hint": hint}); err != nil {
		return TokenResult{}, err
	}
	return TokenResult{Token: token, Hint: hint, Commands: setupCommands(s.lanBase(), token)}, nil
}

// PreviewFramePNG renders the frame this board would get right now. The panel
// shows it in a popover so a person can check the screen without walking over.
func (s *Server) PreviewFramePNG(ctx context.Context, deviceID string, scale int) ([]byte, error) {
	if scale < 1 || scale > 4 {
		scale = 2
	}
	loc, err := s.Location(ctx)
	if err != nil {
		return nil, err
	}
	// buildFrame reads the device id from the path and the client address for
	// the weather fallback; a request built here carries the id and no address,
	// so the frame is the one a local fetch would produce.
	r, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"/v1/devices/"+url.PathEscape(deviceID)+"/frame", nil)
	if err != nil {
		return nil, err
	}
	r.SetPathValue("id", deviceID)
	fr, err := s.buildFrame(ctx, r, s.clock().In(loc), -1, false)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := fr.WritePNG(&buf, scale); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ProxySummary is the "current proxy" line the settings window prints: which
// address is in force and where it came from. It is the same string the page's
// settings card shows, so the two windows cannot disagree.
func (s *Server) ProxySummary(ctx context.Context) string {
	return s.proxySettingsSummary(ctx)
}

// SnapshotByName finds a backup by its file name, so the app never passes a
// path around: the name comes from SnapshotList and goes back to it.
func SnapshotByName(snaps []backup.Snapshot, name string) (backup.Snapshot, error) {
	for _, sn := range snaps {
		if sn.Name == name {
			return sn, nil
		}
	}
	return backup.Snapshot{}, errors.New("找不到这个备份")
}

// RestoreSnapshot restores the named backup. The name is resolved against the
// list the app was shown, so a stale button cannot point at an arbitrary path.
func (s *Server) RestoreSnapshot(name string) error {
	snaps, err := s.SnapshotList()
	if err != nil {
		return err
	}
	sn, err := SnapshotByName(snaps, name)
	if err != nil {
		return err
	}
	return s.RestoreBackup(sn.Path)
}

// DeviceByID returns one board, revoked or not: the settings list hides
// revoked boards, but a stale row in the app should still explain itself.
func (s *Server) DeviceByID(ctx context.Context, id string) (store.Device, error) {
	return s.st.Device(ctx, id)
}

// SettingValue reads one setting through the same rules the forms use: the
// timezone always comes back as a name the app can show in a popup.
func (s *Server) SettingValue(ctx context.Context, key string) (string, error) {
	st, err := s.st.Settings(ctx)
	if err != nil {
		return "", err
	}
	if key == "timezone" {
		return displayTimezone(st["timezone"]), nil
	}
	return st[key], nil
}

// PatchSettings is one validated batch write for the app's settings window:
// every key is judged before any is stored, so a bad value in the middle of a
// form cannot leave the earlier fields saved, and the host's settings hook
// fires once for the whole diff.
func (s *Server) PatchSettings(ctx context.Context, diff map[string]string) (map[string]string, error) {
	clean := make(map[string]string, len(diff))
	for k, v := range diff {
		v = strings.TrimSpace(v)
		if err := checkSetting(k, v); err != nil {
			return nil, err
		}
		clean[k] = v
	}
	if len(clean) == 0 {
		return s.st.Settings(ctx)
	}
	return s.UpdateSettings(ctx, clean)
}

// SetSetting is one validated write. The app could post to the HTML form, but
// that would drag the page's markup into the binding; this keeps the checks in
// one place and still fires the host's settings hook, so the tray rebuilds.
func (s *Server) SetSetting(ctx context.Context, key, value string) error {
	value = strings.TrimSpace(value)
	if err := checkSetting(key, value); err != nil {
		return err
	}
	_, err := s.UpdateSettings(ctx, map[string]string{key: value})
	return err
}

// checkSetting is the one place a setting's value is judged. The JSON API
// validates a whole patch with it before writing anything, so a bad value in
// the middle of a form leaves the rest unsaved too.
// settingExists refuses names the store does not know. It is separate from the
// value checks so a caller can answer "no such setting" (400) before it decides
// whether this caller may write the setting at all (403): a reader should not
// learn from a 400 that a host-only key exists and merely got a bad value.
func settingExists(key string) error {
	if strings.HasPrefix(key, "auth_") {
		return fmt.Errorf("口令不能这样改，请在网页的「安全」里改")
	}
	if !store.IsKnownSetting(key) {
		return fmt.Errorf("没有这项设置：%s", key)
	}
	return nil
}

func checkSetting(key, value string) error {
	value = strings.TrimSpace(value)
	if err := settingExists(key); err != nil {
		return err
	}
	switch key {
	case "theme":
		if value != "system" && value != "light" && value != "dark" {
			return fmt.Errorf("主题只能是 system、light 或 dark")
		}
	case "tray_mode":
		if value != "progress" && value != "next" && value != "icon" {
			return fmt.Errorf("菜单栏显示只能是 progress、next 或 icon")
		}
	case "sound_volume":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 100 {
			return fmt.Errorf("音量必须是 0–100")
		}
	case "alarm_lead_minutes":
		switch value {
		case "", "0", "5", "10", "15":
		default:
			return fmt.Errorf("提前提醒只能是 0、5、10 或 15 分钟")
		}
	case "proxy_url":
		if value != "" {
			if err := validateProxyURL(value); err != nil {
				return err
			}
		}
	case "timezone":
		if value != "" {
			if _, err := loadLocation(value); err != nil {
				return fmt.Errorf("找不到时区 %s", value)
			}
		}
	case "update_feed":
		if value != "" && !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
			return fmt.Errorf("更新源必须是 http(s) 地址")
		}
	}
	return nil
}

// SettingsSnapshot returns every setting plus the few derived values the
// settings window shows next to them. It strips the secrets the same way the
// HTML settings page does.
// ZoneChoices is the time zone list the settings popup button offers, with the
// person's current zone (or the system's) at the top.
func (s *Server) ZoneChoices(ctx context.Context) []string {
	st, err := s.st.Settings(ctx)
	if err != nil {
		return zoneChoices("")
	}
	return zoneChoices(st["timezone"])
}

func (s *Server) SettingsSnapshot(ctx context.Context) (map[string]string, error) {
	st, err := s.st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(st))
	for k, v := range st {
		out[k] = v
	}
	delete(out, "auth_password_hash")
	delete(out, "auth_session_key")
	if v := out["weather_api_key"]; v != "" {
		out["weather_api_key"] = maskKey(v)
	}
	out["timezone"] = displayTimezone(st["timezone"])
	return out, nil
}

// AddDevice registers one board. The id is the short name the firmware prints
// and the token is minted next, so a half-configured board exists on purpose.
func (s *Server) AddDevice(ctx context.Context, id, name string) (store.Device, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	name = strings.TrimSpace(name)
	if !deviceIDPattern.MatchString(id) {
		return store.Device{}, fmt.Errorf("设备 ID 只能用小写字母、数字、- 和 _（1–32 位）")
	}
	if name == "" {
		name = id
	}
	if _, err := s.st.Device(ctx, id); err == nil {
		return store.Device{}, fmt.Errorf("设备 %s 已存在", id)
	}
	return s.st.CreateDevice(ctx, id, name, "")
}

// RevokeDevice stops a board from fetching frames. The row stays so the name
// and its history are still there, and the settings list explains it.
func (s *Server) RevokeDevice(ctx context.Context, id string) error {
	if !deviceIDPattern.MatchString(id) {
		return fmt.Errorf("设备 ID 无效")
	}
	return s.st.RevokeDevice(ctx, id)
}

// FirmwareVersion is the version of the image a board would download now: the
// staged upgrade if one waits, otherwise the running build.
func (s *Server) FirmwareVersion(ctx context.Context) string {
	st, err := s.st.Settings(ctx)
	if err != nil {
		return ""
	}
	if v := strings.TrimSpace(st["firmware_pending_version"]); v != "" {
		return v
	}
	return strings.TrimSpace(st["firmware_version"])
}

// UploadFirmware stages a firmware image the person picked in a file dialog.
// The version comes from the file name when it carries one, as "today-0.4.0.bin",
// and from the form field otherwise.
func (s *Server) UploadFirmware(ctx context.Context, path, version string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("没有选择固件文件")
	}
	if err := s.SaveFirmware(path, strings.TrimSpace(version)); err != nil {
		return "", err
	}
	st, err := s.st.Settings(ctx)
	if err != nil {
		return "", err
	}
	return st["firmware_pending_version"], nil
}

// PushFirmware tells every board to upgrade on its next poll.
func (s *Server) PushFirmware(ctx context.Context) (string, error) {
	return s.ApplyPendingFirmware(ctx)
}
