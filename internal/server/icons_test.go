package server

import (
	"bytes"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A browser asks for the tab icon before anyone has typed the password. Behind
// requireSession these three would answer 302, which the browser renders as a
// broken image, and the login page would be the only page in the app without a
// mark.
func TestLoginIconsAreServedWithoutASession(t *testing.T) {
	e := newTestEnv(t)
	for _, name := range loginIcons {
		res := e.get("/" + name)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET /%s: %d, want 200", name, res.StatusCode)
		}
		if got := res.Header.Get("Content-Type"); got != "image/png" {
			t.Fatalf("GET /%s: content type %q, want image/png", name, got)
		}
		body := e.body("/" + name)
		if !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
			t.Fatalf("GET /%s: body is not a PNG (%d bytes)", name, len(body))
		}
	}
}

// The name comes from the URL, so a traversal attempt must not read outside
// the bundle.
func TestIconRejectsPathsOutsideTheBundle(t *testing.T) {
	e := newTestEnv(t)
	for _, p := range []string{"/..%2f..%2fgo.mod", "/favicon-light.png/../index.html"} {
		body := e.body(p)
		if bytes.Contains(body, []byte("module today")) {
			t.Fatalf("%s leaked go.mod", p)
		}
	}
}

// The page has to name the icons it wants, or the browser never asks.
func TestLoginPageNamesItsIcons(t *testing.T) {
	e := newTestEnv(t)
	body := string(e.body("/login"))
	for _, want := range []string{
		`<link rel="icon" href="/favicon-light.png"`,
		`<link rel="icon" href="/favicon-dark.png"`,
		`<link rel="apple-touch-icon" href="/apple-touch-icon.png">`,
		`<img class="mark" src="/favicon-light.png"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("login page is missing %q", want)
		}
	}
}

// iOS fills transparent pixels with black, so the touch icon must be opaque.
// Both copies are committed — the drawn source and the one the bundle embeds —
// so the assertion holds without a local build.
func TestAppleTouchIconIsOpaque(t *testing.T) {
	for _, p := range []string{
		filepath.Join("..", "..", "resources", "apple-touch-icon.png"),
		filepath.Join("webapp", "dist", "apple-touch-icon.png"),
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: decode: %v", p, err)
		}
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a != 0xffff {
					t.Fatalf("%s: pixel %d,%d is not opaque; iOS would paint it black", p, x, y)
				}
			}
		}
	}
}
