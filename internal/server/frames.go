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
	cursor := -1
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, _ = strconv.Atoi(raw)
	}
	forced := s.takeForcedRefresh(device.ID)
	clientForce := r.URL.Query().Get("force") == "1"
	force := clientForce || forced
	fr, err := s.buildFrame(ctx, now, cursor, force)
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
	if next, err := s.st.NextStart(ctx, now); err == nil && next != nil {
		event := next.In(local.Location()).Add(-5 * time.Minute)
		if event.Before(wake) {
			wake = event
			refresh = true
		}
	}
	if capWake := local.Add(6 * time.Hour); capWake.Before(wake) {
		wake = capWake
		refresh = false
	}
	if wake.Before(now) {
		wake = now.Add(time.Minute)
	}
	return wake, refresh
}

type ackRequest struct {
	FrameSeq  int64   `json:"frame_seq"`
	BatteryMV int     `json:"battery_mv,omitempty"`
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
		fresh, err := s.st.RecordOp(ctx, op.OpID, id, op.ItemID, op.Status)
		if err != nil {
			s.log.Printf("server: ack op: %v", err)
			resp.Skipped++
			continue
		}
		if !fresh {
			resp.Duplicates++
			continue
		}
		if err := s.st.SetStatus(ctx, op.ItemID, op.Status); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				resp.Skipped++
				continue
			}
			s.log.Printf("server: ack status: %v", err)
			resp.Skipped++
			continue
		}
		resp.Applied++
	}
	if req.BatteryMV > 0 {
		_ = s.st.TouchDevice(ctx, id, req.BatteryMV)
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
