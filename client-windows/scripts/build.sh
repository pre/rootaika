#!/usr/bin/env bash
# Cross-compiles the combined rootaika.exe for Windows amd64 into dist/ and prints
# its SHA256. Run from any Linux/macOS box with the Go toolchain. This is the build
# half of release.sh, split out so you can produce a binary without publishing.
#
# Usage: scripts/build.sh v1.2.0   (version defaults to "dev")
set -euo pipefail

VERSION="${1:-dev}"
CLIENT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$CLIENT_ROOT/dist"
ASSET="rootaika.exe"
OUT="$DIST/$ASSET"

mkdir -p "$DIST"

echo "Building $ASSET ($VERSION) for windows/amd64..." >&2
(
  cd "$CLIENT_ROOT"
  GOOS=windows GOARCH=amd64 go build \
    -ldflags "-H=windowsgui -X rootaika/client-windows/internal/version.Version=$VERSION" \
    -o "$OUT" ./cmd/rootaika
)

if command -v sha256sum >/dev/null 2>&1; then
  SHA256="$(sha256sum "$OUT" | awk '{print $1}')"
else
  SHA256="$(shasum -a 256 "$OUT" | awk '{print $1}')"
fi
echo "Built $OUT" >&2
echo "  sha256 = $SHA256" >&2

# Emit the bare hash on stdout so callers (release.sh) can capture it.
echo "$SHA256"
