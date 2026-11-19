package server

import (
	"context"
	"time"

	"today/internal/store"
)

// HostItem is one row the tray menu shows. It carries only what a menu needs:
// id, title, time, and whether it is done.
type HostItem struct {
	ID     string
	Title  string
	Done   bool
	At     *time.Time // start time for timed items; nil for all-day
	AllDay bool
}

// HostDevice is one device as the menu and the settings card show it.
type HostDevice struct {
	ID       string
	Name     string
	Online   bool
	LastSeen *time.Time
	Battery  int     // millivolts, as the board reports it
	BatteryP int     // percent, mapped like the panel widget; -1 unknown
	TempC    float64 // 0 when the board has no climate reading
	Humidity float64
	Firmware string
	City     string
}

// OnlineWindow is the freshness that counts as "online": the board reports
// every 60 s, so three misses in a row means it is not there.
const OnlineWindow = 3 * time.Minute

// TodayItems returns today's visible items in display order. The tray uses it
// to render the checkboxes without importing store.
func (s *Server) TodayItems(ctx context.Context) ([]HostItem, error) {
	day, _, err := s.Today(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.st.ItemsOn(ctx, day.Format(store.DateLayout))
	if err != nil {
		return nil, err
	}
	out := make([]HostItem, 0, len(items))
	for _, it := range items {
		if it.Status == store.StatusCancelled {
			continue
		}
		out = append(out, HostItem{
			ID: it.ID, Title: it.Title, Done: it.Done(),
			At: it.StartsAt, AllDay: it.AllDay,
		})
	}
	return out, nil
}

// SetItemDone toggles one item through the same store write the web page's
// checkbox uses, and asks every board for a fresh frame so the row shows up
// quickly. No ops row is written: ops exists to de-duplicate a device
// re-sending the same acknowledgement, and the menu has no such outbox. A
// device that had the old state simply gets the new frame and re-renders.
func (s *Server) SetItemDone(ctx context.Context, id string, done bool) error {
	status := store.StatusOpen
	if done {
		status = store.StatusDone
	}
	if err := s.st.SetStatus(ctx, id, status); err != nil {
		return err
	}
	s.ForceFullRefresh()
	return nil
}

// batteryPercent maps a cell voltage to 0..100, or -1 when the board reports
// no usable reading. The curve matches firmware/src/widgets.cpp and the web
// page; it lives here so the host does not have to import firmware numbers.
func batteryPercent(mv int) int {
	if mv < 2500 {
		return -1
	}
	points := []struct{ mv, pct int }{
		{3300, 0}, {3600, 10}, {3700, 30}, {3800, 55}, {3950, 75}, {4100, 95}, {4200, 100},
	}
	if mv <= points[0].mv {
		return 0
	}
	last := len(points) - 1
	if mv >= points[last].mv {
		return 100
	}
	for i := 1; i <= last; i++ {
		if mv <= points[i].mv {
			span := points[i].mv - points[i-1].mv
			dp := points[i].pct - points[i-1].pct
			return points[i-1].pct + (mv-points[i-1].mv)*dp/span
		}
	}
	return 100
}

// Devices returns the device list with the fields the menu shows.
func (s *Server) Devices(ctx context.Context) ([]HostDevice, error) {
	devices, err := s.st.Devices(ctx)
	if err != nil {
		return nil, err
	}
	st, _ := s.st.Settings(ctx)
	now := time.Now()
	out := make([]HostDevice, 0, len(devices))
	for _, d := range devices {
		if d.RevokedAt != nil {
			continue
		}
		online := d.LastSeenAt != nil && now.Sub(*d.LastSeenAt) <= OnlineWindow
		out = append(out, HostDevice{
			ID: d.ID, Name: d.Name, Online: online, LastSeen: d.LastSeenAt,
			Battery: d.BatteryMV, BatteryP: batteryPercent(d.BatteryMV),
			TempC: d.TempC, Humidity: d.Humidity,
			Firmware: st["firmware_version"], City: st["weather_city_"+d.ID],
		})
	}
	return out, nil
}
