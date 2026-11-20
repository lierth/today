package host

import (
	"context"
	"fmt"
	"strings"
	"time"

	"today/internal/server"
	"today/internal/store"

	"github.com/egoist/mygo"
)

// Panel is the service the Svelte panel and the settings window call through
// mygo's bindings. It is a thin adapter on purpose: every rule lives in
// server.Service (the same code the web UI reaches over /api/v2), so the panel
// cannot drift from the page or the board.
//
// Methods may take a context first: mygo cancels it when the page goes away,
// so a database read left over from a closed panel does not hold anything.
type Panel struct {
	h *Host
}

// NewPanel wires the bindings to the running host. It is separate from Host
// so tests can build one without a tray.
func NewPanel(h *Host) *Panel { return &Panel{h: h} }

// BindPanel registers the service with mygo. Call it before mygo.App.Run:
// the client is written from the registered services, so a late binding would
// be missing from mygo.ts.
func BindPanel(h *Host) {
	mygo.BindAs("Panel", NewPanel(h))
}

// GenerateOnly is `go tool mygo generate`'s way in. mygo writes the client
// from App.Run, and that runs main up to Run: booting the database first would
// open the person's real store just to write a .ts file. With nil host no
// method is ever called, only their signatures are reflected.
func GenerateOnly() error {
	BindPanel(nil)
	return mygo.App.Run()
}

// Config is what the panel needs from the app rather than from the database:
// the version string and whether the person is in a dev build.
type Config struct {
	Version string `json:"version"`
	Dev     bool   `json:"dev"`
}

// Item is one row of the day as the panel shows it.
type Item struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Done    bool   `json:"done"`
	Pinned  bool   `json:"pinned"`
	Carried bool   `json:"carried"`
	// Time is "10:00" for a timed item and "" for an all-day one. The panel
	// prints it as-is; formatting a clock is the panel's job, not Go's.
	Time string `json:"time"`
}

// Carried is a leftover from an earlier day, offered for "move to today".
type Carried struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Day is "11月12日": the day it was left on.
	Day string `json:"day"`
}

// Device is one board's state line at the bottom of the panel.
type Device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Online  bool   `json:"online"`
	Battery string `json:"battery"` // "76%", "" when the board never reported
	Climate string `json:"climate"` // "23° 45%", "" without a sensor
	Since   string `json:"since"`   // "在线" / "离线 2 小时"
}

// TodayView is the panel's whole payload: the day header, the rows, the
// leftovers and the devices. One call, because the panel is rebuilt on every
// open and a round trip per section would show it half-empty for a moment.
type TodayView struct {
	Date    string    `json:"date"`    // 11月19日
	Weekday string    `json:"weekday"` // 周三
	Lunar   string    `json:"lunar"`   // 农历十月廿八
	Total   int       `json:"total"`
	Done    int       `json:"done"`
	Items   []Item    `json:"items"`
	Carried []Carried `json:"carried"`
	Devices []Device  `json:"devices"`
	Weather string    `json:"weather"`
}

// Today assembles the panel. A stale clock in the panel is impossible: the
// service reads the same clock the board and the page do.
func (p *Panel) Today(ctx context.Context) (TodayView, error) {
	svc := p.h.srv.Service()
	view, err := svc.Today(ctx, server.Hint{})
	if err != nil {
		return TodayView{}, err
	}
	out := TodayView{
		Date:    view.Day.Label,
		Weekday: view.Day.Weekday,
		Lunar:   view.Day.Lunar.Label,
		Total:   view.Total,
		Done:    view.Done,
		Weather: view.WeatherLine,
	}
	for _, it := range view.Items {
		out.Items = append(out.Items, panelItem(it))
	}
	for _, c := range view.Carried {
		out.Carried = append(out.Carried, Carried{ID: c.ID, Title: c.Title, Day: c.DayLabel})
	}
	devices, err := p.h.srv.Devices(ctx)
	if err != nil {
		return TodayView{}, err
	}
	for _, d := range devices {
		out.Devices = append(out.Devices, deviceRow(d))
	}
	return out, nil
}

func panelItem(it store.Item) Item {
	out := Item{
		ID:      it.ID,
		Title:   it.Title,
		Done:    it.Done(),
		Pinned:  it.Pinned,
		Carried: it.CarriedFrom != "",
	}
	if it.StartsAt != nil && !it.AllDay {
		out.Time = it.StartsAt.Format("15:04")
	}
	return out
}

func deviceRow(d server.HostDevice) Device {
	out := Device{
		ID:      d.ID,
		Name:    d.Name,
		Online:  d.Online,
		Battery: batteryLabel(d.BatteryP),
		Climate: climateLabel(d.TempC, d.Humidity),
		Since:   sinceLabel(d.Online, d.LastSeen),
	}
	return out
}

func batteryLabel(pct int) string {
	if pct < 0 {
		return ""
	}
	return fmt.Sprintf("%d%%", pct)
}

func climateLabel(temp, humidity float64) string {
	switch {
	case temp == 0 && humidity == 0:
		return ""
	case humidity == 0:
		return fmt.Sprintf("%.0f°", temp)
	default:
		return fmt.Sprintf("%.0f° %.0f%%", temp, humidity)
	}
}

func sinceLabel(online bool, lastSeen *time.Time) string {
	if online {
		return "在线"
	}
	if lastSeen == nil {
		return "从未上线"
	}
	return "离线 " + humanSince(time.Since(*lastSeen))
}

// SetDone ticks one row. It writes through Server.SetItemDone, which is the
// same path the tray menu uses: the board is asked for a fresh frame and the
// op log stays out of it (ops only dedupe device acknowledgements).
func (p *Panel) SetDone(ctx context.Context, id string, done bool) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("缺事项 id")
	}
	return p.h.srv.SetItemDone(ctx, id, done)
}

// Add stores one line of natural language, exactly like the quick-add box on
// the page: the parser is the only difference between "明天 10:30 站会" and a
// title with a date.
func (p *Panel) Add(ctx context.Context, text string) (Item, error) {
	if strings.TrimSpace(text) == "" {
		return Item{}, fmt.Errorf("先写点什么")
	}
	svc := p.h.srv.Service()
	it, err := svc.CreateItem(ctx, server.ItemInput{Raw: text})
	if err != nil {
		return Item{}, err
	}
	return panelItem(it), nil
}

// Parse previews what the parser understood, for the tags under the input.
func (p *Panel) Parse(ctx context.Context, text string) ([]string, error) {
	svc := p.h.srv.Service()
	q, err := svc.Parse(ctx, text)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, 3)
	if q.Date != "" {
		if d, err := time.ParseInLocation(store.DateLayout, q.Date, time.Local); err == nil {
			out = append(out, d.Format("1月2日"))
		}
	}
	if q.Time != "" {
		out = append(out, q.Time)
	}
	if q.RRule != "" {
		out = append(out, "重复")
	}
	return out, nil
}

// Tags is Parse with the verbatim slices: the panel draws one chip per tag and
// removes a chip by cutting its Text out of the draft, so the label (which is
// normalized) is not enough.
func (p *Panel) Tags(ctx context.Context, text string) ([]Tag, error) {
	q, err := p.h.srv.Service().Parse(ctx, text)
	if err != nil {
		return nil, err
	}
	out := make([]Tag, 0, len(q.Fragments))
	for _, f := range q.Fragments {
		label := f.Text
		switch f.Kind {
		case "date":
			if d, err := time.ParseInLocation(store.DateLayout, q.Date, time.Local); err == nil {
				label = d.Format("1月2日")
			}
		case "time":
			label = q.Time
		case "repeat":
			label = repeatLabel(q.RRule)
		}
		out = append(out, Tag{Kind: f.Kind, Label: label, Text: f.Text})
	}
	return out, nil
}

// repeatLabel names a rule in the fewest words the panel can show.
func repeatLabel(expr string) string {
	switch {
	case strings.HasPrefix(expr, "FREQ=DAILY"):
		return "每天"
	case strings.HasPrefix(expr, "FREQ=WEEKLY"):
		return "每周"
	case strings.HasPrefix(expr, "FREQ=MONTHLY"):
		return "每月"
	default:
		return "重复"
	}
}

// PreviewSound asks one board for its test tone. The flag lives in memory
// until the board's next poll, so pressing twice before it polls is harmless.
func (p *Panel) PreviewSound(ctx context.Context, deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("缺设备 id")
	}
	p.h.srv.RequestPreview(deviceID)
	p.h.srv.ForceRefresh(deviceID)
	return nil
}

// RefreshDevice forces one board to rebuild its frame on the next poll, which
// is what "立即刷新" under a device means everywhere else.
func (p *Panel) RefreshDevice(ctx context.Context, deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("缺设备 id")
	}
	p.h.srv.ForceRefresh(deviceID)
	return nil
}

// RefreshAll is the panel's refresh button: every board rebuilds on its next
// poll, which is what "refresh" means in the tray and on the page alike.
func (p *Panel) RefreshAll(ctx context.Context) error {
	if p == nil || p.h == nil {
		return errNoHost
	}
	p.h.srv.ForceFullRefresh()
	p.h.kick()
	return nil
}

// Carry moves leftovers to today: one by id, or every leftover with all.
// The result is the number moved; skipped leftovers carry their reason.
func (p *Panel) Carry(ctx context.Context, ids []string, all bool) (CarryResult, error) {
	res, err := p.h.srv.Service().Carry(ctx, ids, all)
	if err != nil {
		return CarryResult{}, err
	}
	return CarryResult{Moved: res.Moved, Skipped: res.Skipped}, nil
}

// CarryResult is what a carry moved and what it refused, with a reason.
type CarryResult struct {
	Moved   int               `json:"moved"`
	Skipped map[string]string `json:"skipped"`
}

// Settings returns the stored values with secrets masked, the same map the
// settings page shows. Update goes through Server so the tray menu rebuilds
// when a title-relevant key changes.
func (p *Panel) Settings(ctx context.Context) (map[string]string, error) {
	return p.h.srv.Service().Settings(ctx)
}

// UpdateSettings writes a diff. Empty values delete nothing: the page sends
// only what changed, and PutSettings keeps the rest.
//
// The rules live on the server side (PatchSettings), not here: the same
// settings are reachable from the browser's PATCH, and a binding that only
// checked IsKnownSetting would let the panel store a value the web route
// refuses — worst of all an auth_ row.
func (p *Panel) UpdateSettings(ctx context.Context, diff map[string]string) error {
	if len(diff) == 0 {
		return nil
	}
	_, err := p.h.srv.PatchSettings(ctx, diff)
	return err
}

// Version is what the About pane prints.
func (p *Panel) Version() Config {
	return Config{Version: Version, Dev: p.h.dev}
}

// Prefs is the small slice of settings the shell needs before it draws:
// the theme, the color-blind switch and the language of the day header. It is
// separate from Settings so the panel can paint without waiting for the whole
// settings snapshot.
func (p *Panel) Prefs(ctx context.Context) (Prefs, error) {
	st, err := p.h.srv.Service().Settings(ctx)
	if err != nil {
		return Prefs{}, err
	}
	return Prefs{
		Theme:      st["theme"],
		ColorBlind: st["color_blind"] == "true",
		Dev:        p.h.dev,
		Version:    Version,
	}, nil
}

// Prefs mirrors the settings the shell reads on every open.
type Prefs struct {
	Theme      string `json:"theme"`
	ColorBlind bool   `json:"color_blind"`
	Dev        bool   `json:"dev"`
	Version    string `json:"version"`
}

// OpenSettings opens the settings window on one tab: "", "devices",
// "display", "network", "data" or "about".
func (p *Panel) OpenSettings(ctx context.Context, tab string) error {
	p.h.openSettings(tab)
	return nil
}

// OpenPage opens a page of the web UI in the default browser. The panel's
// "打开网页" does not embed a WebView: the page is the same one a phone gets,
// and mygo's own page already has the bindings.
func (p *Panel) OpenPage(ctx context.Context, path string) error {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	p.h.open(path)
	return nil
}
