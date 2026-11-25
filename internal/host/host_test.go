package host

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestDeepLinkTarget(t *testing.T) {
	const base = "http://127.0.0.1:8787"
	cases := map[string]string{
		"":                   base + "/",
		"today://":           base + "/",
		"today://app":        base + "/",
		"today://settings":   base + "/settings",
		"today://items":      base + "/search",
		"today://item/abc":   base + "/?item=abc",
		"today://item/a/b":   base + "/search",
		"today://unknown":    base + "/",
		"https://example.co": "https://example.co",
		"garbage":            base + "/",
	}
	for raw, want := range cases {
		if got := deepLinkTarget(raw, base); got != want {
			t.Fatalf("deepLinkTarget(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestWebAddr(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:8787":     "127.0.0.1:8787",
		":8787":            "127.0.0.1:8787",
		"[::]:8787":        "127.0.0.1:8787",
		"127.0.0.1":        "127.0.0.1",
		"192.168.1.2:8787": "192.168.1.2:8787",
	}
	for in, want := range cases {
		if got := webAddr(in); got != want {
			t.Fatalf("webAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPortOf(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:8787":  "8787",
		"[::]:8787":     "8787",
		"127.0.0.1:0":   "",
		"127.0.0.1":     "",
		"unix:/tmp/x":   "",
		"192.168.1:900": "900",
	}
	for in, want := range cases {
		if got := portOf(in); got != want {
			t.Fatalf("portOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// A stranger can hold 127.0.0.1:8787 while Today holds the wildcard address;
// the probe must skip the stranger and keep the server that answers with our
// instance id.
func TestProbeBaseSkipsForeignListener(t *testing.T) {
	stranger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer stranger.Close()

	ours := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Today-Instance", "abc123")
		_, _ = w.Write([]byte("ok"))
	}))
	defer ours.Close()

	idx := strings.LastIndex(ours.URL, ":")
	addr := "0.0.0.0" + ours.URL[idx:]
	base, ok := probeBase(addr, "abc123")
	if !ok {
		t.Fatalf("probeBase(%q) found no server", addr)
	}
	if base != "http://"+ours.Listener.Addr().String() {
		t.Fatalf("probeBase(%q) = %q, want our server", addr, base)
	}
	if _, ok := probeBase(addr, "other"); ok {
		t.Fatal("probeBase accepted a server with a different instance id")
	}
}

// A plain HTTP listener without the marker is not Today: probing must fail
// even though it answers 200.
func TestProbeBaseRequiresMarker(t *testing.T) {
	stranger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer stranger.Close()

	idx := strings.LastIndex(stranger.URL, ":")
	addr := "0.0.0.0" + stranger.URL[idx:]
	if _, ok := probeBase(addr, ""); ok {
		t.Fatal("probeBase trusted a listener that sends no instance id")
	}
}

// The quit hook runs again when the OS ends the session, so the cleanup has
// to be safe to call twice — and it must stop the background loop first, so
// nothing reads the store after it closed.
func TestCloseRunsCleanupOnce(t *testing.T) {
	h := New(nil, true, nil)
	released, stopped := 0, 0
	h.release = func() { released++ }
	h.shutdown = func() error { stopped++; return nil }

	loopDone := make(chan struct{})
	go func() {
		h.loop()
		close(loopDone)
	}()

	h.close("quit")
	h.close("session end")

	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("loop kept running after close")
	}
	if released != 1 || stopped != 1 {
		t.Fatalf("cleanup ran %d/%d times, want once each", released, stopped)
	}
}

// A shutdown error is reported but must not stop the release from happening:
// the process is going away either way.
func TestCloseReportsShutdownError(t *testing.T) {
	h := New(nil, true, nil)
	released := false
	h.release = func() { released = true }
	h.shutdown = func() error { return errors.New("boom") }

	h.close("quit")
	if !released {
		t.Fatal("release did not run before the failing shutdown")
	}
}

func TestCloseWithoutCleanupStillMarksQuitting(t *testing.T) {
	h := New(nil, true, nil)
	h.close("quit")
	h.close("session end")
	select {
	case <-h.done:
	default:
		t.Fatal("cached panel would prevent quitting without cleanup hooks")
	}
}

func TestPanelDismissRetainsContentButAllowsQuit(t *testing.T) {
	h := New(nil, true, nil)
	h.panelWanted = true
	dismiss := &mygo.CloseEvent{}
	h.onPanelClose(dismiss)
	if !dismiss.DefaultPrevented() || h.panelWanted {
		t.Fatal("dismiss should retain the WebView and cancel a pending open")
	}
	h.close("quit")
	quit := &mygo.CloseEvent{}
	h.onPanelClose(quit)
	if quit.DefaultPrevented() {
		t.Fatal("cached panel blocked app quit")
	}
}

func TestLoginSettingWithTrayDoesNotDeadlock(t *testing.T) {
	h := New(nil, true, nil)
	h.settings = map[string]string{"open_at_login": "false"}
	h.tray = &mygo.Tray{}
	changed := make(chan bool, 1)
	go func() {
		changed <- h.setSettings(map[string]string{"open_at_login": "true"})
	}()
	select {
	case got := <-changed:
		if !got || h.setting("open_at_login") != "true" {
			t.Fatal("login setting was not applied")
		}
	case <-time.After(time.Second):
		t.Fatal("updating settings with an existing tray deadlocked")
	}
}

func TestTrayTitleIsOptional(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings map[string]string
		want     string
	}{
		{"default", nil, "icon"},
		{"hidden", map[string]string{"tray_mode": "icon"}, "icon"},
		{"progress", map[string]string{"tray_mode": "progress"}, "progress"},
		{"next", map[string]string{"tray_mode": "next"}, "next"},
		{"legacy shown", map[string]string{"tray_title": "true"}, "progress"},
		{"legacy hidden", map[string]string{"tray_title": "false"}, "icon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(nil, true, nil)
			h.settings = tc.settings
			if got := h.trayMode(); got != tc.want {
				t.Fatalf("trayMode = %q, want %q", got, tc.want)
			}
			if tc.want == "icon" && h.title() != "" {
				t.Fatal("icon-only mode still shows a title")
			}
		})
	}
}
