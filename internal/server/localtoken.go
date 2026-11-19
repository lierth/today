package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// localTokenTTL is the lifetime of the tray's one-shot login link. Short on
// purpose: the URL is in the browser history and in any proxy log.
const localTokenTTL = 60 * time.Second

// localTokenPath is where the link lands. It is not the panel: the panel is a
// read-only view, this one mints a real session cookie.
const localTokenPath = "/local"

// localTokenStore remembers used nonces. A signature alone proves who made the
// link, not that it is fresh: without this, copying the URL twice would log in
// twice. Pruned on every mint, so it never grows past a minute of links.
type localTokenStore struct {
	mu   sync.Mutex
	used map[string]time.Time
}

func (s *localTokenStore) consume(nonce string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used == nil {
		s.used = make(map[string]time.Time)
	}
	if _, seen := s.used[nonce]; seen {
		return false
	}
	s.used[nonce] = now
	for k, at := range s.used {
		if now.Sub(at) > localTokenTTL {
			delete(s.used, k)
		}
	}
	return true
}

// localTokenMAC binds the nonce, the expiry and the destination page.
func localTokenMAC(key []byte, nonce, next string, exp int64) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("local:" + nonce + ":" + next + ":" + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// LocalLoginToken signs a one-shot login URL for `next`. `now` is a parameter
// so the tests can mint an expired token without sleeping.
func (s *Server) LocalLoginToken(ctx context.Context, next string, now time.Time) (string, error) {
	key, err := s.loadSessionKey(ctx)
	if err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(randomBytes(16))
	exp := now.Add(localTokenTTL).Unix()
	return buildLocalURL(next, nonce, exp, localTokenMAC(key, nonce, next, exp)), nil
}

func buildLocalURL(next, nonce string, exp int64, sig string) string {
	return localTokenPath + "?n=" + nonce + "&e=" + strconv.FormatInt(exp, 10) +
		"&x=" + url.QueryEscape(next) + "&s=" + sig
}

// validateLocalToken is the whole check as a pure function: fields present, not
// expired, signature valid, and not used before. `consume` is called only when
// the signature checks out, so a forged nonce cannot burn a real one.
func (s *Server) validateLocalToken(r *http.Request, now time.Time) (string, bool) {
	// Not ip.IsLoopback(RemoteAddr): Funnel proxies public traffic from
	// 127.0.0.1, so a loopback peer with proxy headers is the internet.
	if !isLocalRequest(r) {
		return "", false
	}
	q := r.URL.Query()
	nonce, expText, next, sig := q.Get("n"), q.Get("e"), q.Get("x"), q.Get("s")
	exp, err := strconv.ParseInt(expText, 10, 64)
	if err != nil || localTokenError(nonce, next, exp, sig, now) != "" {
		return "", false
	}
	key, err := s.loadSessionKey(r.Context())
	if err != nil {
		return "", false
	}
	want := localTokenMAC(key, nonce, next, exp)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return "", false
	}
	if !s.localTokens.consume(nonce, now) {
		return "", false
	}
	return next, true
}

// handleLocalLogin trades the one-shot link for a session cookie and sends the
// browser on to the requested page. It is the only route where a cookie is set
// without a password.
func (s *Server) handleLocalLogin(w http.ResponseWriter, r *http.Request) {
	next, ok := s.validateLocalToken(r, time.Now())
	if !ok {
		s.log.Printf("server: local login rejected from %s", clientIP(r))
		http.Error(w, "local login link expired or already used", http.StatusUnauthorized)
		return
	}
	s.setSessionCookie(w, r)
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/app"
	}
	http.Redirect(w, r, next, http.StatusFound)
}

// localTokenError explains a rejection in the log without echoing the token.
func localTokenError(nonce, next string, exp int64, sig string, now time.Time) string {
	switch {
	case nonce == "" || sig == "" || next == "":
		return "missing fields"
	case exp < now.Unix():
		return "expired"
	default:
		return ""
	}
}
