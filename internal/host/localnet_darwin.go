//go:build darwin

package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// bundleIsAdhoc reads the signature flags of the running bundle. `codesign -dv`
// prints "flags=0x2(adhoc)" for an ad-hoc signature and nothing for a
// Developer ID one. Failure to run codesign means "not ad-hoc": we must not
// tell every source build to re-grant.
func bundleIsAdhoc() bool {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return false
	}
	bundle := exe
	for range 4 {
		bundle = filepath.Dir(bundle)
		if strings.HasSuffix(bundle, ".app") {
			break
		}
	}
	if !strings.HasSuffix(bundle, ".app") {
		return false
	}
	out, err := exec.Command("codesign", "-dv", bundle).CombinedOutput()
	if err != nil && len(out) == 0 {
		return false
	}
	return strings.Contains(string(out), "adhoc")
}
