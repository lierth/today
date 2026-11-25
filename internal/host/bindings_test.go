package host

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"today/internal/server"
	"today/internal/store"
)

// newPanelEnv builds the real server (and its temp database) under a Host, so
// the bindings are exercised through the same code the app runs. Nothing here
// touches the tray, the keychain or the network.
func newPanelEnv(t *testing.T) (*Host, *Panel, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "today.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(st, server.Config{
		Addr: "127.0.0.1:0", DataDir: dir, Dev: true,
		Logger: log.New(io.Discard, "", 0),
		Paths: server.Paths{
			Root: dir, DB: filepath.Join(dir, "today.db"),
			Backups: filepath.Join(dir, "backups"), Firmware: filepath.Join(dir, "firmware"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutSettings(context.Background(), map[string]string{"timezone": "Asia/Shanghai"}); err != nil {
		t.Fatal(err)
	}
	pin := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)
	st.SetNow(func() time.Time { return pin })
	h := New(srv, true, log.New(io.Discard, "", 0))
	return h, NewPanel(h), st
}

func TestPanelTodayShape(t *testing.T) {
	_, p, st := newPanelEnv(t)
	ctx := context.Background()
	if _, err := st.CreateItem(ctx, store.Item{Title: "写周报", AllDay: true, DateLocal: "2026-11-19"}, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateItem(ctx, store.Item{Title: "站会", DateLocal: "2026-11-19", StartsAt: new(time.Date(2026, 11, 19, 10, 30, 0, 0, time.UTC))}, time.UTC); err != nil {
		t.Fatal(err)
	}
	view, err := p.Today(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.Date != "11月19日" || view.Weekday != "周四" {
		t.Fatalf("day = %q %q, want 11月19日 周四", view.Date, view.Weekday)
	}
	if !strings.HasPrefix(view.Lunar, "农历") {
		t.Fatalf("lunar = %q, want 农历…", view.Lunar)
	}
	if view.Total != 2 || view.Done != 0 {
		t.Fatalf("progress = %d/%d, want 0/2", view.Done, view.Total)
	}
	if len(view.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(view.Items))
	}
	// The timed row carries its clock; the all-day one must not pretend to.
	times := map[string]string{}
	for _, it := range view.Items {
		times[it.Title] = it.Time
	}
	if times["站会"] != "10:30" {
		t.Fatalf("站会 time = %q, want 10:30", times["站会"])
	}
	if times["写周报"] != "" {
		t.Fatalf("all-day time = %q, want empty", times["写周报"])
	}
}

func TestTrayProgressUsesDoneOverTotalWithEvenSpacing(t *testing.T) {
	h, _, st := newPanelEnv(t)
	ctx := context.Background()
	h.settings = map[string]string{"tray_mode": "progress"}
	for _, status := range []string{store.StatusDone, store.StatusOpen} {
		if _, err := st.CreateItem(ctx, store.Item{Title: "事项", AllDay: true, Status: status}, time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.title(); got != "1 / 2" {
		t.Fatalf("tray progress = %q, want 1 / 2", got)
	}
}

func TestPanelSetDoneAndAdd(t *testing.T) {
	_, p, _ := newPanelEnv(t)
	ctx := context.Background()
	it, err := p.Add(ctx, "明天 10:30 站会")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "站会" || it.Time != "10:30" {
		t.Fatalf("add = %+v, want 站会 at 10:30 tomorrow", it)
	}
	if err := p.SetDone(ctx, it.ID, true); err != nil {
		t.Fatal(err)
	}
	view, err := p.Today(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.Total != 0 {
		t.Fatalf("today has %d rows, want 0: the item was added for tomorrow", view.Total)
	}
	if err := p.SetDone(ctx, "", true); err == nil {
		t.Fatal("empty id must be refused")
	}
	if err := p.SetDone(ctx, "nope", true); err == nil {
		t.Fatal("unknown id must be refused")
	}
}

func TestPanelParseTags(t *testing.T) {
	_, p, _ := newPanelEnv(t)
	tags, err := p.Parse(context.Background(), "明天 10:30 站会")
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0] != "11月20日" || tags[1] != "10:30" {
		t.Fatalf("tags = %v, want [11月20日 10:30]", tags)
	}
	tags, err = p.Parse(context.Background(), "每天 背单词")
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0] != "重复" {
		t.Fatalf("tags = %v, want [重复]", tags)
	}
}

func TestPanelSettingsRejectUnknownKey(t *testing.T) {
	_, p, _ := newPanelEnv(t)
	ctx := context.Background()
	if err := p.UpdateSettings(ctx, map[string]string{"nope": "1"}); err == nil {
		t.Fatal("unknown key must be refused")
	}
	if err := p.UpdateSettings(ctx, map[string]string{"night_off": "true"}); err != nil {
		t.Fatal(err)
	}
	st, err := p.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st["night_off"] != "true" {
		t.Fatalf("night_off = %q, want true", st["night_off"])
	}
}

func TestPanelPreviewSoundMarksTheBoard(t *testing.T) {
	h, p, st := newPanelEnv(t)
	ctx := context.Background()
	if _, err := st.CreateDevice(ctx, "dev1", "书桌", ""); err != nil {
		t.Fatal(err)
	}
	if err := p.PreviewSound(ctx, "dev1"); err != nil {
		t.Fatal(err)
	}
	if !previewPending(t, h.srv, "dev1") {
		t.Fatal("preview flag was not set for dev1")
	}
	if err := p.PreviewSound(ctx, ""); err == nil {
		t.Fatal("empty device id must be refused")
	}
}

// previewPending asks the server whether the one-shot flag is queued. The
// server keeps the check unexported; the frame request consumes it, so the test
// reads it from the same package's test hook instead of racing a frame.
func previewPending(t *testing.T, srv *server.Server, id string) bool {
	t.Helper()
	return srv.PreviewPendingForTest(id)
}

func TestPanelVersion(t *testing.T) {
	_, p, _ := newPanelEnv(t)
	old := Version
	Version = "9.9.9"
	t.Cleanup(func() { Version = old })
	cfg := p.Version()
	if cfg.Version != "9.9.9" || !cfg.Dev {
		t.Fatalf("version = %+v, want 9.9.9 dev", cfg)
	}
}
