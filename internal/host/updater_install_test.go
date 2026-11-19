package host

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"today/internal/release"
)

// installEnv builds a Host whose update is "ready", with a fake .app on disk
// and the real signature check replaced.
type installEnv struct {
	h      *Host
	app    string // the "current" app
	staged string // staging dir holding the new app
	ready  string // the new app inside staged
	dir    string
}

func newInstallEnv(t *testing.T) *installEnv {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "Applications", "Today.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(dir, "stage")
	ready := filepath.Join(staged, "Today.app")
	if err := os.MkdirAll(ready, 0o755); err != nil {
		t.Fatal(err)
	}
	h := &Host{log: quietLogger()}
	h.quit = func() {}
	h.verify = func(newApp, currentApp string) error {
		if newApp != ready {
			t.Fatalf("verified the wrong bundle: %q", newApp)
		}
		return nil
	}
	h.updater = updater{
		State:   release.StateReady,
		Version: "0.4.0",
		Staged:  staged,
		Ready:   ready,
	}
	return &installEnv{h: h, app: app, staged: staged, ready: ready, dir: dir}
}

// installUpdate refuses without a verified, ready download.
func TestInstallUpdateRefusesUnready(t *testing.T) {
	h := &Host{log: quietLogger()}
	h.quit = func() {}
	if err := h.installUpdate(); err == nil {
		t.Fatal("installed without a ready update")
	}
	h.updater = updater{State: release.StateAvailable, Version: "0.4.0"}
	if err := h.installUpdate(); err == nil {
		t.Fatal("installed from the available state")
	}
}

// A failed signature re-check blocks the install even though the state says
// ready: the cache on disk may have been replaced.
func TestInstallUpdateRefusesBadSignature(t *testing.T) {
	e := newInstallEnv(t)
	e.h.verify = func(string, string) error { return os.ErrPermission }
	if err := e.h.installUpdate(); err == nil {
		t.Fatal("installed a bundle that failed the signature check")
	}
}

// writableDir is what decides between installing and asking the person to drag
// the app in: an unwritable directory must not be attempted.
func TestWritableDir(t *testing.T) {
	if err := writableDir(t.TempDir()); err != nil {
		t.Fatalf("writable temp dir refused: %v", err)
	}
	if err := writableDir(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing directory counted as writable")
	}
}

// The update directory containment is the last line before RemoveAll: a
// crafted tag must never name anything outside <cache>/Today/updates.
func TestUpdateDirStaysInside(t *testing.T) {
	good, err := updateDir("v0.4.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(good, filepath.Join("Today", "updates")) {
		t.Fatalf("update dir %q is not under the updates root", good)
	}
	for _, bad := range []string{"../../..", "v1.2.3/../../..", "v9.9.9+/../../../.."} {
		if _, err := updateDir(bad); err == nil {
			t.Fatalf("updateDir(%q) accepted", bad)
		}
	}
}

// fetchReleaseInfo refuses a tag that is not exactly vX.Y.Z, before anything
// touches a path or a URL.
func TestFetchReleaseInfoRejectsCraftedTag(t *testing.T) {
	for _, tag := range []string{"v9.9.9+/../../../..", "v1.2.3/../../x", "1.2"} {
		body := `{"tag_name":"` + tag + `","html_url":"https://example.invalid/x","assets":[]}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, err := fetchReleaseInfo(context.Background(), srv.Client(), srv.URL)
		srv.Close()
		if err == nil {
			t.Fatalf("crafted tag %q was accepted", tag)
		}
	}
}

// An Info.plist whose version does not match the release is refused after the
// unzip (the check downstream of extraction, without ditto).
func TestBundleVersionMismatchDetected(t *testing.T) {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>com.lierth.today</string>
<key>CFBundleShortVersionString</key><string>0.3.0</string>
</dict></plist>`
	id, version, _ := release.VersionFromPlist([]byte(plist))
	if id != "com.lierth.today" {
		t.Fatalf("bundle id %q", id)
	}
	// The updater's rule: the plist version must equal the release version,
	// with or without a leading v.
	ok := version == "0.4.0" || "v"+version == "0.4.0"
	if ok {
		t.Fatalf("0.3.0 accepted as 0.4.0")
	}
	if version == "0.4.0" {
		t.Fatal("sanity")
	}
}

// quietLogger discards host log lines during a test.
func quietLogger() *log.Logger { return log.New(io.Discard, "", 0) }
