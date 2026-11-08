#!/bin/sh
# Launches the development app bundle through launchd (open), which gives it a
# real GUI session even when the calling shell is sandboxed.
set -e
cd "$(dirname "$0")/.."
APP=".mygo/dev/darwin-arm64/书桌 Dev.app"
if [ ! -d "$APP" ]; then
  echo "development app not built yet: run 'bun run dev' first" >&2
  exit 1
fi
open --env MYGO_DEV_URL=http://localhost:5173 "$APP"
