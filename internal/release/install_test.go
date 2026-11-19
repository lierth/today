package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ioreg output with the idle counter, and one without it.
func TestIdleForParse(t *testing.T) {
	out := `+-o IOHIDSystem  <class IOHIDSystem, id 0x1000003a8, registered>
  {
    "HIDIdleTime" = 734000000000
    "HIDIdleTimeMicroseconds" = 0
  }`
	got, ok := IdleForParse(out)
	if !ok || got != 734*time.Second {
		t.Fatalf("idle = %v (%v)", got, ok)
	}
	if _, ok := IdleForParse("nothing here"); ok {
		t.Fatal("parsed a line that is not there")
	}
	if _, ok := IdleForParse(`"HIDIdleTime" = abc`); ok {
		t.Fatal("parsed junk as idle time")
	}
	if _, ok := IdleForParse(`"HIDIdleTime" = -5`); ok {
		t.Fatal("negative idle accepted")
	}
}

func TestOnAC(t *testing.T) {
	ac := "Now drawing from 'AC Power'\n -InternalBattery-0 (id=123)\t100%; charged; 0:00 remaining present: true"
	battery := "Now drawing from 'Battery Power'\n -InternalBattery-0 (id=123)\t81%; discharging; 3:12 remaining present: true"
	if !OnAC(ac) {
		t.Fatal("AC not detected")
	}
	if OnAC(battery) {
		t.Fatal("battery read as AC")
	}
	if OnAC("") {
		t.Fatal("empty read as AC")
	}
}

// The idle rule: both conditions, and a missing reading never qualifies.
func TestIdleEligible(t *testing.T) {
	cases := []struct {
		idle      time.Duration
		have      bool
		ac        bool
		threshold time.Duration
		want      bool
	}{
		{11 * time.Minute, true, true, 10 * time.Minute, true},
		{9 * time.Minute, true, true, 10 * time.Minute, false},
		{11 * time.Minute, true, false, 10 * time.Minute, false},
		{11 * time.Minute, false, true, 10 * time.Minute, false},
		{11 * time.Minute, true, true, 0, true}, // default threshold
	}
	for _, c := range cases {
		if got := IdleEligible(c.idle, c.have, c.ac, c.threshold); got != c.want {
			t.Errorf("IdleEligible(%v,%v,%v,%v) = %v", c.idle, c.have, c.ac, c.threshold, got)
		}
	}
}

// The plist reader takes the two fields the installer checks.
func TestVersionFromPlist(t *testing.T) {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.lierth.today</string>
	<key>CFBundleShortVersionString</key>
	<string>0.4.0</string>
	<key>CFBundleVersion</key>
	<string>0.4.0</string>
</dict>
</plist>`
	id, version, _ := VersionFromPlist([]byte(plist))
	if id != "com.lierth.today" || version != "0.4.0" {
		t.Fatalf("got %q %q", id, version)
	}
	id, version, _ = VersionFromPlist([]byte("<plist></plist>"))
	if id != "" || version != "" {
		t.Fatalf("empty plist produced %q %q", id, version)
	}
	// A plist with only CFBundleVersion still yields a version.
	short := `<key>CFBundleVersion</key><string>1.2.3</string>`
	if _, v, _ := VersionFromPlist([]byte(short)); v != "1.2.3" {
		t.Fatalf("CFBundleVersion fallback: %q", v)
	}
}

// BundleInside finds the .app and refuses a directory without one.
func TestBundleInside(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Today.app", "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := BundleInside(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "Today.app" {
		t.Fatalf("got %q", got)
	}
	if _, err := BundleInside(t.TempDir()); err == nil {
		t.Fatal("empty directory accepted")
	}
}

// The generated script names the three paths and the rollback branches, and
// quotes a path with a space correctly.
func TestInstallScript(t *testing.T) {
	plan := InstallPlan{
		CurrentApp: "/Applications/Today.app",
		NewApp:     "/tmp/stage/Today.app",
		OldApp:     "/Applications/Today.app.old",
		PID:        4242,
	}
	script := InstallScript(plan, 30)
	for _, want := range []string{
		"kill -0 4242", "'/Applications/Today.app'", "'/Applications/Today.app.old'",
		"'/tmp/stage/Today.app'", "rolling back", "open '/Applications/Today.app'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	spaced := InstallScript(InstallPlan{
		CurrentApp: "/Users/a b/Today.app", OldApp: "/Users/a b/Today.app.old",
		NewApp: "/tmp/x/Today.app", PID: 1,
	}, 5)
	if !strings.Contains(spaced, `'/Users/a b/Today.app'`) {
		t.Fatal("space in a path not quoted")
	}
	if !strings.Contains(spaced, `[ "$i" -ge 5 ]`) {
		t.Fatal("wait bound missing")
	}
}

// CleanupOldBundle leaves a fresh .old alone and removes an old one.
func TestCleanupOldBundle(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Today.app")
	if err := os.MkdirAll(app+".old", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CleanupOldBundle(app, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(app + ".old"); err != nil {
		t.Fatal("fresh .old removed")
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(app+".old", past, past); err != nil {
		t.Fatal(err)
	}
	if err := CleanupOldBundle(app, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(app + ".old"); !os.IsNotExist(err) {
		t.Fatal("stale .old kept")
	}
	// Nothing to clean is not an error.
	if err := CleanupOldBundle(filepath.Join(dir, "Missing.app"), time.Hour); err != nil {
		t.Fatal(err)
	}
}
