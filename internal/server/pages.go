package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"today/internal/frame"
	"today/internal/store"
)

type pageData struct {
	Title       string
	Nav         string
	DateLabel   string
	Items       []store.Item
	All         []store.Item
	Item        store.Item
	HasItem     bool
	Past        []store.Item
	Settings    map[string]string
	Devices     []store.Device
	Stats       store.Stats
	Total       int
	Done        int
	HasProgress bool
	Token       string
	PanelToken  string
	PreviewID   string
	PreviewIDs  []string
	Zones       []string
	SetupCmds   []string
	ListenAddr  string
	ListenNow   string
	BindNote    string
	Message     string
	Error       string

	Weather     weatherStatus
	WeatherLine string // the today page's one-line forecast
	FocusAdd    bool   // /app?focus=add: put the caret in the quick-add field

	// The "carried over" block on the today page.
	Carried   []store.Item
	CarriedAt []carryRow
	AutoCarry bool

	// BoardLimit is how many of today's rows fit on the panel; the page draws
	// a divider after that many. It comes from the server so the template
	// never hard-codes the frame's rule.
	BoardLimit int

	// The coming-up view.
	Upcoming []upcomingGroup
	// Proxy card.
	ProxySummary string
	ProxyTest    string
	// Update card: the host's state sentence.
	UpdateStatus string
	// Data and backups.
	Backup    BackupStatus
	Snapshots []snapshotRow
	DataRoot  string
	DataNote  string

	// History statistics.
	WeekDone     int
	LastWeekDone int
	// Search results.
	Query   string
	Results []upcomingGroup
}

// snapshotRow is one restorable file for the page.
type snapshotRow struct {
	Name  string
	Size  string
	When  string
	Where string
	Cloud bool
}

// carryRow is one line of the "unfinished from earlier" block: the item plus
// the Chinese day label the page shows.
type carryRow struct {
	store.Item
	DayLabel string
}

// upcomingGroup is one heading of the coming-up list.
type upcomingGroup struct {
	Label string
	Items []store.Item
}

var weekdayZH = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"timeLabel":     itemTimeLabel,
		"dateLabel":     itemDateLabel,
		"dateValue":     itemDateValue,
		"timeValue":     itemTimeValue,
		"statusLabel":   itemStatusLabel,
		"fmtTime":       fmtTime,
		"powerLabel":    powerLabel,
		"noteFirstLine": noteFirstLine,
		"rruleNumber":   rruleNumber,
		"rruleHasFreq":  rruleHasFreq,
		"rruleByDay":    rruleByDay,
		"rruleDayOn":    rruleDayOn,
		"rruleUntil":    rruleUntil,
		"json":          func(v any) string { b, _ := json.Marshal(v); return string(b) },
		"online":        deviceOnline,
		"batteryPct":    deviceBatteryLabel,
		"climate":       deviceClimate,
		"since":         deviceSince,
	}
}

// deviceOnline applies the same three-minute window the tray menu uses, so the
// page and the menu never disagree about whether a board is there.
// rruleNumber reads one numeric part of an RRULE, or `def` when absent.
func rruleNumber(rule, key string, def int) int {
	for _, part := range strings.Split(rule, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok || !strings.EqualFold(k, key) {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func rruleHasFreq(rule, freq string) bool {
	for _, part := range strings.Split(rule, ";") {
		k, v, ok := strings.Cut(part, "=")
		if ok && strings.EqualFold(k, "FREQ") {
			return strings.EqualFold(v, freq)
		}
	}
	return false
}

// rruleByDay lists the BYDAY tokens, or nil.
func rruleByDay(rule string) []string {
	for _, part := range strings.Split(rule, ";") {
		k, v, ok := strings.Cut(part, "=")
		if ok && strings.EqualFold(k, "BYDAY") {
			out := []string{}
			for _, tok := range strings.Split(v, ",") {
				if tok = strings.TrimSpace(tok); tok != "" {
					out = append(out, strings.ToUpper(tok))
				}
			}
			return out
		}
	}
	return nil
}

func rruleDayOn(days []string, day string) bool {
	for _, d := range days {
		if d == day {
			return true
		}
	}
	return false
}

// rruleUntil returns the UNTIL date as yyyy-mm-dd (empty when absent), so a
// date input can show it.
func rruleUntil(rule string) string {
	for _, part := range strings.Split(rule, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok || !strings.EqualFold(k, "UNTIL") {
			continue
		}
		v = strings.TrimSpace(v)
		for _, layout := range []string{"20060102T150405Z", "20060102T150405", "20060102", "2006-01-02"} {
			if t, err := time.Parse(layout, v); err == nil {
				return t.Format("2006-01-02")
			}
		}
		// RFC3339 with dashes and colons.
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// noteFirstLine is the hover text for the note marker: the first line of the
// note, trimmed to something a tooltip can show.
func noteFirstLine(it store.Item) string {
	line := it.Description
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	r := []rune(line)
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return line
}

func deviceOnline(d store.Device) bool {
	return d.LastSeenAt != nil && time.Since(*d.LastSeenAt) <= OnlineWindow
}

func deviceBatteryLabel(d store.Device) string {
	if p := batteryPercent(d.BatteryMV); p >= 0 {
		return strconv.Itoa(p) + "%"
	}
	return "—"
}

func deviceClimate(d store.Device) string {
	if d.TempC == 0 && d.Humidity == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f° %.0f%%", d.TempC, d.Humidity)
}

func deviceSince(d store.Device) string {
	if d.LastSeenAt == nil {
		return "从未连接"
	}
	return fmtTime(d.LastSeenAt)
}

func powerLabel(v string) string {
	switch v {
	case "usb":
		return "USB 供电"
	case "charging":
		return "充电中"
	case "battery":
		return "电池供电"
	default:
		return "供电状态未知"
	}
}

func itemTimeLabel(it store.Item) string {
	if it.AllDay {
		return "全天"
	}
	if it.StartsAt == nil {
		return "—"
	}
	return it.StartsAt.Format("15:04")
}

func itemDateLabel(it store.Item) string {
	t, err := time.Parse(store.DateLayout, it.DateLocal)
	if err != nil {
		return "—"
	}
	return t.Format("1月2日")
}

func itemDateValue(it store.Item) string {
	if it.DateLocal == "" {
		return ""
	}
	return it.DateLocal
}

func itemTimeValue(it store.Item) string {
	if it.AllDay || it.StartsAt == nil {
		return ""
	}
	return it.StartsAt.Format("15:04")
}

func itemStatusLabel(it store.Item) string {
	switch it.Status {
	case store.StatusDone:
		return "已完成"
	case store.StatusCancelled:
		return "已取消"
	}
	return "未完成"
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.Format("01-02 15:04")
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data pageData) {
	s.renderStatus(w, r, http.StatusOK, name, data)
}

func (s *Server) renderStatus(w http.ResponseWriter, _ *http.Request, code int, name string, data pageData) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.log.Printf("server: render %s: %v", name, err)
		http.Error(w, "template", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = buf.WriteTo(w)
}

func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	day, _, err := s.Today(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	items, err := s.st.ItemsOn(ctx, day.Format(store.DateLayout))
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	total, done := 0, 0
	for _, it := range items {
		if it.Status == store.StatusCancelled {
			continue
		}
		total++
		if it.Done() {
			done++
		}
	}
	devices, err := s.st.Devices(ctx)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	settings, err := s.st.Settings(ctx)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	carried, err := s.st.Unfinished(ctx, day.Format(store.DateLayout), loc)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	carryRows := make([]carryRow, 0, len(carried))
	for _, it := range carried {
		label, err := time.ParseInLocation(store.DateLayout, it.DateLocal, loc)
		dayLabel := it.DateLocal
		if err == nil {
			dayLabel = label.Format("1月2日")
		}
		carryRows = append(carryRows, carryRow{Item: it, DayLabel: dayLabel})
	}
	s.render(w, r, "app.html", pageData{
		Title:       "今天",
		Nav:         "today",
		WeatherLine: s.todayWeatherLine(ctx, r),
		DateLabel:   day.Format("1月2日") + " " + weekdayZH[day.Weekday()],
		Items:       items,
		Total:       total,
		Done:        done,
		PreviewID:   previewDevice(devices),
		PreviewIDs:  liveDevices(devices),
		FocusAdd:    r.URL.Query().Get("focus") == "add",
		CarriedAt:   carryRows,
		AutoCarry:   store.BoolSetting(settings, "auto_carry"),
		BoardLimit:  frame.BoardRowLimit,
	})
}

func (s *Server) handleItemPage(w http.ResponseWriter, r *http.Request) {
	it, err := s.st.Item(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "item.html", pageData{Title: it.Title, Nav: "items", Item: it, HasItem: true})
}

// allItems lists every live item, ordered for the management page.
func (s *Server) allItems(ctx context.Context) ([]store.Item, error) {
	day, _, err := s.Today(ctx)
	if err != nil {
		return nil, err
	}
	return s.st.AllItems(ctx, day.Format(store.DateLayout), 500)
}

func (s *Server) handleItemsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	day, _, err := s.Today(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	groups, err := s.upcomingGroups(ctx, day)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "items.html", pageData{
		Title: "事项", Nav: "items", Upcoming: groups,
	})
}

// upcomingGroups buckets the live items the /items page shows: overdue, today,
// tomorrow, this week, later, undated, then done (folded by the template).
func (s *Server) upcomingGroups(ctx context.Context, day time.Time) ([]upcomingGroup, error) {
	today := day.Format(store.DateLayout)
	loc, err := s.Location(ctx)
	if err != nil {
		return nil, err
	}
	// A wide window keeps "later" meaningful; the store caps the rows.
	items, err := s.st.Upcoming(ctx, today, 400, 400)
	if err != nil {
		return nil, err
	}
	overdue, err := s.st.Unfinished(ctx, today, loc)
	if err != nil {
		return nil, err
	}
	weekEnd := day.AddDate(0, 0, 7-(int(day.Weekday())+6)%7).Format(store.DateLayout)
	tomorrow := day.AddDate(0, 0, 1).Format(store.DateLayout)

	var gOver, gToday, gTomorrow, gWeek, gLater, gUndated []store.Item
	for _, it := range overdue {
		gOver = append(gOver, it)
	}
	for _, it := range items {
		switch {
		case it.DateLocal == today:
			gToday = append(gToday, it)
		case it.DateLocal == tomorrow:
			gTomorrow = append(gTomorrow, it)
		case it.DateLocal < today:
			// Only impossible when a row was carried between the two reads.
			gOver = append(gOver, it)
		case it.DateLocal < weekEnd:
			gWeek = append(gWeek, it)
		default:
			gLater = append(gLater, it)
		}
	}
	// Done items are shown folded at the bottom; the template does the fold.
	done, err := s.st.DoneItems(ctx, day.AddDate(0, 0, -30).Format(store.DateLayout), 100)
	if err != nil {
		return nil, err
	}
	out := []upcomingGroup{
		{"已逾期", gOver},
		{"今天", gToday},
		{"明天", gTomorrow},
		{"本周", gWeek},
		{"以后", gLater},
		{"无日期", gUndated},
		{"已完成（近 30 天）", done},
	}
	kept := out[:0]
	for _, g := range out {
		if len(g.Items) > 0 {
			kept = append(kept, g)
		}
	}
	return kept, nil
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	day, _, err := s.Today(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	past, err := s.st.PastItems(ctx, day.Format(store.DateLayout), 200)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	// Week statistics: this Monday-to-Monday window and the one before it.
	thisMonday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	nextMonday := thisMonday.AddDate(0, 0, 7)
	lastMonday := thisMonday.AddDate(0, 0, -7)
	doneThis, _, err := s.st.WeekStats(ctx, thisMonday.Format(store.DateLayout),
		nextMonday.Format(store.DateLayout))
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	doneLast, _, err := s.st.WeekStats(ctx, lastMonday.Format(store.DateLayout),
		thisMonday.Format(store.DateLayout))
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "history.html", pageData{
		Title: "历史", Nav: "history", Past: past,
		WeekDone: doneThis, LastWeekDone: doneLast,
	})
}

func (s *Server) settingsData(ctx context.Context) (pageData, error) {
	st, err := s.st.Settings(ctx)
	if err != nil {
		return pageData{}, err
	}
	devices, err := s.st.Devices(ctx)
	if err != nil {
		return pageData{}, err
	}
	day, _, err := s.Today(ctx)
	if err != nil {
		return pageData{}, err
	}
	stats, err := s.st.Stats(ctx, day.Format(store.DateLayout))
	if err != nil {
		return pageData{}, err
	}
	status, snaps, err := s.backupsPageData(ctx)
	if err != nil {
		return pageData{}, err
	}
	rows := make([]snapshotRow, 0, len(snaps))
	for _, snap := range snaps {
		where := "本地"
		if snap.Cloud {
			where = "iCloud"
		}
		rows = append(rows, snapshotRow{
			Name:  snap.Name,
			Size:  humanSize(snap.Size),
			When:  snap.ModTime.Format("2006-01-02 15:04"),
			Where: where,
			Cloud: snap.Cloud,
		})
	}
	return pageData{
		Title:        "设置",
		Nav:          "settings",
		Settings:     shownSettings(st),
		Devices:      devices,
		Stats:        stats,
		ListenAddr:   s.ListenLabel(),
		ListenNow:    s.Addr(),
		BindNote:     s.BindNote(),
		PreviewID:    previewDevice(devices),
		PreviewIDs:   liveDevices(devices),
		Zones:        zoneChoices(displayTimezone(st["timezone"])),
		Weather:      s.weatherStatus(ctx),
		ProxySummary: s.proxySettingsSummary(ctx),
		Backup:       status,
		Snapshots:    rows,
		DataRoot:     s.cfg.Paths.Root,
		DataNote:     s.cfg.Paths.DataNote,
	}, nil
}

// shownSettings replaces the legacy "Local" (and an empty zone) with the
// machine's IANA name so the field never stores an untranslatable value.
func shownSettings(st map[string]string) map[string]string {
	out := make(map[string]string, len(st))
	for k, v := range st {
		if k == "weather_api_key" {
			out[k] = maskKey(v)
			continue
		}
		out[k] = v
	}
	tz := out["timezone"]
	if tz == "" || tz == "Local" {
		if name := systemTimezone(); name != "" {
			out["timezone"] = name
		}
	}
	return out
}

// liveDevices lists the ids whose preview should be offered on the today page.
func liveDevices(devices []store.Device) []string {
	out := make([]string, 0, len(devices))
	for _, d := range devices {
		if d.RevokedAt == nil {
			out = append(out, d.ID)
		}
	}
	return out
}

// setupCommands is the serial provisioning block shown after a token is made.
// The Wi-Fi line is a placeholder: the password is not stored anywhere.
func setupCommands(base, token string) []string {
	if token == "" {
		return nil
	}
	return []string{
		"wifi <SSID> <password>",
		"server " + base,
		"device dev1",
		"token " + token,
	}
}

// previewDevice picks the device whose frame the pages link to: the first
// live one, so a revoked or single dev1 does not leave a dead link behind.
func previewDevice(devices []store.Device) string {
	for _, d := range devices {
		if d.RevokedAt == nil {
			return d.ID
		}
	}
	return ""
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.settingsData(r.Context())
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "settings.html", data)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleItemsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var (
		items []store.Item
		err   error
	)
	if r.URL.Query().Get("scope") == "all" {
		items, err = s.allItems(ctx)
	} else {
		day, _, derr := s.Today(ctx)
		if derr != nil {
			http.Error(w, "timezone", http.StatusInternalServerError)
			return
		}
		items, err = s.st.ItemsOn(ctx, day.Format(store.DateLayout))
	}
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func isJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Content-Type"), "application/json")
}

func (s *Server) handleItemCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	day := s.clock().In(loc)
	today := day.Format(store.DateLayout)
	var in store.Item
	if isJSON(r) {
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		in.Title = r.FormValue("title")
		in.Description = r.FormValue("description")
		in.RRule = strings.TrimSpace(r.FormValue("rrule"))
		date := strings.TrimSpace(r.FormValue("date"))
		clock := strings.TrimSpace(r.FormValue("time"))
		// The quick-add boxes post one free-form line. With JS on, the tag
		// strip posts `parsed=1` plus the title/date/time/rrule it displayed;
		// those fields are taken as-is and the line is never re-parsed. That is
		// what makes a removed tag stay removed: re-parsing would find the
		// fragment again. Without JS, the line is parsed here.
		if raw := strings.TrimSpace(r.FormValue("raw")); raw != "" && r.FormValue("parsed") == "1" {
			in.Title = strings.TrimSpace(r.FormValue("title"))
			if in.Title == "" {
				in.Title = raw
			}
			in.DateLocal = strings.TrimSpace(r.FormValue("date"))
			clock = strings.TrimSpace(r.FormValue("time"))
			in.RRule = strings.TrimSpace(r.FormValue("rrule"))
			if raw != "" && in.DateLocal == "" && clock == "" && in.RRule == "" &&
				in.Title == raw {
				// The strip showed no tags at all: the line is just a title.
			}
		} else if raw := strings.TrimSpace(r.FormValue("raw")); raw != "" {
			loc, err := s.Location(ctx)
			if err != nil {
				http.Error(w, "timezone", http.StatusInternalServerError)
				return
			}
			got := ParseQuickAdd(raw, s.clock().In(loc), loc)
			in.Title = got.Title
			if v := strings.TrimSpace(r.FormValue("date")); v != "" {
				in.DateLocal = v
			} else if got.Date != "" {
				in.DateLocal = got.Date
			}
			if v := strings.TrimSpace(r.FormValue("time")); v != "" {
				clock = v
			} else if got.Time != "" {
				clock = got.Time
			}
			if v := strings.TrimSpace(r.FormValue("rrule")); v != "" {
				in.RRule = v
			} else if got.RRule != "" {
				in.RRule = got.RRule
			}
			if got.Title == raw && got.Date == "" && got.Time == "" && got.RRule == "" {
				// Nothing was recognized: the line is just a title, even when
				// it looks like a bare fragment ("明天").
				in.Title = raw
			}
			// The explicit boxes next to the field are a second source; they
			// win over the parser only when the tag strip is absent.
			if d := strings.TrimSpace(r.FormValue("date_manual")); d != "" && in.DateLocal == "" {
				in.DateLocal = d
			}
			if c := strings.TrimSpace(r.FormValue("time_manual")); c != "" && clock == "" {
				clock = c
			}
		}
		if date == "" {
			date = in.DateLocal
		}
		if date == "" {
			date = today
		}
		if clock == "" && in.RRule != "" {
			clock = "09:00"
		}
		if clock != "" {
			start, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
			if err != nil {
				http.Error(w, "bad time", http.StatusBadRequest)
				return
			}
			in.StartsAt = &start
		} else {
			in.AllDay = true
			in.DateLocal = date
		}
	}
	item, err := s.st.CreateItem(ctx, in, loc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if item.RRule != "" {
		if _, err := s.st.Materialize(ctx, loc, s.clock(), 31); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if isJSON(r) {
		writeJSON(w, http.StatusCreated, item)
		return
	}
	view := r.FormValue("view")
	if view == "" {
		view = "today"
	}
	if r.Header.Get("HX-Request") == "true" {
		s.respondItems(w, r, view)
		return
	}
	target := "/app"
	if view == "items" {
		target = "/items"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// undoTrigger is the HX-Trigger payload the page's toast listens for.
func undoTrigger(text, undoPath string) string {
	b, _ := json.Marshal(map[string]string{
		"todayToast": text,
		"todayUndo":  undoPath,
	})
	return string(b)
}

func (s *Server) handleItemPatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	loc, err := s.Location(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	var p store.Patch
	view := "today"
	if isJSON(r) {
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		form := r.Form
		view = form.Get("view")
		full := form.Get("full") == "1"
		if form.Has("title") {
			v := form.Get("title")
			p.Title = &v
		}
		if form.Has("description") {
			v := form.Get("description")
			p.Description = &v
		}
		if form.Has("status") {
			v := form.Get("status")
			p.Status = &v
		}
		if full || form.Has("pinned") {
			v := form.Get("pinned") != ""
			p.Pinned = &v
		}
		if full || form.Has("all_day") {
			v := form.Get("all_day") != ""
			p.AllDay = &v
		}
		if v, ok := formRRule(form); ok {
			p.RRule = &v
		}
		if full || form.Has("date") || form.Has("time") {
			date := strings.TrimSpace(form.Get("date"))
			clock := strings.TrimSpace(form.Get("time"))
			if date == "" {
				date = s.clock().In(loc).Format(store.DateLayout)
			}
			if clock == "" {
				v := ""
				p.StartsAt = &v
			} else {
				start, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
				if err != nil {
					http.Error(w, "bad time", http.StatusBadRequest)
					return
				}
				v := timeStr(start)
				p.StartsAt = &v
			}
			p.DateLocal = &date
		}
	}
	// Remember the state a tick replaced: the toast offers "撤销" and the
	// endpoint needs to know what to put back.
	var priorStatus string
	if p.Status != nil {
		if before, err := s.st.Item(ctx, id); err == nil {
			priorStatus = before.Status
		}
	}
	item, err := s.st.UpdateItem(ctx, id, p, loc)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	if p.Status != nil && priorStatus != "" && priorStatus != item.Status {
		s.ForceFullRefresh()
		if !isJSON(r) {
			text := "已勾选"
			if item.Status != store.StatusDone {
				text = "已取消勾选"
			}
			w.Header().Set("HX-Trigger", undoTrigger(text,
				"/api/items/"+id+"/status/"+priorStatus))
		}
	}
	if isJSON(r) {
		writeJSON(w, http.StatusOK, item)
		return
	}
	if view == "" {
		view = "today"
	}
	if view == "item" && r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/items", http.StatusSeeOther)
		return
	}
	s.respondItems(w, r, view)
}

// formRRule reads the repeat editor. The preset select posts either a ready
// RRULE or the literal "custom", in which case the three custom fields (every
// N weeks, the weekday set, the end rule) are assembled here. It reports
// false when the form did not carry a repeat choice at all, so a status-only
// patch leaves the rule alone.
func formRRule(form url.Values) (string, bool) {
	if !form.Has("rrule") && !form.Has("rrule_preset") {
		return "", false
	}
	preset := strings.TrimSpace(form.Get("rrule_preset"))
	switch preset {
	case "", "none":
		if !form.Has("rrule") {
			return "", false
		}
		raw := strings.TrimSpace(form.Get("rrule"))
		if raw == "" {
			return "", true
		}
		return raw, true
	case "weekdays":
		return "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", true
	case "custom":
		return assembleRRule(form)
	}
	return preset, true
}

// assembleRRule builds FREQ/INTERVAL/BYDAY/COUNT/UNTIL from the custom fields.
func assembleRRule(form url.Values) (string, bool) {
	freq := strings.ToUpper(strings.TrimSpace(form.Get("rrule_freq")))
	switch freq {
	case "DAILY", "WEEKLY", "MONTHLY":
	default:
		freq = "WEEKLY"
	}
	interval := 1
	if n, err := strconv.Atoi(strings.TrimSpace(form.Get("rrule_interval"))); err == nil &&
		n >= 1 && n <= 365 {
		interval = n
	}
	parts := []string{"FREQ=" + freq}
	if interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(interval))
	}
	if freq == "WEEKLY" {
		var days []string
		for _, tok := range form["rrule_day"] {
			switch strings.ToUpper(tok) {
			case "MO", "TU", "WE", "TH", "FR", "SA", "SU":
				days = append(days, strings.ToUpper(tok))
			}
		}
		if len(days) > 0 && len(days) < 7 {
			parts = append(parts, "BYDAY="+strings.Join(days, ","))
		}
	}
	switch form.Get("rrule_end") {
	case "count":
		if n, err := strconv.Atoi(strings.TrimSpace(form.Get("rrule_count"))); err == nil &&
			n >= 1 && n <= 10000 {
			parts = append(parts, "COUNT="+strconv.Itoa(n))
		}
	case "until":
		if v := strings.TrimSpace(form.Get("rrule_until")); v != "" {
			if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
				parts = append(parts, "UNTIL="+t.Format("20060102"))
			}
		}
	}
	return strings.Join(parts, ";"), true
}

func (s *Server) handleItemDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteItem(r.Context(), r.PathValue("id")); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	s.ForceFullRefresh()
	// The row is gone; offer the undo toast for five seconds.
	w.Header().Set("HX-Trigger", undoTrigger("已删除", "/api/items/"+r.PathValue("id")+"/restore"))
	view := r.URL.Query().Get("view")
	if view == "" {
		view = r.FormValue("view")
	}
	if r.Header.Get("HX-Request") == "true" {
		s.respondItems(w, r, view)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleItemDeletePost(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteItem(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/items", http.StatusSeeOther)
}

// respondItems re-renders the list partial the caller targeted.
func (s *Server) respondItems(w http.ResponseWriter, r *http.Request, view string) {
	ctx := r.Context()
	if view == "items" {
		items, err := s.allItems(ctx)
		if err != nil {
			http.Error(w, "db", http.StatusInternalServerError)
			return
		}
		s.render(w, r, "items_list.html", pageData{All: items})
		return
	}
	day, _, err := s.Today(ctx)
	if err != nil {
		http.Error(w, "timezone", http.StatusInternalServerError)
		return
	}
	items, err := s.st.ItemsOn(ctx, day.Format(store.DateLayout))
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	total, done := 0, 0
	for _, it := range items {
		if it.Status == store.StatusCancelled {
			continue
		}
		total++
		if it.Done() {
			done++
		}
	}
	s.render(w, r, "today_list.html", pageData{
		Items:       items,
		Total:       total,
		Done:        done,
		HasProgress: true,
		DateLabel:   day.Format("1月2日") + " " + weekdayZH[day.Weekday()],
		BoardLimit:  frame.BoardRowLimit,
	})
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	st, err := s.st.Settings(r.Context())
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	out := make(map[string]string, len(st))
	for k, v := range st {
		switch k {
		case "auth_password_hash", "auth_session_key":
			continue
		case "weather_api_key":
			// Never echo the real key; the page shows the mask and posting it
			// back leaves the stored value alone.
			out[k] = maskKey(v)
			continue
		}
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
}

var deviceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func (s *Server) handleSettingsPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxFirmwareBytes+(1<<20))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: "上传内容过大或格式错误"}, http.StatusBadRequest)
			return
		}
	} else if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	action := r.FormValue("action")
	var msg string
	switch action {
	case "upload_firmware":
		version, err := s.saveFirmware(r)
		if err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		msg = "固件已上传：" + version + "，设备下次取帧后升级"
	case "password":
		pw := r.FormValue("passphrase")
		if err := s.SetPassword(ctx, pw); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/login")
			w.WriteHeader(http.StatusOK)
			return
		}
		s.clearSessionCookie(w, r)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	case "logout_all":
		if err := s.RotateSessionKey(ctx); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		s.clearSessionCookie(w, r)
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/login")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	case "add_device":
		id := strings.ToLower(strings.TrimSpace(r.FormValue("device_id")))
		name := strings.TrimSpace(r.FormValue("device_name"))
		if !deviceIDPattern.MatchString(id) {
			s.renderSettingsPanel(w, r, pageData{Error: "设备 ID 只能用小写字母、数字、- 和 _（1–32 位）。"}, http.StatusBadRequest)
			return
		}
		if name == "" {
			name = id
		}
		if _, err := s.st.Device(ctx, id); err == nil {
			s.renderSettingsPanel(w, r, pageData{Error: "设备 " + id + " 已存在。"}, http.StatusBadRequest)
			return
		}
		if _, err := s.st.CreateDevice(ctx, id, name, ""); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		msg = "设备已添加，接着为它生成 token。"
	case "device_token":
		id := r.FormValue("device")
		token := newDeviceToken()
		if err := s.st.SetDeviceToken(ctx, id, tokenHash(token)); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		hint := token
		if len(hint) > 4 {
			hint = "…" + hint[len(hint)-4:]
		}
		_, _ = s.UpdateSettings(ctx, map[string]string{"device_token_hint": hint})
		msg = "新 token 已生成，旧 token 立即失效。"
		data, err := s.settingsData(ctx)
		if err != nil {
			http.Error(w, "db", http.StatusInternalServerError)
			return
		}
		data.Token = token
		data.SetupCmds = setupCommands(s.lanBase(), token)
		data.Message = msg
		s.render(w, r, "settings_panel.html", data)
		return
	case "revoke_device":
		id := r.FormValue("device")
		if err := s.st.RevokeDevice(ctx, id); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		msg = "设备已吊销。"
	case "preview_device":
		id := strings.TrimSpace(r.FormValue("device"))
		if !deviceIDPattern.MatchString(id) {
			s.renderSettingsPanel(w, r, pageData{Error: "设备 ID 无效。"}, http.StatusBadRequest)
			return
		}
		// Per device, in memory: the board hears it on its next poll, and this
		// form never touches the sound settings.
		s.RequestPreview(id)
		msg = "已请求设备 " + id + " 试听：它会在 1 分钟内响一声。"
	case "sound":
		vol := strings.TrimSpace(r.FormValue("sound_volume"))
		if vol == "" {
			vol = "60"
		}
		n, err := strconv.Atoi(vol)
		if err != nil || n < 0 || n > 100 {
			s.renderSettingsPanel(w, r, pageData{Error: "音量必须是 0–100。"}, http.StatusBadRequest)
			return
		}
		lead := strings.TrimSpace(r.FormValue("alarm_lead_minutes"))
		switch lead {
		case "", "0", "5", "10", "15":
		default:
			lead = "0"
		}
		if lead == "" {
			lead = "0"
		}
		updates := map[string]string{
			"sound_volume":       strconv.Itoa(n),
			"sound_keys":         boolString(r.FormValue("sound_keys") != ""),
			"sound_alarm":        boolString(r.FormValue("sound_alarm") != ""),
			"alarm_lead_minutes": lead,
		}
		if _, err := s.UpdateSettings(ctx, updates); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		msg = "声音设置已保存。"
	case "push_firmware":
		version, err := s.ApplyPendingFirmware(ctx)
		if err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		msg = "已推送固件 " + version + "：设备会在下一次取帧时升级。"
	case "skip_update":
		v := strings.TrimSpace(r.FormValue("version"))
		if v == "" {
			s.renderSettingsPanel(w, r, pageData{Error: "缺少版本号。"}, http.StatusBadRequest)
			return
		}
		if _, err := s.UpdateSettings(ctx, map[string]string{"update_skip_version": v}); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		msg = "已跳过 " + v + "，下次发布再提示。"
	case "refresh_device":
		id := strings.TrimSpace(r.FormValue("device"))
		if !deviceIDPattern.MatchString(id) {
			s.renderSettingsPanel(w, r, pageData{Error: "设备 ID 无效。"}, http.StatusBadRequest)
			return
		}
		// Per device, in memory: the board repaints on its next poll instead of
		// waiting for the bitmap to change.
		s.ForceRefresh(id)
		msg = "已请求设备 " + id + " 立即刷新：它会在 1 分钟内重画。"
	case "device_city":
		id := r.FormValue("device")
		if !deviceIDPattern.MatchString(id) {
			s.renderSettingsPanel(w, r, pageData{Error: "设备 ID 无效。"}, http.StatusBadRequest)
			return
		}
		if _, err := s.UpdateSettings(ctx, map[string]string{
			"weather_city_" + id: strings.TrimSpace(r.FormValue("weather_city")),
		}); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		msg = "设备 " + id + " 的天气城市已保存。"
	case "weather":
		updates := map[string]string{
			"weather_city":        strings.TrimSpace(r.FormValue("weather_city")),
			"weather_ip_fallback": boolString(r.FormValue("weather_ip_fallback") != ""),
		}
		// The page shows "••••3f2a"; posting that back must not overwrite the
		// real key, and an empty box clears it only when the person asks.
		raw := strings.TrimSpace(r.FormValue("weather_api_key"))
		switch {
		case raw == "":
			updates["weather_api_key"] = ""
		case strings.HasPrefix(raw, "••••"):
			// unchanged
		default:
			if len(raw) < 8 {
				s.renderSettingsPanel(w, r, pageData{Error: "WeatherAPI key 太短。"}, http.StatusBadRequest)
				return
			}
			updates["weather_api_key"] = raw
		}
		if _, err := s.UpdateSettings(ctx, updates); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		msg = "天气设置已保存。"
	case "localnet_request":
		// The host owns the probe and System Settings; the page only pokes it
		// through settings, same as the funnel button.
		if _, err := s.UpdateSettings(ctx, map[string]string{
			"localnet_request": strconv.FormatInt(time.Now().UnixNano(), 10),
		}); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusInternalServerError)
			return
		}
		msg = "已请求宿主重新申请本地网络权限；若仍然被拒绝，会打开系统设置。"
	default:
		if r.FormValue("funnel_continue") != "" {
			// The host owns Tailscale; the page pokes it through settings and
			// the host's OnSettingsChanged callback runs the CLI outside this
			// request.
			if _, err := s.UpdateSettings(ctx, map[string]string{
				"funnel":         "true",
				"funnel_request": strconv.FormatInt(time.Now().UnixNano(), 10),
			}); err != nil {
				s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusInternalServerError)
				return
			}
			msg = "已请求宿主继续设置外网，按提示在浏览器完成授权。"
			break
		}
		tz := strings.TrimSpace(r.FormValue("timezone"))
		if tz == "Local" {
			tz = systemTimezone()
		}
		if tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				s.renderSettingsPanel(w, r, pageData{Error: "时区无效：" + tz}, http.StatusBadRequest)
				return
			}
		}
		// A bare port keeps the interface already in use; the field is
		// compared after expansion so resubmitting the same port is a no-op
		// even when the label and the listen address are spelled differently.
		listen := strings.TrimSpace(r.FormValue("listen_addr"))
		listenChanged := false
		if listen != "" {
			target := ExpandAddr(s.Addr(), listen)
			if err := ValidateAddr(target); err != nil {
				s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
				return
			}
			listenChanged = !sameListen(target, s.Addr())
		}
		feed := strings.TrimSpace(r.FormValue("update_feed"))
		if feed != "" && !strings.HasPrefix(feed, "https://") && !strings.HasPrefix(feed, "http://") {
			s.renderSettingsPanel(w, r, pageData{Error: "更新源必须是 http(s) 地址。"}, http.StatusBadRequest)
			return
		}
		diff := map[string]string{}
		// Only keys the form actually carried: the settings page is split into
		// cards, and a card that lacks a checkbox must not turn it off.
		if _, ok := r.Form["open_at_login"]; ok {
			diff["open_at_login"] = boolString(r.Form["open_at_login"][0] != "")
		}
		if _, ok := r.Form["tray_mode"]; ok {
			diff["tray_mode"] = trayModeValue(r.FormValue("tray_mode"))
		}
		if _, ok := r.Form["funnel"]; ok {
			diff["funnel"] = boolString(r.FormValue("funnel") != "")
		}
		if _, ok := r.Form["night_off"]; ok {
			diff["night_off"] = boolString(r.FormValue("night_off") != "")
		}
		if _, ok := r.Form["auto_carry"]; ok {
			diff["auto_carry"] = boolString(r.FormValue("auto_carry") != "")
		}
		if _, ok := r.Form["proxy_mode_present"]; ok {
			diff["proxy_mode"] = proxyModeValue(r.FormValue("proxy_mode") != "")
		}
		if _, ok := r.Form["update_auto_check_present"]; ok {
			diff["update_auto_check"] = boolString(r.FormValue("update_auto_check") != "")
		}
		if _, ok := r.Form["backup_icloud_present"]; ok {
			diff["backup_icloud"] = boolString(r.FormValue("backup_icloud") != "")
		}
		if _, ok := r.Form["update_auto_download_present"]; ok {
			diff["update_auto_download"] = boolString(r.FormValue("update_auto_download") != "")
		}
		if raw, ok := r.Form["proxy_url"]; ok {
			v := strings.TrimSpace(raw[0])
			if v != "" {
				if err := validateProxyURL(v); err != nil {
					s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
					return
				}
			}
			diff["proxy_url"] = v
		}
		for _, key := range []string{"night_start", "night_end"} {
			if _, ok := r.Form[key]; !ok {
				continue
			}
			v := strings.TrimSpace(r.FormValue(key))
			if v == "" {
				continue
			}
			if _, ok := minutesOfDay(v); !ok {
				s.renderSettingsPanel(w, r, pageData{Error: "夜间时间格式无效：" + v}, http.StatusBadRequest)
				return
			}
			diff[key] = v
		}
		if _, ok := r.Form["update_feed"]; ok {
			diff["update_feed"] = feed
		}
		if _, ok := r.Form["timezone"]; ok && tz != "" {
			diff["timezone"] = tz
		}
		if _, err := s.UpdateSettings(ctx, diff); err != nil {
			s.renderSettingsPanel(w, r, pageData{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		msg = "设置已保存。"
		if r.FormValue("proxy_test") != "" {
			// The button asks for a live check; do it after saving so it
			// tests what was just written.
			res := s.proxyTest(ctx)
			switch {
			case res.OK:
				msg = fmt.Sprintf("连接成功：%s，HTTP %d，%d ms", res.Proxy, res.Status, res.Millis)
			default:
				msg = "连接失败：" + res.Error
			}
			data, err := s.settingsData(ctx)
			if err != nil {
				http.Error(w, "db", http.StatusInternalServerError)
				return
			}
			data.Message = msg
			s.render(w, r, "settings_panel.html", data)
			return
		}
		if listenChanged {
			if err := s.SetListenAddr(listen); err != nil {
				msg = ""
				s.renderSettingsPanel(w, r, pageData{Error: "设置已保存，但端口没换成：" + err.Error()}, http.StatusBadRequest)
				return
			}
			msg = "端口已改为 " + s.Addr() + "，旧地址不再应答。"
		}
	}
	data, err := s.settingsData(ctx)
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	data.Message = msg
	s.render(w, r, "settings_panel.html", data)
}

// humanSize renders a byte count for the backup list.
func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func proxyModeValue(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// validateProxyURL accepts the three schemes the resolver understands and
// rejects anything without a host and port.
func validateProxyURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("代理地址无效：需要 host:port")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return errors.New("代理协议只能是 http、https 或 socks5")
	}
	if u.Port() == "" {
		return errors.New("代理地址需要带端口，例如 127.0.0.1:6152")
	}
	return nil
}

func (s *Server) renderSettingsPanel(w http.ResponseWriter, r *http.Request, data pageData, code int) {
	full, err := s.settingsData(r.Context())
	if err != nil {
		http.Error(w, "db", http.StatusInternalServerError)
		return
	}
	if data.Error != "" {
		full.Error = data.Error
	}
	if data.Message != "" {
		full.Message = data.Message
	}
	if data.Token != "" {
		full.Token = data.Token
	}
	s.renderStatus(w, r, code, "settings_panel.html", full)
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func timeStr(t time.Time) string { return t.Format(time.RFC3339Nano) }

// trayModeValue validates the three-way menu-bar setting. An unknown value
// keeps the previous behaviour instead of blanking the title.
func trayModeValue(v string) string {
	switch v {
	case "progress", "next", "icon":
		return v
	}
	return "progress"
}
