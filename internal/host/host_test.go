package host

import "testing"

func TestDeepLinkTarget(t *testing.T) {
	const base = "http://127.0.0.1:8787"
	cases := map[string]string{
		"":                   base + "/app",
		"today://":           base + "/app",
		"today://app":        base + "/app",
		"today://settings":   base + "/settings",
		"today://items":      base + "/items",
		"today://item/abc":   base + "/items/abc",
		"today://unknown":    base + "/app",
		"https://example.co": "https://example.co",
		"garbage":            base + "/app",
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
