#!/usr/bin/env bash
# flash.sh — build, flash and provision the Today RLCD firmware.
#
# Everything here is bash + the tools PlatformIO already installs, so a fresh
# Apple Silicon Mac only needs PlatformIO itself:
#   brew install platformio          (or: uv tool install platformio)
# https://docs.platformio.org/en/latest/core/installation/index.html
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

ENV_NAME="esp32-s3-devkitc1-n16r8"
BUILD_DIR=".pio/build/${ENV_NAME}"
DIST_DIR="dist"
MERGED="${DIST_DIR}/today-rlcd-merged.bin"
PORT=""
SSID=""
PASS=""
SERVER=""
DEVICE=""
TOKEN=""
DRY_RUN=0
NO_INSTALL=0

usage() {
  cat <<'EOF'
Usage: ./flash.sh <command> [options]

Commands
  doctor                  Check pio/esptool and list serial ports
  build                   Compile the firmware (pio run)
  flash [--port P]        Compile and upload over USB
  monitor [--port P]      Open the serial console (115200)
  merge                   Write dist/today-rlcd-merged.bin for browser flashing
  provision --server URL --device ID --token T [--ssid S --pass P]
                          Send the credentials over the serial config window
  provision --dry-run ... Print the lines that would be sent (token masked)

Options
  --port P     Serial device, e.g. /dev/cu.usbmodem1101
  --ssid S     Wi-Fi SSID (optional)
  --pass P     Wi-Fi password (optional)
  --server URL Server base, e.g. http://192.168.1.10:8787
  --device ID  Device id from Settings → 设备 (default: dev1)
  --token T    Device token from Settings → 设备
  --dry-run    With provision: print, do not touch the serial port
  --no-install Do not offer to install a missing tool

Notes
  * BOOT is a strapping pin: never hold it while powering on unless you want
    the download mode. For a normal boot just click PWR.
  * The board's USB port is the USB-Serial/JTAG one; a charge-only cable shows
    no /dev/cu.usbmodem* device.
EOF
}

die() { printf 'flash.sh: %s\n' "$*" >&2; exit 1; }

have() { command -v "$1" >/dev/null 2>&1; }

pio_cmd() {
  if have pio; then
    pio "$@"
  elif [[ -x "${HOME}/.platformio/penv/bin/pio" ]]; then
    "${HOME}/.platformio/penv/bin/pio" "$@"
  else
    die "PlatformIO is not installed. Install it with: brew install platformio   (or: uv tool install platformio)"
  fi
}

esptool_cmd() {
  # PlatformIO ships esptool in its package store; prefer that over a global one
  # so the version matches the build.
  pio_cmd pkg exec -- esptool.py "$@"
}

ports() {
  local found=()
  for p in /dev/cu.usbmodem* /dev/cu.usbserial-*; do
    [[ -e "$p" ]] && found+=("$p")
  done
  ((${#found[@]})) && printf '%s\n' "${found[@]}"
  return 0
}

pick_port() {
  if [[ -n "$PORT" ]]; then
    printf '%s' "$PORT"
    return
  fi
  local list
  list="$(ports)"
  local count
  count="$(printf '%s' "$list" | grep -c . || true)"
  if [[ "$count" == "1" ]]; then
    printf '%s' "$list"
    return
  fi
  if [[ "$count" == "0" ]]; then
    die "no /dev/cu.usbmodem* found. Is the USB-C cable a data cable, and is the board powered on (click PWR)?"
  fi
  printf 'several serial ports:\n%s\n' "$list" >&2
  die "pick one with --port"
}

cmd_doctor() {
  local problems=0
  printf 'Today RLCD firmware doctor\n\n'
  if have pio; then
    printf 'pio        %s (%s)\n' "$(pio --version 2>/dev/null | head -1)" "$(command -v pio)"
  elif [[ -x "${HOME}/.platformio/penv/bin/pio" ]]; then
    printf 'pio        %s\n' "${HOME}/.platformio/penv/bin/pio"
  else
    printf 'pio        MISSING — install: brew install platformio   (or: uv tool install platformio)\n'
    problems=$((problems + 1))
  fi

  if [[ -d "${HOME}/.platformio" ]]; then
    printf 'platformio %s\n' "${HOME}/.platformio"
  else
    printf 'platformio no ~/.platformio yet; the first `pio run` downloads the toolchain\n'
  fi

  local list
  list="$(ports)"
  if [[ -n "$list" ]]; then
    printf 'serial     %s\n' "$list"
  else
    printf 'serial     none — plug the board into USB and click PWR\n'
    printf '           (charge-only cables never appear as /dev/cu.usbmodem*)\n'
  fi

  if have esptool.py || have esptool; then
    printf 'esptool    %s (standalone)\n' "$(command -v esptool.py || command -v esptool)"
  else
    printf 'esptool    not global; PlatformIO provides it: pio pkg exec -- esptool.py ...\n'
  fi

  printf '\nWeb Serial (optional): Chrome or Edge on http://localhost, Safari does not implement it\n'

  if have python3; then
    if python3 test/check_partitions.py >/tmp/today-partitions.$$ 2>&1; then
      printf 'partitions %s\n' "$(tail -1 /tmp/today-partitions.$$)"
    else
      printf 'partitions FAIL\n'
      cat /tmp/today-partitions.$$
      problems=$((problems + 1))
    fi
    rm -f /tmp/today-partitions.$$
  fi

  if [[ "$problems" != "0" ]]; then
    exit 1
  fi
}

cmd_build() {
  if have python3; then
    python3 test/check_partitions.py || die "partitions.csv is inconsistent; fix it before building"
  fi
  pio_cmd run -e "$ENV_NAME"
}

cmd_flash() {
  local port
  port="$(pick_port)"
  pio_cmd run -e "$ENV_NAME" -t upload --upload-port "$port"
}

cmd_monitor() {
  local port
  port="$(pick_port)"
  pio_cmd device monitor -e "$ENV_NAME" --port "$port"
}

cmd_merge() {
  cmd_build
  mkdir -p "$DIST_DIR"
  # Do not hand-write offsets: reuse exactly what `pio upload` writes, plus
  # the application image at its partition offset from the same metadata.
  local meta
  meta="$(mktemp -t today-meta.XXXXXX)"
  pio_cmd project metadata --json-output >"$meta"
  # The app offset is read from the partition table, not hard-coded: it is
  # the ota_0 start (0x10000 with the current partitions.csv).
  local app_offset
  app_offset="$(python3 - "$(pwd)/partitions.csv" <<'PY'
import csv, sys
for row in csv.reader(open(sys.argv[1])):
    if not row or row[0].strip().startswith('#') or len(row) < 4:
        continue
    if row[1].strip() == 'app':
        print(row[3].strip())
        break
PY
)"
  local args=()
  while IFS=$'\t' read -r offset path; do
    [[ -n "$offset" ]] && args+=("$offset" "$path")
  done < <(python3 - "$meta" "$ENV_NAME" "$app_offset" "$BUILD_DIR/firmware.bin" <<'PY'
import json, sys
meta, env, app_offset, app_path = sys.argv[1:5]
data = json.load(open(meta))[env]
images = list(data.get("extra", {}).get("flash_images", []))
images.append({"offset": app_offset, "path": app_path})
for image in images:
    print(image["offset"] + "\t" + image["path"])
PY
)
  rm -f "$meta"
  esptool_cmd --chip esp32s3 merge_bin \
    --flash_mode keep \
    --flash_freq keep \
    --flash_size keep \
    --output "$MERGED" \
    "${args[@]}"
  printf '\nwrote %s (%s bytes)\n' "$MERGED" "$(wc -c <"$MERGED" | tr -d ' ')"
  printf 'flash it with: esptool.py --chip esp32s3 write_flash 0x0 %s\n' "$MERGED"
}

mask_token() {
  local t="$1"
  if ((${#t} <= 6)); then
    printf '***'
  else
    printf '%s…%s' "${t:0:3}" "${t: -3}"
  fi
}

provision_lines() {
  [[ -n "$SSID" ]] && printf 'wifi %s %s\n' "$SSID" "$PASS"
  [[ -n "$SERVER" ]] && printf 'server %s\n' "$SERVER"
  [[ -n "$DEVICE" ]] && printf 'device %s\n' "$DEVICE"
  [[ -n "$TOKEN" ]] && printf 'token %s\n' "$TOKEN"
  printf 'show\n'
}

cmd_provision() {
  [[ -n "$DRY_RUN" ]] || :
  if [[ "$DRY_RUN" == "1" ]]; then
    printf 'would send over serial (token and password masked):\n'
    provision_lines | sed -E 's/^(token ).*/\1***/; s/^(wifi [^ ]+ ).*/\1***/'
    return 0
  fi
  [[ -n "$SERVER" ]] || die "provision needs --server (e.g. http://192.168.1.10:8787)"
  [[ -n "$TOKEN" ]] || die "provision needs --token (Settings → 设备 → 生成 token)"
  [[ -n "$DEVICE" ]] || DEVICE=dev1

  local port
  port="$(pick_port)"
  printf 'Using %s\n' "$port"
  printf 'Tap RESET (or replug USB) now; the firmware opens a 10-minute serial window on a cold boot.\n'
  read -r -p 'Press Enter once the banner "Today firmware" appears…' _ || true

  # termios only: 115200 8N1, raw, no echo. No new dependency.
  local before
  before="$(stty -g -f "$port")"
  trap 'stty -f "$port" "$before" 2>/dev/null || true' EXIT
  stty -f "$port" 115200 raw -echo -hupcl

  exec 3>"$port"
  while IFS= read -r line; do
    printf '%s\n' "$line" >&3
    sleep 0.2
  done < <(provision_lines)
  printf 'sent. Reading `show` output for 2 s…\n'
  sleep 2
  exec 3>&-
  # Read back whatever the board printed; this never repeats the token.
  timeout 2 cat "$port" | tail -20 || true
  printf '\nIf the banner shows token=(set) and the right ssid/server, provisioning is done.\n'
}

# --- arg parsing -----------------------------------------------------------
[[ $# -gt 0 ]] || { usage; exit 1; }
CMD="$1"
shift
while [[ $# -gt 0 ]]; do
  case "$1" in
    --port) PORT="${2:-}"; shift 2 ;;
    --ssid) SSID="${2:-}"; shift 2 ;;
    --pass) PASS="${2:-}"; shift 2 ;;
    --server) SERVER="${2:-}"; shift 2 ;;
    --device) DEVICE="${2:-}"; shift 2 ;;
    --token) TOKEN="${2:-}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --no-install) NO_INSTALL=1; shift ;;
    -h|--help|help) usage; exit 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
done

case "$CMD" in
  doctor) cmd_doctor ;;
  build) cmd_build ;;
  flash) cmd_flash ;;
  monitor) cmd_monitor ;;
  merge) cmd_merge ;;
  provision) cmd_provision ;;
  -h|--help|help) usage ;;
  *) usage; die "unknown command: $CMD" ;;
esac
