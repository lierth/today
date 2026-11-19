package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newCarryStore(t *testing.T) (*Store, *time.Location) {
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

// newPinnedStore is newCarryStore with the clock fixed to the date the
// fixtures use. Without this, every dated fixture here is a time bomb: the
// rows stop being "today" the day the calendar moves past them.
func newPinnedStore(t *testing.T, day string) (*Store, *time.Location) {
	t.Helper()
	st, loc := newCarryStore(t)
	pin := atDay(loc, day, 10, 0)
	st.SetNow(func() time.Time { return *pin })
	return st, loc
}

func atDay(loc *time.Location, day string, hh, mm int) *time.Time {
	v, err := time.ParseInLocation(DateLayout+" 15:04", day+" 00:00", loc)
	if err != nil {
		panic(err)
	}
	v = v.Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute)
	return &v
}

// Only open, non-series items from the last two weeks are offered; a done row,
// an old row and a series occurrence stay out of "之前未完成".
func TestUnfinishedWindow(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	today := "2026-11-19"
	mk := func(title, day string, status string, series string) Item {
		t.Helper()
		it, err := st.CreateItem(ctx, Item{
			Title: title, StartsAt: atDay(loc, day, 9, 0), Status: status, SeriesID: series,
		}, loc)
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	mk("昨天的", "2026-11-18", StatusOpen, "")
	mk("两周前的", "2026-11-05", StatusOpen, "")   // exactly 14 days back: inside
	mk("三周前的", "2026-10-20", StatusOpen, "")   // outside
	mk("已完成的", "2026-11-18", StatusDone, "")   // not open
	mk("重复实例", "2026-11-18", StatusOpen, "s1") // past series instance

	items, err := st.Unfinished(ctx, today, loc)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range items {
		got[it.Title] = true
	}
	for _, want := range []string{"昨天的", "两周前的"} {
		if !got[want] {
			t.Errorf("%q missing from unfinished", want)
		}
	}
	for _, not := range []string{"三周前的", "已完成的", "重复实例"} {
		if got[not] {
			t.Errorf("%q must not be offered", not)
		}
	}
	if len(items) != 2 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	// Oldest first: the page reads chronologically.
	if items[0].Title != "两周前的" {
		t.Fatalf("order: %s first", items[0].Title)
	}
}

// Carrying keeps the clock time, records where it came from, and leaves a
// done or already-today row alone.
func TestCarryItemsKeepsClockAndMarksOrigin(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	it, err := st.CreateItem(ctx, Item{Title: "写周报", StartsAt: atDay(loc, "2026-11-17", 21, 30)}, loc)
	if err != nil {
		t.Fatal(err)
	}
	done, err := st.CreateItem(ctx, Item{
		Title: "已完成", StartsAt: atDay(loc, "2026-11-17", 8, 0), Status: StatusDone,
	}, loc)
	if err != nil {
		t.Fatal(err)
	}
	res, err := st.CarryItems(ctx, []string{it.ID, done.ID, "missing"}, "2026-11-19", loc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Moved != 1 {
		t.Fatalf("moved %d items, want 1 (skipped %v)", res.Moved, res.Skipped)
	}
	if len(res.Skipped) != 2 {
		t.Fatalf("skips: %v", res.Skipped)
	}
	got, err := st.Item(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DateLocal != "2026-11-19" {
		t.Fatalf("date_local = %q", got.DateLocal)
	}
	if got.CarriedFrom != "2026-11-17" {
		t.Fatalf("carried_from = %q", got.CarriedFrom)
	}
	if got.StartsAt == nil || got.StartsAt.Format("15:04") != "21:30" {
		t.Fatalf("clock moved: %v", got.StartsAt)
	}
	// Moving again keeps the original slip date, not yesterday's.
	if _, err := st.CarryItems(ctx, []string{it.ID}, "2026-11-20", loc); err != nil {
		t.Fatal(err)
	}
	again, _ := st.Item(ctx, it.ID)
	if again.CarriedFrom != "2026-11-17" {
		t.Fatalf("second carry rewrote the origin: %q", again.CarriedFrom)
	}
	// A done item never moves.
	if d, _ := st.Item(ctx, done.ID); d.DateLocal != "2026-11-17" {
		t.Fatalf("done item moved to %q", d.DateLocal)
	}
}

// Manual order only applies to dragged rows; an untouched day keeps its clock
// order, and dragging puts the rows in the submitted order.
func TestReorderAndManualRank(t *testing.T) {
	st, loc := newPinnedStore(t, "2026-11-19")
	ctx := context.Background()
	early, _ := st.CreateItem(ctx, Item{Title: "早", StartsAt: atDay(loc, "2026-11-19", 8, 0)}, loc)
	late, _ := st.CreateItem(ctx, Item{Title: "晚", StartsAt: atDay(loc, "2026-11-19", 17, 0)}, loc)
	free, _ := st.CreateItem(ctx, Item{Title: "全天", AllDay: true, DateLocal: "2026-11-19"}, loc)

	titles := func() []string {
		t.Helper()
		items, err := st.ItemsOn(ctx, "2026-11-19")
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(items))
		for i, it := range items {
			out[i] = it.Title
		}
		return out
	}
	if got := titles(); got[0] != "全天" || got[1] != "早" || got[2] != "晚" {
		t.Fatalf("time order: %v", got)
	}
	if err := st.Reorder(ctx, []string{late.ID, early.ID, free.ID}, loc); err != nil {
		t.Fatal(err)
	}
	if got := titles(); got[0] != "晚" || got[1] != "早" || got[2] != "全天" {
		t.Fatalf("manual order: %v", got)
	}
	// Editing the clock afterwards must not lose the manual order.
	later := atDay(loc, "2026-11-19", 23, 0)
	v := timeStr(*later)
	if _, err := st.UpdateItem(ctx, early.ID, Patch{StartsAt: &v}, loc); err != nil {
		t.Fatal(err)
	}
	if got := titles(); got[0] != "晚" || got[1] != "早" {
		t.Fatalf("manual order after edit: %v", got)
	}
}

// Pinned always wins over both the clock and the drag order.
func TestPinnedBeatsManualRank(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	a, _ := st.CreateItem(ctx, Item{Title: "A", StartsAt: atDay(loc, "2026-11-19", 8, 0)}, loc)
	b, _ := st.CreateItem(ctx, Item{Title: "B", StartsAt: atDay(loc, "2026-11-19", 9, 0)}, loc)
	if err := st.Reorder(ctx, []string{a.ID, b.ID}, loc); err != nil {
		t.Fatal(err)
	}
	pin := true
	if _, err := st.UpdateItem(ctx, b.ID, Patch{Pinned: &pin}, loc); err != nil {
		t.Fatal(err)
	}
	items, _ := st.ItemsOn(ctx, "2026-11-19")
	if items[0].Title != "B" {
		t.Fatalf("pinned row is not first: %+v", items)
	}
}

// A delete can be undone, and the restored row is live again.
func TestRestoreItem(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	it, _ := st.CreateItem(ctx, Item{Title: "删我", DateLocal: "2026-11-19", AllDay: true}, loc)
	if err := st.DeleteItem(ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Item(ctx, it.ID); err == nil {
		t.Fatal("deleted item still loads")
	}
	back, err := st.RestoreItem(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != it.ID || back.Title != "删我" {
		t.Fatalf("restore: %+v", back)
	}
	if _, err := st.RestoreItem(ctx, it.ID); err == nil {
		t.Fatal("restoring a live item must fail")
	}
}

// Search covers the title and the note, and a literal % in the query does not
// turn into a wildcard.
func TestSearchTitleAndNote(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	mk := func(title, note string) {
		t.Helper()
		if _, err := st.CreateItem(ctx, Item{
			Title: title, Description: note, DateLocal: "2026-11-19", AllDay: true,
		}, loc); err != nil {
			t.Fatal(err)
		}
	}
	mk("买牛奶", "顺路买鸡蛋")
	mk("写周报", "")
	mk("100% 完成", "百分比")

	for _, c := range []struct {
		q    string
		want int
	}{
		{"牛奶", 1},
		{"鸡蛋", 1},
		{"周报", 1},
		{"%", 1}, // only the literal-percent title, not everything
		{"不存在的", 0},
		{"", 0},
	} {
		got, err := st.Search(ctx, c.q, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != c.want {
			t.Errorf("search %q: %d hits, want %d", c.q, len(got), c.want)
		}
	}
}

// The upcoming list shows a series once, at its soonest instance.
func TestUpcomingCollapsesSeries(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	anchor := atDay(loc, "2026-11-19", 9, 0)
	master, err := st.CreateItem(ctx, Item{
		Title: "站会", StartsAt: anchor, RRule: "FREQ=DAILY",
	}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Materialize(ctx, loc, *anchor, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateItem(ctx, Item{
		Title: "体检", StartsAt: atDay(loc, "2026-11-22", 8, 0),
	}, loc); err != nil {
		t.Fatal(err)
	}
	items, err := st.Upcoming(ctx, "2026-11-19", 30, 100)
	if err != nil {
		t.Fatal(err)
	}
	series := 0
	for _, it := range items {
		if it.SeriesID == master.ID {
			series++
		}
	}
	if series != 1 {
		t.Fatalf("series rows: %d", series)
	}
	var days []string
	for _, it := range items {
		days = append(days, it.DateLocal+" "+it.Title)
	}
	if days[0] != "2026-11-19 站会" {
		t.Fatalf("order: %v", days)
	}
}

// BYDAY weekly rules: Monday-first expansion, the anchor's own week starts at
// the anchor, INTERVAL skips whole weeks, COUNT counts real occurrences and
// UNTIL stops the expansion.
func TestOccurrenceByDay(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	mk := func(start string, rule string) recurrence {
		t.Helper()
		rec, err := parseRRule(rule, loc)
		if err != nil {
			t.Fatalf("%s: %v", rule, err)
		}
		return rec
	}
	// 2026-11-18 is a Wednesday.
	anchor := time.Date(2026, 11, 18, 10, 0, 0, 0, loc)
	rec := mk("", "FREQ=WEEKLY;BYDAY=MO,WE,FR")
	var got []string
	for i := 0; i < 6; i++ {
		got = append(got, occurrence(anchor, rec, i).Format("01-02 15:04"))
	}
	want := []string{"11-18 10:00", "11-20 10:00", "11-23 10:00", "11-25 10:00", "11-27 10:00", "11-30 10:00"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byday: got %v want %v", got, want)
		}
	}

	// The order the rule writes the days does not matter.
	rec = mk("", "FREQ=WEEKLY;BYDAY=FR,MO,WE")
	if a, b := occurrence(anchor, rec, 1).Format("01-02"), "11-20"; a != b {
		t.Fatalf("sorted byday: %s", a)
	}

	// INTERVAL=2: the anchor's week fires Wed 11-18 and Fri 11-20, then the
	// rule waits two weeks and continues on Monday 11-30.
	rec = mk("", "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR")
	if got := occurrence(anchor, rec, 2).Format("01-02"); got != "11-30" {
		t.Fatalf("interval 2: %s", got)
	}
	if got := occurrence(anchor, rec, 3).Format("01-02"); got != "12-02" {
		t.Fatalf("interval 2 second week: %s", got)
	}

	// A Monday anchor with BYDAY=MO,WE starts on that Monday.
	monday := time.Date(2026, 11, 16, 10, 0, 0, 0, loc)
	rec = mk("", "FREQ=WEEKLY;BYDAY=MO,WE")
	if got := occurrence(monday, rec, 0).Format("01-02"); got != "11-16" {
		t.Fatalf("monday anchor: %s", got)
	}

	// Workdays.
	rec = mk("", "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR")
	var days []string
	for i := 0; i < 5; i++ {
		days = append(days, occurrence(monday, rec, i).Format("Mon"))
	}
	if days[0] != "Mon" || days[4] != "Fri" {
		t.Fatalf("workdays: %v", days)
	}

	// BYDAY with DAILY is refused rather than half-honoured.
	if _, err := parseRRule("FREQ=DAILY;BYDAY=MO", loc); err == nil {
		t.Fatal("DAILY;BYDAY accepted")
	}
	if _, err := parseRRule("FREQ=WEEKLY;BYDAY=1MO", loc); err == nil {
		t.Fatal("numeric BYDAY prefix accepted")
	}
}

// COUNT counts the occurrences a BYDAY rule actually produces, not the weeks.
func TestByDayCountAndUntil(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	anchor := atDay(loc, "2026-11-18", 9, 0) // Wednesday
	_, err := st.CreateItem(ctx, Item{
		Title: "三次", StartsAt: anchor, RRule: "FREQ=WEEKLY;BYDAY=MO,WE;COUNT=3",
	}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Materialize(ctx, loc, *anchor, 60); err != nil {
		t.Fatal(err)
	}
	items, err := st.AllItems(ctx, "2026-11-18", 100)
	if err != nil {
		t.Fatal(err)
	}
	var dates []string
	for _, it := range items {
		if it.SeriesID != "" {
			dates = append(dates, it.DateLocal)
		}
	}
	// The master is on 11-18 (Wed); the two generated rows are 11-23 and
	// 11-25, and nothing after that.
	want := map[string]bool{"2026-11-23": true, "2026-11-25": true}
	if len(dates) != len(want) {
		t.Fatalf("count=3 generated %v", dates)
	}
	for _, d := range dates {
		if !want[d] {
			t.Fatalf("unexpected instance %s (all %v)", d, dates)
		}
	}
}

// UNTIL ends a BYDAY series before the window does.
func TestByDayUntil(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	anchor := atDay(loc, "2026-11-18", 9, 0)
	_, err := st.CreateItem(ctx, Item{
		Title: "到月底", StartsAt: anchor,
		RRule: "FREQ=WEEKLY;BYDAY=MO,WE,FR;UNTIL=20261127T235959Z",
	}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Materialize(ctx, loc, *anchor, 60); err != nil {
		t.Fatal(err)
	}
	items, err := st.AllItems(ctx, "2026-11-18", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.SeriesID == "" {
			continue
		}
		if it.DateLocal > "2026-11-27" {
			t.Fatalf("instance after UNTIL: %s", it.DateLocal)
		}
	}
}

// The week stats read a half-open local-date range.
func TestWeekStats(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	for _, c := range []struct {
		day, status string
	}{
		{"2026-11-09", StatusDone},      // last Monday
		{"2026-11-10", StatusDone},      // last Tuesday
		{"2026-11-16", StatusDone},      // this Monday
		{"2026-11-17", StatusOpen},      // this Tuesday, open
		{"2026-11-18", StatusDone},      // this Wednesday
		{"2026-11-15", StatusDone},      // Sunday of last week
		{"2026-11-16", StatusCancelled}, // not counted at all
	} {
		if _, err := st.CreateItem(ctx, Item{
			Title: c.day, DateLocal: c.day, AllDay: true, Status: c.status,
		}, loc); err != nil {
			t.Fatal(err)
		}
	}
	done, total, err := st.WeekStats(ctx, "2026-11-16", "2026-11-23")
	if err != nil {
		t.Fatal(err)
	}
	if done != 2 || total != 3 {
		t.Fatalf("this week: done=%d total=%d, want 2/3", done, total)
	}
}

// A series master and a series instance must never be offered, and a direct
// request to carry one is refused with a reason instead of shifting the series
// or colliding with the unique index.
func TestCarryRefusesSeriesRows(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	anchor := atDay(loc, "2026-11-16", 9, 0)
	master, err := st.CreateItem(ctx, Item{
		Title: "站会", StartsAt: anchor, RRule: "FREQ=WEEKLY;BYDAY=MO,WE,FR",
	}, loc)
	if err != nil {
		t.Fatal(err)
	}
	// Materialize creates instances; nothing may offer them for carrying.
	if _, err := st.Materialize(ctx, loc, *anchor, 30); err != nil {
		t.Fatal(err)
	}
	items, err := st.Unfinished(ctx, "2026-11-20", loc)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == master.ID || it.SeriesID != "" {
			t.Fatalf("series row offered: %+v", it)
		}
	}

	// Direct requests are refused with reasons; a plain row in the same batch
	// still moves.
	plain, err := st.CreateItem(ctx, Item{Title: "普通", StartsAt: atDay(loc, "2026-11-18", 8, 0)}, loc)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.AllItems(ctx, "2026-11-16", 100)
	if err != nil {
		t.Fatal(err)
	}
	var instanceID string
	for _, it := range rows {
		if it.SeriesID == master.ID {
			instanceID = it.ID
			break
		}
	}
	if instanceID == "" {
		t.Fatal("no generated instance to test with")
	}
	res, err := st.CarryItems(ctx, []string{master.ID, instanceID, plain.ID}, "2026-11-20", loc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Moved != 1 {
		t.Fatalf("moved %d, want 1; skipped %v", res.Moved, res.Skipped)
	}
	if res.Skipped[master.ID] == "" || res.Skipped[instanceID] == "" {
		t.Fatalf("series rows not explained: %v", res.Skipped)
	}
	// The master is untouched: the series did not shift.
	after, err := st.Item(ctx, master.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.DateLocal != "2026-11-16" || after.StartsAt.Format("01-02") != "11-16" {
		t.Fatalf("master moved: %s %v", after.DateLocal, after.StartsAt)
	}
}

// A BYDAY series created on a day that is not in the rule starts on the first
// listed day: entering "每周一三五" on a Thursday must not leave a Thursday
// master and skip the first Friday.
func TestByDayMasterAlignsToFirstListedDay(t *testing.T) {
	st, loc := newCarryStore(t)
	ctx := context.Background()
	// 2026-11-19 is a Thursday.
	entered := atDay(loc, "2026-11-19", 9, 30)
	master, err := st.CreateItem(ctx, Item{
		Title: "晨会", StartsAt: entered, RRule: "FREQ=WEEKLY;BYDAY=MO,WE,FR",
	}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := master.StartsAt.Format("2006-01-02 Mon"); got != "2026-11-20 Fri" {
		t.Fatalf("master starts %s, want the first Friday", got)
	}
	if master.DateLocal != "2026-11-20" {
		t.Fatalf("date_local = %s", master.DateLocal)
	}
	// The first generated instance is the next Monday, and there is no
	// Thursday row.
	if _, err := st.Materialize(ctx, loc, *master.StartsAt, 30); err != nil {
		t.Fatal(err)
	}
	rows, err := st.AllItems(ctx, "2026-11-19", 100)
	if err != nil {
		t.Fatal(err)
	}
	var dates []string
	for _, it := range rows {
		if it.SeriesID == master.ID {
			dates = append(dates, it.DateLocal)
		}
	}
	if len(dates) == 0 {
		t.Fatal("no instances generated")
	}
	if dates[0] != "2026-11-23" {
		t.Fatalf("first instance %s, want Monday 11-23 (instances %v)", dates[0], dates)
	}
	for _, d := range dates {
		if d == "2026-11-19" {
			t.Fatalf("a Thursday instance exists: %v", dates)
		}
	}
}

// 每月15号 is BYMONTHDAY=15, not "every month on the anchor's day", and the
// 31st is skipped in months that do not have it.
func TestMonthlyByMonthDay(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := parseRRule("FREQ=MONTHLY;BYMONTHDAY=15", loc)
	if err != nil {
		t.Fatal(err)
	}
	// 2026-11-19 is a Thursday; the first occurrence is 11-15 (before the
	// anchor is allowed: the rule names the day, the anchor only sets the
	// month and the clock).
	anchor := time.Date(2026, 11, 19, 9, 30, 0, 0, loc)
	first := occurrence(anchor, rec, 0)
	if first.Format("2006-01-02") != "2026-11-15" {
		t.Fatalf("first = %s", first)
	}
	if first.Format("15:04") != "09:30" {
		t.Fatalf("clock lost: %s", first)
	}
	if got := occurrence(anchor, rec, 1).Format("2006-01-02"); got != "2026-12-15" {
		t.Fatalf("second = %s", got)
	}

	// The 31st skips February, April, June…
	rec31, err := parseRRule("FREQ=MONTHLY;BYMONTHDAY=31", loc)
	if err != nil {
		t.Fatal(err)
	}
	jan := time.Date(2027, 1, 31, 8, 0, 0, 0, loc)
	var dates []string
	for i := 0; i < 4; i++ {
		dates = append(dates, occurrence(jan, rec31, i).Format("01-02"))
	}
	want := []string{"01-31", "03-31", "05-31", "07-31"}
	for i := range want {
		if dates[i] != want[i] {
			t.Fatalf("BYMONTHDAY=31: got %v want %v", dates, want)
		}
	}

	// An unknown key is an error, not silently dropped.
	if _, err := parseRRule("FREQ=MONTHLY;BYFOO=3", loc); err == nil {
		t.Fatal("unknown key accepted")
	}
	// BYMONTHDAY with DAILY is refused.
	if _, err := parseRRule("FREQ=DAILY;BYMONTHDAY=15", loc); err == nil {
		t.Fatal("DAILY;BYMONTHDAY accepted")
	}
}
