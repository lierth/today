package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
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
	// Pin the clock too. Fixtures here use relative dates (day-2, day+1), but
	// the store/server default to the real clock; pinning keeps a run that
	// straddles local midnight (or a wall clock someone changed) from splitting
	// "today" between the two halves of one test.
	pin := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	st.SetNow(func() time.Time { return pin })
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

// readBody drains a response the caller already has, so a test can look at a
// JSON error without opening the transport again.
func (e *testEnv) readBody(resp *http.Response) string {
	e.t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	_ = resp.Body.Close()
	return string(b)
}

// logout drops the session cookie without a round trip, for tests that need to
// look at the same address as a stranger.
func (e *testEnv) logout() {
	e.t.Helper()
	for _, c := range e.client.Jar.Cookies(mustURL(e.t, e.ts.URL)) {
		if c.Name == sessionCookie {
			c.Value = ""
			c.MaxAge = -1
			e.client.Jar.SetCookies(mustURL(e.t, e.ts.URL), []*http.Cookie{c})
		}
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
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
	now := e.srv.Clock().In(e.loc)
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
