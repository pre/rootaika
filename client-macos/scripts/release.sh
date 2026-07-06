#!/usr/bin/env bash
# Builds the release rootaika-mac binary and publishes it as a public GitHub
# release. Must run on macOS (SwiftPM cannot cross-compile) with `gh`
# authenticated against github.com/pre/rootaika.
#
# The printed version triple is what the daemon's OTA needs: create a client
# version row with it in the server admin UI and assign it to the mac devices.
#
# Usage: scripts/release.sh v1.2.0
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <version-tag>   e.g. $0 v1.2.0" >&2
  exit 2
fi

VERSION="$1"
CLIENT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$CLIENT_ROOT/dist"
ASSET="rootaika-mac"
OUT="$DIST/$ASSET"
INSTALLER="$CLIENT_ROOT/packaging/install.sh"

SHA256="$("$CLIENT_ROOT/scripts/build.sh" "$VERSION")"

if [[ ! -f "$INSTALLER" ]]; then
  echo "install.sh not found at $INSTALLER" >&2
  exit 1
fi

echo "Creating GitHub release $VERSION..."
gh release create "$VERSION" "$OUT" "$INSTALLER" \
  --repo pre/rootaika \
  --title "$VERSION" \
  --notes "rootaika macOS client $VERSION"

cat <<EOF

Release published. To roll it out over the air, add a client version in the
server admin UI with this triple and assign it to the mac devices:

  version  = $VERSION
  artifact = $ASSET
  sha256   = $SHA256
EOF
