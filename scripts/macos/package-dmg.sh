#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="$ROOT/build/macos"
APP="$BUILD/LinkVideo.Monitor.app"
VERSION="${MACOS_VERSION:-0.1.0-dev}"
DMG="$BUILD/LinkVideo.Monitor_macOS_${VERSION}.dmg"
PKG="$BUILD/LinkVideo.Monitor_macOS_${VERSION}.pkg"
UNINSTALLER="$ROOT/packaging/macos/Uninstall LinkVideo Monitor.command"
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/linkvideo-monitor-dmg.XXXXXX")"

cleanup() {
  rm -rf "$STAGE"
}
trap cleanup EXIT

if [[ ! -d "$APP" ]]; then
  echo "Не найден $APP. Сначала выполните scripts/macos/build-app.sh" >&2
  exit 1
fi

# Keep drag-to-Applications available for development/testing while also
# exposing the proper PKG installer when package-pkg.sh has already run.
ditto "$APP" "$STAGE/LinkVideo.Monitor.app"
ln -s /Applications "$STAGE/Applications"
cp "$UNINSTALLER" "$STAGE/Uninstall LinkVideo Monitor.command"
chmod 755 "$STAGE/Uninstall LinkVideo Monitor.command"
if [[ -f "$PKG" ]]; then
  cp "$PKG" "$STAGE/Install LinkVideo Monitor.pkg"
fi

rm -f "$DMG"
hdiutil create \
  -volname "LinkVideo Monitor" \
  -srcfolder "$STAGE" \
  -format UDZO \
  -ov \
  "$DMG"

echo "Built DMG: $DMG"
