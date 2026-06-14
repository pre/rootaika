#!/usr/bin/env bash
# Cross-compiles the combined rootaika.exe for Windows amd64, publishes it as a
# public GitHub release, and prints the version triple to paste into the admin
# UI. Run from a Linux box with `gh` authenticated against github.com/pre/rootaika.
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
ASSET="rootaika.exe"
OUT="$DIST/$ASSET"
INSTALLER="$CLIENT_ROOT/scripts/install.ps1"

SHA256="$("$CLIENT_ROOT/scripts/build.sh" "$VERSION")"

if [[ ! -f "$INSTALLER" ]]; then
  echo "install.ps1 not found at $INSTALLER" >&2
  exit 1
fi

echo "Creating GitHub release $VERSION..."
gh release create "$VERSION" "$OUT" "$INSTALLER" \
  --repo pre/rootaika \
  --title "$VERSION" \
  --notes "rootaika Windows client $VERSION"

cat <<EOF

Release published. Paste this triple into the admin settings (global version) or
a per-device override:

  desired_client_version = $VERSION
  client_artifact_name   = $ASSET
  client_sha256          = $SHA256
EOF
