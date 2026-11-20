#!/bin/sh
# Build the React frontend and put the bundle where Go embeds it.
#
# The repository commits only a placeholder page (internal/server/webapp/dist),
# so `go test ./...` and `go build ./...` work on a machine without Node. A
# released app must carry its real pages, so this script builds the bundle and
# copies it to internal/server/webapp/build, which `//go:embed all:build` picks
# up at compile time. Run it before `go tool mygo build` or scripts/release.sh;
# plain `go build` during development can skip it, because webapp.Handler
# prefers frontend/web/dist when that directory exists on disk.
set -eu

cd "$(dirname "$0")/.."

if ! command -v pnpm >/dev/null 2>&1; then
  echo "build-web: pnpm not found; install Node 22 and pnpm 12 first (see docs/developing.md)" >&2
  exit 1
fi

echo "== building the web frontend"
pnpm -C frontend install --frozen-lockfile
pnpm -C frontend build

DEST=internal/server/webapp/build
rm -rf "$DEST"
mkdir -p "$DEST"
# -a keeps the hashed asset names Vite produced; the .gitkeep is what lets the
# empty directory be committed and go:embed see it.
cp -a frontend/web/dist/. "$DEST"/
touch "$DEST/.gitkeep"
echo "build-web: wrote $(find "$DEST" -type f | wc -l | tr -d ' ') files to $DEST"
