#!/bin/bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD="$ROOT/build/macos-runtime"
FFMPEG="$BUILD/ffmpeg-universal"

mkdir -p "$BUILD"

if [[ -z "${FFMPEG_BINARY:-}" ]]; then
  /bin/bash "$ROOT/scripts/macos/fetch-ffmpeg.sh" "$FFMPEG"
  export FFMPEG_BINARY="$FFMPEG"
fi

exec /bin/bash "$ROOT/scripts/macos/build-app.sh"
