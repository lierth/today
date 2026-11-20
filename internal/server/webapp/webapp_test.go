package webapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A machine without Node still runs `go test ./...`, so the package must
// answer something useful with no build on disk: the placeholder explains
// how to build, instead of a blank 404.
func TestPlaceholderWithoutBuild(t *testing.T) {
	h := handlerFor(http.FS(FS()))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "pnpm") {
		t.Fatalf("placeholder does not say how to build: %q", rec.Body.String()[:120])
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type = %q", ct)
	}
}

// testBuild writes a miniature Vite output: a shell, a hashed asset and an
// image, which is all the handler has to tell apart.
func testBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<!doctype html><title>today</title>")
	write("assets/index-abc123.js", "console.log(1)")
	write("favicon.png", "png")
	return dir
}

func TestServesBuildAndFallsBackToShell(t *testing.T) {
	h := handlerFor(http.Dir(testBuild(t)))

	cases := []struct {
		path       string
		code       int
		body       string
		cache      string
		hasContent bool
	}{
		// Routing is the browser's: a deep link must reach the shell.
		{path: "/app/items/xyz", code: 200, body: "<!doctype html>", cache: "no-cache"},
		{path: "/", code: 200, body: "<!doctype html>", cache: "no-cache"},
		// A real file keeps its own identity and may be cached hard.
		{path: "/assets/index-abc123.js", code: 200, body: "console.log(1)", cache: "immutable"},
		// Names with an extension that do not exist are mistakes, not routes.
		{path: "/assets/index-gone.js", code: 404},
		{path: "/favicon.png", code: 200, body: "png"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.code {
			t.Fatalf("%s: status = %d, want %d", c.path, rec.Code, c.code)
		}
		if c.body != "" && !strings.Contains(rec.Body.String(), c.body) {
			t.Fatalf("%s: body = %q, want it to contain %q", c.path, rec.Body.String(), c.body)
		}
		if c.cache != "" && !strings.Contains(rec.Header().Get("Cache-Control"), c.cache) {
			t.Fatalf("%s: cache-control = %q, want %q", c.path, rec.Header().Get("Cache-Control"), c.cache)
		}
	}
}

// A directory request must not list files: that leaks the build's shape and
// is never what the page wants.
func TestDirectoryIsNotListed(t *testing.T) {
	h := handlerFor(http.Dir(testBuild(t)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/", nil))
	if strings.Contains(rec.Body.String(), "index-abc123.js") {
		t.Fatalf("directory listing leaked: %q", rec.Body.String())
	}
	if rec.Code != 200 && rec.Code != 404 {
		t.Fatalf("status = %d", rec.Code)
	}
}

// A released app serves the bundle embedded at build time. Reading the working
// directory would let a stale dist/ in whatever folder the process was started
// from answer instead — someone else's build, at our address.
func TestProductionIgnoresTheWorkingDirectory(t *testing.T) {
	dir := testBuild(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<!doctype html><title>from the working directory</title>")

	// dev=false must not look at distDir. The package's own distDir is the
	// checkout's frontend/web/dist, which may exist; the assertion therefore
	// pins the behaviour through the helper the Handler uses, not the real
	// directory.
	if _, ok := diskBuild(false); ok {
		t.Fatal("production would read the working directory")
	}
	t.Setenv("TODAY_WEB_DIST", dir)
	if _, ok := diskBuild(false); !ok {
		t.Fatal("an explicit TODAY_WEB_DIST should be honoured")
	}
}
