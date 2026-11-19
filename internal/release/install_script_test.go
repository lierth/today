package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runScript executes the generated installer with `open` shimmed, so the swap
// logic runs for real without starting an app.
//
// `marker` is the startup marker the script waits for; the shim writes it when
// `writeMarker` is true, which is how a successful launch and a crash on
// launch are told apart.
func runScript(t *testing.T, script, dir, marker string, writeMarker bool) string {
	t.Helper()
	shim := filepath.Join(dir, "bin")
	if err := os.MkdirAll(shim, 0o755); err != nil {
		t.Fatal(err)
	}
	// `open` records the call and, like a healthy new version, writes the
	// startup marker.
	open := filepath.Join(shim, "open")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("echo \"$@\" >> \"$(dirname \"$0\")/opened\"\n")
	if writeMarker {
		b.WriteString("printf '%s' \"$(date)\" > " + shQuote(marker) + "\n")
	}
	if err := os.WriteFile(open, []byte(b.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	path := shim + string(os.PathListSeparator) + os.Getenv("PATH")
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(), "PATH="+path)
	out, err := cmd.CombinedOutput()
	// A rollback path exits 1 on purpose; the callers assert on the log and the
	// resulting files rather than the exit code.
	_ = err
	return string(out)
}

func TestInstallScriptSwapsBundle(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Applications", "Today.app")
	stage := filepath.Join(dir, "stage")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "OLD"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stage, "Today.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "Today.app", "NEW"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "started")
	plan := InstallPlan{
		CurrentApp:    app,
		NewApp:        filepath.Join(stage, "Today.app"),
		OldApp:        app + ".old",
		Script:        filepath.Join(dir, "install.sh"),
		StartedMarker: marker,
		// A PID that is certainly gone: the wait loop must not block.
		PID: 999999,
	}
	script := InstallScript(plan, 2)
	if err := os.WriteFile(plan.Script, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	runScript(t, plan.Script, dir, marker, true)

	if _, err := os.Stat(filepath.Join(app, "NEW")); err != nil {
		t.Fatal("new bundle not moved in")
	}
	if _, err := os.Stat(plan.OldApp); err != nil {
		t.Fatal("old bundle not kept as .old")
	}
	opened, err := os.ReadFile(filepath.Join(dir, "bin", "opened"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(opened)) != app {
		t.Fatalf("open called with %q", opened)
	}
}

// "open" succeeding is not proof the app came up: with no startup marker the
// script rolls back and starts the old bundle again.
func TestInstallScriptRollsBackWithoutStartupMarker(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Today.app")
	stage := filepath.Join(dir, "stage")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "OLD"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stage, "Today.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "Today.app", "NEW"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "started")
	plan := InstallPlan{
		CurrentApp: app, NewApp: filepath.Join(stage, "Today.app"),
		OldApp: app + ".old", Script: filepath.Join(dir, "install.sh"),
		StartedMarker: marker, PID: 999999,
	}
	if err := os.WriteFile(plan.Script, []byte(InstallScript(plan, 1)), 0o755); err != nil {
		t.Fatal(err)
	}
	runScript(t, plan.Script, dir, marker, false)

	if _, err := os.Stat(filepath.Join(app, "OLD")); err != nil {
		t.Fatal("old bundle not restored after a launch that never signalled")
	}
	logs, err := os.ReadFile(filepath.Join(dir, "install.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logs), "never signalled startup") {
		t.Fatalf("no rollback in the log:\n%s", logs)
	}
}

// With the destination impossible the script must put the old bundle back and
// start it, rather than leaving the machine without an app.
func TestInstallScriptRollsBack(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Today.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	// The "new" app does not exist, so the second mv fails.
	marker := filepath.Join(dir, "started")
	plan := InstallPlan{
		CurrentApp:    app,
		NewApp:        filepath.Join(dir, "missing", "Today.app"),
		OldApp:        app + ".old",
		Script:        filepath.Join(dir, "install.sh"),
		StartedMarker: marker,
		PID:           999999,
	}
	if err := os.WriteFile(plan.Script, []byte(InstallScript(plan, 1)), 0o755); err != nil {
		t.Fatal(err)
	}
	runScript(t, plan.Script, dir, marker, true)
	if _, err := os.Stat(app); err != nil {
		t.Fatal("old bundle not restored")
	}
	logs, err := os.ReadFile(filepath.Join(dir, "install.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logs), "rolling back") {
		t.Fatalf("no rollback in the log:\n%s", logs)
	}
	opened, err := os.ReadFile(filepath.Join(dir, "bin", "opened"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opened), app) {
		t.Fatalf("old app not started: %q", opened)
	}
}

// The wait bound gives up instead of hanging forever on a live process.
func TestInstallScriptWaitBound(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Today.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := InstallPlan{
		CurrentApp: app, NewApp: app + ".new", OldApp: app + ".old",
		Script:        filepath.Join(dir, "install.sh"),
		StartedMarker: filepath.Join(dir, "started"),
		// The test process itself is alive, so kill -0 always succeeds.
		PID: os.Getpid(),
	}
	if err := os.WriteFile(plan.Script, []byte(InstallScript(plan, 1)), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	cmd := exec.Command("/bin/sh", plan.Script)
	_ = cmd.Run() // exits 1 on purpose
	if time.Since(start) > 5*time.Second {
		t.Fatal("script did not honour its wait bound")
	}
	logs, _ := os.ReadFile(filepath.Join(dir, "install.log"))
	if !strings.Contains(string(logs), "did not exit") {
		t.Fatalf("no timeout in the log:\n%s", logs)
	}
}
