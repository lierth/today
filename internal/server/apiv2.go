package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"today/internal/store"
)

// The /api/v2 JSON API is the React page's whole world: every rule lives in
// Service, this file only translates between JSON and Go values.
//
// The rules of the road:
//   - every route needs a session cookie (these are the person's own rows);
//   - every write must pass checkWriteOrigin, like the HTML forms;
//   - errors are {"error": "..."} with a Chinese message the UI can show
//     as-is, never an internal error string.

// apiError is the one shape every failure takes.
type apiError struct {
	Error string `json:"error"`
}

// explain turns an internal error into a sentence the page may show.
//
// The API never returns an internal error string: those carry
// paths, SQL and driver messages, and this API answers a Funnel visitor with
// the password as readily as it answers the Mac. Anything the person can act
// on is already phrased as Chinese text and passes through; the rest is
// logged and replaced.
func (s *Server) explain(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	msg := err.Error()
	if isUserFacing(msg) {
		return msg
	}
	if s.log != nil {
		s.log.Printf("api: %v", err)
	}
	return fallback
}

// isUserFacing reports whether an error is a sentence written for the person
// rather than something a library produced.
//
// The rule is deliberately narrow: a message we wrote contains Han characters
// and no colon-prefixed package name ("store:", "sql:", "http:"). That keeps a
// driver error from slipping through just because it happens to start with a
// capital letter.
func isUserFacing(msg string) bool {
	if msg == "" || strings.Contains(msg, ":") {
		return false
	}
	for _, r := range msg {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func writeAPIError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, apiError{Error: msg})
}

// decodeJSON reads one JSON body and rejects anything else, so a form post
// cannot slip through the JSON routes by accident.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeAPIError(w, http.StatusUnsupportedMediaType, "请用 JSON 提交")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		writeAPIError(w, http.StatusBadRequest, "请求内容看不懂")
		return false
	}
	return true
}

func (s *Server) routesV2() {
	m := s.mux
	s.registerOrganizeAPI(m)
	m.HandleFunc("GET /api/v2/day", s.requireSession(s.apiDay))
	m.HandleFunc("GET /api/v2/today", s.requireSession(s.apiToday))
	m.HandleFunc("GET /api/v2/items", s.requireSession(s.apiItems))
	m.HandleFunc("POST /api/v2/items", s.requireSession(s.apiItemCreate))
	m.HandleFunc("GET /api/v2/items/{id}", s.requireSession(s.apiItemGet))
	m.HandleFunc("PATCH /api/v2/items/{id}", s.requireSession(s.apiItemPatch))
	m.HandleFunc("DELETE /api/v2/items/{id}", s.requireSession(s.apiItemDelete))
	m.HandleFunc("POST /api/v2/items/{id}/restore", s.requireSession(s.apiItemRestore))
	m.HandleFunc("POST /api/v2/items/{id}/status", s.requireSession(s.apiItemStatus))
	m.HandleFunc("POST /api/v2/items/carry", s.requireSession(s.apiCarry))
	m.HandleFunc("POST /api/v2/items/uncarry", s.requireSession(s.apiUncarry))
	m.HandleFunc("POST /api/v2/items/reorder", s.requireSession(s.apiReorder))
	m.HandleFunc("POST /api/v2/parse", s.requireSession(s.apiParse))
	m.HandleFunc("GET /api/v2/upcoming", s.requireSession(s.apiUpcoming))
	m.HandleFunc("GET /api/v2/history", s.requireSession(s.apiHistory))
	m.HandleFunc("GET /api/v2/search", s.requireSession(s.apiSearch))
	m.HandleFunc("GET /api/v2/devices", s.requireSession(s.apiDevices))
	m.HandleFunc("POST /api/v2/devices/{id}/refresh", s.requireSession(s.apiDeviceRefresh))
	m.HandleFunc("POST /api/v2/devices/{id}/preview-sound", s.requireSession(s.apiDevicePreviewSound))
	m.HandleFunc("GET /api/v2/settings", s.requireSession(s.apiSettingsGet))
	m.HandleFunc("PATCH /api/v2/settings", s.requireSession(s.apiSettingsPatch))
	m.HandleFunc("GET /api/v2/weather", s.requireSession(s.apiWeather))
	m.HandleFunc("GET /api/v2/account", s.requireSession(s.apiAccountGet))
	m.HandleFunc("POST /api/v2/account/password", s.requireSession(s.apiPassword))
	m.HandleFunc("POST /api/v2/account/logout-all", s.requireSession(s.apiLogoutAll))
	m.HandleFunc("PUT /api/v2/account/theme", s.requireSession(s.apiTheme))

	// Moving the listener, backups, restoring the database, minting a device
	// token and flashing firmware used to be form posts here. They are gone
	// from HTTP on purpose: the browser cannot prove it is the person at the
	// Mac, and a Funnel visitor with the password could otherwise restore an
	// old database or push an image onto every board. The app's mygo bindings
	// call the same Server methods directly.

	// Anything else under the API prefix is a 404 in JSON, not the SPA: a
	// fetch that guesses a route must not receive a page of HTML.
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		m.HandleFunc(method+" /api/v2/", s.requireSession(s.apiNotFound))
	}
}

// webWritableSettings is a whitelist, not a blacklist, and that is the whole
// point: a blacklist only refuses the keys someone thought of, and the ones
// nobody thought of are exactly the dangerous ones. firmware_version was the
// proof — writing it publishes a staged image to every board, and it was not
// on the list. A new setting is therefore web-writable only if someone adds it
// here on purpose.
//
// Today the browser may change nothing but its own appearance: the web settings
// page is deliberately thin (theme and password), everything else — devices,
// backups, firmware, network, weather — is the Mac app's job. The two entries
// below exist because the web page writes them through the generic patch when
// the account menu changes the theme or turns on colour-blind mode.
var webWritableSettings = map[string]bool{
	"theme":       true,
	"color_blind": true,
}

// webWritable reports whether a browser may write this setting.
func webWritable(key string) bool { return webWritableSettings[key] }

// apiNotFound answers an unknown /api/v2 path in the shape every other API
// failure takes.
func (s *Server) apiNotFound(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, http.StatusNotFound, "没有这个接口")
}

// apiHint turns a request into what the rules need to know about the caller.
func apiHint(r *http.Request) Hint {
	return Hint{IP: clientIP(r), Funnel: isFunnelRequest(r)}
}

// apiDayPayload is the header block every page starts with.
type apiDayPayload struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Weekday  string `json:"weekday"`
	Lunar    string `json:"lunar"`
	LunarTag string `json:"lunar_tag"`
	Progress string `json:"progress"`
	Total    int    `json:"total"`
	Done     int    `json:"done"`
	Weather  string `json:"weather"`
}

func (s *Server) apiDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	view, err := s.svc.Today(ctx, apiHint(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "数据库读取失败")
		return
	}
	day, _, err := s.svc.day(ctx)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "时区读取失败")
		return
	}
	writeJSON(w, http.StatusOK, apiDayPayload{
		Key: day.Key, Label: day.Label, Weekday: day.Weekday,
		Lunar: day.Lunar.Label, LunarTag: day.Lunar.Tag,
		Progress: progressText(view.Done, view.Total), Total: view.Total, Done: view.Done,
		Weather: view.WeatherLine,
	})
}

// apiToday is the Today page in one response: the day, its rows, the
// leftovers waiting to be carried, and the boards worth previewing.
func (s *Server) apiToday(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Today(r.Context(), apiHint(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "数据库读取失败")
		return
	}
	type carried struct {
		store.Item
		DayLabel string `json:"day_label"`
	}
	rows := make([]carried, 0, len(view.Carried))
	for _, c := range view.Carried {
		rows = append(rows, carried{Item: c.Item, DayLabel: c.DayLabel})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"day": map[string]any{
			"key": view.Day.Key, "label": view.Day.Label, "weekday": view.Day.Weekday,
			"lunar": view.Day.Lunar.Label,
		},
		"items":       view.Items,
		"total":       view.Total,
		"done":        view.Done,
		"carried":     rows,
		"auto_carry":  view.AutoCarry,
		"board_limit": view.BoardLimit,
		"weather":     view.WeatherLine,
		"preview_id":  view.PreviewID,
	})
}

// apiItems lists rows: ?view=today (default) is the day, ?view=all is the
// management list, ?view=done folds the finished ones in.
func (s *Server) apiItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.URL.Query().Get("view") {
	case "all":
		items, err := s.svc.AllItems(ctx, 0)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "数据库读取失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	default:
		view, err := s.svc.Today(ctx, apiHint(r))
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "数据库读取失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": view.Items, "total": view.Total, "done": view.Done,
			"day": view.Day.Key,
		})
	}
}

// apiItemBody is the JSON a create or a patch carries. Pointers on the patch
// keep "absent" and "clear" different.
type apiItemBody struct {
	CategoryID  *string `json:"category_id"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Date        *string `json:"date"`
	Time        *string `json:"time"`
	RRule       *string `json:"rrule"`
	Raw         *string `json:"raw"`
	Parsed      *bool   `json:"parsed"`
	Status      *string `json:"status"`
	Pinned      *bool   `json:"pinned"`
	AllDay      *bool   `json:"all_day"`
	SeriesScope *string `json:"series_scope"`
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *Server) apiItemCreate(w http.ResponseWriter, r *http.Request) {
	var body apiItemBody
	if !decodeJSON(w, r, &body) {
		return
	}
	in := ItemInput{
		CategoryID:  deref(body.CategoryID),
		Title:       deref(body.Title),
		Description: deref(body.Description),
		Date:        deref(body.Date),
		Time:        deref(body.Time),
		RRule:       deref(body.RRule),
		Raw:         deref(body.Raw),
		Parsed:      body.Parsed != nil && *body.Parsed,
	}
	item, err := s.svc.CreateItem(r.Context(), in)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, s.explain(err, "没能添加这条事项"))
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) apiItemGet(w http.ResponseWriter, r *http.Request) {
	item, err := s.svc.Item(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "这条事项不在了")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "数据库读取失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) apiItemPatch(w http.ResponseWriter, r *http.Request) {
	var body apiItemBody
	if !decodeJSON(w, r, &body) {
		return
	}
	p := store.Patch{
		CategoryID: body.CategoryID,
		Title:      body.Title, Description: body.Description,
		Status: body.Status, Pinned: body.Pinned, AllDay: body.AllDay,
		RRule: body.RRule,
	}
	if body.Date != nil || body.Time != nil {
		// date + time are the two fields the UI edits; the store wants the
		// computed instant and the calendar day.
		day, loc, err := s.svc.day(r.Context())
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "时区读取失败")
			return
		}
		date := deref(body.Date)
		if date == "" {
			date = day.Key
		}
		starts := ""
		if clock := deref(body.Time); clock != "" {
			t, err := parseLocalTime(date, clock, loc)
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			starts = t.Format("2006-01-02T15:04:05Z07:00")
			date = t.In(loc).Format(store.DateLayout)
		}
		p.StartsAt = &starts
		p.DateLocal = &date
	}
	id := r.PathValue("id")
	_, loc, locErr := s.svc.day(r.Context())
	if locErr != nil {
		loc = nil
	}
	// The fields the UI offers an undo for: a tick, a pin, "move to
	// tomorrow". Read the old row first so the toast can send it back.
	before, priorErr := s.svc.Item(r.Context(), id)
	prior := ""
	if p.Status != nil {
		prior = s.svc.PriorStatus(r.Context(), id)
	}
	item, err := s.svc.PatchItem(r.Context(), id, p)
	if errors.Is(err, store.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "这条事项不在了")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, s.explain(err, "没能保存这条事项"))
		return
	}
	out := map[string]any{"item": item}
	if prior != "" && prior != item.Status {
		// A tick: the UI shows a toast and posts the old status back.
		out["undo"] = undoOf(http.MethodPost, "/api/v2/items/"+item.ID+"/status",
			map[string]string{"status": prior})
	} else if priorErr == nil {
		// A pin or a move: hand back the few fields that changed, so the
		// same PATCH can undo itself without the toast reconstructing it.
		back := map[string]any{}
		if p.Pinned != nil && *p.Pinned != before.Pinned {
			back["pinned"] = before.Pinned
		}
		if p.DateLocal != nil && *p.DateLocal != before.DateLocal {
			back["date"] = before.DateLocal
			back["time"] = clockOf(before, loc)
		}
		if len(back) > 0 {
			out["undo"] = undoOf(http.MethodPatch, "/api/v2/items/"+item.ID, back)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// undoOf is the shape every undo takes: the method, the path, and the body to
// send. The page never invents a URL, it replays what it was handed.
func undoOf(method, path string, body any) map[string]any {
	out := map[string]any{"method": method, "path": path}
	if body != nil {
		out["body"] = body
	}
	return out
}

// clockOf renders an item's start as the wall clock the drawer edits
// ("10:30"), or "" when it has no time. The day's own zone is the one the
// person sees, so the undo restores the same "10:30" they were looking at.
func clockOf(it store.Item, loc *time.Location) string {
	if it.StartsAt == nil || loc == nil {
		return ""
	}
	return it.StartsAt.In(loc).Format("15:04")
}

func (s *Server) apiItemDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.svc.Delete(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "这条事项不在了")
		return
	} else if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"undo": "/api/v2/items/" + id + "/restore",
	})
}

func (s *Server) apiItemRestore(w http.ResponseWriter, r *http.Request) {
	item, err := s.svc.Restore(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "这条事项不在了")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "恢复失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) apiItemStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	switch body.Status {
	case store.StatusOpen, store.StatusDone, store.StatusCancelled:
	default:
		writeAPIError(w, http.StatusBadRequest, "状态只能是 open、done 或 cancelled")
		return
	}
	item, err := s.svc.SetStatus(r.Context(), r.PathValue("id"), body.Status)
	if errors.Is(err, store.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "这条事项不在了")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) apiCarry(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	if r.ContentLength > 0 && !decodeJSON(w, r, &body) {
		return
	}
	res, err := s.svc.Carry(r.Context(), body.IDs, body.All)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "顺延失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"moved": res.Moved, "skipped": res.Skipped,
		"undo": map[string]string{"method": "POST", "path": "/api/v2/items/uncarry"},
	})
}

func (s *Server) apiUncarry(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	n, err := s.svc.Uncarry(r.Context(), body.IDs)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "撤销失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"moved": n})
}

func (s *Server) apiReorder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.IDs) == 0 {
		writeAPIError(w, http.StatusBadRequest, "没有要排序的事项")
		return
	}
	if err := s.svc.Reorder(r.Context(), body.IDs); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "排序失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

func (s *Server) apiParse(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Raw string `json:"raw"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	got, err := s.svc.Parse(r.Context(), body.Raw)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "时区读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"title": got.Title, "date": got.Date, "time": got.Time, "rrule": got.RRule,
		"tags": quickAddTags(got),
	})
}

// quickAddTag is one recognised fragment, already phrased for the strip.
type quickAddTag struct {
	Kind  string `json:"kind"` // date | time | repeat
	Label string `json:"label"`
	// Text is the slice of the draft the chip stands for, so the × can delete
	// exactly that: "10:30" and "十点半" parse to the same moment, so the old
	// date or time field cannot restore the original wording.
	Text string `json:"text"`
}

// quickAddTags phrases the parser's output the way the chip strip shows it.
// The UI can delete a chip by dropping the field it names.
func quickAddTags(q QuickAdd) []quickAddTag {
	out := make([]quickAddTag, 0, 3)
	text := make(map[string]string, len(q.Fragments))
	for _, f := range q.Fragments {
		if _, seen := text[f.Kind]; !seen {
			text[f.Kind] = f.Text
		}
	}
	if q.Date != "" {
		label := q.Date
		if t, err := parseDateLabel(q.Date); err == nil {
			label = t
		}
		out = append(out, quickAddTag{Kind: "date", Label: label, Text: text["date"]})
	}
	if q.Time != "" {
		out = append(out, quickAddTag{Kind: "time", Label: q.Time, Text: text["time"]})
	}
	if q.RRule != "" {
		out = append(out, quickAddTag{Kind: "repeat", Label: rruleLabel(q.RRule), Text: text["repeat"]})
	}
	return out
}

func (s *Server) apiUpcoming(w http.ResponseWriter, r *http.Request) {
	groups, err := s.svc.Upcoming(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "数据库读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) apiHistory(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.History(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "数据库读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"groups": view.Groups, "past": view.Past,
		"done_this_week": view.DoneThisWeek, "done_last_week": view.DoneLastWeek,
	})
}

func (s *Server) apiSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"groups": []Group{}})
		return
	}
	groups, err := s.svc.Search(r.Context(), query, 100)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "搜索失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups, "query": query})
}

func (s *Server) apiDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.svc.Devices(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "数据库读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (s *Server) apiDeviceRefresh(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "缺少设备 id")
		return
	}
	s.svc.RefreshDevice(id)
	writeJSON(w, http.StatusOK, map[string]string{"result": "queued"})
}

func (s *Server) apiDevicePreviewSound(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "缺少设备 id")
		return
	}
	s.RequestPreview(id)
	s.svc.RefreshDevice(id)
	writeJSON(w, http.StatusOK, map[string]string{"result": "queued"})
}

func (s *Server) apiSettingsGet(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.Settings(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "数据库读取失败")
		return
	}
	zone := displayTimezone(st["timezone"])
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": st, "zones": zoneChoices(zone),
		"proxy_summary": s.proxySettingsSummary(r.Context()),
		"weather":       s.svc.WeatherStatus(r.Context()),
		// No absolute path here. The note is a sentence a person can act on;
		// the location of the database is the machine's business.
		"data_note": s.cfg.Paths.DataNote,
	})
}

// apiSettingsPatch takes only known keys: an unknown name would be stored
// forever and read by nobody.
func (s *Server) apiSettingsPatch(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if !decodeJSON(w, r, &body) {
		return
	}
	// Three passes, so the answer never depends on map order: an unknown name
	// is a 400, an existing name this caller may not write is a 403 whatever
	// the value, and only a setting the web page owns gets its value checked.
	for k := range body {
		if err := settingExists(k); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// Only what the web page actually owns. Everything else — the proxy, the
	// update feed, the firmware, iCloud, Tailscale, the tray — belongs to the
	// Mac app, and a browser may read it but never write it.
	for k := range body {
		if !webWritable(k) {
			writeAPIError(w, http.StatusForbidden, "这项设置只能在 Mac app 里改")
			return
		}
	}
	diff := make(map[string]string, len(body))
	for k, v := range body {
		if err := checkSetting(k, v); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		diff[k] = v
	}
	if len(diff) == 0 {
		writeAPIError(w, http.StatusBadRequest, "没有要改的设置")
		return
	}
	st, err := s.svc.UpdateSettings(r.Context(), diff)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": st})
}

func (s *Server) apiWeather(w http.ResponseWriter, r *http.Request) {
	line, err := s.svc.WeatherLine(r.Context(), apiHint(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "天气读取失败")
		return
	}
	st, _ := s.svc.RawSettings(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"line": line, "city": st["weather_city"],
		"key_set": strings.TrimSpace(st["weather_api_key"]) != "",
	})
}

// apiAccountGet is what the avatar menu needs: the theme and whether a
// password is set (nothing secret ever leaves).
func (s *Server) apiAccountGet(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.RawSettings(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "数据库读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"theme":        themeValue(st["theme"]),
		"has_password": strings.TrimSpace(st["auth_password_hash"]) != "",
	})
}

func (s *Server) apiPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	ctx := r.Context()
	ok, err := s.CheckPassword(ctx, body.Current)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "口令读取失败")
		return
	}
	if !ok {
		// Guessing the current password is the same attack as guessing it at
		// the login form, so it waits behind the same limiter and its answer
		// is deliberately vague.
		ip := clientIP(r)
		now := time.Now()
		if !s.limiter.allow(ip, now) {
			w.Header().Set("Retry-After", "60")
			writeAPIError(w, http.StatusTooManyRequests, "试得太多，请稍后再试")
			return
		}
		delay := s.limiter.fail(ip, now)
		// Same pause the login form takes: it is what turns a guess into a
		// slow, countable event. Tests swap s.sleep out.
		s.sleep(delay)
		writeAPIError(w, http.StatusForbidden, "原口令不对")
		return
	}
	s.limiter.success(clientIP(r))
	// The length rule lives in SetPassword; checking it here as well keeps a
	// short password a 400 with a sentence rather than a 500 from the store.
	if len(strings.TrimSpace(body.Next)) < MinPasswordLength {
		writeAPIError(w, http.StatusBadRequest, "新口令至少 8 位")
		return
	}
	if err := s.SetPassword(ctx, body.Next); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

func (s *Server) apiLogoutAll(w http.ResponseWriter, r *http.Request) {
	if err := s.RotateSessionKey(r.Context()); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

func (s *Server) apiTheme(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Theme string `json:"theme"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	switch body.Theme {
	case "system", "light", "dark":
	default:
		writeAPIError(w, http.StatusBadRequest, "主题只能是 system、light 或 dark")
		return
	}
	if _, err := s.svc.UpdateSettings(r.Context(), map[string]string{"theme": body.Theme}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"theme": body.Theme})
}

// progressText is the "3/7" the header and the tray both show.
func progressText(done, total int) string {
	if total <= 0 {
		return "0/0"
	}
	return strconv.Itoa(done) + "/" + strconv.Itoa(total)
}

// themeValue normalises the stored theme: an unknown value means "system",
// so a half-migrated database does not leave a front end without a theme.
func themeValue(v string) string {
	switch strings.TrimSpace(v) {
	case "light", "dark":
		return strings.TrimSpace(v)
	default:
		return "system"
	}
}

// parseDateLabel phrases a date for a chip: "11月19日" and, when it is close,
// "今天"/"明天". The chip strip stays consistent with the site's date labels.
func parseDateLabel(date string) (string, error) {
	t, err := time.ParseInLocation(store.DateLayout, date, time.Local)
	if err != nil {
		return "", err
	}
	return t.Format("1月2日"), nil
}

// rruleLabel phrases a repeat rule the way the chip shows it. The supported
// subset is small (daily, weekdays, weekly by day, monthly), so a table plus
// the BYDAY list covers it; anything else falls back to "重复".
func rruleLabel(expr string) string {
	up := strings.ToUpper(strings.TrimSpace(expr))
	if up == "" {
		return ""
	}
	var interval int
	if v := rruleNumber(up, "INTERVAL", 0); v > 0 {
		interval = v
	}
	days := repeatDayLabels(rruleByDay(up))
	switch {
	case strings.Contains(up, "FREQ=DAILY"):
		if interval > 1 {
			return "每隔 " + strconv.Itoa(interval) + " 天"
		}
		return "每天"
	case strings.Contains(up, "FREQ=WEEKLY"):
		if len(days) > 0 {
			if interval > 1 {
				return "每隔 " + strconv.Itoa(interval) + " 周 " + strings.Join(days, "")
			}
			return "每周" + strings.Join(days, "")
		}
		if interval > 1 {
			return "每隔 " + strconv.Itoa(interval) + " 周"
		}
		return "每周"
	case strings.Contains(up, "FREQ=MONTHLY"):
		if d := rruleNumber(up, "BYMONTHDAY", 0); d > 0 {
			return "每月 " + strconv.Itoa(d) + " 号"
		}
		if interval > 1 {
			return "每隔 " + strconv.Itoa(interval) + " 个月"
		}
		return "每月"
	}
	return "重复"
}

// repeatDayLabels turns BYDAY values into the single characters the chip
// strip can show (一二三四五六日).
func repeatDayLabels(days []string) []string {
	names := map[string]string{"MO": "一", "TU": "二", "WE": "三", "TH": "四",
		"FR": "五", "SA": "六", "SU": "日"}
	out := make([]string, 0, len(days))
	for _, d := range days {
		if name, ok := names[strings.ToUpper(strings.TrimSpace(d))]; ok {
			out = append(out, name)
		}
	}
	return out
}
