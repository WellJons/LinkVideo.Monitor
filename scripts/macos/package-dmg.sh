#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="$ROOT/build/macos"
APP="$BUILD/LinkVideo.Monitor.app"
VERSION="${MACOS_VERSION:-0.1.0-dev}"
DMG="$BUILD/LinkVideo.Monitor_macOS_${VERSION}.dmg"
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

# Keep the DMG as a standard drag-to-Applications distribution. The standalone
# PKG is published separately and must not be embedded here because it contains
# another copy of the same app bundle and nearly doubles the DMG size.
ditto "$APP" "$STAGE/LinkVideo.Monitor.app"
ln -s /Applications "$STAGE/Applications"
cp "$UNINSTALLER" "$STAGE/Uninstall LinkVideo Monitor.command"
chmod 755 "$STAGE/Uninstall LinkVideo Monitor.command"

rm -f "$DMG"
hdiutil create \
  -volname "LinkVideo Monitor" \
  -srcfolder "$STAGE" \
  -format UDZO \
  -ov \
  "$DMG"

echo "Built DMG: $DMG"
