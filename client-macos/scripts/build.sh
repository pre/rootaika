#!/usr/bin/env bash
# Builds the release rootaika-mac binary into dist/ and prints its SHA256. Must
# run on macOS (SwiftPM cannot cross-compile from Linux). This is the build half
# of release.sh, split out so you can produce a binary without publishing.
#
# Usage: scripts/build.sh v1.2.0   (version defaults to "dev")
set -euo pipefail

VERSION="${1:-dev}"
CLIENT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$CLIENT_ROOT/dist"
ASSET="rootaika-mac"
OUT="$DIST/$ASSET"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: the macOS client builds only on macOS (Swift can't cross-compile here)" >&2
  exit 1
fi

mkdir -p "$DIST"

# Inject the version label into Version.swift for the build (the SwiftPM
# equivalent of the Windows ldflags injection) and restore the source after.
VERSION_FILE="$CLIENT_ROOT/Sources/RootaikaMac/Version.swift"
cp "$VERSION_FILE" "$VERSION_FILE.orig"
trap 'mv "$VERSION_FILE.orig" "$VERSION_FILE"' EXIT
sed -i '' "s/static let current = \"dev\"/static let current = \"$VERSION\"/" "$VERSION_FILE"

echo "Building $ASSET ($VERSION) release..." >&2
(
  cd "$CLIENT_ROOT"
  swift build -c release
)
cp "$CLIENT_ROOT/.build/release/RootaikaMac" "$OUT"

SHA256="$(shasum -a 256 "$OUT" | awk '{print $1}')"
echo "Built $OUT" >&2
echo "  sha256 = $SHA256" >&2

# Emit the bare hash on stdout so callers (release.sh) can capture it.
echo "$SHA256"
