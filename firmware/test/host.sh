#!/usr/bin/env bash
# Host-side firmware tests: no Arduino, no board. The source list lives only
# here, so docs and CI cannot drift from it (they did: wifista.cpp was missing).
set -euo pipefail
cd "$(dirname "$0")/../.."
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
clang++ -std=c++17 -Wall -Wextra -I firmware/src \
  firmware/test/pack_test.cpp \
  firmware/src/{st7305,tbf,fontcheck,netescape,portal_plan,sound_synth,sound_policy,screens,version,wifista}.cpp \
  -o "$out/pack_test"
go run ./cmd/todayd preview --demo --out "$out/frame.bin" >/dev/null
# pack_test reads source files relative to firmware/.
(cd firmware && "$out/pack_test" "$out/frame.bin")
