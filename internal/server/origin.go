package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Where a request really came from.
//
// RemoteAddr alone cannot answer that: Tailscale Funnel is a reverse proxy
// inside tailscaled, which connects to 127.0.0.1 on our behalf, so every
// request from the public internet arrives with a loopback RemoteAddr. It
// marks those requests with headers of its own (ipn/ipnlocal/serve.go):
//
//	X-Forwarded-For      the original client address
//	X-Forwarded-Host     the public host name
//	Tailscale-Funnel-Request: ?1
//
// tailscaled uses Set, so a client cannot drop or forge them. Their presence
// is what separates "the person at this Mac" from "the internet".
const funnelHeader = "Tailscale-Funnel-Request"

// A tailnet request through `tailscale serve` (the private one, not Funnel)
// also arrives from 127.0.0.1 with an X-Forwarded-For. It carries no Funnel
// marker, but tailscaled writes Tailscale-User-Login on it (again with Set,
// so a client cannot forge it). Without this, every tailnet visitor would
// share the loopback bucket in the login limiter.
const tailscaleLoginHeader = "Tailscale-User-Login"

// remoteHost is RemoteAddr without the port.
func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func remoteIsLoopback(r *http.Request) bool {
	ip := net.ParseIP(remoteHost(r))
	return ip != nil && ip.IsLoopback()
}

// isFunnelRequest reports that tailscaled forwarded a public request to us.
// A loopback peer that carries the Funnel marker is the internet, not the
// local user, which is the whole reason this file exists.
func isFunnelRequest(r *http.Request) bool {
	return remoteIsLoopback(r) && r.Header.Get(funnelHeader) != ""
}

// isLocalRequest is the only place allowed to answer "may I trust this as the
// person at this Mac". A loopback peer with any proxy header is rejected:
// Funnel adds three, a local reverse proxy may add one, and neither is the
// machine's own browser.
func isLocalRequest(r *http.Request) bool {
	if !remoteIsLoopback(r) {
		return false
	}
	if r.Header.Get(funnelHeader) != "" {
		return false
	}
	if r.Header.Get("X-Forwarded-For") != "" {
		return false
	}
	if r.Header.Get("X-Forwarded-Host") != "" {
		return false
	}
	return true
}

// isTailscaleServe reports a request tailscaled proxied from the tailnet
// through `tailscale serve`, as opposed to a browser on this Mac.
func isTailscaleServe(r *http.Request) bool {
	return remoteIsLoopback(r) && r.Header.Get(tailscaleLoginHeader) != "" &&
		r.Header.Get(funnelHeader) == ""
}

// clientIP is the address the login limiter counts against. X-Forwarded-For
// is honoured only behind Funnel or `tailscale serve`, where tailscaled
// overwrites it; on a direct LAN request the header is whatever the client
// typed, so trusting it would let an attacker rotate fake addresses and dodge
// the per-IP limit forever.
func clientIP(r *http.Request) string {
	if isFunnelRequest(r) || isTailscaleServe(r) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if first != "" {
				return first
			}
		}
	}
	return remoteHost(r)
}

// checkWriteOrigin is the CSRF guard for every state-changing request.
//
// SameSite=Lax is not enough here. A page on another port of 127.0.0.1 (or on
// the LAN address) is the same site, so the browser sends our cookie with a
// cross-origin POST. Two independent signals are accepted:
//
//   - Sec-Fetch-Site: same-origin (modern browsers, not scriptable).
//   - Origin: exactly this request's scheme and host (older browsers).
//
// A request with neither header is either a very old browser or a script
// (curl, the tray's own think). It must carry X-Today-Client:1, which no
// browser adds on its own for a cross-site form post; the page's fetch/HTMX
// wrapper and the host's API client set it.
//
// Requests from tailscaled (Funnel or serve) arrive with Origin of the public
// host; X-Forwarded-Host names it, and it is set by tailscaled, not the client.
func checkWriteOrigin(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead ||
		r.Method == http.MethodOptions {
		return true
	}
	// The device protocol is not a browser and carries no cookie: its POSTs
	// are authenticated by the board's own token, so a cross-site form post
	// cannot do anything with them.
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		return true
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			return false
		}
		if sameHostPort(u.Host, r.Host) {
			return true
		}
		// Behind tailscaled the request's own Host is the loopback address,
		// while the browser's Origin is the public name it is visiting.
		if isFunnelRequest(r) || isTailscaleServe(r) {
			if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
				return sameHostPort(u.Host, fwd)
			}
		}
		return false
	}
	return r.Header.Get("X-Today-Client") == "1"
}

// sameHostPort compares two Host values, treating a missing port as the
// default for the scheme-less form (scheme default ports differ, so an
// explicit :443 against a bare host is left unequal on purpose: the browser
// sends the scheme default it is actually on).
func sameHostPort(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}
