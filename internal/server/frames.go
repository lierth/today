package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"today/internal/frame"
	"today/internal/store"
)

// tokenHash is what the database stores for a device token.
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newDeviceToken returns a fresh plaintext token. Only its hash is stored.
func newDeviceToken() string {
	return b64url(randomBytes(24))
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) authorizeDevice(r *http.Request, id string) (store.Device, bool) {
	d, err := s.st.Device(r.Context(), id)
	if err != nil || d.RevokedAt != nil || d.TokenHash == "" {
		return store.Device{}, false
	}
	token := r.Header.Get("X-Device-Token")
	if token == "" {
		return store.Device{}, false
	}
	if !hmac.Equal([]byte(tokenHash(token)), []byte(d.TokenHash)) {
		return store.Device{}, false
	}
	return d, true
}

// ForceFullRefresh asks every device to repaint its cached frame. It is used
// after a server restart and when the host resumes from sleep; the flag rides
// in X-Refresh rather than the body so the ETag and 304 path stay intact.
func (s *Server) ForceFullRefresh() {
	s.refreshMu.Lock()
	s.refreshAll = true
	s.refreshDone = map[string]bool{}
	s.refreshMu.Unlock()
}

func (s *Server) takeForcedRefresh(deviceID string) bool {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if !s.refreshAll || s.refreshDone[deviceID] {
		return false
	}
	s.refreshDone[deviceID] = true
	return true
}

func (s *Server) handleFrame(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	device, ok := s.authorizeDevice(r, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	now := time.Now().In(loc)
	// The device draws its own cursor (and local strike-through) so moving it
	// never depends on the network and never changes the ETag. Only the web
	// preview takes a cursor parameter.
	forced := s.takeForcedRefresh(device.ID)
	clientForce := r.URL.Query().Get("force") == "1"
	force := clientForce || forced
	fr, err := s.buildFrame(ctx, now, -1, force)
	if err != nil {
		s.log.Printf("server: frame: %v", err)
		http.Error(w, "render", http.StatusInternalServerError)
		return
	}
	body := fr.Encoded
	// ETag describes the content, not the one-shot force_full control bit, so
	// a forced 200 is followed by a normal 304 instead of another full frame.
	etagBody := append([]byte(nil), body...)
	if len(etagBody) > 6 {
		etagBody[6] &^= frame.FlagForceFull
	}
	if len(etagBody) >= 4 {
		etagBody = etagBody[:len(etagBody)-4] // CRC follows the bitmap.
	}
	sum := sha256.Sum256(etagBody)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	seq := s.frameSeq.Add(1)

	h := w.Header()
	if version, err := s.st.Setting(ctx, "firmware_version"); err == nil && version != "" {
		h.Set("X-Firmware-Version", version)
	}
	h.Set("ETag", etag)
	h.Set("X-Frame-Seq", strconv.FormatUint(seq, 10))
	h.Set("X-Server-Now", strconv.FormatInt(time.Now().Unix(), 10))
	// Seconds east of UTC at "now", DST included. The device draws the clock
	// locally, so it needs the offset even when the body is a 304.
	_, offset := now.Zone()
	h.Set("X-UTC-Offset", strconv.Itoa(offset))
	// Night off is a configuration of the whole board, not part of the
	// bitmap: a header keeps the ETag stable when the window changes.
	if night, ok := s.nightWindow(ctx); ok {
		h.Set("X-Night", night)
	} else {
		h.Set("X-Night", "")
	}
	wake, refresh := s.nextWake(ctx, now)
	refresh = refresh || forced || clientForce
	h.Set("X-Next-Wake-At", strconv.FormatInt(wake.Unix(), 10))
	if refresh {
		h.Set("X-Refresh", "1")
	} else {
		h.Set("X-Refresh", "0")
	}
	h.Set("Cache-Control", "no-cache")

	_ = s.st.TouchDevice(ctx, id, 0)
	// A device that asks for force=1 wants the bits even when its ETag still
	// matches: it is about to repaint a blank or half-written panel.
	if !forced && !clientForce && etag == r.Header.Get("If-None-Match") {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

// nightWindow returns "start-end" in local minutes, e.g. "1380-420", and false
// when night-off is disabled. Only a valid HH:MM pair is reported.
func (s *Server) nightWindow(ctx context.Context) (string, bool) {
	settings, err := s.st.Settings(ctx)
	if err != nil || !store.BoolSetting(settings, "night_off") {
		return "", false
	}
	start, sok := minutesOfDay(settings["night_start"])
	end, eok := minutesOfDay(settings["night_end"])
	if !sok || !eok {
		return "", false
	}
	return strconv.Itoa(start) + "-" + strconv.Itoa(end), true
}

func minutesOfDay(hhmm string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(hhmm), ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// buildFrame renders today's board. GET never writes to the database.
func (s *Server) buildFrame(ctx context.Context, now time.Time, cursor int, force bool) (*frame.Frame, error) {
	items, err := s.st.ItemsOn(ctx, now.Format(store.DateLayout))
	if err != nil {
		return nil, err
	}
	frameItems := make([]frame.Item, 0, len(items))
	for _, it := range items {
		if it.Status == store.StatusCancelled {
			continue
		}
		frameItems = append(frameItems, frame.Item{
			ID:       it.ID,
			Title:    it.Title,
			Done:     it.Done(),
			AllDay:   it.AllDay,
			Pinned:   it.Pinned,
			StartsAt: it.StartsAt,
		})
	}
	return frame.Build(frame.Input{
		Now:         now,
		Items:       frameItems,
		CursorRow:   cursor,
		CursorStyle: frame.CursorStyleInvert,
		ForceFull:   force,
	})
}

// nextWake implements: min(next event - 5m, next local 00:05, now + 6h).
func (s *Server) nextWake(ctx context.Context, now time.Time) (time.Time, bool) {
	local := now.In(now.Location())
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 5, 0, 0, local.Location()).AddDate(0, 0, 1)
	wake := midnight
	refresh := false
	// Wake five minutes before the next event so the panel is current by the
	// time it starts. Looking up the first event whose lead has not yet passed
	// keeps an event inside the window from hiding the one after it, and keeps
	// the device from waking every minute until that event starts.
	lead := 5 * time.Minute
	if next, err := s.st.NextStart(ctx, now.Add(lead)); err == nil && next != nil {
		event := next.In(local.Location()).Add(-lead)
		if event.After(now) && event.Before(wake) {
			wake = event
			refresh = true
		}
	}
	if capWake := local.Add(6 * time.Hour); capWake.Before(wake) {
		wake = capWake
		refresh = false
	}
	// Both sides truncate to Unix seconds, so a wake less than a second away
	// can arrive as next == now and the device treats it as "no deadline" and
	// sleeps its own fallback of six hours. Keep a minute of lead, which is
	// also the firmware's minimum sleep.
	if minWake := now.Add(time.Minute); wake.Before(minWake) {
		wake = minWake
	}
	return wake, refresh
}

type ackRequest struct {
	FrameSeq  int64   `json:"frame_seq"`
	BatteryMV int     `json:"battery_mv,omitempty"`
	Power     string  `json:"power,omitempty"` // usb | battery | charging
	Ops       []ackOp `json:"ops"`
}

type ackOp struct {
	OpID   string `json:"op_id"`
	ItemID string `json:"item_id"`
	Status string `json:"status"`
}

type ackResponse struct {
	Applied    int `json:"applied"`
	Duplicates int `json:"duplicates"`
	Skipped    int `json:"skipped"`
}

func (s *Server) handleAcks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.authorizeDevice(r, id); !ok {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	defer r.Body.Close()
	var req ackRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(req.Ops) > 64 {
		http.Error(w, "too many ops", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var resp ackResponse
	for _, op := range req.Ops {
		if op.OpID == "" || op.ItemID == "" {
			resp.Skipped++
			continue
		}
		switch op.Status {
		case store.StatusOpen, store.StatusDone:
		default:
			resp.Skipped++
			continue
		}
		result, err := s.st.ApplyOp(ctx, op.OpID, id, op.ItemID, op.Status)
		if err != nil {
			s.log.Printf("server: ack op: %v", err)
			resp.Skipped++
			continue
		}
		switch result {
		case store.AckApplied:
			resp.Applied++
		case store.AckDuplicate:
			resp.Duplicates++
		default:
			resp.Skipped++
		}
	}
	if req.BatteryMV > 0 {
		_ = s.st.TouchDevice(ctx, id, req.BatteryMV)
	}
	if req.Power != "" {
		_, _ = s.UpdateSettings(ctx, map[string]string{"power_state": req.Power})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	scale, _ := strconv.Atoi(r.URL.Query().Get("scale"))
	if scale < 1 || scale > 4 {
		scale = 2
	}
	cursor := -1
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, _ = strconv.Atoi(raw)
	}
	fr, err := s.buildFrame(ctx, time.Now().In(loc), cursor, false)
	if err != nil {
		http.Error(w, "render", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	if err := fr.WritePNG(w, scale); err != nil {
		http.Error(w, "png", http.StatusInternalServerError)
	}
}
