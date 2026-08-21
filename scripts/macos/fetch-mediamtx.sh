#!/bin/bash
set -euo pipefail

VERSION="${MEDIAMTX_VERSION:-1.20.0}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUTPUT="${1:-$ROOT/build/macos/mediamtx-universal}"
CACHE="${MEDIAMTX_CACHE_DIR:-$ROOT/build/macos/mediamtx-$VERSION}"
BASE_URL="https://github.com/bluenviron/mediamtx/releases/download/v$VERSION"
CHECKSUMS="$CACHE/checksums.sha256"

mkdir -p "$CACHE" "$(dirname "$OUTPUT")"

fetch() {
  local url="$1"
  local target="$2"
  if [[ -s "$target" ]]; then
    return
  fi
  curl --fail --location --retry 3 --retry-delay 2 --silent --show-error \
    "$url" --output "$target.tmp"
  mv "$target.tmp" "$target"
}

fetch "$BASE_URL/checksums.sha256" "$CHECKSUMS"

verify_archive() {
  local archive="$1"
  local name
  name="$(basename "$archive")"
  local expected
  expected="$(awk -v n="$name" '$2 == n || $2 == "*" n {print $1; exit}' "$CHECKSUMS")"
  if [[ ! "$expected" =~ ^[0-9a-fA-F]{64}$ ]]; then
    echo "MediaMTX checksum not found for $name" >&2
    exit 1
  fi
  local actual
  actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  local actual_lower expected_lower
  actual_lower="$(printf '%s' "$actual" | tr '[:upper:]' '[:lower:]')"
  expected_lower="$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')"
  if [[ "$actual_lower" != "$expected_lower" ]]; then
    echo "MediaMTX checksum mismatch for $name" >&2
    echo "expected: $expected" >&2
    echo "actual:   $actual" >&2
    exit 1
  fi

  if command -v gh >/dev/null 2>&1 && [[ -n "${GH_TOKEN:-${GITHUB_TOKEN:-}}" ]]; then
    echo "==> Verifying GitHub attestation for $name"
    gh attestation verify "$archive" --repo bluenviron/mediamtx >/dev/null
  fi
}

for arch in arm64 amd64; do
  archive="$CACHE/mediamtx_v${VERSION}_darwin_${arch}.tar.gz"
  fetch "$BASE_URL/$(basename "$archive")" "$archive"
  verify_archive "$archive"

  extract_dir="$CACHE/$arch"
  rm -rf "$extract_dir"
  mkdir -p "$extract_dir"
  tar -xzf "$archive" -C "$extract_dir" mediamtx
  chmod +x "$extract_dir/mediamtx"
done

lipo -create \
  "$CACHE/arm64/mediamtx" \
  "$CACHE/amd64/mediamtx" \
  -output "$OUTPUT"
chmod +x "$OUTPUT"

archs="$(lipo -archs "$OUTPUT")"
for required in arm64 x86_64; do
  [[ " $archs " == *" $required "* ]] || {
    echo "Universal MediaMTX misses $required: $archs" >&2
    exit 1
  }
done

version_output="$($OUTPUT --version 2>&1 | head -n 1 || true)"
if [[ "$version_output" != *"$VERSION"* ]]; then
  echo "Unexpected MediaMTX version output: $version_output" >&2
  exit 1
fi

echo "MediaMTX $VERSION Universal: $archs"
echo "$OUTPUT"
