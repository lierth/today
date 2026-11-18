package server

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestValidateAddr(t *testing.T) {
	good := []string{"8787", ":8787", "0.0.0.0:8787", "127.0.0.1:9000", "[::1]:9001"}
	for _, addr := range good {
		if err := ValidateAddr(addr); err != nil {
			t.Errorf("ValidateAddr(%q): %v", addr, err)
		}
	}
	bad := []string{"", "   ", "0", ":0", ":99999", "abc", "host:port", ":70000"}
	for _, addr := range bad {
		if err := ValidateAddr(addr); err == nil {
			t.Errorf("ValidateAddr(%q): want error", addr)
		}
	}
}

func TestNormalizeAddr(t *testing.T) {
	cases := map[string]string{
		"8788":           ":8788",
		":8788":          ":8788",
		"0.0.0.0:8788":   "0.0.0.0:8788",
		"127.0.0.1:9000": "127.0.0.1:9000",
		"":               "",
	}
	for in, want := range cases {
		if got := NormalizeAddr(in); got != want {
			t.Errorf("NormalizeAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExpandAddrKeepsHost(t *testing.T) {
	cases := []struct{ current, input, want string }{
		{"127.0.0.1:8891", "8892", "127.0.0.1:8892"},
		{"[::]:8787", "8892", "[::]:8892"},
		{"0.0.0.0:8787", "8892", "0.0.0.0:8892"},
		{"127.0.0.1:8891", "0.0.0.0:8892", "0.0.0.0:8892"},
		{"127.0.0.1:8891", ":8892", ":8892"},
		{"127.0.0.1:8891", "", ""},
	}
	for _, c := range cases {
		if got := ExpandAddr(c.current, c.input); got != c.want {
			t.Errorf("ExpandAddr(%q, %q) = %q, want %q", c.current, c.input, got, c.want)
		}
	}
}

func TestSameListen(t *testing.T) {
	same := [][2]string{
		{":8787", "0.0.0.0:8787"},
		{":8787", "[::]:8787"},
		{"8787", ":8787"},
		{"127.0.0.1:8787", "127.0.0.1:8787"},
	}
	for _, p := range same {
		if !sameListen(p[0], p[1]) {
			t.Errorf("sameListen(%q, %q) = false, want true", p[0], p[1])
		}
	}
	diff := [][2]string{
		{"127.0.0.1:8787", "0.0.0.0:8787"},
		{"127.0.0.1:8787", "127.0.0.1:8788"},
		{"0.0.0.0:8787", "192.168.1.2:8787"},
	}
	for _, p := range diff {
		if sameListen(p[0], p[1]) {
			t.Errorf("sameListen(%q, %q) = true, want false", p[0], p[1])
		}
	}
}

// A rejected port must leave the listener untouched: the user would otherwise
// lose access to the page that reports the mistake.
func TestSettingsRejectsBadPort(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password()
	e.login(pw)
	before := e.srv.Addr()
	form := url.Values{"action": {"general"}, "tray_title": {"1"}, "open_at_login": {"1"}, "listen_addr": {"99999"}}
	resp, err := e.client.PostForm(e.ts.URL+"/api/settings", form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if after := e.srv.Addr(); after != before {
		t.Fatalf("addr moved from %q to %q", before, after)
	}
}

// Saving settings without touching the port must not bounce the listener.
func TestSettingsKeepsListenerWhenPortUnchanged(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password()
	e.login(pw)
	before := e.srv.Addr()
	form := url.Values{"action": {"general"}, "tray_title": {"1"}, "listen_addr": {e.srv.Port()}}
	resp, err := e.client.PostForm(e.ts.URL+"/api/settings", form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if after := e.srv.Addr(); after != before {
		t.Fatalf("addr moved from %q to %q", before, after)
	}
}

func TestSettingsPageShowsPort(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password()
	e.login(pw)
	resp := e.get("/settings")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `name="listen_addr"`) {
		t.Fatal("port field missing from settings page")
	}
	if !strings.Contains(string(body), "127.0.0.1:") {
		t.Fatal("current listener not shown")
	}
}
