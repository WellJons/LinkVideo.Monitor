#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="$ROOT/build/macos"
APP="$BUILD/LinkVideo.Monitor.app"
VERSION="${MACOS_VERSION:-0.1.0-dev}"
BUNDLE_VERSION="${VERSION%%[-+]*}"
PACKAGE_ID="ru.linkvideo.monitor.pkg"
PKG="$BUILD/LinkVideo.Monitor_macOS_${VERSION}.pkg"
PAYLOAD="$BUILD/pkg-root"
SCRIPTS="$BUILD/pkg-scripts"
UNINSTALL_NAME="Uninstall LinkVideo Monitor.command"

if [[ ! -d "$APP" ]]; then
  echo "Application bundle not found: $APP" >&2
  echo "Run scripts/macos/build-app.sh first." >&2
  exit 1
fi

rm -rf "$PAYLOAD" "$SCRIPTS" "$PKG"
mkdir -p "$PAYLOAD/Applications" "$SCRIPTS"

ditto "$APP" "$PAYLOAD/Applications/LinkVideo.Monitor.app"
cp "$ROOT/packaging/macos/$UNINSTALL_NAME" "$PAYLOAD/Applications/$UNINSTALL_NAME"
cp "$ROOT/packaging/macos/pkg-scripts/preinstall" "$SCRIPTS/preinstall"
chmod 755 "$PAYLOAD/Applications/$UNINSTALL_NAME" "$SCRIPTS/preinstall"

pkg_args=(
  --root "$PAYLOAD"
  --identifier "$PACKAGE_ID"
  --version "$BUNDLE_VERSION"
  --install-location /
  --scripts "$SCRIPTS"
)

if [[ -n "${MACOS_INSTALLER_IDENTITY:-}" ]]; then
  pkg_args+=(--sign "$MACOS_INSTALLER_IDENTITY")
fi

pkgbuild "${pkg_args[@]}" "$PKG"

# Validate the package structure without installing it on the CI runner.
EXPANDED="$BUILD/pkg-expanded"
rm -rf "$EXPANDED"
pkgutil --expand "$PKG" "$EXPANDED"
test -f "$EXPANDED/PackageInfo"
test -f "$EXPANDED/Scripts/preinstall"
grep -q "identifier=\"$PACKAGE_ID\"" "$EXPANDED/PackageInfo"
grep -q "version=\"$BUNDLE_VERSION\"" "$EXPANDED/PackageInfo"
rm -rf "$EXPANDED" "$PAYLOAD" "$SCRIPTS"

echo "Built installer: $PKG"
echo "Package id: $PACKAGE_ID"
echo "Package version: $BUNDLE_VERSION"
if [[ -n "${MACOS_INSTALLER_IDENTITY:-}" ]]; then
  echo "Installer signing identity: $MACOS_INSTALLER_IDENTITY"
else
  echo "Installer is unsigned (development build)"
fi
