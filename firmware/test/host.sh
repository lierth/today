#!/usr/bin/env bash
# Host-side firmware tests: no Arduino, no board. The source list lives only
# here, so docs and CI cannot drift from it (they did: wifista.cpp was missing).
set -euo pipefail
cd "$(dirname "$0")/../.."
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
clang++ -std=c++17 -Wall -Wextra -I firmware/src \
  firmware/test/pack_test.cpp \
  firmware/src/{st7305,tbf,fontcheck,netescape,pairing_state,portal_plan,portalscan,sound_synth,sound_policy,screens,version,wifista}.cpp \
  -o "$out/pack_test"
go run ./cmd/todayd preview --demo --db "$out/today.db" --out "$out/frame.bin" >/dev/null
# pack_test reads source files relative to firmware/.
(cd firmware && "$out/pack_test" "$out/frame.bin")
clang++ -std=c++17 -Wall -Wextra -I firmware/src \
  firmware/test/clock_test.cpp firmware/src/sound_policy.cpp -o "$out/clock_test"
"$out/clock_test"
clang++ -std=c++17 -Wall -Wextra -I firmware/src \
  firmware/test/rowstate_test.cpp firmware/src/tbf.cpp -o "$out/rowstate_test"
"$out/rowstate_test"
clang++ -std=c++17 -Wall -Wextra -I firmware/src \
  firmware/test/wire_test.cpp firmware/src/tbf.cpp -o "$out/wire_test"
"$out/wire_test" "$out/frame.bin"
clang++ -std=c++17 -Wall -Wextra -I firmware/test/audio_stubs -I firmware/src -I firmware/include \
  firmware/test/audio_test.cpp firmware/src/audio.cpp -o "$out/audio_test"
"$out/audio_test"
