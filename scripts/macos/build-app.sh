#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="$ROOT/build/macos"
APP="$BUILD/LinkVideo.Monitor.app"
SERVICE_APP="$APP/Contents/Library/LoginItems/LinkVideoServiceHelper.app"
SERVICE_HELPER="$SERVICE_APP/Contents/MacOS/LinkVideoServiceHelper"
SERVICE_BUNDLE_ID="ru.linkvideo.monitor.service-helper"
WORKSPACE_HELPER="$APP/Contents/Resources/linkvideo-workspace-helper"
OVERLAY_HELPER="$APP/Contents/Resources/linkvideo-overlay-helper"
HOTKEY_HELPER="$APP/Contents/Resources/linkvideo-hotkey-helper"
VERSION="${MACOS_VERSION:-0.1.0-dev}"
BUNDLE_VERSION="${VERSION%%[-+]*}"
BUILD_NUMBER="${MACOS_BUILD_NUMBER:-1}"
FFMPEG_TARGET="$APP/Contents/MacOS/ffmpeg.exe"
MEDIAMTX_TARGET="$APP/Contents/MacOS/mediamtx"

rm -rf "$BUILD"
mkdir -p \
  "$BUILD" \
  "$APP/Contents/MacOS" \
  "$APP/Contents/Resources" \
  "$SERVICE_APP/Contents/MacOS"

for arch in arm64 x86_64; do
  goarch="$arch"
  if [[ "$arch" == "x86_64" ]]; then
    goarch="amd64"
  fi

  echo "==> Go app $arch (GOARCH=$goarch, version=$VERSION, CGO=1)"
  CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath \
      -ldflags="-s -w -X main.platformBuildVersion=$VERSION" \
      -o "$BUILD/LinkVideo.Monitor-$arch" "$ROOT/cmd/linkvideo-monitor"

  echo "==> ScreenCaptureKit helper $arch"
  xcrun swiftc -O -whole-module-optimization \
    -target "$arch-apple-macos13.0" \
    "$ROOT/native/macos/screencapture/main.swift" \
    -framework ScreenCaptureKit \
    -framework CoreGraphics \
    -framework CoreMedia \
    -framework CoreVideo \
    -framework AVFoundation \
    -framework AudioToolbox \
    -o "$BUILD/linkvideo-capture-helper-$arch"

  echo "==> Workspace wake helper $arch"
  xcrun swiftc -O -whole-module-optimization \
    -target "$arch-apple-macos13.0" \
    "$ROOT/native/macos/workspace/main.swift" \
    -framework AppKit \
    -o "$BUILD/linkvideo-workspace-helper-$arch"

  echo "==> Recording overlay helper $arch"
  xcrun swiftc -O -whole-module-optimization \
    -target "$arch-apple-macos13.0" \
    "$ROOT/native/macos/overlay/main.swift" \
    -framework AppKit \
    -framework CoreGraphics \
    -o "$BUILD/linkvideo-overlay-helper-$arch"

  echo "==> Global microphone hotkey helper $arch"
  xcrun clang -O2 \
    -arch "$arch" \
    -mmacosx-version-min=13.0 \
    "$ROOT/native/macos/hotkeys/main.c" \
    -framework Carbon \
    -o "$BUILD/linkvideo-hotkey-helper-$arch"

  echo "==> Login item launcher $arch"
  xcrun swiftc -O -whole-module-optimization \
    -target "$arch-apple-macos13.0" \
    "$ROOT/native/macos/servicemanagement/main.swift" \
    -o "$BUILD/LinkVideoServiceHelper-$arch"
done

lipo -create \
  "$BUILD/LinkVideo.Monitor-arm64" \
  "$BUILD/LinkVideo.Monitor-x86_64" \
  -output "$APP/Contents/MacOS/LinkVideo.Monitor"

lipo -create \
  "$BUILD/linkvideo-capture-helper-arm64" \
  "$BUILD/linkvideo-capture-helper-x86_64" \
  -output "$APP/Contents/Resources/linkvideo-capture-helper"

lipo -create \
  "$BUILD/linkvideo-workspace-helper-arm64" \
  "$BUILD/linkvideo-workspace-helper-x86_64" \
  -output "$WORKSPACE_HELPER"

lipo -create \
  "$BUILD/linkvideo-overlay-helper-arm64" \
  "$BUILD/linkvideo-overlay-helper-x86_64" \
  -output "$OVERLAY_HELPER"

lipo -create \
  "$BUILD/linkvideo-hotkey-helper-arm64" \
  "$BUILD/linkvideo-hotkey-helper-x86_64" \
  -output "$HOTKEY_HELPER"

lipo -create \
  "$BUILD/LinkVideoServiceHelper-arm64" \
  "$BUILD/LinkVideoServiceHelper-x86_64" \
  -output "$SERVICE_HELPER"

cp "$ROOT/packaging/macos/Info.plist" "$APP/Contents/Info.plist"
cp "$ROOT/packaging/macos/ServiceHelper-Info.plist" "$SERVICE_APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $BUNDLE_VERSION" "$APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $BUILD_NUMBER" "$APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $BUNDLE_VERSION" "$SERVICE_APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $BUILD_NUMBER" "$SERVICE_APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist" >/dev/null
plutil -lint "$SERVICE_APP/Contents/Info.plist" >/dev/null

actual_service_id="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$SERVICE_APP/Contents/Info.plist")"
if [[ "$actual_service_id" != "$SERVICE_BUNDLE_ID" ]]; then
  echo "Login item bundle id mismatch: $actual_service_id" >&2
  exit 1
fi

if [[ -n "${FFMPEG_BINARY:-}" ]]; then
  cp "$FFMPEG_BINARY" "$FFMPEG_TARGET"
else
  cat > "$FFMPEG_TARGET" <<'EOF'
#!/bin/bash
set -e

if [[ -n "${LINKVIDEO_FFMPEG:-}" && -x "${LINKVIDEO_FFMPEG}" ]]; then
  exec "${LINKVIDEO_FFMPEG}" "$@"
fi

for candidate in /opt/homebrew/bin/ffmpeg /usr/local/bin/ffmpeg; do
  if [[ -x "$candidate" ]]; then
    exec "$candidate" "$@"
  fi
done

if command -v ffmpeg >/dev/null 2>&1; then
  exec "$(command -v ffmpeg)" "$@"
fi

echo "LinkVideo Monitor: FFmpeg не найден. Для development-сборки установите: brew install ffmpeg" >&2
exit 127
EOF
fi

# Keep MediaMTX platform-specific as well. Production builds should pass a
# signed Universal MEDIAMTX_BINARY. Development builds discover Homebrew/PATH.
if [[ -n "${MEDIAMTX_BINARY:-}" ]]; then
  cp "$MEDIAMTX_BINARY" "$MEDIAMTX_TARGET"
else
  cat > "$MEDIAMTX_TARGET" <<'EOF'
#!/bin/bash
set -e

if [[ -n "${LINKVIDEO_MEDIAMTX:-}" && -x "${LINKVIDEO_MEDIAMTX}" ]]; then
  exec "${LINKVIDEO_MEDIAMTX}" "$@"
fi

for candidate in /opt/homebrew/bin/mediamtx /usr/local/bin/mediamtx; do
  if [[ -x "$candidate" ]]; then
    exec "$candidate" "$@"
  fi
done

if command -v mediamtx >/dev/null 2>&1; then
  resolved="$(command -v mediamtx)"
  if [[ "$resolved" != "$0" ]]; then
    exec "$resolved" "$@"
  fi
fi

echo "LinkVideo Monitor: MediaMTX не найден. Для development-сборки установите: brew install mediamtx" >&2
exit 127
EOF
fi

chmod +x \
  "$APP/Contents/MacOS/LinkVideo.Monitor" \
  "$APP/Contents/MacOS/ffmpeg.exe" \
  "$MEDIAMTX_TARGET" \
  "$APP/Contents/Resources/linkvideo-capture-helper" \
  "$WORKSPACE_HELPER" \
  "$OVERLAY_HELPER" \
  "$HOTKEY_HELPER" \
  "$SERVICE_HELPER"

# CI/development signing only. Release builds will use Developer ID + notarization.
codesign --force --sign - "$APP/Contents/Resources/linkvideo-capture-helper"
codesign --force --sign - "$WORKSPACE_HELPER"
codesign --force --sign - "$OVERLAY_HELPER"
codesign --force --sign - "$HOTKEY_HELPER"
codesign --force --sign - "$SERVICE_HELPER"
codesign --force --deep --sign - "$SERVICE_APP"
if file "$FFMPEG_TARGET" | grep -q 'Mach-O'; then
  codesign --force --sign - "$FFMPEG_TARGET"
fi
if file "$MEDIAMTX_TARGET" | grep -q 'Mach-O'; then
  codesign --force --sign - "$MEDIAMTX_TARGET"
fi
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$SERVICE_APP"
codesign --verify --deep --strict "$APP"

app_archs="$(lipo -archs "$APP/Contents/MacOS/LinkVideo.Monitor")"
capture_archs="$(lipo -archs "$APP/Contents/Resources/linkvideo-capture-helper")"
workspace_archs="$(lipo -archs "$WORKSPACE_HELPER")"
overlay_archs="$(lipo -archs "$OVERLAY_HELPER")"
hotkey_archs="$(lipo -archs "$HOTKEY_HELPER")"
service_archs="$(lipo -archs "$SERVICE_HELPER")"
for required in arm64 x86_64; do
  [[ " $app_archs " == *" $required "* ]] || { echo "Main app misses $required" >&2; exit 1; }
  [[ " $capture_archs " == *" $required "* ]] || { echo "Capture helper misses $required" >&2; exit 1; }
  [[ " $workspace_archs " == *" $required "* ]] || { echo "Workspace helper misses $required" >&2; exit 1; }
  [[ " $overlay_archs " == *" $required "* ]] || { echo "Overlay helper misses $required" >&2; exit 1; }
  [[ " $hotkey_archs " == *" $required "* ]] || { echo "Hotkey helper misses $required" >&2; exit 1; }
  [[ " $service_archs " == *" $required "* ]] || { echo "Login item misses $required" >&2; exit 1; }
done

# Probe the ServiceManagement bridge from the actual main executable. Ad-hoc
# CI builds can legitimately report not-found because they are not installed
# and signed with the production Developer ID. Production/release jobs can set
# MACOS_REQUIRE_SERVICE_STATUS=1 to make that state fatal.
startup_status="$("$APP/Contents/MacOS/LinkVideo.Monitor" --startup-status)"
if [[ "$startup_status" == "unknown" ]]; then
  echo "Unknown ServiceManagement login item status" >&2
  exit 1
fi
if [[ "$startup_status" == "not-found" ]]; then
  if [[ "${MACOS_REQUIRE_SERVICE_STATUS:-0}" == "1" ]]; then
    echo "ServiceManagement cannot resolve production login item" >&2
    exit 1
  fi
  echo "Ad-hoc development build: ServiceManagement status is not-found; production signing will re-check registration"
fi

echo "Built: $APP"
echo "Release version: $VERSION"
echo "Bundle version: $BUNDLE_VERSION ($BUILD_NUMBER)"
echo "App architectures: $app_archs"
echo "Capture helper architectures: $capture_archs"
echo "Workspace helper architectures: $workspace_archs"
echo "Overlay helper architectures: $overlay_archs"
echo "Hotkey helper architectures: $hotkey_archs"
echo "Login item architectures: $service_archs"
echo "Autostart login item status: $startup_status"
