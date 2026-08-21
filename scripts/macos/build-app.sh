#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="$ROOT/build/macos"
APP="$BUILD/LinkVideo.Monitor.app"
VERSION="${MACOS_VERSION:-0.1.0-dev}"
BUNDLE_VERSION="${VERSION%%[-+]*}"
BUILD_NUMBER="${MACOS_BUILD_NUMBER:-1}"
FFMPEG_TARGET="$APP/Contents/MacOS/ffmpeg.exe"
SERVICE_HELPER="$APP/Contents/Resources/linkvideo-service-helper"
AGENT_PLIST="$APP/Contents/Library/LaunchAgents/ru.linkvideo.monitor.autostart.plist"

rm -rf "$BUILD"
mkdir -p "$BUILD" "$APP/Contents/MacOS" "$APP/Contents/Resources" "$APP/Contents/Library/LaunchAgents"

for arch in arm64 x86_64; do
  goarch="$arch"
  if [[ "$arch" == "x86_64" ]]; then
    goarch="amd64"
  fi

  echo "==> Go app $arch (GOARCH=$goarch, version=$VERSION)"
  CGO_ENABLED=0 GOOS=darwin GOARCH="$goarch" \
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

  echo "==> ServiceManagement helper $arch"
  xcrun swiftc -O -whole-module-optimization \
    -target "$arch-apple-macos13.0" \
    "$ROOT/native/macos/servicemanagement/main.swift" \
    -framework ServiceManagement \
    -o "$BUILD/linkvideo-service-helper-$arch"
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
  "$BUILD/linkvideo-service-helper-arm64" \
  "$BUILD/linkvideo-service-helper-x86_64" \
  -output "$SERVICE_HELPER"

cp "$ROOT/packaging/macos/Info.plist" "$APP/Contents/Info.plist"
cp "$ROOT/packaging/macos/ru.linkvideo.monitor.autostart.plist" "$AGENT_PLIST"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $BUNDLE_VERSION" "$APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $BUILD_NUMBER" "$APP/Contents/Info.plist"
plutil -lint "$AGENT_PLIST" >/dev/null

# The existing common configuration still uses the historical executable name
# "ffmpeg.exe". On macOS we intentionally provide that name inside Contents/MacOS
# so the shared resolveExecutable() path works without Windows-specific changes.
# Release builds should pass FFMPEG_BINARY pointing to the bundled Universal
# FFmpeg binary. Development builds use a tiny launcher that discovers Homebrew.
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

chmod +x \
  "$APP/Contents/MacOS/LinkVideo.Monitor" \
  "$APP/Contents/MacOS/ffmpeg.exe" \
  "$APP/Contents/Resources/linkvideo-capture-helper" \
  "$SERVICE_HELPER"

# CI/development signing only. Release builds will use Developer ID + notarization.
codesign --force --sign - "$APP/Contents/Resources/linkvideo-capture-helper"
codesign --force --sign - "$SERVICE_HELPER"
if file "$FFMPEG_TARGET" | grep -q 'Mach-O'; then
  codesign --force --sign - "$FFMPEG_TARGET"
fi
codesign --force --deep --sign - "$APP"

startup_status="$($SERVICE_HELPER --startup-status)"
if [[ "$startup_status" == "not-found" || "$startup_status" == "unknown" ]]; then
  echo "ServiceManagement cannot resolve bundled LaunchAgent: $startup_status" >&2
  exit 1
fi

echo "Built: $APP"
echo "Release version: $VERSION"
echo "Bundle version: $BUNDLE_VERSION ($BUILD_NUMBER)"
echo "App architectures: $(lipo -archs "$APP/Contents/MacOS/LinkVideo.Monitor")"
echo "Capture helper architectures: $(lipo -archs "$APP/Contents/Resources/linkvideo-capture-helper")"
echo "Service helper architectures: $(lipo -archs "$SERVICE_HELPER")"
echo "Autostart agent status: $startup_status"
