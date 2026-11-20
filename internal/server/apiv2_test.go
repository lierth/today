package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"today/internal/store"
)

// The JSON API is what the React page speaks, so each route gets the same
// three questions the HTML routes answer: who may call it, what it refuses,
// and what a normal call returns.

// apiCall posts or gets JSON as the logged-in session.
func (e *testEnv) apiCall(method, path string, body any) *http.Response {
	e.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req := e.req(method, e.ts.URL+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return e.do(req)
}

// apiJSON decodes one response body, failing on a non-2xx status.
func (e *testEnv) apiJSON(method, path string, body any, out any) {
	e.t.Helper()
	resp := e.apiCall(method, path, body)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e.t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("%s %s: %v (%s)", method, path, err, raw)
		}
	}
}

func TestAPIV2NeedsSession(t *testing.T) {
	e := newTestEnv(t)
	e.password()
	for _, path := range []string{
		"/api/v2/day", "/api/v2/today", "/api/v2/items", "/api/v2/upcoming",
		"/api/v2/history", "/api/v2/devices", "/api/v2/settings", "/api/v2/account",
	} {
		resp := e.get(path)
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("GET %s without a session: %d, want a redirect or 401", path, resp.StatusCode)
		}
	}
}

func TestAPIV2ItemsLifecycle(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())

	// Create: the parser turns "明天 10:30 每周 站会" into tags, the title
	// keeps the words that were not recognised.
	var created struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		DateLocal string `json:"date_local"`
		Status    string `json:"status"`
		StartsAt  string `json:"starts_at"`
	}
	e.apiJSON("POST", "/api/v2/items", map[string]any{"raw": "明天 10:30 站会"}, &created)
	if created.Title != "站会" {
		t.Fatalf("title = %q, want 站会", created.Title)
	}
	if created.DateLocal != "2026-11-20" {
		t.Fatalf("date = %q, want 2026-11-20", created.DateLocal)
	}
	if !strings.HasPrefix(created.StartsAt, "2026-11-20T10:30") {
		t.Fatalf("starts_at = %q", created.StartsAt)
	}

	// Parsing alone answers the chip strip.
	var parsed struct {
		Title string `json:"title"`
		Tags  []struct {
			Kind  string `json:"kind"`
			Label string `json:"label"`
		} `json:"tags"`
	}
	e.apiJSON("POST", "/api/v2/parse", map[string]any{"raw": "明天 10:30 站会"}, &parsed)
	if len(parsed.Tags) != 2 || parsed.Tags[0].Kind != "date" || parsed.Tags[1].Kind != "time" {
		t.Fatalf("tags = %+v", parsed.Tags)
	}

	// Ticking answers with the row and the undo target.
	var ticked struct {
		Item struct {
			Status string `json:"status"`
		} `json:"item"`
		Undo struct {
			Method string `json:"method"`
			Path   string `json:"path"`
			Body   struct {
				Status string `json:"status"`
			} `json:"body"`
		} `json:"undo"`
	}
	e.apiJSON("PATCH", "/api/v2/items/"+created.ID, map[string]any{"status": "done"}, &ticked)
	if ticked.Item.Status != "done" {
		t.Fatalf("status = %q", ticked.Item.Status)
	}
	if ticked.Undo.Body.Status != "open" || !strings.HasSuffix(ticked.Undo.Path, "/status") {
		t.Fatalf("undo = %+v", ticked.Undo)
	}
	if ticked.Undo.Method != "POST" {
		t.Fatalf("undo method = %q, want POST", ticked.Undo.Method)
	}

	// The undo route takes the old status back.
	var reopened struct {
		Status string `json:"status"`
	}
	e.apiJSON("POST", "/api/v2/items/"+created.ID+"/status", map[string]any{"status": "open"}, &reopened)
	if reopened.Status != "open" {
		t.Fatalf("status after undo = %q", reopened.Status)
	}

	// Delete answers with its own undo, and restore undoes it.
	var deleted struct {
		Undo string `json:"undo"`
	}
	e.apiJSON("DELETE", "/api/v2/items/"+created.ID, nil, &deleted)
	if deleted.Undo != "/api/v2/items/"+created.ID+"/restore" {
		t.Fatalf("delete undo = %q", deleted.Undo)
	}
	resp := e.apiCall("POST", "/api/v2/items/"+created.ID+"/restore", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore: %d", resp.StatusCode)
	}
}

// A pin and a move-to-tomorrow are quiet changes with no dialog to confirm
// them, so the response carries the few fields that were replaced and the page
// can offer one tap back.
func TestAPIV2PatchHandsBackItsOwnUndo(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())

	var created struct {
		ID string `json:"id"`
	}
	e.apiJSON("POST", "/api/v2/items", map[string]any{"raw": "明天 10:30 站会"}, &created)

	var pinned struct {
		Undo struct {
			Method string         `json:"method"`
			Path   string         `json:"path"`
			Body   map[string]any `json:"body"`
		} `json:"undo"`
	}
	e.apiJSON("PATCH", "/api/v2/items/"+created.ID, map[string]any{"pinned": true}, &pinned)
	if pinned.Undo.Method != http.MethodPatch {
		t.Fatalf("undo method = %q, want PATCH", pinned.Undo.Method)
	}
	if pinned.Undo.Body["pinned"] != false {
		t.Fatalf("undo body = %+v, want pinned=false", pinned.Undo.Body)
	}

	// Replaying it puts the row back.
	var back struct {
		Item struct {
			Pinned bool `json:"pinned"`
		} `json:"item"`
	}
	e.apiJSON("PATCH", pinned.Undo.Path, pinned.Undo.Body, &back)
	if back.Item.Pinned {
		t.Fatal("the undo did not clear the pin")
	}

	// The same for a date move: date and clock come back together, because a
	// date without its clock would silently clear the time.
	var moved struct {
		Undo struct {
			Body map[string]any `json:"body"`
		} `json:"undo"`
	}
	e.apiJSON("PATCH", "/api/v2/items/"+created.ID, map[string]any{"date": "2026-11-21"}, &moved)
	if moved.Undo.Body["date"] != "2026-11-20" || moved.Undo.Body["time"] != "10:30" {
		t.Fatalf("undo body = %+v, want date 2026-11-20 time 10:30", moved.Undo.Body)
	}
}

func TestAPIV2TodayShape(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	e.apiJSON("POST", "/api/v2/items", map[string]any{"title": "写周报"}, nil)
	e.apiJSON("POST", "/api/v2/items", map[string]any{"title": "买牛奶"}, nil)

	var today struct {
		Day struct {
			Key     string `json:"key"`
			Label   string `json:"label"`
			Weekday string `json:"weekday"`
			Lunar   string `json:"lunar"`
		} `json:"day"`
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
		Total      int  `json:"total"`
		Done       int  `json:"done"`
		AutoCarry  bool `json:"auto_carry"`
		BoardLimit int  `json:"board_limit"`
	}
	e.apiJSON("GET", "/api/v2/today", nil, &today)
	if today.Day.Key != "2026-11-19" || today.Day.Weekday != "周四" {
		t.Fatalf("day = %+v", today.Day)
	}
	if today.Day.Lunar == "" {
		t.Fatal("lunar date is empty")
	}
	if today.Total != 2 || today.Done != 0 || len(today.Items) != 2 {
		t.Fatalf("total=%d done=%d items=%d", today.Total, today.Done, len(today.Items))
	}
	if today.BoardLimit == 0 {
		t.Fatal("board_limit is zero")
	}

	var day struct {
		Progress string `json:"progress"`
		Total    int    `json:"total"`
	}
	e.apiJSON("GET", "/api/v2/day", nil, &day)
	if day.Progress != "0/2" {
		t.Fatalf("progress = %q", day.Progress)
	}
}

func TestAPIV2RejectsBadInput(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())

	cases := []struct {
		name   string
		method string
		path   string
		body   any
		code   int
	}{
		{"unknown status", "POST", "/api/v2/items/x/status", map[string]any{"status": "maybe"}, http.StatusBadRequest},
		{"unknown setting", "PATCH", "/api/v2/settings", map[string]any{"nonsense": "1"}, http.StatusBadRequest},
		{"unknown theme", "PUT", "/api/v2/account/theme", map[string]any{"theme": "neon"}, http.StatusBadRequest},
		{"bad reorder", "POST", "/api/v2/items/reorder", map[string]any{"ids": []string{}}, http.StatusBadRequest},
		{"missing item", "GET", "/api/v2/items/nope", nil, http.StatusNotFound},
	}
	for _, c := range cases {
		resp := e.apiCall(c.method, c.path, c.body)
		resp.Body.Close()
		if resp.StatusCode != c.code {
			t.Fatalf("%s: %d, want %d", c.name, resp.StatusCode, c.code)
		}
	}

	// A form post must not pass for JSON.
	resp, err := e.client.PostForm(e.ts.URL+"/api/v2/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("form post: %d, want 415", resp.StatusCode)
	}
}

func TestAPIV2WriteNeedsOrigin(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())
	// A bare client: no Origin, no Sec-Fetch-Site, no X-Today-Client.
	bare := &http.Client{Jar: e.client.Jar}
	body := strings.NewReader(`{"title":"x"}`)
	req, err := http.NewRequest("POST", e.ts.URL+"/api/v2/items", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := bare.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site write: %d, want 403", resp.StatusCode)
	}
}

func TestAPIV2CarryAndReorder(t *testing.T) {
	e := newTestEnv(t)
	e.login(e.password())

	// An item from yesterday, then carry it: the person's one-tap fix.
	yesterday := "2026-11-18"
	it, err := e.st.CreateItem(t.Context(), store.Item{Title: "昨天的活", AllDay: true, DateLocal: yesterday}, e.loc)
	if err != nil {
		t.Fatal(err)
	}
	var carried struct {
		Moved   int               `json:"moved"`
		Skipped map[string]string `json:"skipped"`
	}
	e.apiJSON("POST", "/api/v2/items/carry", map[string]any{"ids": []string{it.ID}}, &carried)
	if carried.Moved != 1 {
		t.Fatalf("moved = %d, skipped = %v", carried.Moved, carried.Skipped)
	}
	var uncarried struct {
		Moved int `json:"moved"`
	}
	e.apiJSON("POST", "/api/v2/items/uncarry", map[string]any{"ids": []string{it.ID}}, &uncarried)
	if uncarried.Moved != 1 {
		t.Fatalf("uncarried = %d", uncarried.Moved)
	}
}
