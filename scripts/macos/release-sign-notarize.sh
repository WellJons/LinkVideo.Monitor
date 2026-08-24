#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="$ROOT/build/macos"
APP="$BUILD/LinkVideo.Monitor.app"
VERSION="${MACOS_VERSION:-0.1.0}"
PKG="$BUILD/LinkVideo.Monitor_macOS_${VERSION}.pkg"
DMG="$BUILD/LinkVideo.Monitor_macOS_${VERSION}.dmg"
ZIP="$BUILD/LinkVideo.Monitor_macOS_${VERSION}.zip"
NOTARY_ZIP="$BUILD/.LinkVideo.Monitor_notarization_${VERSION}.zip"

APP_IDENTITY="${MACOS_APP_IDENTITY:-}"
INSTALLER_IDENTITY="${MACOS_INSTALLER_IDENTITY:-}"
NOTARY_PROFILE="${MACOS_NOTARY_PROFILE:-}"
NOTARY_APPLE_ID="${MACOS_NOTARY_APPLE_ID:-}"
NOTARY_TEAM_ID="${MACOS_NOTARY_TEAM_ID:-}"
NOTARY_PASSWORD="${MACOS_NOTARY_PASSWORD:-}"

usage() {
  cat <<'EOF'
Usage:
  scripts/macos/release-sign-notarize.sh --check
  scripts/macos/release-sign-notarize.sh

Required for a production release:
  MACOS_VERSION
  MACOS_APP_IDENTITY        Developer ID Application identity
  MACOS_INSTALLER_IDENTITY  Developer ID Installer identity

Notary credentials: either
  MACOS_NOTARY_PROFILE      keychain profile created by `xcrun notarytool store-credentials`
or all of
  MACOS_NOTARY_APPLE_ID
  MACOS_NOTARY_TEAM_ID
  MACOS_NOTARY_PASSWORD     app-specific password

Optional:
  MACOS_ALLOW_DEV_RELEASE=1 permits a version containing "dev".
EOF
}

fail() {
  echo "release-sign-notarize: $*" >&2
  exit 1
}

require_tool() {
  command -v "$1" >/dev/null 2>&1 || fail "required tool not found: $1"
}

check_toolchain() {
  require_tool bash
  require_tool codesign
  require_tool security
  require_tool ditto
  require_tool pkgutil
  require_tool pkgbuild
  require_tool spctl
  require_tool hdiutil
  require_tool xcrun

  xcrun -f notarytool >/dev/null 2>&1 || fail "xcrun notarytool is unavailable"
  xcrun -f stapler >/dev/null 2>&1 || fail "xcrun stapler is unavailable"

  bash -n "$ROOT/scripts/macos/build-app.sh"
  bash -n "$ROOT/scripts/macos/build-bundled-app.sh"
  bash -n "$ROOT/scripts/macos/package-pkg.sh"
  bash -n "$ROOT/scripts/macos/package-dmg.sh"
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

check_toolchain

if [[ "${1:-}" == "--check" ]]; then
  echo "macOS release signing/notarization toolchain check passed"
  exit 0
fi

if [[ $# -ne 0 ]]; then
  usage >&2
  exit 2
fi

[[ -n "$APP_IDENTITY" ]] || fail "MACOS_APP_IDENTITY is required"
[[ -n "$INSTALLER_IDENTITY" ]] || fail "MACOS_INSTALLER_IDENTITY is required"
[[ -n "$VERSION" ]] || fail "MACOS_VERSION is required"
if [[ "$VERSION" == *dev* && "${MACOS_ALLOW_DEV_RELEASE:-0}" != "1" ]]; then
  fail "refusing to notarize development version '$VERSION' (set MACOS_ALLOW_DEV_RELEASE=1 to override)"
fi

NOTARY_ARGS=()
if [[ -n "$NOTARY_PROFILE" ]]; then
  NOTARY_ARGS=(--keychain-profile "$NOTARY_PROFILE")
elif [[ -n "$NOTARY_APPLE_ID" && -n "$NOTARY_TEAM_ID" && -n "$NOTARY_PASSWORD" ]]; then
  NOTARY_ARGS=(
    --apple-id "$NOTARY_APPLE_ID"
    --team-id "$NOTARY_TEAM_ID"
    --password "$NOTARY_PASSWORD"
  )
else
  fail "configure MACOS_NOTARY_PROFILE or MACOS_NOTARY_APPLE_ID/MACOS_NOTARY_TEAM_ID/MACOS_NOTARY_PASSWORD"
fi

sign_binary() {
  local path="$1"
  [[ -e "$path" ]] || fail "missing code to sign: $path"
  codesign --force --options runtime --timestamp --sign "$APP_IDENTITY" "$path"
}

sign_bundle() {
  local path="$1"
  [[ -d "$path" ]] || fail "missing bundle to sign: $path"
  codesign --force --options runtime --timestamp --sign "$APP_IDENTITY" "$path"
}

verify_developer_id_app() {
  local details
  details="$(codesign -dv --verbose=4 "$APP" 2>&1)"
  echo "$details" | grep -q '^Authority=Developer ID Application:' || fail "app is not signed with Developer ID Application"
  echo "$details" | grep -q '^Timestamp=' || fail "app signature has no secure timestamp"
  echo "$details" | grep -Eq '^CodeDirectory .*flags=.*runtime' || fail "Hardened Runtime is not enabled"
}

submit_notary() {
  local artifact="$1"
  [[ -f "$artifact" ]] || fail "notarization artifact not found: $artifact"
  echo "==> Notarizing $(basename "$artifact")"
  xcrun notarytool submit "$artifact" "${NOTARY_ARGS[@]}" --wait
}

staple_and_validate() {
  local artifact="$1"
  echo "==> Stapling $(basename "$artifact")"
  xcrun stapler staple "$artifact"
  xcrun stapler validate "$artifact"
}

echo "==> Building self-contained Universal macOS application"
MACOS_VERSION="$VERSION" bash "$ROOT/scripts/macos/build-bundled-app.sh"
[[ -d "$APP" ]] || fail "application build did not produce $APP"

SERVICE_APP="$APP/Contents/Library/LoginItems/LinkVideoServiceHelper.app"
SERVICE_HELPER="$SERVICE_APP/Contents/MacOS/LinkVideoServiceHelper"
URL_APP="$APP/Contents/Library/Helpers/LinkVideoURLHandler.app"
URL_HELPER="$URL_APP/Contents/MacOS/LinkVideoURLHandler"

# Sign inner code first. The parent bundle is signed last; no --deep signing is
# used for release signatures, so every distributed executable has an explicit
# Developer ID + Hardened Runtime + secure timestamp signature.
echo "==> Signing nested macOS code"
for binary in \
  "$APP/Contents/Resources/linkvideo-capture-helper" \
  "$APP/Contents/Resources/linkvideo-workspace-helper" \
  "$APP/Contents/Resources/linkvideo-overlay-helper" \
  "$APP/Contents/Resources/linkvideo-hotkey-helper" \
  "$APP/Contents/MacOS/ffmpeg.exe" \
  "$APP/Contents/MacOS/mediamtx" \
  "$SERVICE_HELPER" \
  "$URL_HELPER"
do
  sign_binary "$binary"
done
sign_bundle "$SERVICE_APP"
sign_bundle "$URL_APP"
sign_bundle "$APP"

codesign --verify --deep --strict --verbose=2 "$APP"
verify_developer_id_app

# Submit a ZIP only as the transport container for the .app. The resulting
# notarization ticket is stapled to the application bundle before packaging.
rm -f "$NOTARY_ZIP"
ditto -c -k --sequesterRsrc --keepParent "$APP" "$NOTARY_ZIP"
submit_notary "$NOTARY_ZIP"
staple_and_validate "$APP"
rm -f "$NOTARY_ZIP"
codesign --verify --deep --strict --verbose=2 "$APP"
spctl --assess --type execute --verbose=4 "$APP"

# Re-create the distributable ZIP after stapling so the extracted app carries
# the notarization ticket even when Gatekeeper cannot reach Apple's service.
rm -f "$ZIP"
ditto -c -k --sequesterRsrc --keepParent "$APP" "$ZIP"

echo "==> Building signed installer package"
MACOS_VERSION="$VERSION" \
MACOS_INSTALLER_IDENTITY="$INSTALLER_IDENTITY" \
  bash "$ROOT/scripts/macos/package-pkg.sh"
[[ -f "$PKG" ]] || fail "signed installer was not created"
pkg_signature="$(pkgutil --check-signature "$PKG" 2>&1)"
echo "$pkg_signature"
echo "$pkg_signature" | grep -q 'Developer ID Installer:' || fail "PKG is not signed with Developer ID Installer"
submit_notary "$PKG"
staple_and_validate "$PKG"
spctl --assess --type install --verbose=4 "$PKG"

echo "==> Building notarized disk image"
MACOS_VERSION="$VERSION" bash "$ROOT/scripts/macos/package-dmg.sh"
[[ -f "$DMG" ]] || fail "DMG was not created"
submit_notary "$DMG"
staple_and_validate "$DMG"

# Final offline-verifiable checks for every distribution format.
xcrun stapler validate "$APP"
xcrun stapler validate "$PKG"
xcrun stapler validate "$DMG"
codesign --verify --deep --strict --verbose=2 "$APP"
pkgutil --check-signature "$PKG"
spctl --assess --type execute --verbose=4 "$APP"
spctl --assess --type install --verbose=4 "$PKG"

echo "macOS production release is signed and notarized:"
echo "  APP: $APP"
echo "  ZIP: $ZIP"
echo "  PKG: $PKG"
echo "  DMG: $DMG"
