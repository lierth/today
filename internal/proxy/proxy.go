package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mode is the `proxy_mode` setting.
type Mode string

const (
	ModeOff Mode = "off"
	ModeOn  Mode = "on"
)

// Settings is the two-setting rule the resolver reads on every dial.
type Settings struct {
	Mode Mode
	URL  string
}

// SystemProxy is what `scutil --proxy` reports, reduced to the fields a
// desktop app needs. PAC is parsed but never applied: evaluating a PAC script
// without a JS engine is not something this project will pretend to do.
type SystemProxy struct {
	HTTPEnabled  bool
	HTTPHost     string
	HTTPPort     int
	HTTPSEnabled bool
	HTTPSHost    string
	HTTPSPort    int
	SOCKSEnabled bool
	SOCKSHost    string
	SOCKSPort    int
	PACEnabled   bool
	PACURL       string
	Exceptions   []string
}

// ParseScutilProxy reads `scutil --proxy` output. The format is a plist-like
// dictionary:
//
//	<dictionary> {
//	  ExceptionsList : <array> {
//	    0 : *.local
//	    1 : 169.254/16
//	  }
//	  HTTPEnable : 1
//	  HTTPPort : 6152
//	  HTTPProxy : 127.0.0.1
//	  ...
//	}
//
// It is a pure function so the parsing can be tested against a captured,
// redacted sample.
func ParseScutilProxy(out string) SystemProxy {
	var p SystemProxy
	lines := strings.Split(out, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		key, value, ok := strings.Cut(line, " : ")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "HTTPEnable":
			p.HTTPEnabled = value == "1"
		case "HTTPSEnable":
			p.HTTPSEnabled = value == "1"
		case "SOCKSEnable":
			p.SOCKSEnabled = value == "1"
		case "ProxyAutoConfigEnable":
			p.PACEnabled = value == "1"
		case "ProxyAutoConfigURLString":
			p.PACURL = value
		case "HTTPProxy":
			p.HTTPHost = value
		case "HTTPSProxy":
			p.HTTPSHost = value
		case "SOCKSProxy":
			p.SOCKSHost = value
		case "HTTPPort":
			p.HTTPPort = atoiOr(value, 0)
		case "HTTPSPort":
			p.HTTPSPort = atoiOr(value, 0)
		case "SOCKSPort":
			p.SOCKSPort = atoiOr(value, 0)
		case "ExceptionsList":
			// The list continues until the closing brace; entries look like
			// `0 : *.local`.
			for j := i + 1; j < len(lines); j++ {
				entry := strings.TrimSpace(lines[j])
				if strings.HasPrefix(entry, "}") {
					i = j
					break
				}
				if _, v, ok := strings.Cut(entry, " : "); ok {
					if v = strings.TrimSpace(v); v != "" {
						p.Exceptions = append(p.Exceptions, v)
					}
				}
			}
		}
	}
	return p
}

func itoa(n int) string { return strconv.Itoa(n) }

func atoiOr(s string, def int) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// ProxyURL picks the proxy a request should use: HTTPS first, then SOCKS,
// then HTTP. An empty result means "no proxy".
func (p SystemProxy) ProxyURL() string {
	switch {
	case p.HTTPSEnabled && p.HTTPSHost != "" && p.HTTPSPort > 0:
		return "http://" + net.JoinHostPort(p.HTTPSHost, itoa(p.HTTPSPort))
	case p.SOCKSEnabled && p.SOCKSHost != "" && p.SOCKSPort > 0:
		return "socks5://" + net.JoinHostPort(p.SOCKSHost, itoa(p.SOCKSPort))
	case p.HTTPEnabled && p.HTTPHost != "" && p.HTTPPort > 0:
		return "http://" + net.JoinHostPort(p.HTTPHost, itoa(p.HTTPPort))
	}
	return ""
}

// Bypassed reports whether host matches the system's exception list. It
// understands the shapes macOS writes: a bare host, a leading-dot domain
// (".local"), a star domain ("*.example.com"), and CIDR-style prefixes
// ("169.254/16", "10.0.0.0/8") as well as the shorthand "10.*".
func (p SystemProxy) Bypassed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	for _, raw := range p.Exceptions {
		pat := strings.ToLower(strings.TrimSpace(raw))
		if pat == "" {
			continue
		}
		switch {
		case pat == "*":
			return true
		case strings.Contains(pat, "/"):
			// macOS writes prefixes both as CIDR ("169.254/16") and as the
			// short form without a network address; ParseCIDR only takes the
			// first, so fill the missing octets in.
			if ip == nil {
				continue
			}
			cidrText := pat
			if !strings.Contains(cidrText, ".") && !strings.Contains(cidrText, ":") {
				cidrText = "0.0.0.0/" + cidrText
			}
			if _, cidr, err := net.ParseCIDR(cidrText); err == nil && cidr.Contains(ip) {
				return true
			}
			// "169.254/16": append the missing zero octet.
			if _, bits, ok := strings.Cut(pat, "/"); ok && strings.Contains(pat, ".") {
				full := strings.TrimSuffix(pat, "/"+bits)
				for strings.Count(full, ".") < 3 {
					full += ".0"
				}
				if _, cidr, err := net.ParseCIDR(full + "/" + bits); err == nil && cidr.Contains(ip) {
					return true
				}
			}
		case strings.HasPrefix(pat, "*."):
			suffix := pat[1:] // ".example.com"
			if strings.HasSuffix(host, suffix) || host == pat[2:] {
				return true
			}
		case strings.HasPrefix(pat, "."):
			if strings.HasSuffix(host, pat) {
				return true
			}
		case strings.HasSuffix(pat, ".*"):
			// "10.*" is an address prefix, not a name prefix: only a host
			// that literally starts with the octets matches.
			if ip == nil {
				continue
			}
			if strings.HasPrefix(host, strings.TrimSuffix(pat, "*")) {
				return true
			}
		default:
			if host == pat {
				return true
			}
		}
	}
	return false
}

// cache for `scutil --proxy`: the command runs on every outbound dial, and
// macOS network changes are slow compared to that.
type systemProxyCache struct {
	mu      sync.Mutex
	at      time.Time
	value   SystemProxy
	ttl     time.Duration
	command func() (string, error)
}

var sysProxy = &systemProxyCache{
	ttl: 60 * time.Second,
	command: func() (string, error) {
		out, err := exec.Command("scutil", "--proxy").Output()
		return string(out), err
	},
}

// Current returns the system proxy, cached for a minute.
func (c *systemProxyCache) Current() SystemProxy {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < c.ttl {
		return c.value
	}
	out, err := c.command()
	if err != nil {
		// No scutil (a test host) or a transient failure: keep the last value
		// if there is one, otherwise an empty rule means "direct".
		return c.value
	}
	c.value = ParseScutilProxy(out)
	c.at = time.Now()
	return c.value
}

// Resolver turns settings into the proxy a request uses. It is the one place
// that decides, so a test can drive every mode.
type Resolver struct {
	read   func(context.Context) Settings
	cached Settings
	// system reads the system proxy; a test replaces it.
	system func() SystemProxy
	mu     sync.Mutex
}

// NewResolver reads the settings on every dial, so a change in the page takes
// effect on the next request. A nil reader means "proxy off".
func NewResolver(read func(context.Context) Settings) *Resolver {
	if read == nil {
		read = func(context.Context) Settings { return Settings{Mode: ModeOff} }
	}
	return &Resolver{read: read, system: func() SystemProxy { return sysProxy.Current() }}
}

// settings resolves the current mode and URL.
func (r *Resolver) settings() Settings {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.read != nil {
		r.cached = r.read(context.Background())
	}
	return r.cached
}

// ProxyFor is http.Transport.Proxy's shape. It never falls back to a direct
// connection when a proxy is configured: silently bypassing the person's
// proxy is worse than a visible failure.
func (r *Resolver) ProxyFor(req *http.Request) (*url.URL, error) {
	if r == nil {
		return nil, nil
	}
	set := r.settings()
	if set.Mode != ModeOn {
		return nil, nil
	}
	host := req.URL.Hostname()
	if isLoopbackOrLocal(host) {
		return nil, nil
	}
	raw := strings.TrimSpace(set.URL)
	if raw == "" {
		system := r.system
		if system == nil {
			system = func() SystemProxy { return sysProxy.Current() }
		}
		sys := system()
		if sys.PACEnabled {
			// PAC cannot be evaluated; say so instead of guessing.
			return nil, fmt.Errorf("系统使用 PAC 自动配置，本项目不支持；请手动填写代理地址")
		}
		if sys.Bypassed(host) {
			return nil, nil
		}
		raw = sys.ProxyURL()
		if raw == "" {
			return nil, nil
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("代理地址无效：%q", raw)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("不支持的代理协议：%q", u.Scheme)
	}
	return u, nil
}

// isLoopbackOrLocal keeps the local server, the board and LAN probes out of
// the proxy no matter what the settings say. Tailscale's CGNAT range and its
// MagicDNS names are local too: a board reached over the tailnet must not be
// sent through a proxy that cannot resolve it.
func isLoopbackOrLocal(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || tailscaleCGNAT.Contains(ip) ||
			tailscaleULA.Contains(ip)
	}
	lower := strings.ToLower(host)
	return strings.HasSuffix(lower, ".local") ||
		strings.HasSuffix(lower, ".ts.net") ||
		lower == "ts.net"
}

// Tailscale hands out 100.64.0.0/10 (CGNAT) and fd7a:115c:a1e0::/48 (ULA).
var (
	tailscaleCGNAT = mustCIDR("100.64.0.0/10")
	tailscaleULA   = mustCIDR("fd7a:115c:a1e0::/48")
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic("proxy: bad built-in CIDR " + s)
	}
	return n
}

// OutboundClient is the one client every outbound request uses (update feed,
// release download, WeatherAPI). Local traffic never goes through it: the
// board fetches from the server, and the LAN probe dials TCP directly.
func (r *Resolver) OutboundClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:                 r.ProxyFor,
		MaxIdleConns:          8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

// Describe is the one-line summary the settings page shows for "测试连接".
func (r *Resolver) Describe() string {
	set := r.settings()
	if set.Mode != ModeOn {
		return "代理已关闭"
	}
	if set.URL != "" {
		// A hand-filled URL can contain credentials; mask them.
		if u, err := url.Parse(set.URL); err == nil && u.Host != "" {
			return "自定义代理 " + u.Redacted()
		}
		return "自定义代理（地址无效）"
	}
	system := r.system
	if system == nil {
		system = func() SystemProxy { return sysProxy.Current() }
	}
	sys := system()
	if sys.PACEnabled {
		return "系统使用 PAC（不支持，请手动填写）"
	}
	if u := sys.ProxyURL(); u != "" {
		if parsed, err := url.Parse(u); err == nil {
			return "系统代理 " + parsed.Redacted()
		}
		return "系统代理"
	}
	return "系统没有配置代理（直连）"
}
