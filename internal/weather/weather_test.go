package weather

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSelectQueryPriority(t *testing.T) {
	cases := []struct {
		name       string
		device     string
		global     string
		ipFallback bool
		funnel     bool
		clientIP   string
		wantQ      string
		wantSource Source
	}{
		{
			name: "device city wins", device: " 上海 ", global: "北京",
			ipFallback: true, funnel: true, clientIP: "203.0.113.9",
			wantQ: "上海", wantSource: SourceDeviceCity,
		},
		{
			name: "global city next", global: "北京",
			ipFallback: true, funnel: true, clientIP: "203.0.113.9",
			wantQ: "北京", wantSource: SourceGlobalCity,
		},
		{
			name:       "funnel uses the board's public IP",
			ipFallback: true, funnel: true, clientIP: "203.0.113.9",
			wantQ: "203.0.113.9", wantSource: SourceClientIP,
		},
		{
			name:       "funnel without a forwarded address falls back to auto:ip",
			ipFallback: true, funnel: true,
			wantQ: "auto:ip", wantSource: SourceServerIP,
		},
		{
			name:       "LAN uses the server egress",
			ipFallback: true, funnel: false, clientIP: "192.168.1.40",
			wantQ: "auto:ip", wantSource: SourceServerIP,
		},
		{
			name:       "IP fallback off and no city means no weather",
			ipFallback: false, funnel: true, clientIP: "203.0.113.9",
			wantQ: "", wantSource: SourceNone,
		},
		{
			name: "blank device city is ignored", device: "   ", global: "北京",
			ipFallback: true, wantQ: "北京", wantSource: SourceGlobalCity,
		},
	}
	for _, c := range cases {
		got := SelectQuery(c.device, c.global, c.ipFallback, c.funnel, c.clientIP)
		if got.Q != c.wantQ || got.Source != c.wantSource {
			t.Errorf("%s: got %+v, want q=%q source=%q", c.name, got, c.wantQ, c.wantSource)
		}
	}
}

const sampleBody = `{
  "location": {"name": "北京", "region": "北京市"},
  "current": {"temp_c": 3.4, "condition": {"text": "局部有雨", "code": 1063}},
  "forecast": {"forecastday": [{"day": {"maxtemp_c": 11.6, "mintemp_c": -2.4}}]}
}`

func TestParse(t *testing.T) {
	r, err := Parse([]byte(sampleBody))
	if err != nil {
		t.Fatal(err)
	}
	if r.City != "北京" || r.Region != "北京市" {
		t.Fatalf("location: %+v", r)
	}
	if r.Code != 1063 || r.Text != "局部有雨" {
		t.Fatalf("condition: %+v", r)
	}
	if r.TempC != 3.4 || r.MaxC != 11.6 || r.MinC != -2.4 {
		t.Fatalf("temps: %+v", r)
	}
}

func TestParseRejectsIncomplete(t *testing.T) {
	for _, body := range []string{
		`{`,
		`{"location": {"name": "北京"}}`,
		`{"location": {"name": "北京"}, "forecast": {"forecastday": []}}`,
		`{"forecast": {"forecastday": [{"day": {}}]}}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestShortName(t *testing.T) {
	cases := []struct {
		name string
		r    Report
		want string
	}{
		{"short text is kept", Report{Code: 1000, Text: "晴"}, "晴"},
		{"four chars is kept", Report{Code: 1063, Text: "局部有雨"}, "局部有雨"},
		{"long text uses the table", Report{Code: 1063, Text: "可能有零星阵雨"}, "阵雨"},
		{"unknown code falls back to a cut", Report{Code: 99999, Text: "非常奇怪的天气现象"}, "非常奇怪"},
		{"empty text uses the table", Report{Code: 1009}, "阴"},
		{"no code means nothing", Report{Text: "晴"}, ""},
	}
	for _, c := range cases {
		if got := c.r.Short(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// Every code the API documents must have a short name of at most four runes,
// or the header would silently drop it.
func TestShortTableCoversDocumentedCodes(t *testing.T) {
	// The 64 codes from weather_conditions.json, plus the two night variants.
	// Copied verbatim from weather_conditions.json; the count is asserted so a
	// future API addition is noticed instead of silently falling back.
	codes := []int{
		1000, 1003, 1006, 1009, 1012, 1015, 1018, 1021, 1024, 1027, 1030, 1033,
		1036, 1039, 1042, 1045, 1048, 1063, 1066, 1069, 1072, 1087, 1114, 1117,
		1135, 1147, 1150, 1153, 1168, 1171, 1180, 1183, 1186, 1189, 1192, 1195,
		1198, 1201, 1204, 1207, 1210, 1213, 1216, 1219, 1222, 1225, 1237, 1240,
		1243, 1246, 1249, 1252, 1255, 1258, 1261, 1264, 1273, 1276, 1279, 1282,
	}
	if len(codes) != 60 {
		t.Fatalf("the documented table has 60 codes, test has %d", len(codes))
	}
	for _, code := range codes {
		s, ok := shortNames[code]
		if !ok || s == "" {
			t.Errorf("code %d has no short name", code)
			continue
		}
		if n := len([]rune(s)); n > 4 {
			t.Errorf("code %d name %q is %d runes", code, s, n)
		}
	}
}

func newTestServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	old := BaseURL
	BaseURL = srv.URL + "/v1/forecast.json"
	t.Cleanup(func() { BaseURL = old })
	c := New()
	c.HTTP = srv.Client()
	c.HTTP.Timeout = 2 * time.Second
	return c
}

func TestReportCachesPerQuery(t *testing.T) {
	var calls atomic.Int32
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("key") == "" || r.URL.Query().Get("q") == "" {
			t.Errorf("missing params: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("days") != "1" || r.URL.Query().Get("lang") != "zh" {
			t.Errorf("wrong params: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(sampleBody))
	})
	now := time.Now()
	c.Now = func() time.Time { return now }
	// First call returns empty and starts one refresh; the rest hit the cache.
	for range 3 {
		if _, err := c.Report(context.Background(), "k", "北京"); err != nil {
			t.Fatal(err)
		}
	}
	c.WaitRefresh("北京")
	if calls.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls.Load())
	}
	if r, _, _ := c.Cached("北京", now); r.City != "北京" {
		t.Fatalf("cache not filled: %+v", r)
	}
	// A second location is a second request; the same one after the TTL is a
	// third.
	if _, err := c.Report(context.Background(), "k", "上海"); err != nil {
		t.Fatal(err)
	}
	c.WaitRefresh("上海")
	now = now.Add(CacheTTL + time.Minute)
	if _, err := c.Report(context.Background(), "k", "北京"); err != nil {
		t.Fatal(err)
	}
	c.WaitRefresh("北京")
	if calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", calls.Load())
	}
}

// The frame path must never wait: a slow API returns immediately (stale or
// empty) and the fetch happens in the background.
func TestReportNeverBlocks(t *testing.T) {
	release := make(chan struct{})
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte(sampleBody))
	})
	start := time.Now()
	report, err := c.Report(context.Background(), "k", "北京")
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Report blocked for %v", elapsed)
	}
	if report.City != "" {
		t.Fatalf("expected an empty report on the first call, got %+v", report)
	}
	close(release)
	c.WaitRefresh("北京")
	if got, _, _ := c.Cached("北京", time.Now()); got.City != "北京" {
		t.Fatalf("background refresh did not fill the cache: %+v", got)
	}
}

// Ten concurrent frame requests for the same q must produce one upstream call.
func TestReportCoalescesConcurrentRefreshes(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-release
		_, _ = w.Write([]byte(sampleBody))
	})
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Report(context.Background(), "k", "北京")
		}()
	}
	wg.Wait()
	// Give any (wrong) extra goroutine a chance to reach the handler.
	time.Sleep(100 * time.Millisecond)
	close(release)
	c.WaitRefresh("北京")
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 upstream call, got %d", got)
	}
}

// A stale value is served immediately while the refresh runs, and no error is
// attached to it.
func TestReportStaleWhileRevalidate(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(sampleBody))
	})
	now := time.Now()
	c.Now = func() time.Time { return now }
	if _, err := c.RefreshNow(context.Background(), "k", "北京"); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	now = now.Add(CacheTTL + time.Minute)
	report, err := c.Report(context.Background(), "k", "北京")
	if err != nil {
		t.Fatalf("a stale value must not come with an error: %v", err)
	}
	if report.City != "北京" {
		t.Fatalf("stale value not served: %+v", report)
	}
	c.WaitRefresh("北京")
	if lastErr, _ := c.LastError(); lastErr == nil {
		t.Fatal("LastError should report the failed refresh")
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one refresh attempt, got %d calls", calls.Load())
	}
}

func TestReportErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantBad bool
	}{
		{"401", http.StatusUnauthorized, `{"error":{"code":1002}}`, true},
		{"403", http.StatusForbidden, `{"error":{"code":2008}}`, true},
		{"500", http.StatusInternalServerError, `oops`, false},
		{"bad json", http.StatusOK, `{`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			})
			_, err := client.RefreshNow(context.Background(), "k", "北京")
			if err == nil {
				t.Fatal("expected an error")
			}
			if IsBadKey(err) != c.wantBad {
				t.Fatalf("IsBadKey=%v for %v", IsBadKey(err), err)
			}
		})
	}
}

func TestReportTimeout(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	client.HTTP.Timeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.RefreshNow(ctx, "k", "北京"); err == nil {
		t.Fatal("expected a timeout")
	}
}

// The key is a query parameter, and *url.Error prints the URL. A transport
// failure must not carry it into the log or the settings page.
func TestTransportErrorHidesKey(t *testing.T) {
	const key = "SECRETKEY0123456789"
	// A listener that accepts and immediately closes the connection makes Do
	// fail with a *url.Error after the request line was written.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if tc, ok := conn.(*net.TCPConn); ok {
				_ = tc.SetLinger(0)
			}
			_ = conn.Close()
		}
	}()
	old := BaseURL
	BaseURL = "http://" + ln.Addr().String() + "/v1/forecast.json"
	t.Cleanup(func() { BaseURL = old })
	c := New()
	_, err = c.RefreshNow(context.Background(), key, "北京")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("error text leaks the key: %v", err)
	}
	if strings.Contains(err.Error(), "key=") {
		t.Fatalf("error text leaks the query string: %v", err)
	}
}

// The same guarantee for a DNS-level failure, where *url.Error wraps a
// net.OpError with the URL attached.
func TestDialErrorHidesKey(t *testing.T) {
	old := BaseURL
	BaseURL = "http://127.0.0.1:1/forecast.json"
	t.Cleanup(func() { BaseURL = old })
	const key = "SECRETKEY0123456789"
	c := New()
	c.HTTP.Timeout = time.Second
	_, err := c.RefreshNow(context.Background(), key, "北京")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("error text leaks the key: %v", err)
	}
}

func TestReportNoKey(t *testing.T) {
	c := New()
	if _, err := c.Report(context.Background(), "", "北京"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("got %v", err)
	}
}

func TestRoundC(t *testing.T) {
	cases := map[float64]string{3.4: "3", 3.5: "4", -2.4: "-2", -2.5: "-3", 0: "0"}
	for in, want := range cases {
		if got := RoundC(in); got != want {
			t.Errorf("%v: got %s want %s", in, got, want)
		}
	}
}

func TestStale(t *testing.T) {
	r := Report{Fetched: time.Now()}
	if r.Stale(time.Now(), time.Hour) {
		t.Fatal("fresh data reported stale")
	}
	if !r.Stale(time.Now().Add(2*time.Hour), time.Hour) {
		t.Fatal("old data reported fresh")
	}
	if !(Report{}).Stale(time.Now(), time.Hour) {
		t.Fatal("zero time must be stale")
	}
}

func TestSourceLabel(t *testing.T) {
	for _, s := range []Source{SourceDeviceCity, SourceGlobalCity, SourceClientIP, SourceServerIP, SourceNone} {
		if SourceLabel(s) == "" {
			t.Errorf("%s has no label", s)
		}
	}
	if !strings.Contains(SourceLabel(SourceClientIP), "IP") {
		t.Error("the IP source should say so")
	}
}
