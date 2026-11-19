package host

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestMapLocalNetworkError(t *testing.T) {
	dialTimeout := &net.DNSError{Err: "i/o timeout", Name: "10.0.0.2", IsTimeout: true}
	cases := []struct {
		name string
		err  error
		want LocalNetworkState
	}{
		{"no error", nil, LocalNetworkOK},
		{"refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, LocalNetworkOK},
		{"reset", syscall.ECONNRESET, LocalNetworkOK},
		{"host unreachable", syscall.EHOSTUNREACH, LocalNetworkDenied},
		{"host down", syscall.EHOSTDOWN, LocalNetworkDenied},
		{"permission", syscall.EACCES, LocalNetworkDenied},
		{"operation not permitted", syscall.EPERM, LocalNetworkDenied},
		{"wrapped host unreachable", fmt.Errorf("dial: %w", syscall.EHOSTUNREACH), LocalNetworkDenied},
		{"network unreachable", syscall.ENETUNREACH, LocalNetworkUnknown},
		{"network down", syscall.ENETDOWN, LocalNetworkUnknown},
		{"address unavailable", syscall.EADDRNOTAVAIL, LocalNetworkUnknown},
		{"deadline", os.ErrDeadlineExceeded, LocalNetworkUnknown},
		{"dns timeout", dialTimeout, LocalNetworkUnknown},
		{"closed socket", net.ErrClosed, LocalNetworkUnknown},
		{"timeout error", timeoutErr{}, LocalNetworkUnknown},
	}
	for _, c := range cases {
		if got := MapLocalNetworkError(c.err); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestLocalNetworkNeedsAttention(t *testing.T) {
	cases := map[LocalNetworkState]bool{
		LocalNetworkOK:      false,
		LocalNetworkDenied:  true,
		LocalNetworkUnknown: true,
		"":                  true, // before the first probe: unknown
	}
	for state, want := range cases {
		if got := LocalNetworkNeedsAttention(state); got != want {
			t.Errorf("%q: got %v, want %v", state, got, want)
		}
	}
}

func TestLocalNetworkExplanation(t *testing.T) {
	if s := LocalNetworkExplanation(LocalNetworkOK, "10.0.0.2"); s != "" {
		t.Errorf("granted must be silent, got %q", s)
	}
	if s := LocalNetworkExplanation(LocalNetworkDenied, "10.0.0.2"); s == "" {
		t.Error("denied needs an explanation")
	}
	if s := LocalNetworkExplanation(LocalNetworkUnknown, ""); s == "" {
		t.Error("unknown needs an explanation")
	}
}

// The probe must not claim success when there is no LAN to test, and the
// opt-out must work without touching the network (CI, servers).
func TestTriggerLocalNetworkOptOut(t *testing.T) {
	t.Setenv("TODAY_NO_LOCAL_NETWORK_PROBE", "1")
	state, detail := TriggerLocalNetwork("8787", time.Millisecond)
	if state != LocalNetworkOK || detail != "probe disabled" {
		t.Fatalf("got %q %q", state, detail)
	}
}

func TestAdhocEnvironmentOverride(t *testing.T) {
	t.Setenv("TODAY_BUNDLE_ADHOC", "1")
	if !AdhocSigned() {
		t.Fatal("TODAY_BUNDLE_ADHOC=1 must report ad-hoc")
	}
}

func TestLocalNetworkURLIsTheSecurityPane(t *testing.T) {
	want := "x-apple.systempreferences:com.apple.preference.security"
	if localNetworkURL != want {
		t.Fatalf("got %q", localNetworkURL)
	}
}

func TestErrorsAsKeepsWrapping(t *testing.T) {
	err := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", syscall.EHOSTUNREACH))
	if !errors.As(err, new(syscall.Errno)) {
		t.Fatal("test setup: errno must be reachable through wrapping")
	}
}
