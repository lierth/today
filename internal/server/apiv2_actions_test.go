package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"today/internal/store"
)

// These were HTML-form tests before the React page replaced the templates.
// The rules did not change, so the checks moved with them: same questions,
// asked in JSON.

// The settings snapshot never carries a secret, and a masked weather key is
// still recognisable as "set" by the page.
func TestAPIV2SettingsHidesSecrets(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	if _, err := e.srv.UpdateSettings(context.Background(), map[string]string{
		"weather_api_key": "abcdef1234567890",
		"weather_city":    "北京",
	}); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Settings map[string]string `json:"settings"`
	}
	e.apiJSON("GET", "/api/v2/settings", nil, &out)
	if _, ok := out.Settings["auth_password_hash"]; ok {
		t.Fatal("the password hash left the server")
	}
	if _, ok := out.Settings["auth_session_key"]; ok {
		t.Fatal("the session key left the server")
	}
	masked := out.Settings["weather_api_key"]
	if masked == "" || masked == "abcdef1234567890" {
		t.Fatalf("weather key not masked: %q", masked)
	}
	if !strings.HasSuffix(masked, "7890") {
		t.Fatalf("a mask should keep the last four digits: %q", masked)
	}
}

// A bad value anywhere in the patch refuses the whole patch: a settings form
// must not save half of itself.
func TestAPIV2SettingsPatchIsAllOrNothing(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	// Only theme and colour-blind are web-writable, so the two failure modes a
	// browser can reach are an unknown name and a bad value for a name it owns.
	before, _ := e.st.Setting(context.Background(), "color_blind")

	resp := e.apiCall("PATCH", "/api/v2/settings", map[string]string{
		"color_blind":      "true",
		"alarm_lead_weeks": "3",
	})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown key: %d, want 400", resp.StatusCode)
	}
	if after, _ := e.st.Setting(context.Background(), "color_blind"); after != before {
		t.Fatalf("half a patch was saved: color_blind %q → %q", before, after)
	}

	resp = e.apiCall("PATCH", "/api/v2/settings", map[string]string{
		"color_blind": "true",
		"theme":       "neon",
	})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad theme: %d, want 400", resp.StatusCode)
	}
	if after, _ := e.st.Setting(context.Background(), "color_blind"); after != before {
		t.Fatalf("half a patch was saved: color_blind %q → %q", before, after)
	}
}

// Backups are the data tab's job, and the tab talks to the app's bindings,
// not to HTTP: a Funnel visitor must not be able to rewrite the store. The
// methods still have to work, which is what this pins.
func TestBackupAndList(t *testing.T) {
	e := newTestEnv(t)
	if err := e.srv.RunBackup("2026-11-19"); err != nil {
		t.Fatalf("backup: %v", err)
	}
	listed, err := e.srv.SnapshotList()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) == 0 {
		t.Fatal("the backup we just made is not in the list")
	}
	// The HTTP door is shut even for a signed-in browser.
	e.login(e.password())
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v2/backups"},
		{"GET", "/api/v2/backups"},
		{"POST", "/api/v2/backups/restore"},
		{"PUT", "/api/v2/listen"},
		{"POST", "/api/v2/devices"},
		{"POST", "/api/v2/devices/dev1/token"},
		{"POST", "/api/v2/devices/dev1/revoke"},
		{"GET", "/api/v2/devices/dev1/preview.png"},
		{"POST", "/api/v2/firmware"},
		{"PUT", "/api/v2/firmware"},
		{"POST", "/api/v2/firmware/push"},
	} {
		resp := e.apiCall(c.method, c.path, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404 (host-only action)", c.method, c.path, resp.StatusCode)
		}
	}
}

// The token endpoint answers the four setup lines in one call, because the
// token is shown once and never again.
func TestAPIV2DeviceTokenAndRevoke(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	var device struct {
		ID string `json:"id"`
	}
	created, err := e.srv.AddDevice(context.Background(), "dev1", "桌上")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	device.ID = created.ID
	if device.ID != "dev1" {
		t.Fatalf("created %q", device.ID)
	}

	var tok struct {
		Token    string   `json:"token"`
		Hint     string   `json:"hint"`
		Commands []string `json:"commands"`
	}
	result, err := e.srv.GenerateDeviceToken(context.Background(), "dev1")
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	tok.Token, tok.Hint, tok.Commands = result.Token, result.Hint, result.Commands
	if len(tok.Token) < 8 {
		t.Fatalf("token too short: %q", tok.Token)
	}
	if !strings.Contains(tok.Hint, tok.Token[len(tok.Token)-4:]) {
		t.Fatalf("hint %q does not show the tail of %q", tok.Hint, tok.Token)
	}
	if len(tok.Commands) != 4 {
		t.Fatalf("setup lines = %d, want 4: %v", len(tok.Commands), tok.Commands)
	}
	for _, want := range []string{"wifi ", "server ", "device dev1", "token " + tok.Token} {
		found := false
		for _, line := range tok.Commands {
			if strings.HasPrefix(line, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing setup line %q in %v", want, tok.Commands)
		}
	}

	// The id is the frame URL, so it is lowercased rather than refused: a
	// person typing "Dev2" gets the board they meant.
	upper, err := e.srv.AddDevice(context.Background(), "Dev2", "")
	if err != nil {
		t.Fatalf("uppercase id: %v", err)
	}
	if upper.ID != "dev2" {
		t.Fatalf("uppercase id became %q", upper.ID)
	}
	// An id with a space is still refused, because no URL can carry it.
	if _, err := e.srv.AddDevice(context.Background(), "dev 3", ""); err == nil {
		t.Fatal("an id with a space was accepted")
	}

	if err := e.srv.RevokeDevice(context.Background(), "dev1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	devices, _ := e.st.Devices(context.Background())
	for _, d := range devices {
		if d.ID == "dev1" && d.RevokedAt == nil {
			t.Fatal("revoke left the device live")
		}
	}
}

// The preview is the frame the board would fetch next, as a data URL.
func TestAPIV2DevicePreviewPNG(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	if _, err := e.st.CreateDevice(context.Background(), "dev1", "桌上", ""); err != nil {
		t.Fatal(err)
	}
	pngBytes, err := e.srv.PreviewFramePNG(context.Background(), "dev1", 2)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	// The app shows it as a data URL; the padding has to survive that trip.
	raw := base64.StdEncoding.EncodeToString(pngBytes)
	if got := strings.TrimPrefix("data:image/png;base64,"+raw, "data:image/png;base64,"); got == "" {
		t.Fatalf("not a data URL: %.40s", raw)
	}
	img, err := png.Decode(base64.NewDecoder(base64.StdEncoding, strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("preview is not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 800 || b.Dy() != 600 {
		t.Fatalf("preview is %dx%d, want 800x600 at scale 2", b.Dx(), b.Dy())
	}
}

// The firmware upload belongs to the Mac app's settings window. It has to
// validate before it replaces anything, and the push is what actually hands
// the image to the boards; the HTTP door stays shut.
func TestFirmwareUploadAndPush(t *testing.T) {
	e := newTestEnv(t)

	body := make([]byte, 4096)
	body[0] = 0xE9
	binary.LittleEndian.PutUint16(body[12:14], 9)
	binary.LittleEndian.PutUint32(body[32:36], 0xABCD5432)
	if err := e.srv.StageFirmware(bytes.NewReader(body), "1.2.3"); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := os.Stat(e.srv.firmwareSlotPath("a")); err != nil {
		t.Fatalf("firmware not on disk: %v", err)
	}

	// A file without the ESP32 header is refused before it replaces anything.
	if err := e.srv.StageFirmware(strings.NewReader("not a firmware"), ""); err == nil {
		t.Fatal("garbage upload was accepted")
	}
	merged := append([]byte(nil), body...)
	binary.LittleEndian.PutUint32(merged[32:36], 0x50)
	if err := e.srv.StageFirmware(bytes.NewReader(merged), "1.2.4"); err == nil {
		t.Fatal("a USB bootloader image was accepted for OTA")
	}
	if err := e.srv.StageFirmware(bytes.NewReader(body), "rlcd-1.2.4"); err == nil {
		t.Fatal("a version the board cannot compare was accepted")
	}
	staged, err := os.ReadFile(e.srv.firmwareSlotPath("a"))
	if err != nil || !bytes.Equal(staged, body) {
		t.Fatalf("a rejected upload changed the staged image: %v", err)
	}

	version, err := e.srv.ApplyPendingFirmware(context.Background())
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if version != "1.2.3" {
		t.Fatalf("pushed %q", version)
	}

	e.login(e.password())
	resp := e.apiCall("PUT", "/api/v2/firmware", map[string]string{})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("firmware over HTTP: %d, want 404", resp.StatusCode)
	}
}

func TestFirmwareStagingPreservesLiveImage(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if _, err := e.st.EnsureDevice(ctx, "fwtest", "firmware test"); err != nil {
		t.Fatal(err)
	}
	token := "firmware-test-token"
	sum := sha256.Sum256([]byte(token))
	if err := e.st.SetDeviceToken(ctx, "fwtest", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	image := func(marker byte) []byte {
		b := make([]byte, 4096)
		b[0], b[300] = 0xE9, marker
		binary.LittleEndian.PutUint16(b[12:14], 9)
		binary.LittleEndian.PutUint32(b[32:36], 0xABCD5432)
		return b
	}
	check := func(want []byte, version string) {
		t.Helper()
		req := e.req(http.MethodGet, e.ts.URL+"/v1/devices/fwtest/firmware", nil)
		req.Header.Set("X-Device-Token", token)
		resp := e.do(req)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusOK || !bytes.Equal(body, want) || resp.Header.Get("X-Firmware-Version") != version {
			t.Fatalf("live firmware: status=%d version=%q correct bytes=%v err=%v", resp.StatusCode, resp.Header.Get("X-Firmware-Version"), bytes.Equal(body, want), err)
		}
	}
	// Preserve the original single-file format on an existing installation.
	live := image(1)
	if err := os.MkdirAll(e.srv.cfg.Paths.Firmware, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.srv.firmwarePath(), live, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.st.PutSettings(ctx, map[string]string{"firmware_version": "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	version := "1.0.0"
	for i, next := range []string{"2.0.0", "3.0.0", "4.0.0"} {
		staged := image(byte(i + 2))
		if err := e.srv.StageFirmware(bytes.NewReader(staged), next); err != nil {
			t.Fatal(err)
		}
		check(live, version)
		if _, err := e.srv.ApplyPendingFirmware(ctx); err != nil {
			t.Fatal(err)
		}
		check(staged, next)
		live, version = staged, next
	}
}

// Funnel and local-network buttons belong to the Mac app: the browser can see
// their state but must not be able to ask the machine to open a tunnel. The
// app's own writes still land, and they still fire the host hook.
func TestHostRequestsAreMacOnly(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	ctx := context.Background()

	resp := e.apiCall("PATCH", "/api/v2/settings", map[string]string{
		"funnel":         "true",
		"funnel_request": "1",
	})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("funnel over HTTP: %d, want 403", resp.StatusCode)
	}
	if v, _ := e.st.Setting(ctx, "funnel"); v == "true" {
		t.Fatal("the refused patch was written anyway")
	}

	resp = e.apiCall("PATCH", "/api/v2/settings", map[string]string{"localnet_request": "2"})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("localnet over HTTP: %d, want 403", resp.StatusCode)
	}

	// The app's binding path is the one that may write these.
	if err := e.srv.SetSetting(ctx, "funnel_request", "3"); err != nil {
		t.Fatalf("host write: %v", err)
	}
	if v, _ := e.st.Setting(ctx, "funnel_request"); v != "3" {
		t.Fatalf("funnel_request = %q", v)
	}
}

// The item list and the day header are one call, so the page never shows a
// date from one response and rows from another.
func TestAPIV2TodayMatchesDay(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	ctx := context.Background()
	loc, _ := e.srv.Location(ctx)
	day, _, _ := e.srv.Today(ctx)
	at := time.Date(day.Year(), day.Month(), day.Day(), 9, 30, 0, 0, loc)
	if _, err := e.st.CreateItem(ctx, store.Item{Title: "站会", StartsAt: &at}, loc); err != nil {
		t.Fatal(err)
	}

	var today struct {
		Day struct {
			Key   string `json:"key"`
			Label string `json:"label"`
		} `json:"day"`
		Items []struct {
			Title     string `json:"title"`
			DateLocal string `json:"date_local"`
		} `json:"items"`
		Total int `json:"total"`
	}
	e.apiJSON("GET", "/api/v2/today", nil, &today)
	if today.Total != 1 || len(today.Items) != 1 {
		t.Fatalf("total = %d, items = %d", today.Total, len(today.Items))
	}
	if today.Items[0].DateLocal != today.Day.Key {
		t.Fatalf("row date %q under header %q", today.Items[0].DateLocal, today.Day.Key)
	}
	if today.Day.Label == "" {
		t.Fatal("no date label")
	}
}

// Moving the listener to a port nobody else holds works. It is a Mac app
// action (the settings window), so the test calls the method the binding
// calls; HTTP never reaches it.
func TestListenMovesTheServer(t *testing.T) {
	e := newTestEnv(t)
	before := e.srv.Addr()

	free := freeAddr(t)
	if err := e.srv.SetListenAddr(free); err != nil {
		t.Skipf("could not bind %s here: %v", free, err)
	}
	if e.srv.Addr() == before {
		t.Fatalf("listener did not move from %s", before)
	}
	if !strings.HasSuffix(e.srv.Addr(), free[strings.LastIndex(free, ":"):]) {
		t.Fatalf("listener at %s, asked for %s", e.srv.Addr(), free)
	}
}

// freeAddr asks the kernel for a port, then lets it go.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no free port available")
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// The path that used to be an HTML page is now the app shell; a stranger gets
// the login form instead (the page route is behind requireSession).
func TestShellNeedsSession(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password()
	noFollow := &http.Client{
		Jar:           e.client.Jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, path := range []string{"/", "/upcoming", "/history", "/search?q=x"} {
		resp, err := noFollow.Get(e.ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login" {
			t.Fatalf("%s without a session: %d %q", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}

	// The login form itself is served without a session and answers a wrong
	// password on the same page.
	form, err := noFollow.Get(e.ts.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(form.Body)
	_ = form.Body.Close()
	if form.StatusCode != http.StatusOK || !strings.Contains(string(body), "口令") {
		t.Fatalf("login page: %d %.80s", form.StatusCode, body)
	}

	e.login(pw)
	resp, err := noFollow.Get(e.ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `id="root"`) {
		t.Fatalf("app shell: %d %.120s", resp.StatusCode, body)
	}
}
