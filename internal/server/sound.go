package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"today/internal/store"
)

// alarmLimit caps X-Alarms. Ten reminders in a day is already a lot for one
// panel; the firmware keeps exactly this many.
const alarmLimit = 10

// alarmList is today's remaining reminders as Unix seconds, ascending, comma
// separated. Only open, timed items count: an all-day item has no moment to
// ring at, and a completed item must not ring.
//
// The lead time ("提前提醒") is applied here, so the device only compares
// timestamps and never has to know the setting.
func (s *Server) alarmList(ctx context.Context, now time.Time) string {
	items, err := s.st.ItemsOn(ctx, now.Format(store.DateLayout))
	if err != nil {
		return ""
	}
	settings, err := s.st.Settings(ctx)
	if err != nil {
		settings = map[string]string{}
	}
	lead := 0
	if v, err := strconv.Atoi(strings.TrimSpace(settings["alarm_lead_minutes"])); err == nil &&
		v >= 0 && v <= 60 {
		lead = v
	}
	out := make([]string, 0, alarmLimit)
	for _, it := range items {
		if len(out) >= alarmLimit {
			break
		}
		if it.Status == store.StatusCancelled || it.Done() || it.AllDay || it.StartsAt == nil {
			continue
		}
		at := it.StartsAt.Add(-time.Duration(lead) * time.Minute)
		if at.Before(now) {
			// A reminder that already passed today is not re-armed: ticking an
			// old item at 21:00 must not chime for the 09:00 meeting.
			continue
		}
		out = append(out, strconv.FormatInt(at.Unix(), 10))
	}
	return strings.Join(out, ",")
}

// soundPolicy is X-Sound: vol=60;keys=1;alarm=1. The device caches it in NVS so
// a boot without Wi-Fi still respects the person's choice.
func (s *Server) soundPolicy(ctx context.Context) string {
	st, err := s.st.Settings(ctx)
	if err != nil {
		st = map[string]string{}
	}
	vol := 60
	if v, err := strconv.Atoi(strings.TrimSpace(st["sound_volume"])); err == nil && v >= 0 && v <= 100 {
		vol = v
	}
	return fmt.Sprintf("vol=%d;keys=%d;alarm=%d", vol,
		onOff(st["sound_keys"], true), onOff(st["sound_alarm"], true))
}

func onOff(v string, fallback bool) int {
	switch strings.TrimSpace(v) {
	case "":
		if fallback {
			return 1
		}
		return 0
	case "false", "0", "off":
		return 0
	default:
		return 1
	}
}

// Preview is a one-shot "play a test tone on this device" request.
//
// It lives in memory, per device: a global settings key would ring whichever
// board polls first, and writing settings inside the GET frame handler breaks
// PLAN §9 ("GET never writes"). A mutex makes check-and-clear atomic, so two
// concurrent frame requests for the same board cannot both get the flag.
type previewSet struct {
	mu   sync.Mutex
	want map[string]bool
}

// RequestPreview marks one device for a test tone; the next frame it fetches
// carries X-Sound-Preview: 1.
func (s *Server) RequestPreview(deviceID string) {
	if deviceID == "" {
		return
	}
	s.preview.mu.Lock()
	if s.preview.want == nil {
		s.preview.want = make(map[string]bool)
	}
	s.preview.want[deviceID] = true
	s.preview.mu.Unlock()
	// The tone rides on a frame response; without a repaint the board would
	// wait for its next changed bitmap, up to a minute.
	s.ForceRefresh(deviceID)
}

// takePreview consumes the flag atomically.
func (s *Server) takePreview(deviceID string) bool {
	s.preview.mu.Lock()
	defer s.preview.mu.Unlock()
	if !s.preview.want[deviceID] {
		return false
	}
	delete(s.preview.want, deviceID)
	return true
}

func (s *Server) previewPending(deviceID string) bool {
	s.preview.mu.Lock()
	defer s.preview.mu.Unlock()
	return s.preview.want[deviceID]
}

// PreviewPendingForTest reports whether a one-shot preview is queued for the
// board. The frame handler consumes the flag, so a test that wants to see it
// queued must ask before fetching a frame.
func (s *Server) PreviewPendingForTest(deviceID string) bool { return s.previewPending(deviceID) }
