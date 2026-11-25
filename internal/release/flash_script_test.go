package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirmwareMergeWithSystemBash(t *testing.T) {
	source, err := os.ReadFile("../../firmware/flash.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"firmware/flash.sh":                 string(source),
		"firmware/partitions.csv":           "app0,app,ota_0,0x10000,3M\n",
		"firmware/test/check_partitions.py": "print('partition fixture')\n",
		"bin/pio": `#!/bin/sh
case "$1" in
  run) exit 0 ;;
  project)
    printf '%s\n' '{"esp32-s3-devkitc1-n16r8":{"extra":{"flash_images":[{"offset":"0x0","path":"tool dir/bootloader.bin"},{"offset":"0x8000","path":"tool dir/partitions.bin"},{"offset":"0xe000","path":"tool dir/boot_app0.bin"}]}}}'
    ;;
  pkg)
    printf '%s\n' "$@" > "$PIO_CALLS"
    while [ "$#" -gt 1 ]; do
      if [ "$1" = '--output' ]; then printf 'merged fixture' > "$2"; break; fi
      shift
    done
    ;;
  *) exit 1 ;;
esac
`,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	calls := filepath.Join(dir, "pio-args")
	cmd := exec.Command("/bin/bash", filepath.Join(dir, "firmware", "flash.sh"), "merge")
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"), "PIO_CALLS="+calls)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("system bash merge failed: %v\n%s", err, out)
	}
	args, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	want := "0x0\ntool dir/bootloader.bin\n0x8000\ntool dir/partitions.bin\n0xe000\ntool dir/boot_app0.bin\n0x10000\n.pio/build/esp32-s3-devkitc1-n16r8/firmware.bin\n"
	if !strings.HasSuffix(string(args), want) {
		t.Fatalf("wrong image offsets or split paths: %s", args)
	}
}
