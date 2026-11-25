package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildScriptValidatesVersionBeforeWritingArtifacts(t *testing.T) {
	source, err := os.ReadFile("../../scripts/release.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.2.3", "v0.3.0", "", "v", "1.2", "v1.2.3-rc1", "v1.2.3/../../x", "1.2.3\n4.5.6"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			scripts := filepath.Join(dir, "scripts")
			if err := os.Mkdir(scripts, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(scripts, "release.sh")
			if err := os.WriteFile(path, source, 0o755); err != nil {
				t.Fatal(err)
			}
			// Stop before any real build; exit 42 proves the version reached it.
			if err := os.WriteFile(filepath.Join(scripts, "build-web.sh"), []byte("#!/bin/sh\nexit 42\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/bash", path, version)
			cmd.Env = append(os.Environ(), "SKIP_APP=0", "PUBLIC_RELEASE=0", "NOTARY_PROFILE=")
			out, err := cmd.CombinedOutput()
			want := 2
			if StrictTag(version) {
				want = 42
			}
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != want {
				t.Fatalf("exit = %v, want %d; output: %s", err, want, out)
			}
			if want == 2 {
				if _, err := os.Stat(filepath.Join(dir, "dist")); !os.IsNotExist(err) {
					t.Fatal("invalid version created an artifact directory")
				}
			} else if !strings.Contains(string(out), "building Today "+strings.TrimPrefix(version, "v")) {
				t.Fatalf("version was not normalized: %s", out)
			}
		})
	}
}
