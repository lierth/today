package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"today/internal/store"
)

func TestAlarmList(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 8, 0, 0, 0, e.loc)
	at := func(h, m int) *time.Time {
		v := time.Date(2026, 11, 18, h, m, 0, 0, e.loc)
		return &v
	}
	mk := func(title string, start *time.Time, allDay bool, status string) {
		t.Helper()
		if _, err := e.st.CreateItem(ctx, store.Item{
			Title: title, StartsAt: start, AllDay: allDay, Status: status,
		}, e.loc); err != nil {
			t.Fatal(err)
		}
	}
	mk("早会", at(9, 30), false, store.StatusOpen)
	mk("全天", nil, true, store.StatusOpen)
	mk("已取消", at(10, 0), false, store.StatusCancelled)
	mk("已过去", at(7, 0), false, store.StatusOpen)

	got := e.srv.alarmList(ctx, now)
	want := at(9, 30).Unix()
	if got != strconv.FormatInt(want, 10) {
		t.Fatalf("alarm list: got %q want %d", got, want)
	}

	// A done item never rings, even if it has a start time.
	items, err := e.st.ItemsOn(ctx, now.Format(store.DateLayout))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Title == "早会" {
			if err := e.st.SetStatus(ctx, it.ID, store.StatusDone); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := e.srv.alarmList(ctx, now); got != "" {
		t.Fatalf("a done item still rings: %q", got)
	}
}

func TestAlarmLeadTime(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 8, 0, 0, 0, e.loc)
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, e.loc)
	if _, err := e.st.CreateItem(ctx, store.Item{Title: "站会", StartsAt: &start}, e.loc); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		lead string
		want int64
	}{
		{"", start.Unix()},
		{"0", start.Unix()},
		{"5", start.Add(-5 * time.Minute).Unix()},
		{"15", start.Add(-15 * time.Minute).Unix()},
		{"999", start.Unix()}, // out of range falls back to on time
		{"abc", start.Unix()}, // junk falls back to on time
		{"-5", start.Unix()},  // negative is not a lead time
	} {
		if err := e.st.PutSettings(ctx, map[string]string{"alarm_lead_minutes": c.lead}); err != nil {
			t.Fatal(err)
		}
		got := e.srv.alarmList(ctx, now)
		if got != strconv.FormatInt(c.want, 10) {
			t.Errorf("lead %q: got %q want %d", c.lead, got, c.want)
		}
	}
}

func TestAlarmListCapsAtTen(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, e.loc)
	for i := range 15 {
		start := now.Add(time.Duration(i+1) * time.Hour)
		if _, err := e.st.CreateItem(ctx, store.Item{Title: "事项", StartsAt: &start}, e.loc); err != nil {
			t.Fatal(err)
		}
	}
	got := e.srv.alarmList(ctx, now)
	if n := len(strings.Split(got, ",")); n != alarmLimit {
		t.Fatalf("got %d alarms: %q", n, got)
	}
}

func TestSoundPolicy(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if got := e.srv.soundPolicy(ctx); got != "vol=60;keys=1;alarm=1" {
		t.Fatalf("defaults: %q", got)
	}
	if err := e.st.PutSettings(ctx, map[string]string{
		"sound_volume": "20", "sound_keys": "false", "sound_alarm": "false",
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.srv.soundPolicy(ctx); got != "vol=20;keys=0;alarm=0" {
		t.Fatalf("configured: %q", got)
	}
	// Junk volume keeps the default instead of muting the board by accident.
	if err := e.st.PutSettings(ctx, map[string]string{"sound_volume": "loud"}); err != nil {
		t.Fatal(err)
	}
	if got := e.srv.soundPolicy(ctx); !strings.HasPrefix(got, "vol=60;") {
		t.Fatalf("junk volume: %q", got)
	}
}

// A preview is per device and one-shot, and it must not touch the settings
// table (PLAN §9: GET never writes).
func TestPreviewIsPerDeviceAndOneShot(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.srv.RequestPreview("dev1")
	e.srv.RequestPreview("dev2")
	if !e.srv.takePreview("dev1") {
		t.Fatal("dev1 should have a pending preview")
	}
	if e.srv.takePreview("dev1") {
		t.Fatal("dev1 must not preview twice")
	}
	if !e.srv.takePreview("dev2") {
		t.Fatal("dev2 has its own flag")
	}
	if e.srv.takePreview("dev3") {
		t.Fatal("an untouched device must stay silent")
	}
	// Two devices triggered separately each ring exactly once.
	e.srv.RequestPreview("dev1")
	e.srv.RequestPreview("dev1")
	if !e.srv.takePreview("dev1") {
		t.Fatal("re-requesting still yields one preview")
	}
	if e.srv.takePreview("dev1") {
		t.Fatal("coalesced duplicates must not queue a second tone")
	}
	// The settings table is untouched: previews live in memory only.
	if err := e.st.PutSettings(ctx, map[string]string{"sound_preview": "1"}); err == nil {
		t.Fatal("sound_preview is still a known setting; the preview must not touch the database")
	}
}

// Concurrent frame requests for the same device: exactly one carries the flag.
func TestPreviewConcurrentFrames(t *testing.T) {
	e := newTestEnv(t)
	deviceToken := newDeviceToken()
	ctx := context.Background()
	if _, err := e.st.EnsureDevice(ctx, "dev1", "测试设备"); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(deviceToken))
	if err := e.st.SetDeviceToken(ctx, "dev1", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	e.srv.RequestPreview("dev1")
	var wg sync.WaitGroup
	var hits atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodGet, e.ts.URL+"/v1/devices/dev1/frame", nil)
			if err != nil {
				return
			}
			req.Header.Set("X-Device-Token", deviceToken)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			if resp.Header.Get("X-Sound-Preview") == "1" {
				hits.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected exactly one preview, got %d", got)
	}
}

// Two boards, two tones: asking for one board must leave the other silent, and
// each board hears its own request exactly once over HTTP.
func TestPreviewRingsTheRequestedDeviceOnly(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	tokens := map[string]string{}
	for _, id := range []string{"dev1", "dev2"} {
		if _, err := e.st.EnsureDevice(ctx, id, id); err != nil {
			t.Fatal(err)
		}
		token := newDeviceToken()
		tokens[id] = token
		sum := sha256.Sum256([]byte(token))
		if err := e.st.SetDeviceToken(ctx, id, hex.EncodeToString(sum[:])); err != nil {
			t.Fatal(err)
		}
	}
	frame := func(id string) *http.Response {
		req, err := http.NewRequest(http.MethodGet, e.ts.URL+"/v1/devices/"+id+"/frame", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Device-Token", tokens[id])
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp
	}

	e.srv.RequestPreview("dev2")
	if got := frame("dev1").Header.Get("X-Sound-Preview"); got != "" {
		t.Fatalf("dev1 rang for a dev2 request: %q", got)
	}
	if got := frame("dev2").Header.Get("X-Sound-Preview"); got != "1" {
		t.Fatalf("dev2 did not ring: %q", got)
	}
	if got := frame("dev2").Header.Get("X-Sound-Preview"); got != "" {
		t.Fatalf("dev2 rang twice: %q", got)
	}

	// And the other way round, in case the map is keyed wrong.
	e.srv.RequestPreview("dev1")
	if got := frame("dev2").Header.Get("X-Sound-Preview"); got != "" {
		t.Fatalf("dev2 rang for a dev1 request: %q", got)
	}
	if got := frame("dev1").Header.Get("X-Sound-Preview"); got != "1" {
		t.Fatalf("dev1 did not ring: %q", got)
	}
}

// A test tone must not wait for the bitmap to change: the response that
// carries X-Sound-Preview: 1 also carries X-Refresh: 1.
func TestPreviewForcesRefresh(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if _, err := e.st.EnsureDevice(ctx, "dev1", "测试设备"); err != nil {
		t.Fatal(err)
	}
	token := newDeviceToken()
	sum := sha256.Sum256([]byte(token))
	if err := e.st.SetDeviceToken(ctx, "dev1", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	frame := func() *http.Response {
		req, err := http.NewRequest(http.MethodGet, e.ts.URL+"/v1/devices/dev1/frame", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Device-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp
	}
	// Drain the startup full refresh so a plain poll would answer 304.
	frame()
	etag := frame().Header.Get("ETag")
	if etag == "" {
		t.Fatal("no etag")
	}
	poll := func() *http.Response {
		req, err := http.NewRequest(http.MethodGet, e.ts.URL+"/v1/devices/dev1/frame", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Device-Token", token)
		req.Header.Set("If-None-Match", etag)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp
	}
	if got := poll().StatusCode; got != http.StatusNotModified {
		t.Fatalf("quiet poll: %d, want 304", got)
	}
	e.srv.RequestPreview("dev1")
	resp := poll()
	if resp.Header.Get("X-Sound-Preview") != "1" {
		t.Fatalf("no preview header: %v", resp.Header)
	}
	if resp.Header.Get("X-Refresh") != "1" {
		t.Fatalf("preview did not ask for a repaint: %v", resp.Header)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview frame: %d, want 200", resp.StatusCode)
	}
}
