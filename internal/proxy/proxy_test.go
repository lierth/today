package proxy

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A redacted capture of `scutil --proxy` from a Mac with an HTTP proxy, a
// SOCKS proxy and the usual exception list. The addresses are the stock
// examples, not a real machine.
const scutilSample = `<dictionary> {
  ExceptionsList : <array> {
    0 : *.local
    1 : 169.254/16
    2 : 10.*
    3 : 127.*
    4 : 172.16.*
    5 : intranet.example.com
  }
  FTPPassive : 1
  HTTPEnable : 1
  HTTPPort : 6152
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 6152
  HTTPSProxy : 127.0.0.1
  ProxyAutoConfigEnable : 0
  SOCKSEnable : 1
  SOCKSPort : 1080
  SOCKSProxy : 127.0.0.1
}
`

func TestParseScutilProxy(t *testing.T) {
	p := ParseScutilProxy(scutilSample)
	if !p.HTTPEnabled || p.HTTPPort != 6152 || p.HTTPHost != "127.0.0.1" {
		t.Fatalf("http: %+v", p)
	}
	if !p.HTTPSEnabled || p.HTTPSPort != 6152 {
		t.Fatalf("https: %+v", p)
	}
	if !p.SOCKSEnabled || p.SOCKSPort != 1080 {
		t.Fatalf("socks: %+v", p)
	}
	if p.PACEnabled {
		t.Fatal("PAC read as enabled")
	}
	want := []string{"*.local", "169.254/16", "10.*", "127.*", "172.16.*", "intranet.example.com"}
	if len(p.Exceptions) != len(want) {
		t.Fatalf("exceptions: %v", p.Exceptions)
	}
	for i := range want {
		if p.Exceptions[i] != want[i] {
			t.Fatalf("exceptions[%d] = %q", i, p.Exceptions[i])
		}
	}
	if got := p.ProxyURL(); got != "http://127.0.0.1:6152" {
		t.Fatalf("ProxyURL = %q (HTTPS wins)", got)
	}
}

// The precedence is HTTPS, then SOCKS, then HTTP; a disabled block is skipped.
func TestSystemProxyPrecedence(t *testing.T) {
	cases := []struct {
		name string
		in   SystemProxy
		want string
	}{
		{"https", SystemProxy{HTTPSEnabled: true, HTTPSHost: "p", HTTPSPort: 1,
			SOCKSEnabled: true, SOCKSHost: "s", SOCKSPort: 2}, "http://p:1"},
		{"socks", SystemProxy{SOCKSEnabled: true, SOCKSHost: "s", SOCKSPort: 2,
			HTTPEnabled: true, HTTPHost: "h", HTTPPort: 3}, "socks5://s:2"},
		{"http", SystemProxy{HTTPEnabled: true, HTTPHost: "h", HTTPPort: 3}, "http://h:3"},
		{"none", SystemProxy{}, ""},
		{"enabled without host", SystemProxy{HTTPSEnabled: true}, ""},
	}
	for _, c := range cases {
		if got := c.in.ProxyURL(); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// Every exception shape macOS writes must be understood, and a name that only
// looks similar must not match.
func TestSystemProxyBypassed(t *testing.T) {
	p := SystemProxy{Exceptions: []string{
		"*.local", "169.254/16", "10.*", "127.*", "172.16.*", "intranet.example.com",
		".corp.example.com", "*.wild.example.com",
	}}
	cases := []struct {
		host string
		want bool
	}{
		{"printer.local", true},
		{"169.254.10.20", true},
		{"10.1.2.3", true},
		{"127.0.0.1", true},
		{"172.16.4.5", true},
		{"intranet.example.com", true},
		{"a.corp.example.com", true},
		{"x.wild.example.com", true},
		{"wild.example.com", true},
		{"api.github.com", false},
		{"10.example.com", false}, // a name, not an address
		{"172.20.1.1", false},     // outside the listed /16
		{"intranet.example.com.evil", false},
	}
	for _, c := range cases {
		if got := p.Bypassed(c.host); got != c.want {
			t.Errorf("Bypassed(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

// newTestResolver builds a resolver with fixed settings and a fixed system
// proxy, so no test touches the real `scutil`.
func newTestResolver(mode, rawURL string, sys SystemProxy) *Resolver {
	r := NewResolver(func(context.Context) Settings {
		return Settings{Mode: Mode(mode), URL: rawURL}
	})
	prev := sysProxy.command
	prevAt := sysProxy.at
	sysProxy.command = func() (string, error) { return "", nil }
	sysProxy.at = timeZero()
	r.system = func() SystemProxy { return sys }
	_ = prev
	_ = prevAt
	return r
}

func timeZero() time.Time { return time.Time{} }

func proxyFor(t *testing.T, r *Resolver, raw string) (*url.URL, error) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return r.ProxyFor(&http.Request{URL: u})
}

// Off means direct; on with a URL means that URL; on without a URL follows the
// system, and PAC refuses loudly rather than silently going direct.
func TestResolverModes(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		url     string
		sys     SystemProxy
		wan     string
		want    string
		wantErr bool
	}{
		{"off ignores a url", "off", "http://p:1", SystemProxy{}, "https://api.github.com/x", "", false},
		{"custom wins over system", "on", "http://p:1", SystemProxy{HTTPSEnabled: true, HTTPSHost: "sys", HTTPSPort: 9000},
			"https://api.github.com/x", "http://p:1", false},
		{"system when url is blank", "on", "", SystemProxy{HTTPSEnabled: true, HTTPSHost: "sys", HTTPSPort: 9000},
			"https://api.github.com/x", "http://sys:9000", false},
		{"socks5 custom", "on", "socks5://127.0.0.1:1080", SystemProxy{},
			"https://api.github.com/x", "socks5://127.0.0.1:1080", false},
		{"bad scheme", "on", "ftp://p:1", SystemProxy{}, "https://api.github.com/x", "", true},
		{"no proxy anywhere", "on", "", SystemProxy{}, "https://api.github.com/x", "", false},
	}
	for _, c := range cases {
		r := newTestResolver(c.mode, c.url, c.sys)
		got, err := proxyFor(t, r, c.wan)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v", c.name, err)
			continue
		}
		if err != nil {
			continue
		}
		text := ""
		if got != nil {
			text = got.String()
		}
		if text != c.want {
			t.Errorf("%s: %q, want %q", c.name, text, c.want)
		}
	}
}

// Local traffic never goes through the proxy, whatever the settings say.
func TestProxySkipsLocalTraffic(t *testing.T) {
	r := newTestResolver("on", "http://p:1", SystemProxy{})
	for _, raw := range []string{
		"http://127.0.0.1:8787/healthz",
		"http://localhost:8787/app",
		"http://192.168.1.5:8787/v1/devices/dev1/frame",
		"http://10.0.0.9/",
	} {
		got, err := proxyFor(t, r, raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if got != nil {
			t.Errorf("%s went through %s", raw, got)
		}
	}
}

// PAC is refused with an explanation instead of quietly dialing direct.
func TestPACRefused(t *testing.T) {
	r := newTestResolver("on", "", SystemProxy{PACEnabled: true, PACURL: "http://wpad/wpad.dat"})
	if _, err := proxyFor(t, r, "https://api.github.com/"); err == nil {
		t.Fatal("PAC accepted")
	} else if !strings.Contains(err.Error(), "PAC") {
		t.Fatalf("error does not mention PAC: %v", err)
	}
}

// The resolver describes itself for the settings page.
func TestProxyDescribe(t *testing.T) {
	cases := []struct {
		mode, url string
		sys       SystemProxy
		want      string
	}{
		{"off", "", SystemProxy{}, "代理已关闭"},
		{"on", "http://p:1", SystemProxy{}, "自定义代理 http://p:1"},
		{"on", "", SystemProxy{HTTPSEnabled: true, HTTPSHost: "s", HTTPSPort: 2}, "系统代理 http://s:2"},
		{"on", "", SystemProxy{PACEnabled: true}, "系统使用 PAC（不支持，请手动填写）"},
		{"on", "", SystemProxy{}, "系统没有配置代理（直连）"},
	}
	for _, c := range cases {
		r := newTestResolver(c.mode, c.url, c.sys)
		if got := r.Describe(); !strings.Contains(got, c.want) {
			t.Errorf("Describe(%s,%s) = %q, want %q", c.mode, c.url, got, c.want)
		}
	}
}

// The real machine's scutil output (captured, addresses unchanged: everything
// is the stock local proxy). The SOCKS key is SOCKSProxy here; SOCKSHost is
// what an older fixture invented and what the parser used to look for.
func TestParseScutilProxyRealCapture(t *testing.T) {
	const real = `<dictionary> {
  ExceptionsList : <array> {
    0 : *zhihu.com
    1 : localhost
    2 : *.local
    3 : 127.*
    4 : 10.*
    5 : 172.16.*
    6 : 192.168.*
  }
  FTPPassive : 1
  HTTPEnable : 1
  HTTPPort : 7688
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7688
  HTTPSProxy : 127.0.0.1
  ProxyAutoConfigEnable : 0
  SOCKSEnable : 1
  SOCKSPort : 7688
  SOCKSProxy : 127.0.0.1
}
`
	p := ParseScutilProxy(real)
	if !p.SOCKSEnabled || p.SOCKSHost != "127.0.0.1" || p.SOCKSPort != 7688 {
		t.Fatalf("SOCKSProxy not read from the real key: %+v", p)
	}
	if len(p.Exceptions) != 7 || p.Exceptions[1] != "localhost" {
		t.Fatalf("exceptions: %v", p.Exceptions)
	}
}

// Tailscale and .local hosts are local: they must never be sent through a
// proxy even when one is configured.
func TestProxySkipsTailscaleAndLocalNames(t *testing.T) {
	r := NewResolver(func(context.Context) Settings {
		return Settings{Mode: ModeOn, URL: "http://127.0.0.1:7890"}
	})
	for _, host := range []string{
		"100.88.1.2",          // CGNAT
		"[fd7a:115c:a1e0::1]", // Tailscale ULA (bracketed: it is an IPv6 URL)
		"board.tailnet.ts.net",
		"printer.local",
		"192.168.1.9",
	} {
		req, err := http.NewRequest("GET", "http://"+host+"/", nil)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		u, err := r.ProxyFor(req)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		if u != nil {
			t.Fatalf("%s went through %s", host, u)
		}
	}
	req, _ := http.NewRequest("GET", "https://api.github.com/", nil)
	u, err := r.ProxyFor(req)
	if err != nil || u == nil {
		t.Fatalf("public host: %v %v", u, err)
	}
}

// A proxy URL with credentials is shown redacted.
func TestDescribeRedactsCredentials(t *testing.T) {
	r := NewResolver(func(context.Context) Settings {
		return Settings{Mode: ModeOn, URL: "http://alice:s3cret@proxy.example.com:8080"}
	})
	if got := r.Describe(); strings.Contains(got, "s3cret") {
		t.Fatalf("password shown: %q", got)
	}
}
