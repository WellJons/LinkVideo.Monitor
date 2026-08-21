#!/bin/bash
set -euo pipefail

FFMPEG_VERSION="${FFMPEG_BUNDLE_VERSION:-8.1.2}"
X264_REVISION="b35605ace3ddf7c1a5d67a2eb553f034aef41d55"
X265_VERSION="4.2"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUTPUT="${1:-$ROOT/build/macos/ffmpeg-universal}"
CACHE="${FFMPEG_CACHE_DIR:-$ROOT/build/macos/ffmpeg-source-$FFMPEG_VERSION}"
JOBS="${FFMPEG_BUILD_JOBS:-$(sysctl -n hw.ncpu 2>/dev/null || echo 4)}"

FFMPEG_ARCHIVE="ffmpeg-$FFMPEG_VERSION.tar.xz"
FFMPEG_URL="https://ffmpeg.org/releases/$FFMPEG_ARCHIVE"
FFMPEG_SHA256="464beb5e7bf0c311e68b45ae2f04e9cc2af88851abb4082231742a74d97b524c"
X264_ARCHIVE="x264-$X264_REVISION.tar.bz2"
X264_URL="https://code.videolan.org/videolan/x264/-/archive/$X264_REVISION/$X264_ARCHIVE"
X264_SHA256="6eeb82934e69fd51e043bd8c5b0d152839638d1ce7aa4eea65a3fedcf83ff224"
X265_ARCHIVE="x265_$X265_VERSION.tar.gz"
X265_URL="https://bitbucket.org/multicoreware/x265_git/downloads/$X265_ARCHIVE"
X265_SHA256="40b1ea0453e0309f0eba934e0ddf533f8f6295966679e8894e8f1c1c8d5e1210"

mkdir -p "$CACHE/downloads" "$(dirname "$OUTPUT")"

fetch() {
  local url="$1"
  local target="$2"
  if [[ -s "$target" ]]; then
    return
  fi
  echo "==> Downloading $(basename "$target")"
  curl --fail --location --retry 3 --retry-delay 2 --silent --show-error \
    "$url" --output "$target.tmp"
  mv "$target.tmp" "$target"
}

verify_sha256() {
  local archive="$1"
  local expected="$2"
  local actual
  actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  if [[ "$actual" != "$expected" ]]; then
    echo "Checksum mismatch for $(basename "$archive")" >&2
    echo "expected: $expected" >&2
    echo "actual:   $actual" >&2
    exit 1
  fi
}

ffmpeg_archive="$CACHE/downloads/$FFMPEG_ARCHIVE"
x264_archive="$CACHE/downloads/$X264_ARCHIVE"
x265_archive="$CACHE/downloads/$X265_ARCHIVE"
fetch "$FFMPEG_URL" "$ffmpeg_archive"
fetch "$X264_URL" "$x264_archive"
fetch "$X265_URL" "$x265_archive"
verify_sha256 "$ffmpeg_archive" "$FFMPEG_SHA256"
verify_sha256 "$x264_archive" "$X264_SHA256"
verify_sha256 "$x265_archive" "$X265_SHA256"

build_arch() {
  local arch="$1"
  local ffarch host
  case "$arch" in
    arm64)
      ffarch="aarch64"
      host="aarch64-apple-darwin"
      ;;
    x86_64)
      ffarch="x86_64"
      host="x86_64-apple-darwin"
      ;;
    *)
      echo "Unsupported FFmpeg architecture: $arch" >&2
      exit 2
      ;;
  esac

  local work="$CACHE/build-$arch"
  local prefix="$CACHE/prefix-$arch"
  rm -rf "$work" "$prefix"
  mkdir -p "$work" "$prefix"

  echo "==> x264 $X264_REVISION ($arch, macOS 13)"
  mkdir -p "$work/x264"
  tar -xjf "$x264_archive" -C "$work/x264" --strip-components=1
  (
    cd "$work/x264"
    CC="clang -arch $arch -mmacosx-version-min=13.0" \
    ./configure \
      --prefix="$prefix" \
      --host="$host" \
      --disable-cli \
      --disable-opencl \
      --disable-lsmash \
      --disable-swscale \
      --disable-ffms \
      --disable-shared \
      --enable-static \
      --disable-asm
    make -j"$JOBS"
    make install
  )

  echo "==> x265 $X265_VERSION ($arch, macOS 13)"
  mkdir -p "$work/x265"
  tar -xzf "$x265_archive" -C "$work/x265" --strip-components=1
  cmake \
    -S "$work/x265/source" \
    -B "$work/x265-build" \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="$prefix" \
    -DCMAKE_OSX_ARCHITECTURES="$arch" \
    -DCMAKE_OSX_DEPLOYMENT_TARGET=13.0 \
    -DENABLE_SHARED=OFF \
    -DENABLE_CLI=OFF \
    -DENABLE_ASSEMBLY=OFF
  cmake --build "$work/x265-build" --parallel "$JOBS"
  cmake --install "$work/x265-build"

  echo "==> FFmpeg $FFMPEG_VERSION ($arch, macOS 13)"
  mkdir -p "$work/ffmpeg"
  tar -xJf "$ffmpeg_archive" -C "$work/ffmpeg" --strip-components=1
  (
    cd "$work/ffmpeg"
    export PKG_CONFIG_PATH="$prefix/lib/pkgconfig"
    ./configure \
      --prefix="$work/ffmpeg-install" \
      --target-os=darwin \
      --arch="$ffarch" \
      --cc=clang \
      --disable-debug \
      --disable-doc \
      --disable-ffplay \
      --disable-ffprobe \
      --disable-shared \
      --enable-static \
      --enable-gpl \
      --enable-libx264 \
      --enable-libx265 \
      --enable-videotoolbox \
      --disable-x86asm \
      --pkg-config-flags=--static \
      --extra-cflags="-O2 -arch $arch -mmacosx-version-min=13.0 -I$prefix/include" \
      --extra-ldflags="-arch $arch -mmacosx-version-min=13.0 -L$prefix/lib"
    make -j"$JOBS" ffmpeg
  )

  cp "$work/ffmpeg/ffmpeg" "$CACHE/ffmpeg-$arch"
  chmod +x "$CACHE/ffmpeg-$arch"
  local actual_archs
  actual_archs="$(lipo -archs "$CACHE/ffmpeg-$arch")"
  [[ " $actual_archs " == *" $arch "* ]] || {
    echo "Built FFmpeg for $arch reports: $actual_archs" >&2
    exit 1
  }
}

build_arch arm64
build_arch x86_64

lipo -create \
  "$CACHE/ffmpeg-arm64" \
  "$CACHE/ffmpeg-x86_64" \
  -output "$OUTPUT"
chmod +x "$OUTPUT"

archs="$(lipo -archs "$OUTPUT")"
for required in arm64 x86_64; do
  [[ " $archs " == *" $required "* ]] || {
    echo "Universal FFmpeg misses $required: $archs" >&2
    exit 1
  }
done

version_output="$($OUTPUT -version 2>&1 | head -n 1 || true)"
if [[ "$version_output" != *"ffmpeg version $FFMPEG_VERSION"* ]]; then
  echo "Unexpected FFmpeg version output: $version_output" >&2
  exit 1
fi

encoders="$($OUTPUT -hide_banner -encoders 2>/dev/null)"
for encoder in h264_videotoolbox hevc_videotoolbox libx264 libx265 aac; do
  if ! printf '%s\n' "$encoders" | grep -q "[[:space:]]$encoder[[:space:]]"; then
    echo "Bundled FFmpeg misses required encoder: $encoder" >&2
    exit 1
  fi
done

for arch in arm64 x86_64; do
  build_info="$(xcrun vtool -show-build -arch "$arch" "$OUTPUT" 2>/dev/null || true)"
  minos="$(printf '%s\n' "$build_info" | awk '$1 == "minos" {print $2; exit}')"
  if [[ -z "$minos" ]]; then
    echo "Cannot determine FFmpeg minimum macOS for $arch" >&2
    exit 1
  fi
  major="${minos%%.*}"
  if [[ ! "$major" =~ ^[0-9]+$ ]] || (( major > 13 )); then
    echo "Bundled FFmpeg $arch requires macOS $minos; LinkVideo target is macOS 13" >&2
    exit 1
  fi
done

echo "FFmpeg $FFMPEG_VERSION built from verified sources: $archs"
echo "x264: $X264_REVISION; x265: $X265_VERSION"
echo "Required encoders: h264_videotoolbox hevc_videotoolbox libx264 libx265 aac"
echo "$OUTPUT"
