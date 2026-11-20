// Package webapp serves the React frontend.
//
// `go test ./...` must run on a machine without Node, and a released .app must
// carry its pages, so the package embeds what it serves:
//
//   - `dist/` holds a committed placeholder that explains how to build;
//   - `build/` is empty in the repository (only .gitkeep is tracked) and holds
//     the real bundle when scripts/build-web.sh runs before a release build.
//
// At run time a local build wins over the embedded copy, so `pnpm -C frontend
// dev` plus `go run .` does not need the Go binary rebuilt:
//
//   - `TODAY_WEB_DIST` names a directory, or
//   - `frontend/web/dist` exists in the working tree.
//
// The desktop app's own bundle is injected by mygo's build, which is a
// different window and does not come through here.
package webapp

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed all:dist all:build
var embedded embed.FS

// distDir is where a local build lands, relative to the repository root.
const distDir = "frontend/web/dist"

// Handler serves the bundle: a local build in development, then the bundle
// embedded at release time, then the placeholder.
//
// The disk copy is only consulted in development (or when TODAY_WEB_DIST is
// set explicitly). A released app must serve the pages it was built with: if
// it read the working directory, starting it from a checkout — or from a
// folder with a stale dist/ — would silently serve someone else's build.
func Handler(dev bool) http.Handler {
	if root, ok := diskBuild(dev); ok {
		return handlerFor(root)
	}
	if bundle := embeddedDir("build"); bundle != nil {
		return handlerFor(http.FS(bundle))
	}
	return handlerFor(http.FS(FS()))
}

// diskBuild is the one place that decides whether a directory may answer
// instead of the embedded bundle: an explicit TODAY_WEB_DIST always may, the
// checkout's dist/ only in development.
func diskBuild(dev bool) (http.FileSystem, bool) {
	if dir := os.Getenv("TODAY_WEB_DIST"); dir != "" {
		return http.Dir(dir), true
	}
	if !dev {
		return nil, false
	}
	if info, err := os.Stat(filepath.Join(distDir, "index.html")); err == nil && !info.IsDir() {
		return http.Dir(distDir), true
	}
	return nil, false
}

// Icon returns one of the few images the app serves from its bundle root
// (favicon-light.png, favicon-dark.png, apple-touch-icon.png).
//
// The login form needs them before there is a session, so it cannot fetch them
// through Handler: that tree sits behind requireSession. Resolving here keeps a
// single source of truth and lets a local build, a release bundle and the
// checked-in placeholder all answer the same way.
//
// The embedded copy wins in a released app for the same reason Handler skips
// the working directory; dev keeps the disk first so a regenerated icon shows
// up without rebuilding.
func Icon(name string, dev bool) ([]byte, string, bool) {
	if name == "" || strings.ContainsAny(name, `/\\`) || strings.Contains(name, "..") {
		return nil, "", false
	}
	if dir := os.Getenv("TODAY_WEB_DIST"); dir != "" {
		if body, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return body, contentTypeOf(name), true
		}
	}
	if !dev {
		if body, ctype, ok := iconFromEmbed(name); ok {
			return body, ctype, true
		}
	}
	if body, err := os.ReadFile(filepath.Join(distDir, name)); err == nil {
		return body, contentTypeOf(name), true
	}
	if body, ctype, ok := iconFromEmbed(name); ok {
		return body, ctype, true
	}
	return nil, "", false
}

func iconFromEmbed(name string) ([]byte, string, bool) {
	for _, sub := range []string{"build", "dist"} {
		if body, err := fs.ReadFile(embedded, sub+"/"+name); err == nil {
			return body, contentTypeOf(name), true
		}
	}
	return nil, "", false
}

// handlerFor is Handler with the source settled. Tests point it at a
// miniature build without touching the tree.
func handlerFor(root http.FileSystem) http.Handler {
	return &spaHandler{root: root}
}

// embeddedDir returns the named directory of the embedded tree, or "" when it
// has no index.html — which is the checked-in state of build/.
func embeddedDir(name string) fs.FS {
	sub, err := fs.Sub(embedded, name)
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

// FS is the placeholder, for tests that do not want a build on disk.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// dist/index.html is committed, so this cannot happen in a sound tree.
		panic("webapp: embedded dist: " + err.Error())
	}
	return sub
}

// spaHandler serves files from root and answers every other path with the
// app's index.html, which is what a single-page app needs on reload.
type spaHandler struct {
	root http.FileSystem
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		h.serveIndex(w, r)
		return
	}
	f, err := h.root.Open(name)
	if err != nil {
		if strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		h.serveIndex(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		h.serveIndex(w, r)
		return
	}
	if ctype := contentTypeOf(name); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	// Vite writes content-hashed names under assets/, so those never change
	// meaning; index.html must be revalidated or a deploy keeps serving the
	// previous page's asset URLs.
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = io.Copy(w, f)
}

func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	f, err := h.root.Open("index.html")
	if err != nil {
		http.Error(w, "web build missing: run pnpm --dir frontend build", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.Copy(w, f)
}

func contentTypeOf(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}
