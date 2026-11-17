package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	sessionCookie = "today_session"
	sessionUser   = "owner"
	sessionTTL    = 30 * 24 * time.Hour

	// Argon2id parameters from docs/PLAN.md §5.1.
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonSaltLen = 16
	argonKeyLen  = 32
)

func b64(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("server: crypto/rand: " + err.Error())
	}
	return b
}

// hashPassword returns a PHC argon2id string.
func hashPassword(pw string) (string, error) {
	if pw == "" {
		return "", errors.New("server: empty password")
	}
	salt := randomBytes(argonSaltLen)
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads, b64(salt), b64(key)), nil
}

// verifyPassword compares a password with a PHC argon2id string.
func verifyPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory uint32
	var iter uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iter, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, iter, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

const passwordAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

func randomPassword(n int) string {
	out := make([]byte, n)
	buf := randomBytes(n)
	for i := range out {
		out[i] = passwordAlphabet[int(buf[i])%len(passwordAlphabet)]
	}
	return string(out)
}

// EnsureAuth makes sure a password hash and a session key exist. It returns
// the generated plaintext password once, or "" when one was already set.
func (s *Server) EnsureAuth(ctx context.Context) (string, error) {
	hash, err := s.st.Setting(ctx, "auth_password_hash")
	if err != nil {
		return "", err
	}
	generated := ""
	if hash == "" {
		pw := randomPassword(12)
		h, err := hashPassword(pw)
		if err != nil {
			return "", err
		}
		if err := s.st.PutSettings(ctx, map[string]string{"auth_password_hash": h}); err != nil {
			return "", err
		}
		generated = pw
	}
	if _, err := s.loadSessionKey(ctx); err != nil {
		return "", err
	}
	return generated, nil
}

// SetPassword replaces the password and rotates the session key, which
// invalidates every existing cookie.
func (s *Server) SetPassword(ctx context.Context, pw string) error {
	if len([]rune(pw)) < 8 {
		return errors.New("口令至少 8 位")
	}
	h, err := hashPassword(pw)
	if err != nil {
		return err
	}
	if err := s.st.PutSettings(ctx, map[string]string{"auth_password_hash": h}); err != nil {
		return err
	}
	return s.RotateSessionKey(ctx)
}

// RotateSessionKey logs every browser session out.
func (s *Server) RotateSessionKey(ctx context.Context) error {
	key := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	if err := s.st.PutSettings(ctx, map[string]string{"auth_session_key": key}); err != nil {
		return err
	}
	s.sessionMu.Lock()
	s.sessionKey = nil
	s.sessionMu.Unlock()
	return nil
}

func (s *Server) loadSessionKey(ctx context.Context) ([]byte, error) {
	s.sessionMu.RLock()
	key := s.sessionKey
	s.sessionMu.RUnlock()
	if key != nil {
		return key, nil
	}
	raw, err := s.st.Setting(ctx, "auth_session_key")
	if err != nil {
		return nil, err
	}
	if raw == "" {
		raw = base64.RawURLEncoding.EncodeToString(randomBytes(32))
		if err := s.st.PutSettings(ctx, map[string]string{"auth_session_key": raw}); err != nil {
			return nil, err
		}
	}
	key, err = base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("server: session key: %w", err)
	}
	s.sessionMu.Lock()
	s.sessionKey = key
	s.sessionMu.Unlock()
	return key, nil
}

func signSession(key []byte, payload string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request) {
	key, err := s.loadSessionKey(r.Context())
	if err != nil {
		s.log.Printf("server: session key: %v", err)
		return
	}
	exp := time.Now().Add(sessionTTL)
	payload := sessionUser + "|" + strconv.FormatInt(exp.Unix(), 10)
	sig := signSession(key, payload)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    payload + "|" + sig,
		Path:     "/",
		Expires:  exp,
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// secureCookie marks the cookie Secure over TLS (directly or via the funnel
// proxy). Local http logins still work; Dev forces it off.
func (s *Server) secureCookie(r *http.Request) bool {
	if s.cfg.Dev {
		return false
	}
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) validSession(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	parts := strings.Split(c.Value, "|")
	if len(parts) != 3 || parts[0] != sessionUser {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() >= exp {
		return false
	}
	key, err := s.loadSessionKey(r.Context())
	if err != nil {
		return false
	}
	want := signSession(key, parts[0]+"|"+parts[1])
	return hmac.Equal([]byte(want), []byte(parts[2]))
}

// requireSession guards browser routes and API endpoints.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.validSession(r) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				if r.Header.Get("HX-Request") == "true" {
					w.Header().Set("HX-Redirect", "/login")
				}
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.validSession(r) {
		http.Redirect(w, r, "/app", http.StatusFound)
		return
	}
	s.render(w, r, "login.html", pageData{Title: "登录"})
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.URL.Query().Get("logout") == "1" {
		s.clearSessionCookie(w, r)
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	ip := clientIP(r)
	now := time.Now()
	if !s.limiter.allow(ip, now) {
		s.renderStatus(w, r, http.StatusTooManyRequests, "login.html",
			pageData{Title: "登录", Error: "尝试太频繁，请稍后再试。"})
		return
	}
	pw := r.FormValue("password")
	hash, err := s.st.Setting(ctx, "auth_password_hash")
	if err != nil {
		s.renderStatus(w, r, http.StatusInternalServerError, "login.html",
			pageData{Title: "登录", Error: "服务暂时不可用。"})
		return
	}
	if pw == "" || !verifyPassword(hash, pw) {
		delay := s.limiter.fail(ip, now)
		_ = s.st.RecordAuthFail(ctx, ip, now)
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
			}
		}
		s.renderStatus(w, r, http.StatusUnauthorized, "login.html",
			pageData{Title: "登录", Error: "口令不对。"})
		return
	}
	s.limiter.success(ip)
	s.setSessionCookie(w, r)
	http.Redirect(w, r, "/app", http.StatusFound)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// loginLimiter enforces 5 attempts per minute per IP and an increasing
// delay after each failure (1s, 2s, 4s, capped at 8s).
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{fails: make(map[string][]time.Time)}
}

func (l *loginLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	list := l.recent(ip, now)
	return len(list) < 5
}

func (l *loginLimiter) fail(ip string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	list := append(l.recent(ip, now), now)
	l.fails[ip] = list
	delay := time.Second << min(len(list)-1, 3)
	if delay > 8*time.Second {
		delay = 8 * time.Second
	}
	return delay
}

func (l *loginLimiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}

func (l *loginLimiter) recent(ip string, now time.Time) []time.Time {
	cutoff := now.Add(-time.Minute)
	list := l.fails[ip]
	kept := list[:0]
	for _, t := range list {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.fails[ip] = kept
	return kept
}
