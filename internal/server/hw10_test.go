package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"today/internal/store"
)

// carryEnv is the common setup: logged in, one past item and one today item.
func carryEnv(t *testing.T) (*testEnv, string, string) {
	t.Helper()
	e := newTestEnv(t)
	pw := e.password()
	e.login(pw)
	ctx := context.Background()
	loc, err := e.srv.Location(ctx)
	if err != nil {
		t.Fatal(err)
	}
	day, _, err := e.srv.Today(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := day.AddDate(0, 0, -2)
	clock := time.Date(before.Year(), before.Month(), before.Day(), 21, 30, 0, 0, loc)
	old, err := e.st.CreateItem(ctx, store.Item{Title: "两天前", StartsAt: &clock}, loc)
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, loc)
	cur, err := e.st.CreateItem(ctx, store.Item{Title: "今天的", StartsAt: &today}, loc)
	if err != nil {
		t.Fatal(err)
	}
	return e, old.ID, cur.ID
}

// The today page offers the past item in the carried block, and the carry
// endpoint moves exactly that one, keeping its clock time.
func TestCarryBlockAndMove(t *testing.T) {
	e, oldID, _ := carryEnv(t)
	body := string(e.body("/app"))
	if !strings.Contains(body, "之前未完成") || !strings.Contains(body, "两天前") {
		t.Fatalf("today page has no carried block: %s", snippet(body))
	}

	form := url.Values{"item": {oldID}, "view": {"today"}}
	resp, err := e.client.PostForm(e.ts.URL+"/api/items/carry", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("carry: %d %s", resp.StatusCode, raw)
	}
	html, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(html), "之前未完成") {
		t.Fatalf("carried block still shown: %s", snippet(string(html)))
	}

	ctx := context.Background()
	it, err := e.st.Item(ctx, oldID)
	if err != nil {
		t.Fatal(err)
	}
	day, _, _ := e.srv.Today(ctx)
	if it.DateLocal != day.Format(store.DateLayout) {
		t.Fatalf("date_local = %q, want today", it.DateLocal)
	}
	if it.CarriedFrom == "" {
		t.Fatal("carried_from not recorded")
	}
	if it.StartsAt == nil || it.StartsAt.Format("15:04") != "21:30" {
		t.Fatalf("clock changed: %v", it.StartsAt)
	}
}

// "全部移到今天" moves every offered row in one request.
func TestCarryAll(t *testing.T) {
	e, oldID, _ := carryEnv(t)
	ctx := context.Background()
	loc, _ := e.srv.Location(ctx)
	day, _, _ := e.srv.Today(ctx)
	second := day.AddDate(0, 0, -1)
	clock := time.Date(second.Year(), second.Month(), second.Day(), 7, 0, 0, 0, loc)
	other, err := e.st.CreateItem(ctx, store.Item{Title: "昨天的", StartsAt: &clock}, loc)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := e.client.PostForm(e.ts.URL+"/api/items/carry", url.Values{"all": {"1"}, "view": {"today"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("carry all: %d", resp.StatusCode)
	}
	for _, id := range []string{oldID, other.ID} {
		it, err := e.st.Item(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if it.DateLocal != day.Format(store.DateLayout) {
			t.Fatalf("%s still on %s", it.Title, it.DateLocal)
		}
	}
	unfinished, err := e.st.Unfinished(ctx, day.Format(store.DateLayout), loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(unfinished) != 0 {
		t.Fatalf("unfinished after carry all: %+v", unfinished)
	}
}

// The drag order is written in one transaction, and the list comes back in
// that order.
func TestReorderEndpoint(t *testing.T) {
	e, oldID, curID := carryEnv(t)
	ctx := context.Background()
	// Carry the old row so both are on today.
	if _, err := e.client.PostForm(e.ts.URL+"/api/items/carry", url.Values{"item": {oldID}}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"item": {curID, oldID}}
	resp, err := e.client.PostForm(e.ts.URL+"/api/items/reorder", form)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reorder: %d", resp.StatusCode)
	}
	day, _, _ := e.srv.Today(ctx)
	items, err := e.st.ItemsOn(ctx, day.Format(store.DateLayout))
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, it := range items {
		titles = append(titles, it.Title)
	}
	if len(titles) < 2 || titles[0] != "今天的" || titles[1] != "两天前" {
		t.Fatalf("order: %v", titles)
	}
}

// The arrow buttons move one row and answer with the list.
func TestMoveUpDown(t *testing.T) {
	e, oldID, curID := carryEnv(t)
	if _, err := e.client.PostForm(e.ts.URL+"/api/items/carry", url.Values{"item": {oldID}}); err != nil {
		t.Fatal(err)
	}
	// Today's 08:00 row is first; move the carried 21:30 row up.
	resp, err := e.client.PostForm(e.ts.URL+"/api/items/move", url.Values{
		"item": {oldID}, "dir": {"up"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move: %d", resp.StatusCode)
	}
	ctx := context.Background()
	day, _, _ := e.srv.Today(ctx)
	items, err := e.st.ItemsOn(ctx, day.Format(store.DateLayout))
	if err != nil {
		t.Fatal(err)
	}
	if items[0].ID != oldID {
		t.Fatalf("first row = %s (%s), want %s", items[0].Title, items[0].ID, oldID)
	}
	// At the top, moving up again must not fail or drop the row.
	resp, err = e.client.PostForm(e.ts.URL+"/api/items/move", url.Values{
		"item": {oldID}, "dir": {"up"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edge move: %d", resp.StatusCode)
	}
	if _, err := e.st.Item(ctx, curID); err != nil {
		t.Fatalf("edge move dropped a row: %v", err)
	}
}

// The undo button restores the deleted row.
func TestDeleteThenRestore(t *testing.T) {
	e, _, curID := carryEnv(t)
	req, err := http.NewRequest(http.MethodDelete, e.ts.URL+"/api/items/"+curID+"?view=today", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if trigger := resp.Header.Get("HX-Trigger"); !strings.Contains(trigger, "todayUndo") {
		t.Fatalf("delete did not offer an undo: %q", trigger)
	}
	resp, err = e.client.Post(e.ts.URL+"/api/items/"+curID+"/restore", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore: %d", resp.StatusCode)
	}
	if _, err := e.st.Item(context.Background(), curID); err != nil {
		t.Fatalf("restored row still gone: %v", err)
	}
}

// The parse endpoint answers what a form submit would save.
func TestParseEndpoint(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	resp, err := e.client.PostForm(e.ts.URL+"/api/items/parse", url.Values{"title": {"明天 下午3点 开会"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("parse: %d", resp.StatusCode)
	}
	var got QuickAdd
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	day, _, _ := e.srv.Today(ctx)
	want := day.AddDate(0, 0, 1).Format(store.DateLayout)
	if got.Title != "开会" || got.Date != want || got.Time != "15:00" {
		t.Fatalf("parse = %+v (want date %s)", got, want)
	}
}

// A quick-add submit with `raw` uses the parser, and an explicit tag override
// wins.
func TestQuickAddSubmitUsesParser(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	resp, err := e.client.PostForm(e.ts.URL+"/api/items", url.Values{
		"raw": {"明天 10点 站会"}, "view": {"today"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("quick add: %d", resp.StatusCode)
	}
	ctx := context.Background()
	day, _, _ := e.srv.Today(ctx)
	items, err := e.st.ItemsOn(ctx, day.AddDate(0, 0, 1).Format(store.DateLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "站会" {
		t.Fatalf("items: %+v", items)
	}
	if items[0].StartsAt.Format("15:04") != "10:00" {
		t.Fatalf("clock: %v", items[0].StartsAt)
	}

	// The tag strip posts the value it showed; a changed tag must win.
	resp, err = e.client.PostForm(e.ts.URL+"/api/items", url.Values{
		"raw": {"明天 10点 站会"}, "time": {"16:00"}, "view": {"today"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	items, err = e.st.ItemsOn(ctx, day.AddDate(0, 0, 1).Format(store.DateLayout))
	if err != nil {
		t.Fatal(err)
	}
	var clocks []string
	for _, it := range items {
		clocks = append(clocks, it.StartsAt.Format("15:04"))
	}
	if len(clocks) != 2 || clocks[0] != "10:00" || clocks[1] != "16:00" {
		t.Fatalf("override did not win: %v", clocks)
	}
}

// Search matches the note as well as the title, and an empty query is an
// empty page rather than everything.
func TestSearchPage(t *testing.T) {
	e, _, curID := carryEnv(t)
	ctx := context.Background()
	note := "记得带上合同"
	if _, err := e.st.UpdateItem(ctx, curID, store.Patch{Description: &note}, e.loc); err != nil {
		t.Fatal(err)
	}
	page := string(e.body("/search?q=" + url.QueryEscape("合同")))
	if !strings.Contains(page, "今天的") {
		t.Fatalf("note search missed the row: %s", snippet(page))
	}
	empty := string(e.body("/search?q="))
	if strings.Contains(empty, "今天的") {
		t.Fatal("empty query listed everything")
	}
}

// The page draws the board-limit cut only when the day has more rows than the
// panel, and it comes from frame.BoardRowLimit (never a literal in the
// template).
func TestBoardLimitDivider(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	ctx := context.Background()
	day, _, _ := e.srv.Today(ctx)
	loc, _ := e.srv.Location(ctx)
	at := func(h, m int) *time.Time {
		v := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc)
		return &v
	}
	for i := range 12 {
		if _, err := e.st.CreateItem(ctx, store.Item{
			Title: "事项 " + itoa(i), StartsAt: at(8+i, 0),
		}, loc); err != nil {
			t.Fatal(err)
		}
	}
	page := string(e.body("/app"))
	if !strings.Contains(page, "以下事项不会显示在板子上") {
		t.Fatal("no board-limit divider with 12 rows")
	}

	// With few rows there is no divider.
	e2 := newTestEnv(t)
	e2.login(e2.password())
	if _, err := e2.st.CreateItem(ctx, store.Item{Title: "只有一件", StartsAt: at(9, 0)}, loc); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(e2.body("/app")), "不会显示在板子上") {
		t.Fatal("divider shown for a short list")
	}
}

// snippet trims a page for a failure message.
func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// A tick offers an undo that puts the previous status back, and the undo
// endpoint accepts it.
func TestStatusUndo(t *testing.T) {
	e, _, curID := carryEnv(t)
	// The tick is a PATCH; the test client's PostForm would get a 405.
	req, err := http.NewRequest(http.MethodPatch, e.ts.URL+"/api/items/"+curID,
		strings.NewReader(url.Values{"status": {"done"}, "view": {"today"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tick: %d", resp.StatusCode)
	}
	trigger := resp.Header.Get("HX-Trigger")
	if !strings.Contains(trigger, "todayUndo") || !strings.Contains(trigger, "/status/open") {
		t.Fatalf("no undo offered: %q", trigger)
	}
	resp, err = e.client.Post(e.ts.URL+"/api/items/"+curID+"/status/open", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo: %d", resp.StatusCode)
	}
	it, err := e.st.Item(context.Background(), curID)
	if err != nil {
		t.Fatal(err)
	}
	if it.Status != store.StatusOpen {
		t.Fatalf("status = %s", it.Status)
	}
}

// Carrying offers an undo that puts the row back on its own day, with its
// carried_from cleared.
func TestCarryUndo(t *testing.T) {
	e, oldID, _ := carryEnv(t)
	before, err := e.st.Item(context.Background(), oldID)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := e.client.PostForm(e.ts.URL+"/api/items/carry", url.Values{"item": {oldID}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	trigger := resp.Header.Get("HX-Trigger")
	if !strings.Contains(trigger, "/api/items/uncarry") {
		t.Fatalf("carry offered no undo: %q", trigger)
	}
	var undoPath string
	var payload map[string]string
	if err := json.Unmarshal([]byte(trigger), &payload); err != nil {
		t.Fatal(err)
	}
	undoPath = payload["todayUndo"]
	resp, err = e.client.Post(e.ts.URL+undoPath, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("uncarry: %d", resp.StatusCode)
	}
	after, err := e.st.Item(context.Background(), oldID)
	if err != nil {
		t.Fatal(err)
	}
	if after.DateLocal != before.DateLocal || after.CarriedFrom != "" {
		t.Fatalf("uncarry: date %s (was %s), carried_from %q",
			after.DateLocal, before.DateLocal, after.CarriedFrom)
	}
}
