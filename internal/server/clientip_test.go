package server

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// The limiter counts per IP. Before this was fixed, a LAN client could rotate
// a fake X-Forwarded-For per attempt and never hit the limit; and behind
// Funnel everyone shared 127.0.0.1, so one attacker could lock the owner out.
func TestClientIPTrustsXFFOnlyBehindFunnel(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		headers map[string]string
		want    string
	}{
		{
			name:   "direct LAN with forged XFF",
			remote: "192.168.1.40:5000",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.7",
			},
			want: "192.168.1.40",
		},
		{
			name:   "direct LAN without XFF",
			remote: "192.168.1.40:5000",
			want:   "192.168.1.40",
		},
		{
			name:   "funnel request uses the forwarded client",
			remote: "127.0.0.1:5000",
			headers: map[string]string{
				"Tailscale-Funnel-Request": "?1",
				"X-Forwarded-For":          "203.0.113.7",
			},
			want: "203.0.113.7",
		},
		{
			name:   "funnel request keeps only the first hop",
			remote: "127.0.0.1:5000",
			headers: map[string]string{
				"Tailscale-Funnel-Request": "?1",
				"X-Forwarded-For":          "203.0.113.7, 10.0.0.1",
			},
			want: "203.0.113.7",
		},
		{
			name:   "funnel marker without XFF falls back to the peer",
			remote: "127.0.0.1:5000",
			headers: map[string]string{
				"Tailscale-Funnel-Request": "?1",
			},
			want: "127.0.0.1",
		},
		{
			name:   "XFF on loopback without the funnel marker is ignored",
			remote: "127.0.0.1:5000",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.7",
			},
			want: "127.0.0.1",
		},
		{
			name:   "ipv6 loopback",
			remote: "[::1]:5000",
			want:   "::1",
		},
	}
	for _, c := range cases {
		req, err := http.NewRequest(http.MethodPost, "/login", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.RemoteAddr = c.remote
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		if got := clientIP(req); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// Two Funnel clients from different addresses must not share a bucket.
func TestLimiterBucketsPerFunnelClient(t *testing.T) {
	l := newLoginLimiter()
	now := time.Now()
	mk := func(xff string) *http.Request {
		req, _ := http.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "127.0.0.1:5000"
		req.Header.Set("Tailscale-Funnel-Request", "?1")
		req.Header.Set("X-Forwarded-For", xff)
		return req
	}
	// Exhaust the first client.
	for range 5 {
		ip := clientIP(mk("203.0.113.7"))
		if !l.allow(ip, now) {
			t.Fatal("first five attempts should be allowed")
		}
		l.fail(ip, now)
	}
	if l.allow(clientIP(mk("203.0.113.7")), now) {
		t.Fatal("the first client should now be limited")
	}
	if !l.allow(clientIP(mk("203.0.113.8")), now) {
		t.Fatal("a different Funnel client must have its own bucket")
	}
}

// And a LAN client cannot escape its bucket by rotating a forged header.
func TestLimiterIgnoresForgedXFFFromLAN(t *testing.T) {
	l := newLoginLimiter()
	now := time.Now()
	for i := range 5 {
		req, _ := http.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "192.168.1.40:5000"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i+1))
		ip := clientIP(req)
		if ip != "192.168.1.40" {
			t.Fatalf("forged XFF leaked into the bucket key: %q", ip)
		}
		l.fail(ip, now)
	}
	if l.allow("192.168.1.40", now) {
		t.Fatal("rotating XFF must not reset the limit")
	}
}

// A LAN request that claims https in X-Forwarded-Proto must not receive a
// Secure cookie it would never send back over plain HTTP.
func TestSecureCookieIgnoresForgedProto(t *testing.T) {
	s := &Server{}
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.40:5000"
	req.Header.Set("X-Forwarded-Proto", "https")
	if s.secureCookie(req) {
		t.Fatal("a direct LAN request must not get a Secure cookie from a header")
	}
	req.RemoteAddr = "127.0.0.1:5000"
	req.Header.Set("Tailscale-Funnel-Request", "?1")
	if !s.secureCookie(req) {
		t.Fatal("a Funnel request over https should get a Secure cookie")
	}
	tlsReq, _ := http.NewRequest(http.MethodGet, "/", nil)
	tlsReq.RemoteAddr = "192.168.1.40:5000"
	tlsReq.TLS = &tlsConnState
	if !s.secureCookie(tlsReq) {
		t.Fatal("real TLS should still mark the cookie Secure")
	}
}

var tlsConnState = tls.ConnectionState{}
