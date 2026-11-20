package server

import (
	"net/http"
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
// lose access to the page that reports the mistake. The port is changed from
// the Mac app's settings window, so the check is on the method it calls; the
// HTTP door answers 404 and changes nothing either.
func TestSettingsRejectsBadPort(t *testing.T) {
	e := newTestEnv(t)
	before := e.srv.Addr()
	if err := e.srv.SetListenAddr("99999"); err == nil {
		t.Fatal("an impossible port was accepted")
	}
	if after := e.srv.Addr(); after != before {
		t.Fatalf("addr moved from %q to %q", before, after)
	}

	e.login(e.password())
	resp := e.apiCall("PUT", "/api/v2/listen", map[string]string{"listen": "99999"})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("HTTP listen: %d, want 404", resp.StatusCode)
	}
	if after := e.srv.Addr(); after != before {
		t.Fatalf("addr moved from %q to %q", before, after)
	}
}
