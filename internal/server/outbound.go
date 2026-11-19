package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"today/internal/proxy"
	"today/internal/store"
)

// newOutboundClient builds the single client every request that leaves this
// machine uses: WeatherAPI, the update feed and release downloads. The proxy
// rule is read from settings on every dial, so changing it in the page takes
// effect without a restart.
func newOutboundClient(st *store.Store, logger interface{ Printf(string, ...any) }) *http.Client {
	resolver := proxy.NewResolver(func(ctx context.Context) proxy.Settings {
		sets, err := st.Settings(ctx)
		if err != nil {
			return proxy.Settings{Mode: proxy.ModeOff}
		}
		return proxy.Settings{Mode: proxy.Mode(sets["proxy_mode"]), URL: sets["proxy_url"]}
	})
	transport := &http.Transport{
		Proxy:               resolver.ProxyFor,
		MaxIdleConns:        8,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		// A proxy that accepts the connection and never answers must fail in
		// a bounded time, without capping the download itself.
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	// No Client.Timeout: a release download through a slow proxy can take
	// minutes, and a whole-request deadline would kill it every time. The API
	// calls that need a bound (the update check, the proxy test) use a context
	// with a deadline instead.
	return &http.Client{Transport: transport}
}

// OutboundAPIClient is the same transport with a short whole-request timeout,
// for small API calls (the update feed, the proxy test). Downloads use
// OutboundClient.
func (s *Server) OutboundAPIClient() *http.Client {
	return &http.Client{Transport: s.outbound.Transport, Timeout: 30 * time.Second}
}

// OutboundClient exposes the one internet-facing client to the host, so the
// update check and the update download follow the same proxy rule. It is a
// narrow accessor, not the server's internals.
func (s *Server) OutboundClient() *http.Client { return s.outbound }

// proxyTestResult is what the settings page shows after 测试连接.
type proxyTestResult struct {
	OK     bool
	Proxy  string // where the request actually went
	Status int
	Millis int64
	Error  string
	Detail string
}

// proxyTest asks GitHub once through the configured proxy and reports what
// happened. It uses a short timeout: the person is waiting on the button.
func (s *Server) proxyTest(ctx context.Context) proxyTestResult {
	start := time.Now()
	res := proxyTestResult{OK: false}
	client := &http.Client{
		Transport: s.outbound.Transport,
		Timeout:   8 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/rate_limit", nil)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Today")
	if p, err := s.proxyForURL(req.URL); err != nil {
		res.Error = err.Error()
		return res
	} else if p != "" {
		res.Proxy = p
	}
	// Pin the transport to this one proxy for the probe. Without the pin a
	// "proxy" that answers nothing would still be skipped by some transports
	// (and a stale pooled connection could hide the problem); the person
	// pressing the button wants a statement about *this* address.
	if res.Proxy != "" && res.Proxy != "直连" {
		u, err := url.Parse(res.Proxy)
		if err != nil {
			res.Error = "代理地址无效：" + err.Error()
			return res
		}
		base := s.outbound.Transport.(*http.Transport).Clone()
		base.Proxy = nil
		base.DialContext = (&net.Dialer{Timeout: 6 * time.Second}).DialContext
		client.Transport = &http.Transport{
			Proxy:               http.ProxyURL(u),
			DialContext:         base.DialContext,
			TLSHandshakeTimeout: 8 * time.Second,
		}
	}
	resp, err := client.Do(req)
	res.Millis = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()
	res.Status = resp.StatusCode
	res.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
	if res.Proxy == "" {
		res.Proxy = "直连"
	}
	if !res.OK {
		res.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return res
}

// proxyForURL reports the proxy the shared client would use for u.
func (s *Server) proxyForURL(u *url.URL) (string, error) {
	if s.outbound == nil || s.outbound.Transport == nil {
		return "", nil
	}
	tr, ok := s.outbound.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		return "", nil
	}
	p, err := tr.Proxy(&http.Request{URL: u})
	if err != nil || p == nil {
		return "", err
	}
	// Redacted, not String: the proxy may carry a user name and password, and
	// this line goes into the settings page and the log.
	return p.Redacted(), nil
}

// proxySettingsSummary is the one-line state the settings card shows.
func (s *Server) proxySettingsSummary(ctx context.Context) string {
	settings, err := s.st.Settings(ctx)
	if err != nil {
		return ""
	}
	resolver := proxy.NewResolver(func(context.Context) proxy.Settings {
		return proxy.Settings{Mode: proxy.Mode(settings["proxy_mode"]), URL: settings["proxy_url"]}
	})
	return resolver.Describe()
}

var _ = sync.Mutex{}
