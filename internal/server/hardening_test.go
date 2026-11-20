package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"today/internal/store"
)

// This file pins the security ring the review asked for: what a browser may
// reach at all, what it may read, and what it may never see.

// funnelGet builds a request the way tailscaled forwards one from the public
// internet: from loopback, with the Funnel marker (the header a client cannot
// set through Funnel itself).
func (e *testEnv) funnel(method, path string, body string) *http.Request {
	e.t.Helper()
	var req *http.Request
	if body == "" {
		req = e.req(method, e.ts.URL+path, nil)
	} else {
		req = e.req(method, e.ts.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(funnelHeader, "1")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Forwarded-Host", "today.example.ts.net")
	req.Header.Set("X-Today-Client", "1")
	return req
}

// The actions that touch this machine — the listener, backups, the boards'
// tokens, the firmware — are gone from HTTP, not merely hidden behind the
// session: a Funnel visitor with the password must not reach them either.
func TestHostActionsAreNotReachableOverFunnel(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	cases := []struct{ method, path, body string }{
		{"PUT", "/api/v2/listen", `{"listen":"127.0.0.1:9999"}`},
		{"POST", "/api/v2/backups", ""},
		{"GET", "/api/v2/backups", ""},
		{"POST", "/api/v2/backups/restore", `{"snapshot":"today-20260101-030000.db"}`},
		{"POST", "/api/v2/devices", `{"id":"dev9"}`},
		{"POST", "/api/v2/devices/dev1/token", ""},
		{"POST", "/api/v2/devices/dev1/revoke", ""},
		{"GET", "/api/v2/devices/dev1/preview.png", ""},
		{"POST", "/api/v2/firmware", ""},
		{"POST", "/api/v2/firmware/push", ""},
	}
	for _, c := range cases {
		resp := e.do(e.funnel(c.method, c.path, c.body))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s %s over Funnel = %d, want 404/403", c.method, c.path, resp.StatusCode)
		}
	}
	// The proof it was refused by the route, not by the session check: the
	// local session still reads the device list.
	if resp := e.get("/api/v2/devices"); resp.StatusCode != http.StatusOK {
		t.Fatalf("local read: %d", resp.StatusCode)
	}
}

// The host-only settings keys cannot be moved from a browser at all.
func TestHostOnlySettingsAreReadOnly(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	ctx := context.Background()
	before, _ := e.st.Setting(ctx, "update_feed")

	for _, key := range []string{
		"proxy_mode", "proxy_url", "update_feed", "update_auto_download",
		"backup_icloud", "funnel", "funnel_request", "localnet_request",
	} {
		resp := e.apiCall("PATCH", "/api/v2/settings", map[string]string{key: "x"})
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("PATCH %s = %d, want 403", key, resp.StatusCode)
		}
	}
	if after, _ := e.st.Setting(ctx, "update_feed"); after != before {
		t.Fatalf("update_feed changed to %q", after)
	}

	// The blacklist era missed keys nobody listed — firmware_version above all,
	// because writing it publishes a staged image to every board on the next
	// poll. Walk the whole table instead of a hand-picked sample: every setting
	// the store knows must be refused except the two the web page owns.
	for _, key := range store.KnownSettings {
		if key == "theme" || key == "color_blind" {
			continue
		}
		// Setting() answers defaults for unset keys, so compare against the
		// value the store had before the request rather than against "".
		before, _ := e.st.Setting(ctx, key)
		value := "x"
		if before != "" {
			value = before + "x"
		}
		resp := e.apiCall("PATCH", "/api/v2/settings", map[string]string{key: value})
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("PATCH %s = %d, want 403 (or 400 for a value check that runs first)",
				key, resp.StatusCode)
		}
		if after, _ := e.st.Setting(ctx, key); after != before {
			t.Fatalf("%s was written: %q → %q", key, before, after)
		}
	}
	// firmware_version is the one that mattered: publishing needs an image, so
	// a board with a staged image must see the version untouched.
	if v, _ := e.st.Setting(ctx, "firmware_version"); v != "" {
		t.Fatalf("firmware_version was written: %q", v)
	}

	// A mixed patch is refused as a whole, so it cannot smuggle the host key
	// in beside a harmless one.
	resp := e.apiCall("PATCH", "/api/v2/settings", map[string]string{
		"theme": "dark", "proxy_mode": "manual",
	})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("mixed patch = %d, want 403", resp.StatusCode)
	}
	if v, _ := e.st.Setting(ctx, "theme"); v == "dark" {
		t.Fatal("the refused patch wrote the theme")
	}
}

// The proxy address reaches the page with its password hidden, because the
// settings response is read by anyone with the session — including a Funnel
// visitor looking at a screenshot.
func TestProxyPasswordIsRedacted(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	ctx := context.Background()
	if _, err := e.srv.PatchSettings(ctx, map[string]string{
		"proxy_mode": "manual",
		"proxy_url":  "http://today:hunter2@127.0.0.1:3128",
	}); err != nil {
		t.Fatal(err)
	}

	var out struct {
		Settings map[string]string `json:"settings"`
	}
	e.apiJSON("GET", "/api/v2/settings", nil, &out)
	got := out.Settings["proxy_url"]
	if strings.Contains(got, "hunter2") {
		t.Fatalf("proxy password reached the page: %q", got)
	}
	if !strings.Contains(got, "127.0.0.1:3128") {
		t.Fatalf("the address should stay readable: %q", got)
	}
	if !strings.Contains(got, "today") {
		t.Fatalf("the user name should stay readable: %q", got)
	}

	// The app still needs the real value to build a transport.
	if real, _ := e.srv.SettingValue(ctx, "proxy_url"); !strings.Contains(real, "hunter2") {
		t.Fatalf("the stored value was altered: %q", real)
	}
}

// A JSON client asks for an address the API does not have: it gets JSON, not
// the app shell. Fetching the shell for /api/v2/anything made a typo look like
// a page and hid a 404 inside a 200.
func TestUnknownAPIRouteAnswersJSON(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	resp := e.apiCall("GET", "/api/v2/nope", nil)
	raw := e.readBody(resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type = %q", ct)
	}
	if !strings.Contains(raw, `"error"`) || strings.Contains(raw, `id="root"`) {
		t.Fatalf("body = %.120s", raw)
	}
	// Without a session it is still a JSON 404, never the shell.
	e.logout()
	resp = e.apiCall("GET", "/api/v2/nope", nil)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("the shell answered an API path")
	}
}

// The login page is inline HTML, so every response carries the no-sniff
// header that stops a browser from re-reading a JSON error as markup.
func TestNoSniffOnEveryAnswer(t *testing.T) {
	e := newTestEnv(t)
	for _, path := range []string{"/login", "/healthz", "/api/v2/nope"} {
		resp := e.get(path)
		_ = resp.Body.Close()
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s: X-Content-Type-Options = %q", path, got)
		}
	}
}

// A password is never accepted by one door and refused by another: the login
// form and the settings page quote the same minimum.
func TestPasswordLengthIsOneRule(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	ctx := context.Background()

	if err := e.srv.SetPassword(ctx, "1234567"); err == nil {
		t.Fatal("seven characters were accepted")
	}
	if err := e.srv.SetPassword(ctx, "12345678"); err != nil {
		t.Fatalf("eight characters were refused: %v", err)
	}

	e.login("12345678")
	resp := e.apiCall("POST", "/api/v2/account/password",
		map[string]string{"current": "12345678", "next": "1234567"})
	raw := e.readBody(resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("short password over HTTP: %d %s", resp.StatusCode, raw)
	}
	if !strings.Contains(raw, "8") {
		t.Fatalf("the message should name the rule: %s", raw)
	}
}

// The current-password check on the settings page is a second place to guess
// a password, so it waits behind the same limiter as the login form.
func TestPasswordChangeIsRateLimited(t *testing.T) {
	e := newTestEnv(t)
	e.srv.sleep = func(time.Duration) {}
	pw := e.password()
	e.login(pw)
	ctx := context.Background()

	last := http.StatusOK
	for i := 0; i < 8; i++ {
		resp := e.apiCall("POST", "/api/v2/account/password",
			map[string]string{"current": "wrong-password", "next": "abcdefgh"})
		_ = resp.Body.Close()
		last = resp.StatusCode
		if last == http.StatusTooManyRequests {
			break
		}
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("guessing went unthrottled: last = %d", last)
	}
	// The real password still works afterwards, so the limiter does not lock
	// the person out for good.
	if err := e.srv.SetPassword(ctx, "abcdefgh"); err != nil {
		t.Fatal(err)
	}
}

// An address that answered a wrong password is throttled at the login form
// too, and one flood of failures anywhere closes the door for everyone for
// the rest of the minute — that is what stops an attacker who rotates the
// X-Forwarded-For value Funnel passes along.
func TestGlobalFailureCeiling(t *testing.T) {
	l := newLoginLimiter()
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	for i := 0; i < globalFailCap; i++ {
		l.fail("10.0.0."+strconv.Itoa(i), now)
	}
	if l.allow("203.0.113.1", now) {
		t.Fatal("a fresh address was allowed past the global ceiling")
	}
	if !l.allow("203.0.113.1", now.Add(61*time.Second)) {
		t.Fatal("the window closed but the door stayed shut")
	}
}

// An internal error never reaches the page: SQL, driver text and absolute
// paths are logged, and the person gets a sentence they can act on.
func TestInternalErrorsAreNotEchoed(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())

	// A title the store refuses for a reason only the database knows.
	resp := e.apiCall("POST", "/api/v2/items", map[string]string{
		"title": strings.Repeat("很长", 4000),
	})
	raw := e.readBody(resp)
	if strings.Contains(raw, "/Users/") || strings.Contains(raw, ".today") {
		t.Fatalf("a path leaked: %.200s", raw)
	}
	for _, leak := range []string{"store:", "sql:", "sqlite", "constraint"} {
		if strings.Contains(strings.ToLower(raw), leak) {
			t.Fatalf("%q leaked: %.200s", leak, raw)
		}
	}

	// The hint rule itself, in both directions.
	if isUserFacing("请填写正确的日期和时间") != true {
		t.Fatal("a sentence we wrote was judged internal")
	}
	if isUserFacing("store: carry load dev1: no such column") != false {
		t.Fatal("a store error was judged user-facing")
	}
	if isUserFacing("constraint failed: UNIQUE constraint failed: items.id") != false {
		t.Fatal("a constraint error was judged user-facing")
	}
}
