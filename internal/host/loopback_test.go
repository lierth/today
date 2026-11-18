package host

import (
	"fmt"
	"net"
	"net/http"
	"testing"
)

// This reproduces the real conflict found on this machine: Today holds the
// wildcard address for a port while an unrelated program holds 127.0.0.1 on
// that same port. The kernel delivers loopback traffic to the specific
// listener, so probing 127.0.0.1 alone would return the stranger's answer.
func TestProbeBasePrefersOurWildcardOverLoopbackSquatter(t *testing.T) {
	wildcard, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Skipf("wildcard listener unavailable: %v", err)
	}
	defer wildcard.Close()
	port := wildcard.Addr().(*net.TCPAddr).Port

	squatter, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("loopback listener on the same port unavailable: %v", err)
	}
	defer squatter.Close()

	ours := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Today-Instance", "ours")
		_, _ = w.Write([]byte("ok"))
	})}
	go func() { _ = ours.Serve(wildcard) }()
	defer ours.Close()

	stranger := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"service":"headroom"}`))
	})}
	go func() { _ = stranger.Serve(squatter) }()
	defer stranger.Close()

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	if got, err := http.Get("http://" + addr + "/healthz"); err == nil {
		_ = got.Body.Close()
	}

	base, ok := probeBase(addr, "ours")
	if !ok {
		t.Fatalf("probeBase(%q) skipped our wildcard listener", addr)
	}
	if base == fmt.Sprintf("http://127.0.0.1:%d", port) {
		t.Fatalf("probeBase(%q) chose the squatter at %s", addr, base)
	}
	if resp, err := http.Get(base + "/healthz"); err == nil {
		id := resp.Header.Get("X-Today-Instance")
		_ = resp.Body.Close()
		if id != "ours" {
			t.Fatalf("%s answered with instance %q, want ours", base, id)
		}
	} else {
		t.Fatalf("probing %s: %v", base, err)
	}
}
