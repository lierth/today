package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"today/internal/store"
)

// A page on another loopback port is the same site, so SameSite=Lax would let
// it POST with the session cookie. The guard must look at the origin instead.
func TestCrossOriginPostRefused(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	body := url.Values{"status": {"done"}, "view": {"today"}}.Encode()
	req, err := http.NewRequest(http.MethodPatch, e.ts.URL+"/api/items/x",
		strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	req.Header.Set("Origin", "http://127.0.0.1:9999")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// e.client adds X-Today-Client, which must not win over an explicit
	// cross-origin marker.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin PATCH: %d, want 403", resp.StatusCode)
	}
}

// The same request from the app itself (same-origin, or the shell's header)
// goes through.
func TestSameOriginAndShellPostAllowed(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	loc, err := e.srv.Location(ctx)
	if err != nil {
		t.Fatal(err)
	}
	day, _, err := e.srv.Today(ctx)
	if err != nil {
		t.Fatal(err)
	}
	it, err := e.st.CreateItem(ctx, store.Item{
		Title:     "测试",
		DateLocal: day.Format(store.DateLayout),
		AllDay:    true,
	}, loc)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		site    string
		origin  string
		allowed bool
	}{
		{"same-origin", "same-origin", "", true},
		{"typed address", "none", "", true},
		{"matching origin", "", e.ts.URL, true},
		{"other origin", "", "http://evil.example.com", false},
		{"other loopback port", "", "http://127.0.0.1:1", false},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(http.MethodPatch, e.ts.URL+"/api/items/"+it.ID,
			strings.NewReader("status=done&view=today"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if c.site != "" {
			req.Header.Set("Sec-Fetch-Site", c.site)
		}
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		req.Header.Del("X-Today-Client")
		// A bare client: the guard must decide from the two headers alone.
		resp, err := (&http.Client{Jar: e.client.Jar}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		got := resp.StatusCode != http.StatusForbidden
		if got != c.allowed {
			t.Errorf("%s: allowed=%v, want %v (status %d)", c.name, got, c.allowed, resp.StatusCode)
		}
	}
}

// A request with neither header is a script: allowed only with the app's own
// marker header.
func TestNoOriginHeadersNeedsClientMarker(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	req, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/api/items/carry",
		strings.NewReader("item=nope"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Jar: e.client.Jar}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unmarked script POST: %d, want 403", resp.StatusCode)
	}
	resp, err = e.client.PostForm(e.ts.URL+"/api/items/carry", url.Values{"item": {"nope"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("the app's own client was refused")
	}
}

// GET stays open: the login page, the panel and the health check have to work
// before a session exists.
func TestGetNotBlockedByOriginGuard(t *testing.T) {
	e := newTestEnv(t)
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/login", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /login: %d", resp.StatusCode)
	}
}

// The board's POST is authenticated by its own token, not the session cookie:
// the origin guard must not stand between the device and /v1/.
func TestDeviceProtocolNotSubjectToOriginGuard(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/v1/devices/dev1/acks", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	if !checkWriteOrigin(req) {
		t.Fatal("a device POST was refused")
	}
}
