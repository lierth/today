package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// The tray opens local pages through this link, so the whole value of it is
// the lock: one minute, one use, loopback only.
func TestLocalLoginTokenLifecycle(t *testing.T) {
	e := newTestEnv(t)
	now := e.srv.Clock()
	link, err := e.srv.LocalLoginToken(context.Background(), "/", now)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := e.client.Get(e.ts.URL + link)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first use: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "id=\"root\"") {
		t.Fatalf("the link should land on the app shell: %s", body)
	}
	// The cookie is now a real session.
	page, err := e.client.Get(e.ts.URL + "/api/v2/today")
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	if page.StatusCode != http.StatusOK {
		t.Fatalf("session after link: %d", page.StatusCode)
	}
}

func TestLocalLoginRejectsReplay(t *testing.T) {
	e := newTestEnv(t)
	link, err := e.srv.LocalLoginToken(context.Background(), "/app", e.srv.Clock())
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.client.Get(e.ts.URL + link)
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	// A fresh client: replay must fail because of the nonce, not the cookie.
	jar, _ := cookiejar.New(nil)
	second := &http.Client{Jar: jar}
	resp, err := second.Get(e.ts.URL + link)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replay: %d", resp.StatusCode)
	}
}

func TestLocalLoginRejectsExpired(t *testing.T) {
	e := newTestEnv(t)
	link, err := e.srv.LocalLoginToken(context.Background(), "/app",
		e.srv.Clock().Add(-2*localTokenTTL))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(e.ts.URL + link)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired: %d", resp.StatusCode)
	}
}

func TestLocalLoginRejectsForgedSignature(t *testing.T) {
	e := newTestEnv(t)
	link, err := e.srv.LocalLoginToken(context.Background(), "/app", e.srv.Clock())
	if err != nil {
		t.Fatal(err)
	}
	// Flip the destination: the MAC covers it.
	tampered := strings.Replace(link, "x=%2Fapp", "x=%2Fsettings", 1)
	resp, err := http.Get(e.ts.URL + tampered)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered next: %d", resp.StatusCode)
	}
}

func TestLocalLoginRejectsNonLoopback(t *testing.T) {
	e := newTestEnv(t)
	now := e.srv.Clock()
	link, err := e.srv.LocalLoginToken(context.Background(), "/app", now)
	if err != nil {
		t.Fatal(err)
	}
	// The check reads RemoteAddr, which only the server can set; build the
	// request the server would see and call the validator directly.
	req := e.req(http.MethodGet, "http://example.invalid"+link, nil)
	req.RemoteAddr = "192.168.1.50:51000"
	if _, ok := e.srv.validateLocalToken(req, now); ok {
		t.Fatal("a LAN address must not be able to redeem the link")
	}
	req.RemoteAddr = "127.0.0.1:51000"
	if _, ok := e.srv.validateLocalToken(req, now); !ok {
		t.Fatal("loopback should redeem it")
	}
	req.RemoteAddr = "[::1]:51000"
	if !isLocalRequest(req) {
		t.Fatal("::1 is local")
	}
	req.RemoteAddr = "10.0.0.7"
	if isLocalRequest(req) {
		t.Fatal("10.0.0.7 is not local")
	}
}

// The bug this guards: Funnel reaches us from 127.0.0.1, so "loopback" alone
// cannot mean "the person at this Mac". A leaked link opened through Funnel
// would otherwise log the internet in.
func TestLocalLoginRejectsFunnelProxy(t *testing.T) {
	e := newTestEnv(t)
	now := e.srv.Clock()
	link, err := e.srv.LocalLoginToken(context.Background(), "/app", now)
	if err != nil {
		t.Fatal(err)
	}
	headers := []string{"Tailscale-Funnel-Request", "X-Forwarded-For", "X-Forwarded-Host"}
	for _, h := range headers {
		req := e.req(http.MethodGet, "http://example.invalid"+link, nil)
		req.RemoteAddr = "127.0.0.1:52000"
		req.Header.Set(h, "?1")
		if _, ok := e.srv.validateLocalToken(req, now); ok {
			t.Fatalf("a loopback request carrying %s must be rejected", h)
		}
	}
	// Without them it is the local browser and works.
	req := e.req(http.MethodGet, "http://example.invalid"+link, nil)
	req.RemoteAddr = "127.0.0.1:52000"
	if _, ok := e.srv.validateLocalToken(req, now); !ok {
		t.Fatal("a plain loopback request should pass")
	}
}

func TestIsLocalRequest(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1000"
	if !isLocalRequest(req) {
		t.Fatal("plain loopback is local")
	}
	req.Header.Set("Tailscale-Funnel-Request", "?1")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if isLocalRequest(req) {
		t.Fatal("Funnel traffic arrives from loopback but is not local")
	}
	if !isFunnelRequest(req) {
		t.Fatal("Funnel marker not recognised")
	}
}

func TestLocalLoginTokenStorePrunes(t *testing.T) {
	s := &localTokenStore{}
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	if !s.consume("a", now) {
		t.Fatal("first use should pass")
	}
	if s.consume("a", now) {
		t.Fatal("second use should fail")
	}
	if !s.consume("b", now.Add(2*localTokenTTL)) {
		t.Fatal("a fresh nonce should pass")
	}
	if _, ok := s.used["a"]; ok {
		t.Fatal("old nonce was not pruned")
	}
}

func TestLocalLoginJSONStillNeedsSession(t *testing.T) {
	e := newTestEnv(t)
	resp, err := http.Get(e.ts.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("api without session: %d", resp.StatusCode)
	}
	var payload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&payload)
}

// `tailscale serve` (tailnet, not Funnel) also proxies from loopback, but it
// marks the request with Tailscale-User-Login instead of the Funnel header.
// The limiter has to count the real client, and the request is still not "the
// person at this Mac".
func TestTailscaleServeIsNotLocal(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "127.0.0.1:1000"
	req.Header.Set("Tailscale-User-Login", "someone@example.com")
	req.Header.Set("X-Forwarded-For", "100.101.102.103")
	if isLocalRequest(req) {
		t.Fatal("a tailnet visitor is not the local user")
	}
	if !isTailscaleServe(req) {
		t.Fatal("serve marker not recognised")
	}
	if got := clientIP(req); got != "100.101.102.103" {
		t.Fatalf("clientIP = %q, want the XFF address", got)
	}
	// Without the login header the same loopback request is local, and the
	// XFF is ignored (it would be client-controlled).
	plain, _ := http.NewRequest(http.MethodPost, "/login", nil)
	plain.RemoteAddr = "127.0.0.1:1000"
	plain.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIP(plain); got != "127.0.0.1" {
		t.Fatalf("plain loopback clientIP = %q", got)
	}
}
