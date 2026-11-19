package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"today/internal/frame"
	"today/internal/store"
	"today/internal/weather"
)

type testEnv struct {
	t      *testing.T
	st     *store.Store
	srv    *Server
	ts     *httptest.Server
	client *http.Client
	loc    *time.Location
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvCloud(t, "")
}

// newTestEnvCloud is newTestEnv with an injected iCloud folder. Every test that
// exercises the cloud path must use a temp directory: the real one belongs to
// the person, not to a test.
func newTestEnvCloud(t *testing.T, cloudDocs string) *testEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "today.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := New(st, Config{
		Addr: "127.0.0.1:0", DataDir: dir, Dev: true,
		Logger: log.New(io.Discard, "", 0),
		Paths: Paths{
			Root: dir, DB: filepath.Join(dir, "today.db"),
			Backups: filepath.Join(dir, "backups"), Firmware: filepath.Join(dir, "firmware"),
			// No iCloud in tests unless the caller injects a temp folder:
			// "" means none.
			CloudDocs: cloudDocs,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	// Tests must not depend on the host's TZ: pin the app timezone so
	// X-UTC-Offset and local dates are the same under TZ=UTC.
	if err := st.PutSettings(context.Background(), map[string]string{"timezone": "Asia/Shanghai"}); err != nil {
		t.Fatal(err)
	}
	// The test client is a script: it sends the header that checkWriteOrigin
	// requires from a caller with no Sec-Fetch-Site and no Origin. A real
	// browser supplies one of those instead.
	plain := &http.Client{Jar: jar}
	client := &http.Client{Jar: jar, Transport: clientHeaderTransport{base: plain.Transport}}
	return &testEnv{t: t, st: st, srv: srv, ts: ts, loc: loc, client: client}
}

func (e *testEnv) password() string {
	e.t.Helper()
	pw, err := e.srv.EnsureAuth(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	if pw == "" {
		e.t.Fatal("no password generated")
	}
	return pw
}

func (e *testEnv) req(method, target string, body io.Reader) *http.Request {
	e.t.Helper()
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		e.t.Fatal(err)
	}
	return req
}

func (e *testEnv) do(req *http.Request) *http.Response {
	e.t.Helper()
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func (e *testEnv) login(pw string) *http.Response {
	e.t.Helper()
	resp, err := e.client.PostForm(e.ts.URL+"/login", url.Values{"password": {pw}})
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func (e *testEnv) get(path string) *http.Response {
	e.t.Helper()
	return e.do(e.req(http.MethodGet, e.ts.URL+path, nil))
}

// body fetches a page as the logged-in session and returns its bytes.
func (e *testEnv) body(path string) []byte {
	e.t.Helper()
	resp := e.get(path)
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("GET %s: %d", path, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func TestAuthFlow(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password()

	noFollow := &http.Client{
		Jar:           e.client.Jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := noFollow.Get(e.ts.URL + "/app")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login" {
		t.Fatalf("unauthenticated /app: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	apiResp, err := noFollow.Get(e.ts.URL + "/api/items")
	if err != nil {
		t.Fatal(err)
	}
	_ = apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated api: %d", apiResp.StatusCode)
	}

	bad := e.login("wrong-password")
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", bad.StatusCode)
	}

	ok := e.login(pw)
	if ok.StatusCode != http.StatusOK || ok.Request.URL.Path != "/app" {
		t.Fatalf("login: %d at %s", ok.StatusCode, ok.Request.URL.Path)
	}
	body, _ := io.ReadAll(ok.Body)
	if !strings.Contains(string(body), "今天") {
		t.Fatal("app page does not render the day")
	}

	health := e.get("/healthz")
	if health.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %d", health.StatusCode)
	}
}

func TestFrameETagAndAcks(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if _, err := e.st.EnsureDevice(ctx, "dev1", "测试设备"); err != nil {
		t.Fatal(err)
	}
	token := "device-token-for-tests"
	sum := sha256.Sum256([]byte(token))
	if err := e.st.SetDeviceToken(ctx, "dev1", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(e.loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, e.loc)
	it, err := e.st.CreateItem(ctx, store.Item{Title: "会议", StartsAt: &start}, e.loc)
	if err != nil {
		t.Fatal(err)
	}

	frameReq := func(etag string) *http.Response {
		req := e.req(http.MethodGet, e.ts.URL+"/v1/devices/dev1/frame", nil)
		req.Header.Set("X-Device-Token", token)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		return e.do(req)
	}

	resp := frameReq("")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("frame: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) < 1000 {
		t.Fatalf("frame too small: %d bytes", len(body))
	}
	etag := resp.Header.Get("ETag")
	if etag == "" || resp.Header.Get("X-Next-Wake-At") == "" || resp.Header.Get("X-Frame-Seq") == "" {
		t.Fatalf("missing headers: %v", resp.Header)
	}
	if resp.Header.Get("X-Refresh") != "1" {
		t.Fatalf("first frame should force a repaint, got X-Refresh %q", resp.Header.Get("X-Refresh"))
	}

	if off := resp.Header.Get("X-UTC-Offset"); off != "28800" {
		t.Fatalf("X-UTC-Offset on 200: %q", off)
	}
	if night := resp.Header.Get("X-Night"); night != "1380-420" {
		t.Fatalf("X-Night on 200: %q", night)
	}
	notModified := frameReq(etag)
	if notModified.StatusCode != http.StatusNotModified {
		t.Fatalf("if-none-match: %d", notModified.StatusCode)
	}
	if off := notModified.Header.Get("X-UTC-Offset"); off != "28800" {
		t.Fatalf("X-UTC-Offset on 304: %q", off)
	}
	if night := notModified.Header.Get("X-Night"); night != "1380-420" {
		t.Fatalf("X-Night on 304: %q", night)
	}
	if b, _ := io.ReadAll(notModified.Body); len(b) != 0 {
		t.Fatalf("304 body: %d bytes", len(b))
	}
	if notModified.Header.Get("X-Refresh") != "0" {
		t.Fatalf("304 should not force a repaint: %q", notModified.Header.Get("X-Refresh"))
	}
	e.srv.ForceFullRefresh()
	forced := frameReq(etag)
	if forced.StatusCode != http.StatusOK || forced.Header.Get("X-Refresh") != "1" {
		t.Fatalf("forced refresh: %d %q", forced.StatusCode, forced.Header.Get("X-Refresh"))
	}
	forcedBody, _ := io.ReadAll(forced.Body)
	if len(forcedBody) < 1000 || forcedBody[6]&frame.FlagForceFull == 0 {
		t.Fatalf("forced frame body: %d flags=%#x", len(forcedBody), forcedBody[6])
	}
	followUp := frameReq(forced.Header.Get("ETag"))
	if followUp.StatusCode != http.StatusNotModified {
		t.Fatalf("frame after forced repaint: %d", followUp.StatusCode)
	}

	// A device wakes with a blank panel and asks for force=1: the cached ETag
	// still matches, so only this branch can get it the bits.
	clientForced := e.req(http.MethodGet, e.ts.URL+"/v1/devices/dev1/frame?force=1", nil)
	clientForced.Header.Set("X-Device-Token", token)
	clientForced.Header.Set("If-None-Match", forced.Header.Get("ETag"))
	clientResp := e.do(clientForced)
	if clientResp.StatusCode != http.StatusOK {
		t.Fatalf("force=1 with a matching etag: %d", clientResp.StatusCode)
	}
	clientBody, _ := io.ReadAll(clientResp.Body)
	if len(clientBody) < 1000 || clientBody[6]&frame.FlagForceFull == 0 {
		t.Fatalf("force=1 body: %d flags=%#x", len(clientBody), clientBody[6])
	}
	if clientResp.Header.Get("X-Refresh") != "1" {
		t.Fatalf("force=1 should force a repaint: %q", clientResp.Header.Get("X-Refresh"))
	}
	// The force was one request only: the next wake is a plain 304 again.
	afterForce := frameReq(clientResp.Header.Get("ETag"))
	if afterForce.StatusCode != http.StatusNotModified {
		t.Fatalf("frame after force=1: %d", afterForce.StatusCode)
	}

	acks := func(ops []map[string]string) ackResponse {
		payload, _ := json.Marshal(map[string]any{"frame_seq": 1, "battery_mv": 3980, "ops": ops})
		req := e.req(http.MethodPost, e.ts.URL+"/v1/devices/dev1/acks", bytes.NewReader(payload))
		req.Header.Set("X-Device-Token", token)
		req.Header.Set("Content-Type", "application/json")
		r := e.do(req)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("acks: %d", r.StatusCode)
		}
		var out ackResponse
		if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	op := map[string]string{"op_id": "op-1", "item_id": it.ID, "status": store.StatusDone}
	if got := acks([]map[string]string{op}); got.Applied != 1 || got.Duplicates != 0 {
		t.Fatalf("first ack: %+v", got)
	}
	if got := acks([]map[string]string{op}); got.Applied != 0 || got.Duplicates != 1 {
		t.Fatalf("duplicate ack: %+v", got)
	}
	if got := acks([]map[string]string{{"op_id": "op-2", "item_id": it.ID, "status": "bogus"}}); got.Skipped != 1 {
		t.Fatalf("bad status ack: %+v", got)
	}
	if stored, err := e.st.Item(ctx, it.ID); err != nil || !stored.Done() {
		t.Fatalf("status not applied: %+v %v", stored, err)
	}
	if d, err := e.st.Device(ctx, "dev1"); err != nil || d.BatteryMV != 3980 {
		t.Fatalf("battery: %+v %v", d, err)
	}

	for _, tk := range []string{"", "wrong"} {
		req := e.req(http.MethodGet, e.ts.URL+"/v1/devices/dev1/frame", nil)
		if tk != "" {
			req.Header.Set("X-Device-Token", tk)
		}
		r := e.do(req)
		if r.StatusCode != http.StatusNotFound {
			t.Fatalf("token %q: %d", tk, r.StatusCode)
		}
	}
}

func TestItemsAPI(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())

	create := func(body string) *http.Response {
		req := e.req(http.MethodPost, e.ts.URL+"/api/items", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		return e.do(req)
	}
	resp := create(`{"title":"写服务端测试","all_day":true,"date_local":"2026-11-17"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	var it store.Item
	if err := json.NewDecoder(resp.Body).Decode(&it); err != nil {
		t.Fatal(err)
	}
	if it.ID == "" || it.Title != "写服务端测试" || !it.AllDay {
		t.Fatalf("created: %+v", it)
	}

	list := e.get("/api/items?scope=all")
	if list.StatusCode != http.StatusOK {
		t.Fatalf("list: %d", list.StatusCode)
	}
	var items []store.Item
	if err := json.NewDecoder(list.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != it.ID {
		t.Fatalf("list: %+v", items)
	}

	patchReq := e.req(http.MethodPatch, e.ts.URL+"/api/items/"+it.ID, strings.NewReader(`{"status":"done"}`))
	patchReq.Header.Set("Content-Type", "application/json")
	patch := e.do(patchReq)
	if patch.StatusCode != http.StatusOK {
		t.Fatalf("patch: %d", patch.StatusCode)
	}
	if stored, err := e.st.Item(context.Background(), it.ID); err != nil || !stored.Done() {
		t.Fatalf("patch stored: %+v %v", stored, err)
	}

	del := e.do(e.req(http.MethodDelete, e.ts.URL+"/api/items/"+it.ID, nil))
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", del.StatusCode)
	}
	if _, err := e.st.Item(context.Background(), it.ID); err == nil {
		t.Fatal("item still readable")
	}
}

func TestSettingsUpdateRotatesSessions(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password()
	e.login(pw)

	var calls int
	var lastOld, lastNew map[string]string
	e.srv.OnSettingsChanged(func(old, new map[string]string) {
		calls++
		lastOld, lastNew = old, new
	})

	form := url.Values{"action": {"general"}, "timezone": {"Asia/Shanghai"}, "tray_mode": {"icon"}}
	resp, err := e.client.PostForm(e.ts.URL+"/api/settings", form)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Fatalf("callback calls: %d", calls)
	}
	if lastOld["tray_mode"] != "progress" || lastNew["tray_mode"] != "icon" {
		t.Fatalf("callback diff: old=%v new=%v", lastOld, lastNew)
	}
	if v, _ := e.st.Setting(context.Background(), "tray_mode"); v != "icon" {
		t.Fatalf("stored tray_mode: %q", v)
	}

	page := e.get("/settings")
	html, _ := io.ReadAll(page.Body)
	re := regexp.MustCompile(`type="password" name="([^"]+)"`)
	m := re.FindSubmatch(html)
	if m == nil {
		t.Fatal("password field not found")
	}
	field := string(m[1])

	change, err := e.client.PostForm(e.ts.URL+"/api/settings", url.Values{
		"action": {"password"},
		field:    {"new-password-123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = change.Body.Close()
	if change.StatusCode != http.StatusOK {
		t.Fatalf("password change: %d", change.StatusCode)
	}

	// The old session cookie signed with the previous key is no longer accepted.
	after := e.get("/app")
	if after.StatusCode != http.StatusOK || after.Request.URL.Path != "/login" {
		t.Fatalf("old session survived rotation: %d at %s", after.StatusCode, after.Request.URL.Path)
	}
	if resp := e.login("new-password-123"); resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/app" {
		t.Fatalf("login with new password: %d at %s", resp.StatusCode, resp.Request.URL.Path)
	}
}

// The device draws its own cursor, so the frame endpoint must ignore any
// cursor query and always return the neutral frame (the ETag stays stable).
func TestDeviceFrameIgnoresCursor(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if _, err := e.st.EnsureDevice(ctx, "dev1", "测试设备"); err != nil {
		t.Fatal(err)
	}
	token := "cursor-token"
	sum := sha256.Sum256([]byte(token))
	if err := e.st.SetDeviceToken(ctx, "dev1", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	get := func(cursor int) *http.Response {
		u := e.ts.URL + "/v1/devices/dev1/frame"
		if cursor >= 0 {
			u += "?cursor=" + strconv.Itoa(cursor)
		}
		req := e.req(http.MethodGet, u, nil)
		req.Header.Set("X-Device-Token", token)
		return e.do(req)
	}
	// The first response consumes the server-start forced repaint; compare
	// the steady state after it.
	_ = get(-1)
	plain := get(-1)
	if plain.StatusCode != http.StatusOK {
		t.Fatalf("frame: %d", plain.StatusCode)
	}
	body, _ := io.ReadAll(plain.Body)
	withCursor := get(2)
	if withCursor.StatusCode != http.StatusOK {
		t.Fatalf("frame with cursor: %d", withCursor.StatusCode)
	}
	other, _ := io.ReadAll(withCursor.Body)
	if !bytes.Equal(body, other) {
		t.Fatal("cursor query changed the frame; the device draws its own cursor")
	}
	if withCursor.Header.Get("ETag") != plain.Header.Get("ETag") {
		t.Fatalf("cursor query changed the ETag: %q vs %q",
			withCursor.Header.Get("ETag"), plain.Header.Get("ETag"))
	}
}

func TestPreviewPNG(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	if _, err := e.st.EnsureDevice(context.Background(), "dev1", "预览"); err != nil {
		t.Fatal(err)
	}
	resp := e.get("/api/devices/dev1/preview.png?scale=2")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type: %q", ct)
	}
	img, err := png.Decode(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 800 || img.Bounds().Dy() != 600 {
		t.Fatalf("preview size: %v", img.Bounds())
	}
}

func TestFunnelContinuePokesHost(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	req := e.req(http.MethodPost, e.ts.URL+"/api/settings",
		strings.NewReader(url.Values{
			"action":          {"general"},
			"funnel_continue": {"1"},
		}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := e.do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("funnel_continue: %d", resp.StatusCode)
	}
	st, err := e.st.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st["funnel"] != "true" || st["funnel_request"] == "" {
		t.Fatalf("host was not poked: funnel=%q request=%q", st["funnel"], st["funnel_request"])
	}
}

func TestLocalNetworkRequestPokesHost(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	req := e.req(http.MethodPost, e.ts.URL+"/api/settings",
		strings.NewReader(url.Values{
			"action": {"localnet_request"},
		}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := e.do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("localnet_request: %d", resp.StatusCode)
	}
	st, err := e.st.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st["localnet_request"] == "" {
		t.Fatalf("host was not poked: request=%q", st["localnet_request"])
	}
}

// A denied local network permission is invisible from the browser; the page
// must say so above the device list, and stay quiet once it is granted.
func TestSettingsShowsLocalNetworkWarning(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	if err := e.st.PutSettings(context.Background(), map[string]string{
		"localnet_state":  "denied",
		"localnet_detail": "macOS 拒绝了本地网络访问（10.0.0.2）。",
	}); err != nil {
		t.Fatal(err)
	}
	resp := e.get("/settings")
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("设备无法连入")) {
		t.Fatalf("denied state should warn: %s", body)
	}
	if !bytes.Contains(body, []byte("申请本地网络权限")) {
		t.Fatalf("denied state should offer the button: %s", body)
	}
	if err := e.st.PutSettings(context.Background(), map[string]string{
		"localnet_state": "ok",
	}); err != nil {
		t.Fatal(err)
	}
	resp = e.get("/settings")
	body, _ = io.ReadAll(resp.Body)
	if bytes.Contains(body, []byte("设备无法连入")) {
		t.Fatalf("granted state must be silent: %s", body)
	}
}

// A web password reset must reach the host so its keychain copy is replaced;
// the server cannot import the keychain, so the hook is the seam.
func TestSetPasswordCallsHook(t *testing.T) {
	e := newTestEnv(t)
	var got string
	e.srv.SetPasswordHook(func(pw string) { got = pw })
	if err := e.srv.SetPassword(context.Background(), "new-password-123"); err != nil {
		t.Fatal(err)
	}
	if got != "new-password-123" {
		t.Fatalf("hook got %q", got)
	}
	ok, err := e.srv.CheckPassword(context.Background(), "new-password-123")
	if err != nil || !ok {
		t.Fatalf("CheckPassword: %v %v", ok, err)
	}
	if ok, _ := e.srv.CheckPassword(context.Background(), "old-password-123"); ok {
		t.Fatal("the old password must stop working")
	}
}

func TestSettingsGetHidesSecrets(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	resp := e.get("/api/settings")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings get: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, key := range []string{"auth_password_hash", "auth_session_key"} {
		if strings.Contains(string(body), key) {
			t.Fatalf("settings leaked %s: %s", key, body)
		}
	}
}

func TestFirmwareUploadAndDownload(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())

	image := append([]byte{0xE9}, bytes.Repeat([]byte{0x5A}, 4096)...)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("firmware", "today.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(image); err != nil {
		t.Fatal(err)
	}
	_ = mw.WriteField("action", "upload_firmware")
	_ = mw.WriteField("firmware_version", "1.2.3")
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := e.req("POST", e.ts.URL+"/api/settings", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp := e.do(req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: %d %s", resp.StatusCode, msg)
	}
	if _, err := os.Stat(filepath.Join(e.srv.cfg.Paths.Firmware, "firmware.bin")); err != nil {
		t.Fatalf("firmware file: %v", err)
	}

	ctx := context.Background()
	if _, err := e.st.EnsureDevice(ctx, "dev1", "设备"); err != nil {
		t.Fatal(err)
	}
	token := "firmware-test-token"
	if err := e.st.SetDeviceToken(ctx, "dev1", tokenHash(token)); err != nil {
		t.Fatal(err)
	}

	frameReq, _ := http.NewRequest("GET", e.ts.URL+"/v1/devices/dev1/frame", nil)
	frameReq.Header.Set("X-Device-Token", token)
	frameResp := e.do(frameReq)
	defer frameResp.Body.Close()
	if frameResp.StatusCode != http.StatusOK {
		t.Fatalf("frame: %d", frameResp.StatusCode)
	}
	if got := frameResp.Header.Get("X-Firmware-Version"); got != "1.2.3" {
		t.Fatalf("frame firmware version %q", got)
	}

	get := func(etag string) *http.Response {
		r, _ := http.NewRequest("GET", e.ts.URL+"/v1/devices/dev1/firmware", nil)
		r.Header.Set("X-Device-Token", token)
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		return e.do(r)
	}
	res := get("")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("download: %d", res.StatusCode)
	}
	got, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, image) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(image))
	}
	etag := res.Header.Get("ETag")
	if etag == "" {
		t.Fatal("missing etag")
	}
	again := get(etag)
	defer again.Body.Close()
	if again.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional download: %d", again.StatusCode)
	}

	bad, _ := http.NewRequest("GET", e.ts.URL+"/v1/devices/dev1/firmware", nil)
	bad.Header.Set("X-Device-Token", "wrong")
	badResp := e.do(bad)
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusNotFound {
		t.Fatalf("bad token: %d", badResp.StatusCode)
	}
}

func TestPanelToken(t *testing.T) {
	e := newTestEnv(t)
	if _, err := e.st.CreateItem(context.Background(), store.Item{
		Title: "面板事项", AllDay: true, DateLocal: time.Now().In(e.loc).Format(store.DateLayout),
	}, e.loc); err != nil {
		t.Fatal(err)
	}
	token, err := e.srv.PanelToken()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(e.ts.URL + "/panel?t=" + url.QueryEscape(token))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("面板事项")) {
		t.Fatalf("panel: %d %s", resp.StatusCode, body)
	}
	bad, err := http.Get(e.ts.URL + "/panel?t=nope")
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad panel token: %d", bad.StatusCode)
	}
	key, err := e.srv.loadSessionKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expired := strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10) + "." + panelMAC(key, time.Now().Add(-time.Minute).Unix())
	old, err := http.Get(e.ts.URL + "/panel?t=" + url.QueryEscape(expired))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Body.Close()
	if old.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired panel token: %d", old.StatusCode)
	}
}

// The panel token is read-only but carries no session; a leaked link must not
// be readable through the public Funnel just because tailscaled proxies it
// from 127.0.0.1.
func TestPanelRejectsFunnelProxy(t *testing.T) {
	e := newTestEnv(t)
	token, err := e.srv.PanelToken()
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, e.ts.URL+"/panel?t="+url.QueryEscape(token), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Tailscale-Funnel-Request", "?1")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("panel through Funnel: %d", resp.StatusCode)
	}
}

func TestPanelShowsTodayWithoutCancelled(t *testing.T) {
	e := newTestEnv(t)
	now := time.Now().In(e.loc)
	today := now.Format(store.DateLayout)
	if _, err := e.st.CreateItem(context.Background(), store.Item{Title: "面板里的会议", AllDay: true, DateLocal: today}, e.loc); err != nil {
		t.Fatal(err)
	}
	cancelled, err := e.st.CreateItem(context.Background(), store.Item{Title: "已取消", AllDay: true, DateLocal: today}, e.loc)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetStatus(context.Background(), cancelled.ID, store.StatusCancelled); err != nil {
		t.Fatal(err)
	}
	token, err := e.srv.PanelToken()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := e.client.Get(e.ts.URL + "/panel?t=" + url.QueryEscape(token))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("panel: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("面板里的会议")) || bytes.Contains(body, []byte("已取消")) {
		t.Fatalf("panel body missing today's item or leaked cancelled: %s", body)
	}
	req, _ := http.NewRequest("GET", e.ts.URL+"/panel?t="+url.QueryEscape(token), nil)
	req.Header.Set("HX-Request", "true")
	part, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer part.Body.Close()
	if part.StatusCode != http.StatusOK {
		t.Fatalf("panel fragment: %d", part.StatusCode)
	}
	partBody, _ := io.ReadAll(part.Body)
	if bytes.Contains(partBody, []byte("<!doctype")) || !bytes.Contains(partBody, []byte("面板里的会议")) {
		t.Fatalf("panel fragment: %s", partBody)
	}
}

func TestTodayListCarriesProgressSwap(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password()
	e.login(pw)
	day, _, err := e.srv.Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateItem(context.Background(), store.Item{Title: "带进度", AllDay: true, DateLocal: day.Format(store.DateLayout)}, e.loc); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"title": {"局部刷新"}, "view": {"today"}}
	req := e.req(http.MethodPost, e.ts.URL+"/api/items", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp := e.do(req)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte(`id="today-list"`)) {
		t.Fatalf("today list missing: %s", body)
	}
	if !bytes.Contains(body, []byte(`id="today-progress"`)) || !bytes.Contains(body, []byte(`hx-swap-oob`)) {
		t.Fatalf("progress should ride along out of band: %s", body)
	}
	if !bytes.Contains(body, []byte("0 / 2")) {
		t.Fatalf("progress total wrong: %s", body)
	}
}

func TestSettingsPanelIsTheSwapTarget(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password()
	e.login(pw)

	page, err := e.client.Get(e.ts.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	html, _ := io.ReadAll(page.Body)
	if !bytes.Contains(html, []byte(`id="settings-panel"`)) {
		t.Fatal("settings page has no #settings-panel element to target")
	}

	form := url.Values{"action": {"device_token"}, "device": {"dev1"}}
	if _, err := e.st.EnsureDevice(context.Background(), "dev1", "开发设备"); err != nil {
		t.Fatal(err)
	}
	req := e.req(http.MethodPost, e.ts.URL+"/api/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp := e.do(req)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings post: %d %s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`id="settings-panel"`)) {
		t.Fatalf("settings partial should carry its own swap target: %s", body)
	}
	if !bytes.Contains(body, []byte(`class="token"`)) {
		t.Fatalf("token should be shown: %s", body)
	}
}

// htmx swaps 4xx bodies only when responseHandling says so; without this the
// error banners under the settings forms never appear.
func TestPagesConfigureHtmxToSwapErrors(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password()
	e.login(pw)
	resp := e.get("/settings")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`name="htmx-config"`)) {
		t.Fatal("pages should carry an htmx config")
	}
	if !bytes.Contains(body, []byte(`"code":"4..","swap":true`)) {
		t.Fatalf("htmx config should swap 4xx responses: %s", body[:200])
	}
}

// The preview link used to hardcode dev1, so a board added later had no way
// to look at its own frame and a revoked dev1 left a dead link behind.
func TestPreviewLinkFollowsTheDevice(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password()
	e.login(pw)
	ctx := context.Background()

	if _, err := e.st.EnsureDevice(ctx, "dev1", "开发设备"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateDevice(ctx, "desk", "桌面屏", ""); err != nil {
		t.Fatal(err)
	}
	body := e.body("/app")
	if !bytes.Contains(body, []byte("/api/devices/dev1/preview.png")) {
		t.Fatalf("today page should preview the first device: %s", body)
	}

	if err := e.st.RevokeDevice(ctx, "dev1"); err != nil {
		t.Fatal(err)
	}
	body = e.body("/app")
	if bytes.Contains(body, []byte("/api/devices/dev1/preview.png")) {
		t.Fatal("revoked device should not be linked")
	}
	if !bytes.Contains(body, []byte("/api/devices/desk/preview.png")) {
		t.Fatalf("next live device should be linked: %s", body)
	}

	set := e.body("/settings")
	if !bytes.Contains(set, []byte("/api/devices/desk/preview.png")) {
		t.Fatalf("settings should preview the live device: %s", set)
	}
}

// Weather must never be able to break the frame: no key, a bad key and a dead
// API all still return 200 with a renderable bitmap.
func TestFrameRendersWithoutWeather(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if _, err := e.st.EnsureDevice(ctx, "dev1", "测试设备"); err != nil {
		t.Fatal(err)
	}
	token := "test-device-token"
	sum := sha256.Sum256([]byte(token))
	if err := e.st.SetDeviceToken(ctx, "dev1", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	fetch := func() int {
		req := e.req(http.MethodGet, e.ts.URL+"/v1/devices/dev1/frame", nil)
		req.Header.Set("X-Device-Token", token)
		resp := e.do(req)
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := fetch(); code != http.StatusOK {
		t.Fatalf("no key: %d", code)
	}
	if err := e.st.PutSettings(ctx, map[string]string{
		"weather_api_key": "k", "weather_city": "北京",
	}); err != nil {
		t.Fatal(err)
	}
	old := weather.BaseURL
	weather.BaseURL = "http://127.0.0.1:1/forecast.json"
	t.Cleanup(func() { weather.BaseURL = old })
	if code := fetch(); code != http.StatusOK {
		t.Fatalf("dead API: %d", code)
	}
}

// The key is never echoed: neither the settings JSON nor the page's HTML may
// contain it, and posting the mask back must leave it alone.
func TestWeatherKeyIsMasked(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	const key = "abcdef0123456789"
	if err := e.st.PutSettings(context.Background(), map[string]string{"weather_api_key": key}); err != nil {
		t.Fatal(err)
	}
	resp := e.get("/api/settings")
	body, _ := io.ReadAll(resp.Body)
	if bytes.Contains(body, []byte(key)) {
		t.Fatalf("settings JSON leaked the key: %s", body)
	}
	if !bytes.Contains(body, []byte("••••6789")) {
		t.Fatalf("expected a mask, got %s", body)
	}
	html := e.body("/settings")
	if bytes.Contains(html, []byte(key)) {
		t.Fatal("settings page leaked the key")
	}
	form := url.Values{"action": {"weather"}, "weather_api_key": {"••••6789"}, "weather_ip_fallback": {"1"}}
	req := e.req(http.MethodPost, e.ts.URL+"/api/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.do(req)
	st, err := e.st.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st["weather_api_key"] != key {
		t.Fatalf("mask overwrote the key: %q", st["weather_api_key"])
	}
	form.Set("weather_api_key", "")
	req = e.req(http.MethodPost, e.ts.URL+"/api/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.do(req)
	if st, _ := e.st.Settings(context.Background()); st["weather_api_key"] != "" {
		t.Fatal("an empty field should clear the key")
	}
}

// clientHeaderTransport marks the test client as the app's own front end,
// which is what the CSRF guard expects from a non-browser caller.
type clientHeaderTransport struct{ base http.RoundTripper }

func (c clientHeaderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Today-Client", "1")
	base := c.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}
