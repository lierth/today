package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"today/internal/frame"
	"today/internal/store"
	"today/internal/weather"
)

// Service is Today's rulebook without the web around it.
//
// The React page, the /api/v2 JSON API and the mygo bindings of the Mac app
// all ask this type the same questions, so "what today shows", "what a
// quick-add line means" and "when a board counts as offline" are decided once.
// A Service method takes a context and plain values: no *http.Request, no
// ResponseWriter, nothing a caller could render by accident. Handlers keep
// the HTTP parts (cookies, redirects, templates).
type Service struct {
	st      *store.Store
	log     *log.Logger
	clock   func() time.Time
	weather *weather.Client

	// refreshAll marks every board for a full redraw; refreshOne marks a
	// single one. The frame handler owns the flags, so the service only asks.
	refreshAll func()
	refreshOne func(deviceID string)

	paths Paths
}

// ServiceOptions is what the server hands its service.
type ServiceOptions struct {
	Store        *store.Store
	Logger       *log.Logger
	Clock        func() time.Time
	Weather      *weather.Client
	RefreshAll   func()
	RefreshOne   func(string)
	Paths        Paths
	OutboundHTTP bool // reserved: the weather client already carries it
}

func newService(o ServiceOptions) *Service {
	if o.Logger == nil {
		o.Logger = log.Default()
	}
	if o.Clock == nil {
		o.Clock = o.Store.Now
	}
	return &Service{
		st:         o.Store,
		log:        o.Logger,
		clock:      o.Clock,
		weather:    o.Weather,
		refreshAll: o.RefreshAll,
		refreshOne: o.RefreshOne,
		paths:      o.Paths,
	}
}

// Hint is what a request knows about the caller that the rules need: the
// address to fall back to when WeatherAPI locates by IP, and whether the
// request arrived through the public funnel (never trust that IP then).
type Hint struct {
	IP     string
	Funnel bool
}

// Day is one calendar day in the person's timezone.
type Day struct {
	Time    time.Time
	Key     string // yyyy-mm-dd
	Label   string // 11月19日
	Weekday string // 周三
	Lunar   frame.LunarInfo
}

func (svc *Service) day(ctx context.Context) (Day, *time.Location, error) {
	loc, err := svc.Location(ctx)
	if err != nil {
		return Day{}, nil, err
	}
	now := svc.clock().In(loc)
	return Day{
		Time:    now,
		Key:     now.Format(store.DateLayout),
		Label:   now.Format("1月2日"),
		Weekday: weekdayZH[now.Weekday()],
		Lunar:   frame.LunarFor(now),
	}, loc, nil
}

// Location is the person's timezone, from the settings row.
func (svc *Service) Location(ctx context.Context) (*time.Location, error) {
	st, err := svc.st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	tz := strings.TrimSpace(st["timezone"])
	if tz == "" {
		return time.Local, nil
	}
	loc, err := loadLocation(tz)
	if err != nil || loc == nil {
		return time.Local, nil
	}
	return loc, nil
}

// Clock is the service's notion of now. Tests pin it through the store.
func (svc *Service) Clock() time.Time { return svc.clock() }

// TodayView is everything the Today page and the tray panel show.
type TodayView struct {
	Day         Day
	Items       []store.Item
	Total       int
	Done        int
	Carried     []CarriedItem
	AutoCarry   bool
	BoardLimit  int
	WeatherLine string
	PreviewID   string
	PreviewIDs  []string
}

// Today assembles the day: its items, the progress, the leftovers from
// earlier days and the weather line.
func (svc *Service) Today(ctx context.Context, hint Hint) (TodayView, error) {
	day, loc, err := svc.day(ctx)
	if err != nil {
		return TodayView{}, err
	}
	items, err := svc.st.ItemsOn(ctx, day.Key)
	if err != nil {
		return TodayView{}, err
	}
	out := TodayView{Day: day, Items: items, BoardLimit: frame.BoardRowLimit}
	for _, it := range items {
		if it.Status == store.StatusCancelled {
			continue
		}
		out.Total++
		if it.Done() {
			out.Done++
		}
	}
	settings, err := svc.st.Settings(ctx)
	if err != nil {
		return TodayView{}, err
	}
	out.AutoCarry = store.BoolSetting(settings, "auto_carry")
	carried, err := svc.st.Unfinished(ctx, day.Key, loc)
	if err != nil {
		return TodayView{}, err
	}
	out.Carried = make([]CarriedItem, 0, len(carried))
	for _, it := range carried {
		out.Carried = append(out.Carried, CarriedItem{Item: it, DayLabel: chineseDay(it.DateLocal, loc)})
	}
	devices, err := svc.st.Devices(ctx)
	if err != nil {
		return TodayView{}, err
	}
	out.PreviewID = previewDevice(devices)
	out.PreviewIDs = liveDevices(devices)
	line, err := svc.WeatherLine(ctx, hint)
	if err == nil {
		out.WeatherLine = line
	}
	return out, nil
}

// Group is a labelled run of items — the shape both /items and /search use.
type Group struct {
	Label string
	Items []store.Item
}

// Upcoming buckets the live items: overdue, today, tomorrow, this week,
// later, then the ones with no date.
func (svc *Service) Upcoming(ctx context.Context) ([]Group, error) {
	day, loc, err := svc.day(ctx)
	if err != nil {
		return nil, err
	}
	items, err := svc.st.Upcoming(ctx, day.Key, 400, 400)
	if err != nil {
		return nil, err
	}
	leftovers, err := svc.st.Unfinished(ctx, day.Key, loc)
	if err != nil {
		return nil, err
	}
	weekEnd := day.Time.AddDate(0, 0, 7-(int(day.Time.Weekday())+6)%7).Format(store.DateLayout)
	tomorrow := day.Time.AddDate(0, 0, 1).Format(store.DateLayout)

	var gOver, gToday, gTomorrow, gWeek, gLater, gUndated []store.Item
	gOver = append(gOver, leftovers...)
	for _, it := range items {
		// A finished row already shows up in the folded "already done" group.
		// Keeping it in its day as well is what made the same checkmark appear
		// twice on one screen ("买牛奶和鸡蛋" under 今天 and under 已完成).
		if it.Status == store.StatusDone {
			continue
		}
		switch {
		case it.DateLocal == "":
			gUndated = append(gUndated, it)
		case it.DateLocal == day.Key:
			gToday = append(gToday, it)
		case it.DateLocal == tomorrow:
			gTomorrow = append(gTomorrow, it)
		case it.DateLocal < day.Key:
			// Only impossible when a row was carried between the two reads.
			gOver = append(gOver, it)
		case it.DateLocal < weekEnd:
			gWeek = append(gWeek, it)
		default:
			gLater = append(gLater, it)
		}
	}
	// Done rows are folded at the bottom by the UI.
	done, err := svc.st.DoneItems(ctx, day.Time.AddDate(0, 0, -30).Format(store.DateLayout), 100)
	if err != nil {
		return nil, err
	}
	out := []Group{
		{Label: "已逾期", Items: gOver},
		{Label: "今天", Items: gToday},
		{Label: "明天", Items: gTomorrow},
		{Label: "本周", Items: gWeek},
		{Label: "以后", Items: gLater},
		{Label: "无日期", Items: gUndated},
		{Label: "已完成（近 30 天）", Items: done},
	}
	kept := out[:0]
	for _, g := range out {
		if len(g.Items) > 0 {
			kept = append(kept, g)
		}
	}
	return kept, nil
}

// HistoryView is the past: what was done, grouped by day, plus the week's
// numbers.
type HistoryView struct {
	Day           Day
	Past          []store.Item
	DoneThisWeek  int
	DoneLastWeek  int
	Groups        []Group
	LastWeekLabel string
}

// History lists the last 200 finished rows and the two weekly totals.
func (svc *Service) History(ctx context.Context) (HistoryView, error) {
	day, loc, err := svc.day(ctx)
	if err != nil {
		return HistoryView{}, err
	}
	past, err := svc.st.PastItems(ctx, day.Key, 200)
	if err != nil {
		return HistoryView{}, err
	}
	monday := day.Time.AddDate(0, 0, -((int(day.Time.Weekday()) + 6) % 7))
	from := monday.Format(store.DateLayout)
	to := monday.AddDate(0, 0, 7).Format(store.DateLayout)
	doneThis, _, err := svc.st.WeekStats(ctx, from, to)
	if err != nil {
		return HistoryView{}, err
	}
	lastFrom := monday.AddDate(0, 0, -7).Format(store.DateLayout)
	doneLast, _, err := svc.st.WeekStats(ctx, lastFrom, from)
	if err != nil {
		return HistoryView{}, err
	}
	out := HistoryView{Day: day, Past: past, DoneThisWeek: doneThis, DoneLastWeek: doneLast}
	index := map[string]int{}
	for _, it := range past {
		i, ok := index[it.DateLocal]
		if !ok {
			i = len(out.Groups)
			index[it.DateLocal] = i
			out.Groups = append(out.Groups, Group{Label: chineseDay(it.DateLocal, loc)})
		}
		out.Groups[i].Items = append(out.Groups[i].Items, it)
	}
	return out, nil
}

// Search finds items by text, grouped by the day they belong to.
func (svc *Service) Search(ctx context.Context, query string, limit int) ([]Group, error) {
	loc, err := svc.Location(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	items, err := svc.st.Search(ctx, strings.TrimSpace(query), limit)
	if err != nil {
		return nil, err
	}
	var groups []Group
	index := map[string]int{}
	for _, it := range items {
		i, ok := index[it.DateLocal]
		if !ok {
			i = len(groups)
			index[it.DateLocal] = i
			groups = append(groups, Group{Label: chineseDay(it.DateLocal, loc)})
		}
		groups[i].Items = append(groups[i].Items, it)
	}
	return groups, nil
}

// AllItems lists every live row, newest first, for the management page.
func (svc *Service) AllItems(ctx context.Context, limit int) ([]store.Item, error) {
	day, _, err := svc.day(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 500
	}
	return svc.st.AllItems(ctx, day.Key, limit)
}

// Item loads one row.
func (svc *Service) Item(ctx context.Context, id string) (store.Item, error) {
	return svc.st.Item(ctx, id)
}

// ItemInput is a new to-do as the UI describes it: a line of text plus the
// tags the strip displays. An empty field means "not mentioned".
type ItemInput struct {
	CategoryID  string
	Title       string
	Description string
	Date        string // yyyy-mm-dd
	Time        string // HH:MM, empty for an all-day row
	RRule       string
	Raw         string // the untagged line, when the caller has no tags
	Parsed      bool   // the caller already applied the tags; do not re-parse
}

// CreateItem adds a to-do, teaching the parser the same thing the web page
// shows: recognised tags become fields, the rest stays the title.
func (svc *Service) CreateItem(ctx context.Context, in ItemInput) (store.Item, error) {
	loc, err := svc.Location(ctx)
	if err != nil {
		return store.Item{}, err
	}
	now := svc.clock().In(loc)
	today := now.Format(store.DateLayout)

	item := store.Item{
		CategoryID:  in.CategoryID,
		Title:       strings.TrimSpace(in.Title),
		Description: strings.TrimSpace(in.Description),
		RRule:       strings.TrimSpace(in.RRule),
		DateLocal:   strings.TrimSpace(in.Date),
	}
	clock := strings.TrimSpace(in.Time)
	raw := strings.TrimSpace(in.Raw)
	switch {
	case raw != "" && in.Parsed:
		// The strip posted what it displayed; re-parsing would resurrect a
		// tag the person deleted with the ×.
		if item.Title == "" {
			item.Title = raw
		}
	case raw != "":
		got := ParseQuickAdd(raw, now, loc)
		item.Title = got.Title
		if item.DateLocal == "" {
			item.DateLocal = got.Date
		}
		if clock == "" {
			clock = got.Time
		}
		if item.RRule == "" {
			item.RRule = got.RRule
		}
		if got.Title == raw && got.Date == "" && got.Time == "" && got.RRule == "" {
			item.Title = raw
		}
	}
	if item.DateLocal == "" {
		item.DateLocal = today
	}
	if clock == "" && item.RRule != "" {
		clock = "09:00"
	}
	if clock != "" {
		start, err := parseLocalTime(item.DateLocal, clock, loc)
		if err != nil {
			return store.Item{}, err
		}
		item.StartsAt = &start
		item.DateLocal = start.In(loc).Format(store.DateLayout)
	} else {
		item.AllDay = true
	}
	created, err := svc.st.CreateItem(ctx, item, loc)
	if err != nil {
		return store.Item{}, err
	}
	if created.RRule != "" {
		if _, err := svc.st.Materialize(ctx, loc, svc.clock(), 31); err != nil {
			return store.Item{}, err
		}
	}
	return created, nil
}

// parseLocalTime turns the date and clock fields into one instant in loc.
// The layout is the one the whole app posts; anything else is a bad request.
func parseLocalTime(date, clock string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("请填写正确的日期和时间")
	}
	return t, nil
}

// PatchItem edits one row. The patch carries pointers, so "absent" and
// "clear" stay different things.
func (svc *Service) PatchItem(ctx context.Context, id string, p store.Patch) (store.Item, error) {
	loc, err := svc.Location(ctx)
	if err != nil {
		return store.Item{}, err
	}
	var prior string
	if p.Status != nil {
		if before, err := svc.st.Item(ctx, id); err == nil {
			prior = before.Status
		}
	}
	item, err := svc.st.UpdateItem(ctx, id, p, loc)
	if err != nil {
		return store.Item{}, err
	}
	if p.Status != nil && prior != "" && prior != item.Status {
		svc.refreshAllDevices()
	}
	return item, nil
}

// PriorStatus is the status a patch replaced, for the undo toast.
func (svc *Service) PriorStatus(ctx context.Context, id string) string {
	it, err := svc.st.Item(ctx, id)
	if err != nil {
		return ""
	}
	return it.Status
}

// SetStatus ticks or unticks a row and repaints the boards.
func (svc *Service) SetStatus(ctx context.Context, id, status string) (store.Item, error) {
	if err := svc.st.SetStatus(ctx, id, status); err != nil {
		return store.Item{}, err
	}
	svc.refreshAllDevices()
	return svc.st.Item(ctx, id)
}

// Delete removes a row (soft delete, restorable).
func (svc *Service) Delete(ctx context.Context, id string) error {
	if err := svc.st.DeleteItem(ctx, id); err != nil {
		return err
	}
	svc.refreshAllDevices()
	return nil
}

// Restore undoes a delete.
func (svc *Service) Restore(ctx context.Context, id string) (store.Item, error) {
	it, err := svc.st.RestoreItem(ctx, id)
	if err != nil {
		return store.Item{}, err
	}
	svc.refreshAllDevices()
	return it, nil
}

// Carry moves yesterday's leftovers to today. "all" carries every row the
// Today page offers; ids carry exactly those.
func (svc *Service) Carry(ctx context.Context, ids []string, all bool) (store.CarryResult, error) {
	day, loc, err := svc.day(ctx)
	if err != nil {
		return store.CarryResult{}, err
	}
	if all {
		ids = nil
		left, err := svc.st.Unfinished(ctx, day.Key, loc)
		if err != nil {
			return store.CarryResult{}, err
		}
		for _, it := range left {
			ids = append(ids, it.ID)
		}
	}
	res, err := svc.st.CarryItems(ctx, ids, day.Key, loc)
	if err != nil {
		return store.CarryResult{}, err
	}
	if res.Moved > 0 {
		svc.refreshAllDevices()
	}
	return res, nil
}

// Uncarry puts rows back on the day they slipped from.
func (svc *Service) Uncarry(ctx context.Context, ids []string) (int, error) {
	loc, err := svc.Location(ctx)
	if err != nil {
		return 0, err
	}
	n, err := svc.st.UncarryItems(ctx, ids, loc)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		svc.refreshAllDevices()
	}
	return n, nil
}

// Reorder rewrites the manual order of today's rows.
func (svc *Service) Reorder(ctx context.Context, ids []string) error {
	loc, err := svc.Location(ctx)
	if err != nil {
		return err
	}
	return svc.st.Reorder(ctx, ids, loc)
}

// Parse turns one line of natural Chinese into tags.
func (svc *Service) Parse(ctx context.Context, text string) (QuickAdd, error) {
	loc, err := svc.Location(ctx)
	if err != nil {
		return QuickAdd{}, err
	}
	return ParseQuickAdd(text, svc.clock().In(loc), loc), nil
}

func (svc *Service) refreshAllDevices() {
	if svc.refreshAll != nil {
		svc.refreshAll()
	}
}

// RefreshDevice asks one board to fetch a full frame on its next poll.
func (svc *Service) RefreshDevice(deviceID string) {
	if svc.refreshOne != nil {
		svc.refreshOne(deviceID)
	}
}

// Settings returns the stored settings with secrets muted: the page and the
// app both show a masked key, so masking happens here and not per caller.
func (svc *Service) Settings(ctx context.Context) (map[string]string, error) {
	st, err := svc.st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	return shownSettings(st), nil
}

// RawSettings is the unmasked map, for callers inside the process that need
// to compare values (never for the API).
func (svc *Service) RawSettings(ctx context.Context) (map[string]string, error) {
	return svc.st.Settings(ctx)
}

// UpdateSettings writes the diff and returns the new masked map.
func (svc *Service) UpdateSettings(ctx context.Context, diff map[string]string) (map[string]string, error) {
	if err := svc.st.PutSettings(ctx, diff); err != nil {
		return nil, err
	}
	return svc.Settings(ctx)
}

// DeviceView is one board as the UI shows it: the row plus the things the
// page would otherwise compute three times.
type DeviceView struct {
	store.Device
	Online       bool   `json:"online"`
	BatteryLabel string `json:"battery_label"`
	Climate      string `json:"climate"`
	Since        string `json:"since"`
	Firmware     string `json:"firmware"`
	City         string `json:"city"`
	Preview      bool   `json:"preview"` // whether this board is the one the preview shows
}

// Devices lists the boards with the derived labels the page needs.
func (svc *Service) Devices(ctx context.Context) ([]DeviceView, error) {
	rows, err := svc.st.Devices(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := svc.st.Settings(ctx)
	if err != nil {
		settings = map[string]string{}
	}
	preview := previewDevice(rows)
	out := make([]DeviceView, 0, len(rows))
	for _, d := range rows {
		if d.RevokedAt != nil {
			continue
		}
		out = append(out, DeviceView{
			Device:       d,
			Online:       deviceOnline(d),
			BatteryLabel: deviceBatteryLabel(d),
			Climate:      deviceClimate(d),
			Since:        deviceSince(d),
			Firmware:     strings.TrimSpace(settings["firmware_version"]),
			City:         strings.TrimSpace(settings["weather_city_"+d.ID]),
			Preview:      d.ID == preview,
		})
	}
	return out, nil
}

// WeatherStatus is what the settings page says about the API key and city.
func (svc *Service) WeatherStatus(ctx context.Context) weatherStatus {
	st, err := svc.st.Settings(ctx)
	if err != nil {
		return weatherStatus{}
	}
	key := strings.TrimSpace(st["weather_api_key"])
	status := weatherStatus{KeySet: key != "", Masked: maskKey(key)}
	if svc.weather != nil {
		lastErr, _ := svc.weather.LastError()
		if lastErr != nil {
			status.KeyBad = weather.IsBadKey(lastErr)
			status.Detail = lastErr.Error()
		}
	}
	loc := weather.SelectQuery("", st["weather_city"], st["weather_ip_fallback"] != "false", false, "")
	status.Source = weather.SourceLabel(loc.Source)
	status.Disabled = loc.Q == ""
	if key != "" && loc.Q != "" && svc.weather != nil {
		// Cache only: rendering the page must never wait on the network.
		if report, ok, fresh := svc.weather.Cached(loc.Q, svc.clock()); ok {
			status.City = report.City
			status.Fetched = report.Fetched
			if fresh {
				status.Detail = ""
			}
		}
	}
	return status
}

// WeatherLine is the one-line forecast under the date, from the same cache
// the frame uses so page and board never disagree. It returns "" when no key
// or city is set.
func (svc *Service) WeatherLine(ctx context.Context, hint Hint) (string, error) {
	st, err := svc.st.Settings(ctx)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(st["weather_api_key"])
	loc := svc.weatherQuery(ctx, hint, "")
	if key == "" || loc.Q == "" || svc.weather == nil {
		return "", nil
	}
	report, err := svc.weather.Report(ctx, key, loc.Q)
	if err != nil && report.City == "" {
		return "", nil
	}
	w := weather.Report{City: report.City, Text: report.Short(), TempC: report.TempC,
		MaxC: report.MaxC, MinC: report.MinC}
	line := w.City
	if line == "" {
		line = "天气"
	}
	return line + " " + w.Text + " " + weather.RoundC(w.TempC) + "°", nil
}

// WeatherLineCached reads only the cache; the tray menu uses it so opening
// the menu never waits on the network.
func (svc *Service) WeatherLineCached(ctx context.Context) string {
	st, err := svc.st.Settings(ctx)
	if err != nil || svc.weather == nil {
		return ""
	}
	key := strings.TrimSpace(st["weather_api_key"])
	if key == "" {
		return ""
	}
	loc := svc.weatherQuery(ctx, Hint{}, "")
	if loc.Q == "" {
		return ""
	}
	report, ok, _ := svc.weather.Cached(loc.Q, svc.clock())
	if !ok {
		return ""
	}
	return report.City + " " + report.Short() + " " + weather.RoundC(report.TempC) + "°"
}

// deviceCity is the per-board city, stored under a device-scoped key so a
// rename or an older database needs no migration.
func (svc *Service) deviceCity(ctx context.Context, deviceID string) string {
	if deviceID == "" {
		return ""
	}
	st, err := svc.st.Settings(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(st["weather_city_"+deviceID])
}

func (svc *Service) weatherQuery(ctx context.Context, hint Hint, deviceID string) weather.Location {
	st, err := svc.st.Settings(ctx)
	if err != nil {
		st = map[string]string{}
	}
	return weather.SelectQuery(
		svc.deviceCity(ctx, deviceID),
		st["weather_city"],
		st["weather_ip_fallback"] != "false",
		hint.Funnel,
		hint.IP,
	)
}

// HeaderWeather is the header's second line for one board.
func (svc *Service) HeaderWeather(ctx context.Context, deviceID string, hint Hint) frame.Weather {
	st, err := svc.st.Settings(ctx)
	if err != nil || svc.weather == nil {
		return frame.Weather{}
	}
	key := strings.TrimSpace(st["weather_api_key"])
	if key == "" {
		return frame.Weather{}
	}
	loc := svc.weatherQuery(ctx, hint, deviceID)
	if loc.Q == "" {
		return frame.Weather{}
	}
	// Stale-while-revalidate: Report answers from cache and kicks a refresh,
	// so a frame never waits on the network.
	report, err := svc.weather.Report(ctx, key, loc.Q)
	if err != nil && report.City == "" {
		svc.log.Printf("server: weather: %v", err)
		return frame.Weather{}
	}
	return frame.Weather{City: report.City, Text: report.Short(),
		TempC: report.TempC, MaxC: report.MaxC, MinC: report.MinC}
}
