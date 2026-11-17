package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) (*Store, *time.Location) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "today.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	return st, loc
}

func at(loc *time.Location, y int, m time.Month, d, hh, mm int) *time.Time {
	t := time.Date(y, m, d, hh, mm, 0, 0, loc)
	return &t
}

func TestItemLifecycle(t *testing.T) {
	st, loc := openTest(t)
	ctx := context.Background()

	in := Item{Title: "写周报", StartsAt: at(loc, 2026, 11, 17, 10, 0), Description: "把本周的进展整理一下"}
	it, err := st.CreateItem(ctx, in, loc)
	if err != nil {
		t.Fatal(err)
	}
	if it.ID == "" || it.Status != StatusOpen || it.DateLocal != "2026-11-17" {
		t.Fatalf("unexpected item: %+v", it)
	}
	if it.SortKey != it.StartsAt.Unix() {
		t.Fatalf("sort key: got %d want %d", it.SortKey, it.StartsAt.Unix())
	}

	day, err := st.ItemsOn(ctx, "2026-11-17")
	if err != nil {
		t.Fatal(err)
	}
	if len(day) != 1 || day[0].ID != it.ID {
		t.Fatalf("ItemsOn: %+v", day)
	}

	title := "写周报（改）"
	pinned := true
	updated, err := st.UpdateItem(ctx, it.ID, Patch{Title: &title, Pinned: &pinned}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != title || !updated.Pinned {
		t.Fatalf("patch not applied: %+v", updated)
	}

	if err := st.SetStatus(ctx, it.ID, StatusDone); err != nil {
		t.Fatal(err)
	}
	total, done, err := st.TodaySummary(ctx, "2026-11-17")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || done != 1 {
		t.Fatalf("summary: %d/%d", done, total)
	}

	if err := st.DeleteItem(ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Item(ctx, it.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted item still readable: %v", err)
	}
	if _, err := st.UpdateItem(ctx, it.ID, Patch{}, loc); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update deleted item: %v", err)
	}
}

func TestTimedItemMovesDate(t *testing.T) {
	st, loc := openTest(t)
	ctx := context.Background()
	it, err := st.CreateItem(ctx, Item{Title: "晚一点的会", StartsAt: at(loc, 2026, 11, 17, 22, 0)}, loc)
	if err != nil {
		t.Fatal(err)
	}
	start := "2026-11-18T01:00:00+08:00"
	if _, err := st.UpdateItem(ctx, it.ID, Patch{StartsAt: &start}, loc); err != nil {
		t.Fatal(err)
	}
	got, err := st.Item(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DateLocal != "2026-11-18" {
		t.Fatalf("date: got %q want 2026-11-18", got.DateLocal)
	}
	if got.AllDay {
		t.Fatal("timed item became all-day")
	}
}

func TestMaterializeDaily(t *testing.T) {
	st, loc := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 17, 8, 0, 0, 0, loc)
	master, err := st.CreateItem(ctx, Item{
		Title: "吃药", StartsAt: at(loc, 2026, 11, 17, 9, 0), RRule: "FREQ=DAILY;COUNT=3",
	}, loc)
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.Materialize(ctx, loc, now, 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("first materialize: got %d want 2", n)
	}
	if n, err := st.Materialize(ctx, loc, now, 7); err != nil || n != 0 {
		t.Fatalf("second materialize: n=%d err=%v", n, err)
	}
	day, err := st.ItemsOn(ctx, "2026-11-18")
	if err != nil {
		t.Fatal(err)
	}
	if len(day) != 1 {
		t.Fatalf("instances on 11-18: %+v", day)
	}
	if day[0].SeriesID != master.ID || day[0].RRule != "" || day[0].Title != "吃药" {
		t.Fatalf("bad instance: %+v", day[0])
	}
	if day[0].StartsAt == nil || day[0].StartsAt.Format("15:04") != "09:00" {
		t.Fatalf("instance time: %+v", day[0].StartsAt)
	}
}

func TestMaterializeWeeklyAndMonthlyClamp(t *testing.T) {
	st, loc := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 30, 8, 0, 0, 0, loc)
	if _, err := st.CreateItem(ctx, Item{Title: "周会", StartsAt: at(loc, 2026, 1, 30, 10, 0), RRule: "FREQ=WEEKLY;INTERVAL=2;COUNT=3"}, loc); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Materialize(ctx, loc, now, 40); err != nil {
		t.Fatal(err)
	}
	day, err := st.ItemsOn(ctx, "2026-02-13")
	if err != nil {
		t.Fatal(err)
	}
	if len(day) != 1 {
		t.Fatalf("weekly instance missing on 02-13: %+v", day)
	}

	if _, err := st.CreateItem(ctx, Item{Title: "月末结算", StartsAt: at(loc, 2026, 1, 31, 18, 0), RRule: "FREQ=MONTHLY;COUNT=3"}, loc); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Materialize(ctx, loc, now, 60); err != nil {
		t.Fatal(err)
	}
	feb, err := st.ItemsOn(ctx, "2026-02-28")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range feb {
		if it.Title == "月末结算" {
			found = true
		}
	}
	if !found {
		t.Fatalf("monthly instance not clamped to 02-28: %+v", feb)
	}
}

func TestSettingsDefaultsAndValidation(t *testing.T) {
	st, _ := openTest(t)
	ctx := context.Background()
	all, err := st.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range DefaultSettings() {
		if all[k] != want {
			t.Fatalf("default %s: got %q want %q", k, all[k], want)
		}
	}
	if err := st.PutSettings(ctx, map[string]string{"nope": "1"}); err == nil {
		t.Fatal("unknown setting accepted")
	}
	if err := st.PutSettings(ctx, map[string]string{"tray_title": "false"}); err != nil {
		t.Fatal(err)
	}
	if v, err := st.Setting(ctx, "tray_title"); err != nil || v != "false" {
		t.Fatalf("tray_title: %q %v", v, err)
	}
}

func TestDevicesAndOps(t *testing.T) {
	st, _ := openTest(t)
	ctx := context.Background()
	d, err := st.EnsureDevice(ctx, "dev1", "开发设备")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "dev1" {
		t.Fatalf("device: %+v", d)
	}
	if w, h, rowH, font := d.Panel(); w != 400 || h != 300 || rowH != 24 || font != 18 {
		t.Fatalf("panel: %dx%d row %d font %d", w, h, rowH, font)
	}
	if err := st.SetDeviceToken(ctx, "dev1", "hash-1"); err != nil {
		t.Fatal(err)
	}
	if err := st.TouchDevice(ctx, "dev1", 4100); err != nil {
		t.Fatal(err)
	}
	got, err := st.Device(ctx, "dev1")
	if err != nil {
		t.Fatal(err)
	}
	if got.TokenHash != "hash-1" || got.BatteryMV != 4100 || got.LastSeenAt == nil {
		t.Fatalf("device after touch: %+v", got)
	}
	if err := st.RevokeDevice(ctx, "dev1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Device(ctx, "dev1"); got.RevokedAt == nil {
		t.Fatal("device not revoked")
	}
	if other, err := st.EnsureDevice(ctx, "dev1", "重装"); err != nil || other.RevokedAt == nil {
		t.Fatalf("EnsureDevice should keep the device revoked: %+v %v", other, err)
	}

	fresh, err := st.RecordOp(ctx, "op-1", "dev1", "item-1", StatusDone)
	if err != nil || !fresh {
		t.Fatalf("first op: fresh=%v err=%v", fresh, err)
	}
	fresh, err = st.RecordOp(ctx, "op-1", "dev1", "item-1", StatusDone)
	if err != nil || fresh {
		t.Fatalf("duplicate op: fresh=%v err=%v", fresh, err)
	}
}

func TestAuthFailures(t *testing.T) {
	st, _ := openTest(t)
	ctx := context.Background()
	now := time.Now()
	for i := 0; i < 3; i++ {
		if err := st.RecordAuthFail(ctx, "127.0.0.1", now); err != nil {
			t.Fatal(err)
		}
	}
	n, err := st.CountAuthFails(ctx, "127.0.0.1", now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("count: got %d want 3", n)
	}
	pruned, err := st.PruneAuthFails(ctx, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 3 {
		t.Fatalf("pruned: %d", pruned)
	}
}

func TestOpenMissingDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "today.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var n int
	if err := st.db.QueryRow("SELECT count(*) FROM sqlite_master").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("schema missing")
	}
	_ = sql.ErrNoRows
}

func TestPastItems(t *testing.T) {
	st, loc := openTest(t)
	ctx := context.Background()
	now := time.Now().In(loc)
	yesterday := now.AddDate(0, 0, -1).Format(DateLayout)
	today := now.Format(DateLayout)

	if _, err := st.CreateItem(ctx, Item{Title: "昨天", AllDay: true, DateLocal: yesterday}, loc); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateItem(ctx, Item{Title: "今天", AllDay: true, DateLocal: today}, loc); err != nil {
		t.Fatal(err)
	}
	past, err := st.PastItems(ctx, today, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(past) != 1 || past[0].Title != "昨天" {
		t.Fatalf("past items: %+v", past)
	}
	if _, err := st.PastItems(ctx, today, -1); err != nil {
		t.Fatalf("default limit: %v", err)
	}
}

func TestRecurringNeedsTime(t *testing.T) {
	st, loc := openTest(t)
	ctx := context.Background()
	if _, err := st.CreateItem(ctx, Item{Title: "全天重复", AllDay: true, RRule: "FREQ=DAILY"}, loc); err == nil {
		t.Fatal("expected recurring item without a time to fail")
	}
	if err := ValidateRRule("FREQ=YEARLY", loc); err == nil {
		t.Fatal("expected unsupported frequency to fail")
	}
	if err := ValidateRRule("FREQ=WEEKLY;INTERVAL=2;COUNT=4", loc); err != nil {
		t.Fatalf("valid rule: %v", err)
	}
}

func TestSeriesEditSyncsFutureInstances(t *testing.T) {
	st, loc := openTest(t)
	ctx := context.Background()
	now := time.Now().In(loc)
	today := now.Format(DateLayout)
	tomorrow := now.AddDate(0, 0, 1).Format(DateLayout)
	start := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, loc)

	master, err := st.CreateItem(ctx, Item{
		Title: "系列", StartsAt: &start, RRule: "FREQ=DAILY;COUNT=4",
	}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Materialize(ctx, loc, now, 7); err != nil {
		t.Fatal(err)
	}
	instances, err := st.ItemsOn(ctx, tomorrow)
	if err != nil || len(instances) != 1 {
		t.Fatalf("tomorrow before edit: %+v err=%v", instances, err)
	}

	later := time.Date(now.Year(), now.Month(), now.Day(), 10, 30, 0, 0, loc)
	title := "系列改名"
	startStr := timeStr(later)
	dateStr := today
	updated, err := st.UpdateItem(ctx, master.ID, Patch{
		Title: &title, StartsAt: &startStr, DateLocal: &dateStr,
	}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != title {
		t.Fatalf("master title: %+v", updated)
	}
	instances, err = st.ItemsOn(ctx, tomorrow)
	if err != nil || len(instances) != 1 {
		t.Fatalf("tomorrow after edit: %+v err=%v", instances, err)
	}
	if instances[0].Title != title || instances[0].StartsAt == nil ||
		instances[0].StartsAt.Format("15:04") != "10:30" ||
		instances[0].DateLocal != tomorrow {
		t.Fatalf("instance not synced: %+v", instances[0])
	}

	rule := "FREQ=DAILY;COUNT=2"
	if _, err := st.UpdateItem(ctx, master.ID, Patch{RRule: &rule}, loc); err != nil {
		t.Fatal(err)
	}
	instances, err = st.ItemsOn(ctx, tomorrow)
	if err != nil || len(instances) != 1 {
		t.Fatalf("tomorrow after rule change: %+v err=%v", instances, err)
	}
	dayAfter := now.AddDate(0, 0, 2).Format(DateLayout)
	if extra, _ := st.ItemsOn(ctx, dayAfter); len(extra) != 0 {
		t.Fatalf("COUNT=2 should not reach day after: %+v", extra)
	}
}
