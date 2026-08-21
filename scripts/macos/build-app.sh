#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="$ROOT/build/macos"
APP="$BUILD/LinkVideo.Monitor.app"
VERSION="${MACOS_VERSION:-0.1.0-dev}"

rm -rf "$BUILD"
mkdir -p "$BUILD" "$APP/Contents/MacOS" "$APP/Contents/Resources"

for arch in arm64 x86_64; do
  goarch="$arch"
  if [[ "$arch" == "x86_64" ]]; then
    goarch="amd64"
  fi

  echo "==> Go app $arch (GOARCH=$goarch)"
  CGO_ENABLED=0 GOOS=darwin GOARCH="$goarch" \
    go build -trimpath -o "$BUILD/LinkVideo.Monitor-$arch" "$ROOT/cmd/linkvideo-monitor"

  echo "==> ScreenCaptureKit helper $arch"
  xcrun swiftc -O -whole-module-optimization \
    -target "$arch-apple-macos13.0" \
    "$ROOT/native/macos/screencapture/main.swift" \
    -framework ScreenCaptureKit \
    -framework CoreGraphics \
    -framework CoreMedia \
    -framework CoreVideo \
    -o "$BUILD/linkvideo-capture-helper-$arch"
done

lipo -create \
  "$BUILD/LinkVideo.Monitor-arm64" \
  "$BUILD/LinkVideo.Monitor-x86_64" \
  -output "$APP/Contents/MacOS/LinkVideo.Monitor"

lipo -create \
  "$BUILD/linkvideo-capture-helper-arm64" \
  "$BUILD/linkvideo-capture-helper-x86_64" \
  -output "$APP/Contents/Resources/linkvideo-capture-helper"

cp "$ROOT/packaging/macos/Info.plist" "$APP/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $VERSION" "$APP/Contents/Info.plist"

if [[ -n "${FFMPEG_BINARY:-}" ]]; then
  cp "$FFMPEG_BINARY" "$APP/Contents/Resources/ffmpeg"
  chmod +x "$APP/Contents/Resources/ffmpeg"
fi

chmod +x "$APP/Contents/MacOS/LinkVideo.Monitor" "$APP/Contents/Resources/linkvideo-capture-helper"

# CI/development signing only. Release builds will use Developer ID + notarization.
codesign --force --sign - "$APP/Contents/Resources/linkvideo-capture-helper"
codesign --force --deep --sign - "$APP"

echo "Built: $APP"
echo "App architectures: $(lipo -archs "$APP/Contents/MacOS/LinkVideo.Monitor")"
echo "Helper architectures: $(lipo -archs "$APP/Contents/Resources/linkvideo-capture-helper")"
