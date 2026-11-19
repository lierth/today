package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BaseURL is the only endpoint used. Tests point it at an httptest server.
var BaseURL = "https://api.weatherapi.com/v1/forecast.json"

// DefaultTimeout bounds one refresh; the frame path must never wait on it.
const DefaultTimeout = 5 * time.Second

// CacheTTL is how long one `q` result is reused. One board refreshes every 30
// minutes, i.e. 48 calls a day against a 100,000/month free allowance.
const CacheTTL = 30 * time.Minute

// ErrNoKey means the person has not configured WeatherAPI yet.
var ErrNoKey = errors.New("weather: no API key")

// ErrBadKey is returned for 401 and 403 so the settings page can say "key
// invalid" instead of "network problem".
var ErrBadKey = errors.New("weather: API key rejected")

// APIError carries a non-2xx answer for the log; it never reaches the frame.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	// The body is capped: an upstream error page can be kilobytes of HTML and
	// must not land in the log or the settings page verbatim.
	body := strings.TrimSpace(e.Body)
	if n := len([]rune(body)); n > 120 {
		body = string([]rune(body)[:120]) + "…"
	}
	return fmt.Sprintf("weather: HTTP %d %s", e.Status, body)
}

// IsBadKey reports whether err means the key itself is wrong.
func IsBadKey(err error) bool {
	var api *APIError
	if errors.As(err, &api) {
		return api.Status == http.StatusUnauthorized || api.Status == http.StatusForbidden
	}
	return errors.Is(err, ErrBadKey)
}

type response struct {
	Location struct {
		Name   string `json:"name"`
		Region string `json:"region"`
	} `json:"location"`
	Current struct {
		TempC     float64 `json:"temp_c"`
		Condition struct {
			Text string `json:"text"`
			Code int    `json:"code"`
		} `json:"condition"`
	} `json:"current"`
	Forecast struct {
		Forecastday []struct {
			Day struct {
				MaxC float64 `json:"maxtemp_c"`
				MinC float64 `json:"mintemp_c"`
			} `json:"day"`
		} `json:"forecastday"`
	} `json:"forecast"`
}

// Parse reads one forecast.json body. It is exported for the tests and for the
// CLI, and it rejects a body that lacks the fields the panel draws, so a
// schema change surfaces as an error instead of a row of zeros.
func Parse(body []byte) (Report, error) {
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return Report{}, fmt.Errorf("weather: parse: %w", err)
	}
	if len(r.Forecast.Forecastday) == 0 {
		return Report{}, errors.New("weather: no forecastday")
	}
	if r.Location.Name == "" {
		return Report{}, errors.New("weather: no location name")
	}
	day := r.Forecast.Forecastday[0]
	return Report{
		City:   r.Location.Name,
		Region: r.Location.Region,
		Text:   r.Current.Condition.Text,
		Code:   r.Current.Condition.Code,
		TempC:  r.Current.TempC,
		MaxC:   day.Day.MaxC,
		MinC:   day.Day.MinC,
	}, nil
}

// Client fetches and caches forecasts. One client per server process; it is
// safe for concurrent use because every frame request reads it.
type Client struct {
	HTTP *http.Client
	Now  func() time.Time // tests pin the clock

	mu      sync.Mutex
	cache   map[string]entry
	lastErr error
	lastAt  time.Time
}

type entry struct {
	report   Report
	at       time.Time
	inflight bool
}

// New returns a client with a bounded HTTP timeout.
func New() *Client {
	return &Client{
		HTTP:  &http.Client{Timeout: DefaultTimeout},
		Now:   time.Now,
		cache: make(map[string]entry),
	}
}

// Report returns the forecast for q for drawing.
//
// It never waits for the network. A fresh cache entry is returned directly; a
// stale entry is returned as-is while a background refresh runs (stale-while-
// revalidate); with no entry at all the zero Report comes back and a refresh
// starts. The returned error is advisory: it is non-nil only when there is no
// usable value, so callers may draw whatever they got. A stale value is never
// paired with an error.
func (c *Client) Report(_ context.Context, key, q string) (Report, error) {
	if key == "" {
		return Report{}, ErrNoKey
	}
	if q == "" {
		return Report{}, errors.New("weather: empty q")
	}
	now := c.Now()
	c.mu.Lock()
	e, ok := c.cache[q]
	if ok && now.Sub(e.at) < CacheTTL {
		c.mu.Unlock()
		return e.report, nil
	}
	if e.inflight || key == "" {
		c.mu.Unlock()
		return e.report, nil // stale value (or zero) while a refresh runs
	}
	e.inflight = true
	c.cache[q] = e
	c.mu.Unlock()

	// The refresh must not inherit the frame request's deadline: the client
	// has already been answered.
	go c.refresh(key, q)
	if ok {
		return e.report, nil
	}
	return Report{}, nil
}

// refresh fetches one q in the background, with its own timeout, and clears
// the in-flight marker whatever happens.
func (c *Client) refresh(key, q string) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	now := c.Now()
	report, err := c.fetch(ctx, key, q, now)
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.cache[q]
	e.inflight = false
	if err != nil {
		c.lastErr = err
		c.lastAt = now
		c.cache[q] = e
		return
	}
	e.report = report
	e.at = now
	c.cache[q] = e
	c.lastErr = nil
}

// WaitRefresh blocks until no refresh for q is in flight. It exists for tests
// and for the settings page's "refresh now"; production frame paths use
// Report, which never blocks.
func (c *Client) WaitRefresh(q string) {
	for {
		c.mu.Lock()
		busy := c.cache[q].inflight
		c.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// RefreshNow forces a synchronous fetch (settings page button). It returns the
// report and any error.
func (c *Client) RefreshNow(ctx context.Context, key, q string) (Report, error) {
	if key == "" {
		return Report{}, ErrNoKey
	}
	now := c.Now()
	report, err := c.fetch(ctx, key, q, now)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.lastErr = err
		c.lastAt = now
		return c.cache[q].report, err
	}
	c.cache[q] = entry{report: report, at: now}
	c.lastErr = nil
	return report, nil
}

// Cached returns the last value for q without touching the network, and
// whether it is still within CacheTTL. The settings page uses it so opening
// the page never waits on an API call.
func (c *Client) Cached(q string, now time.Time) (Report, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[q]
	if !ok {
		return Report{}, false, false
	}
	return e.report, true, now.Sub(e.at) < CacheTTL
}

// LastError is what the settings page shows as "上次失败：…".
func (c *Client) LastError() (error, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr, c.lastAt
}

func (c *Client) fetch(ctx context.Context, key, q string, now time.Time) (Report, error) {
	params := url.Values{}
	params.Set("key", key)
	params.Set("q", q)
	params.Set("days", "1")
	params.Set("lang", "zh")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+"?"+params.Encode(), nil)
	if err != nil {
		return Report{}, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Report{}, redactURLError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Report{}, fmt.Errorf("weather: read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Report{}, &APIError{Status: resp.StatusCode, Body: string(body)}
	}
	report, err := Parse(body)
	if err != nil {
		return Report{}, err
	}
	report.Fetched = now
	return report, nil
}

// RoundC formats a Celsius value the way the board draws it: whole degrees,
// no decimal point.
func RoundC(v float64) string { return strconv.Itoa(int(v + 0.5*sign(v))) }

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// redactURLError strips the request URL from transport errors. The key travels
// as a query parameter, and *url.Error.Error() prints the whole URL, so
// wrapping it verbatim would write the key into the log and the settings page.
// Only the operation and the underlying cause are kept.
func redactURLError(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return fmt.Errorf("weather: %s: %w", uerr.Op, uerr.Err)
	}
	return fmt.Errorf("weather: %w", err)
}
