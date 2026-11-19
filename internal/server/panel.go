package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"today/internal/store"
)

const panelTokenTTL = time.Hour

// PanelToken signs a short-lived read-only URL for the tray flyout. The panel
// is not a session: it only shows today, it cannot change anything, and it
// expires even if the window stays open.
func (s *Server) PanelToken() (string, error) {
	key, err := s.loadSessionKey(context.Background())
	if err != nil {
		return "", err
	}
	exp := time.Now().Add(panelTokenTTL).Unix()
	return strconv.FormatInt(exp, 10) + "." + panelMAC(key, exp), nil
}

func panelMAC(key []byte, exp int64) string {
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "panel:%d", exp)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) panelOK(raw string) bool {
	expText, sig, ok := strings.Cut(raw, ".")
	if !ok || expText == "" || sig == "" {
		return false
	}
	exp, err := strconv.ParseInt(expText, 10, 64)
	if err != nil || exp < time.Now().Unix() {
		return false
	}
	key, err := s.loadSessionKey(context.Background())
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(panelMAC(key, exp)))
}

func (s *Server) handlePanel(w http.ResponseWriter, r *http.Request) {
	// The panel link carries no session, so the source of the request is part
	// of the lock: Funnel proxies public traffic from 127.0.0.1, and a leaked
	// link pasted elsewhere must not be readable through it.
	if !isLocalRequest(r) {
		http.Error(w, "panel link requires the local machine", http.StatusForbidden)
		return
	}
	token := r.URL.Query().Get("t")
	if !s.panelOK(token) {
		http.Error(w, "panel link expired", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	now := s.clock().In(loc)
	date := now.Format(store.DateLayout)
	items, err := s.st.ItemsOn(ctx, date)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	done := 0
	visible := items[:0]
	for _, it := range items {
		if it.Status == store.StatusCancelled {
			continue
		}
		visible = append(visible, it)
		if it.Done() {
			done++
		}
	}
	data := pageData{
		Title:      "Today",
		Nav:        "panel",
		DateLabel:  now.Format("01月02日"),
		Items:      visible,
		Total:      len(visible),
		Done:       done,
		PanelToken: token,
	}
	if r.Header.Get("HX-Request") == "true" {
		s.render(w, r, "panel_list.html", data)
		return
	}
	s.render(w, r, "panel.html", data)
}
