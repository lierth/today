package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"today/internal/frame"
	"today/internal/weather"
)

// deviceCity reads the per-device city from settings under a device-scoped key.
// It lives in settings rather than a devices column so a device rename or a
// database from an older version keeps working without a migration.
func (s *Server) deviceCity(ctx context.Context, deviceID string) string {
	if deviceID == "" {
		return ""
	}
	st, err := s.st.Settings(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(st["weather_city_"+deviceID])
}

// weatherQuery picks the q for this request (see weather.SelectQuery) and
// reports the funnel IP only when the request really came through Funnel.
func (s *Server) weatherQuery(ctx context.Context, r *http.Request, deviceID string) weather.Location {
	st, err := s.st.Settings(ctx)
	if err != nil {
		st = map[string]string{}
	}
	funnel := r != nil && isFunnelRequest(r)
	ip := ""
	if r != nil {
		ip = clientIP(r)
	}
	return weather.SelectQuery(
		s.deviceCity(ctx, deviceID),
		st["weather_city"],
		st["weather_ip_fallback"] != "false",
		funnel,
		ip,
	)
}

// headerWeather returns the weather drawn on the second header line. Any
// failure yields the zero value: the frame must render no matter what the
// network or the API does.
func (s *Server) headerWeather(ctx context.Context, r *http.Request, deviceID string) frame.Weather {
	st, err := s.st.Settings(ctx)
	if err != nil {
		return frame.Weather{}
	}
	key := strings.TrimSpace(st["weather_api_key"])
	if key == "" {
		return frame.Weather{}
	}
	loc := s.weatherQuery(ctx, r, deviceID)
	if loc.Q == "" {
		return frame.Weather{}
	}
	// Stale-while-revalidate: Report answers from cache (fresh or stale) and
	// kicks a background refresh, so a frame never waits on the network and a
	// burst of devices asking at once produces one upstream request.
	report, err := s.weather.Report(ctx, key, loc.Q)
	if err != nil && report.City == "" {
		s.log.Printf("server: weather: %v", err)
		return frame.Weather{}
	}
	return frame.Weather{
		City:  report.City,
		Text:  report.Short(),
		TempC: report.TempC,
		MaxC:  report.MaxC,
		MinC:  report.MinC,
	}
}

// weatherStatus is what the settings page shows under the key field.
type weatherStatus struct {
	KeySet   bool
	KeyBad   bool
	Masked   string
	City     string // last resolved location name
	Source   string // human label for where the location came from
	Detail   string
	Fetched  time.Time
	Disabled bool
}

// redactURL hides the password in a proxy URL. The settings page shows the
// address so a person can see which proxy is configured, but the password in
// it is a secret just like the weather key: anyone who can read the settings
// (a Funnel visitor with the password, a screenshot, a log) must not get it.
func redactURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "••••"
	}
	if u.User == nil {
		return raw
	}
	return u.Redacted()
}

// maskKey keeps the last four characters visible, the way API consoles do.
func maskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "••••"
	}
	return "••••" + key[len(key)-4:]
}

func (s *Server) weatherStatus(ctx context.Context) weatherStatus {
	st, err := s.st.Settings(ctx)
	if err != nil {
		return weatherStatus{}
	}
	key := strings.TrimSpace(st["weather_api_key"])
	status := weatherStatus{KeySet: key != "", Masked: maskKey(key)}
	lastErr, at := s.weather.LastError()
	status.Detail = ""
	if lastErr != nil {
		status.KeyBad = weather.IsBadKey(lastErr)
		status.Detail = lastErr.Error()
		_ = at
	}
	loc := weather.SelectQuery("", st["weather_city"], st["weather_ip_fallback"] != "false", false, "")
	status.Source = weather.SourceLabel(loc.Source)
	status.Disabled = loc.Q == ""
	if key != "" && loc.Q != "" {
		// Cache only: rendering the settings page must never wait on the
		// network, and the frame path refreshes the cache anyway.
		if report, ok, fresh := s.weather.Cached(loc.Q, time.Now()); ok {
			status.City = report.City
			status.Fetched = report.Fetched
			if fresh {
				status.Detail = ""
			}
		}
	}
	return status
}

// todayWeatherLine is the one-line forecast under the date on /app. It reuses
// the same selection and cache as the frame, so the page and the board never
// disagree.
func (s *Server) todayWeatherLine(ctx context.Context, r *http.Request) string {
	st, err := s.st.Settings(ctx)
	if err != nil {
		return ""
	}
	key := strings.TrimSpace(st["weather_api_key"])
	loc := s.weatherQuery(ctx, r, "")
	if key == "" || loc.Q == "" {
		return ""
	}
	report, err := s.weather.Report(ctx, key, loc.Q)
	if err != nil && report.City == "" {
		return ""
	}
	w := weather.Report{
		City:  report.City,
		Text:  report.Short(),
		TempC: report.TempC,
		MaxC:  report.MaxC,
		MinC:  report.MinC,
	}
	line := w.City
	if line == "" {
		line = "天气"
	}
	return line + " " + w.Text + " " + weather.RoundC(w.TempC) + "°"
}

// WeatherLine is the tray's one-line forecast, read from the cache only (the
// menu must never wait on the network). It returns "" when nothing is cached.
func (s *Server) WeatherLine(ctx context.Context) string {
	st, err := s.st.Settings(ctx)
	if err != nil {
		return ""
	}
	key := strings.TrimSpace(st["weather_api_key"])
	if key == "" {
		return ""
	}
	loc := s.weatherQuery(ctx, nil, "")
	if loc.Q == "" {
		return ""
	}
	report, ok, _ := s.weather.Cached(loc.Q, time.Now())
	if !ok {
		return ""
	}
	return report.City + " " + report.Short() + " " + weather.RoundC(report.TempC) + "°"
}
