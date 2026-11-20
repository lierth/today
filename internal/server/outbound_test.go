package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The proxy settings decide where an outbound request goes. This drives the
// real transport rule: off is direct, a custom URL is used, and a loopback
// target is never proxied.
func TestOutboundProxyRule(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()

	proxyFor := func(raw string) (string, error) {
		u, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return "", err
		}
		tr := e.srv.OutboundClient().Transport.(*http.Transport)
		p, err := tr.Proxy(req)
		if err != nil || p == nil {
			return "", err
		}
		return p.String(), nil
	}

	// Default: proxy on, no URL → system proxy (or direct on a Mac without
	// one; the test asserts the custom URL path only, which is deterministic).
	if _, err := e.srv.UpdateSettings(ctx, map[string]string{
		"proxy_mode": "off", "proxy_url": "http://127.0.0.1:3128",
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := proxyFor("https://api.github.com/x"); err != nil || got != "" {
		t.Fatalf("off mode used %q (%v)", got, err)
	}

	if _, err := e.srv.UpdateSettings(ctx, map[string]string{"proxy_mode": "on"}); err != nil {
		t.Fatal(err)
	}
	if got, err := proxyFor("https://api.github.com/x"); err != nil || got != "http://127.0.0.1:3128" {
		t.Fatalf("custom proxy: %q (%v)", got, err)
	}
	// The board and the local server never go through a proxy.
	for _, local := range []string{
		"http://127.0.0.1:8787/healthz",
		"http://192.168.1.9:8787/v1/devices/dev1/frame",
	} {
		if got, err := proxyFor(local); err != nil || got != "" {
			t.Fatalf("%s proxied to %q (%v)", local, got, err)
		}
	}

	// A bad URL is refused with a message on the settings page, not stored.
	// The proxy lives in the Mac app, so the message comes from the method
	// its binding calls; over HTTP the whole key is off limits.
	if err := e.srv.SetSetting(ctx, "proxy_url", "ftp://nope:1"); err == nil {
		t.Fatal("a bad proxy URL was accepted")
	}
	if v, _ := e.st.Setting(ctx, "proxy_url"); v != "http://127.0.0.1:3128" {
		t.Fatalf("bad value stored: %q", v)
	}
}

// The settings card states the mode, and the checkbox pair (hidden marker +
// checkbox) can switch it off, which a bare checkbox cannot express.
// Turning the proxy off is a plain "off"; the settings window states the mode
// instead of relying on a checkbox that cannot tell "unset" from "off".
func TestProxyTogglePersists(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	// The Mac app writes through PatchSettings; the browser cannot reach it.
	if _, err := e.srv.PatchSettings(ctx, map[string]string{"proxy_mode": "off", "proxy_url": ""}); err != nil {
		t.Fatalf("patch proxy_mode: %v", err)
	}
	if v, _ := e.st.Setting(ctx, "proxy_mode"); v != "off" {
		t.Fatalf("proxy_mode = %q", v)
	}
	st, _ := e.srv.Service().Settings(ctx)
	if st["proxy_mode"] != "off" {
		t.Fatalf("settings read back %q", st["proxy_mode"])
	}
}

// A refused proxy must be reported, not silently bypassed: the probe pins the
// transport to the address it is testing.
func TestProxyTestPinsTheProxy(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	// A listener that accepts and closes: an unreachable "proxy".
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	if _, err := e.srv.UpdateSettings(ctx, map[string]string{
		"proxy_mode": "on", "proxy_url": "http://" + ln.Addr().String(),
	}); err != nil {
		t.Fatal(err)
	}
	res := e.srv.proxyTest(ctx)
	if res.OK {
		t.Fatalf("a refused proxy reported success: %+v", res)
	}
	if !strings.Contains(res.Error, "proxyconnect") && !strings.Contains(res.Error, "connect") &&
		!strings.Contains(res.Error, "EOF") {
		t.Fatalf("unexpected error: %q", res.Error)
	}
}

// Nothing in the outbound path uses http.DefaultClient: a test server that
// counts requests sees exactly the calls the app makes.
func TestOutboundClientIsShared(t *testing.T) {
	e := newTestEnv(t)
	seen := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"9.9.9","url":"https://example.invalid/"}`))
	}))
	defer srv.Close()
	if _, err := e.srv.UpdateSettings(context.Background(), map[string]string{
		"proxy_mode": "off", "update_feed": srv.URL,
	}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := e.srv.OutboundClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if seen != 1 {
		t.Fatalf("server saw %d requests", seen)
	}
}

// The download client must not carry a whole-request deadline: a release
// through a slow proxy takes minutes and a Client.Timeout would kill it. The
// header timeout is the guard instead, and the API client still has a bound.
func TestOutboundClientsSplitTimeouts(t *testing.T) {
	e := newTestEnv(t)
	if got := e.srv.OutboundClient().Timeout; got != 0 {
		t.Fatalf("download client Timeout = %v, want none", got)
	}
	if got := e.srv.OutboundAPIClient().Timeout; got <= 0 {
		t.Fatalf("API client has no Timeout")
	}
	tr, ok := e.srv.OutboundClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type %T", e.srv.OutboundClient().Transport)
	}
	if tr.ResponseHeaderTimeout <= 0 {
		t.Fatal("no ResponseHeaderTimeout: a silent proxy would hang forever")
	}
}
