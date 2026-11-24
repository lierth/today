#!/bin/sh
# Build, sign and package one release locally. CI calls this same script, so a
# local dry run and a tagged release cannot drift.
#
#   scripts/release.sh <version>
#
# Environment:
#   CODESIGN_IDENTITY  signing identity ("Today Self-Signed" or a Developer ID).
#                      Unset means ad-hoc: the zip still runs, but macOS treats
#                      every update as a new app and TCC permissions are lost.
#   TODAY_RELEASE_KEY  ed25519 private key (base64). Unset and no
#                      TODAY_RELEASE_KEY_FILE means the manifest cannot be
#                      signed, and the script stops.
#   TODAY_RELEASE_KEY_FILE  read the key from a file (0600) instead.
#   ALLOW_UNSIGNED_RELEASE=1  allow manual-download releases without update keys.
#   SKIP_APP=1         only rebuild the firmware image (used by the board job).
#   PUBLIC_RELEASE=1   require Developer ID signing and Apple notarization.
#   NOTARY_PROFILE    notarytool keychain profile stored with store-credentials.
#
# Output goes to dist/release/: Today-<ver>-arm64.zip, Today-<ver>.dmg,
# today-rlcd-<ver>.bin, SHA256SUMS and SHA256SUMS.sig.
set -eu
cd "$(dirname "$0")/.."

VER="${1:-}"
if [ -z "$VER" ]; then
  echo "usage: scripts/release.sh <version>   (e.g. 0.4.0)" >&2
  exit 2
fi
VER="${VER#v}"
OUT="dist/release"
mkdir -p "$OUT"

PUBLIC_KEY="${TODAY_RELEASE_PUBLIC_KEY:-}"
# REPO defaults to the GitHub Actions slug, so the built app points its update
# check at this repository's releases without anyone pasting a URL.
REPO="${TODAY_RELEASE_REPO:-${GITHUB_REPOSITORY:-}}"
LDFLAGS="-X today/internal/release.PublicKey=${PUBLIC_KEY}"
LDFLAGS="$LDFLAGS -X today/internal/release.VersionNumber=${VER}"
LDFLAGS="$LDFLAGS -X today/internal/host.Version=${VER}"
LDFLAGS="$LDFLAGS -X main.version=${VER}"
if [ -n "$REPO" ] && [ -n "$PUBLIC_KEY" ]; then
  LDFLAGS="$LDFLAGS -X today/internal/host.ReleaseRepo=${REPO}"
fi

if [ "${SKIP_APP:-0}" != "1" ]; then
  if [ "${PUBLIC_RELEASE:-0}" = "1" ] || [ -n "${NOTARY_PROFILE:-}" ]; then
    case "${CODESIGN_IDENTITY:-}" in
      "Developer ID Application:"*) ;;
      *) echo "public releases require a Developer ID Application certificate" >&2; exit 1 ;;
    esac
    if [ -z "${NOTARY_PROFILE:-}" ]; then
      echo "public releases require NOTARY_PROFILE for Apple notarization" >&2
      exit 1
    fi
  fi
  echo "== building Today ${VER}"
  # The React pages are embedded into the Go binary, so they have to exist
  # before the compile: build-web.sh also produces the desktop bundle mygo
  # picks up from frontendDist.
  scripts/build-web.sh
  # mygo takes the bundle version from mygo.json and has no env override, so
  # the tag is written there for the build and the file is restored afterwards
  # (never hand-edited, never committed with a build-time version). -ldflags
  # carries the Go-side strings: the menu, the release state machine and the
  # User-Agent the update check sends.
  cp mygo.json "$OUT/mygo.json.bak"
  trap 'mv "$OUT/mygo.json.bak" mygo.json 2>/dev/null || true' EXIT
  sed -i '' "s/\"version\": *\"[^\"]*\"/\"version\": \"${VER}\"/" mygo.json
  MYGO_LDFLAGS="$LDFLAGS" go tool mygo build -platform darwin/arm64 -skip-build-command -skip-dmg -skip-notarize
  mv "$OUT/mygo.json.bak" mygo.json
  trap - EXIT
  APP="dist/darwin-arm64/Today.app"
  if [ ! -d "$APP" ]; then
    echo "build produced no Today.app" >&2
    exit 1
  fi
  # Ship the documented recovery CLI in the same signed bundle.
  GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$APP/Contents/MacOS/todayd" ./cmd/todayd

  if [ -n "${CODESIGN_IDENTITY:-}" ]; then
    echo "== signing with ${CODESIGN_IDENTITY}"
    case "$CODESIGN_IDENTITY" in
      "Developer ID Application:"*)
        codesign --force --deep --options runtime --timestamp --sign "$CODESIGN_IDENTITY" "$APP" ;;
      *) codesign --force --deep --sign "$CODESIGN_IDENTITY" "$APP" ;;
    esac
    codesign --verify --deep --strict --verbose=2 "$APP"
  else
    echo "!! CODESIGN_IDENTITY is not set: ad-hoc signing. Updates will reset"
    echo "!! macOS permissions (Local Network, keychain prompts)."
    codesign --force --deep --sign - "$APP"
  fi

  echo "== packing zip and dmg"
  ZIP="$OUT/Today-${VER}-arm64.zip"
  ditto -c -k --keepParent "$APP" "$ZIP"
  if [ -n "${NOTARY_PROFILE:-}" ]; then
    xcrun notarytool submit "$ZIP" --keychain-profile "$NOTARY_PROFILE" --wait
    xcrun stapler staple "$APP"
    spctl --assess --type execute --verbose=2 "$APP"
    ditto -c -k --keepParent "$APP" "$ZIP"
  fi
  if command -v hdiutil >/dev/null 2>&1; then
    STAGE="$(mktemp -d)"
    cp -R "$APP" "$STAGE/"
    ln -s /Applications "$STAGE/Applications"
    hdiutil create -quiet -volname "Today ${VER}" -srcfolder "$STAGE" \
      -ov -format UDZO "$OUT/Today-${VER}.dmg"
    rm -rf "$STAGE"
    if [ -n "${NOTARY_PROFILE:-}" ]; then
      codesign --sign "$CODESIGN_IDENTITY" --timestamp "$OUT/Today-${VER}.dmg"
      xcrun notarytool submit "$OUT/Today-${VER}.dmg" --keychain-profile "$NOTARY_PROFILE" --wait
      xcrun stapler staple "$OUT/Today-${VER}.dmg"
      xcrun stapler validate "$OUT/Today-${VER}.dmg"
    fi
  else
    echo "hdiutil missing; dmg skipped"
  fi
fi

echo "== building firmware"
( cd firmware && TODAY_FW_VERSION="$VER" ./flash.sh merge )
# Two images, because the two consumers need different things:
#   today-rlcd-<ver>.bin       the application only, written to the OTA slot;
#   today-rlcd-<ver>-full.bin  the merged image from offset 0, for a USB flash.
# Publishing only the merged image (as an earlier version did) makes OTA write
# a bootloader into the app partition and bricks the board.
APP_IMAGE="firmware/.pio/build/esp32-s3-devkitc1-n16r8/firmware.bin"
if [ ! -f "$APP_IMAGE" ]; then
  APP_IMAGE="$(find firmware/.pio/build -name firmware.bin -type f | head -1)"
fi
if [ -z "$APP_IMAGE" ] || [ ! -f "$APP_IMAGE" ]; then
  echo "no firmware.bin found after the build" >&2
  exit 1
fi
cp "$APP_IMAGE" "$OUT/today-rlcd-${VER}.bin"
cp firmware/dist/today-rlcd-merged.bin "$OUT/today-rlcd-${VER}-full.bin"

echo "== signing the manifest"
SIGNED=1
# Go's flag package stops at the first positional argument, so the flags come
# before the directory. The key is either in the environment (CI) or in a file
# (a local release).
if [ -n "${TODAY_RELEASE_KEY_FILE:-}" ]; then
  go run ./cmd/todayd release-sign --key-file "$TODAY_RELEASE_KEY_FILE" "$OUT"
elif [ -n "${TODAY_RELEASE_KEY:-}" ]; then
  go run ./cmd/todayd release-sign "$OUT"
elif [ "${ALLOW_UNSIGNED_RELEASE:-0}" = "1" ] && [ -z "$PUBLIC_KEY" ]; then
  SIGNED=0
  rm -f "$OUT/SHA256SUMS.sig"
  (
    cd "$OUT"
    for artifact in "Today-${VER}-arm64.zip" "Today-${VER}.dmg" \
      "today-rlcd-${VER}.bin" "today-rlcd-${VER}-full.bin"; do
      if [ -f "$artifact" ]; then shasum -a 256 "$artifact"; fi
    done > SHA256SUMS
  )
  echo "No update key: this release uses manual downloads; automatic updates stay disabled."
else
  echo "TODAY_RELEASE_KEY or TODAY_RELEASE_KEY_FILE must be set" >&2
  exit 1
fi

echo "== verifying"
if [ "$SIGNED" = "1" ]; then
  go run ./cmd/todayd release-verify --public-key "$PUBLIC_KEY" "$OUT"
else
  ( cd "$OUT" && shasum -a 256 -c SHA256SUMS )
fi
echo "== done: $OUT"
ls -l "$OUT"
