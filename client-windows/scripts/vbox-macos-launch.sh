#!/usr/bin/env bash
# Builds rootaika.exe on macOS/Linux and asks the Windows VirtualBox watcher to
# restart the test client from the shared repository folder.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/vbox-macos-launch.sh [options]

Options:
  --server-url URL          Server URL for the Windows client.
  --client-username USER    Client Basic Auth username.
  --client-password PASS    Client Basic Auth password.
  --version VERSION         Version string baked into rootaika.exe.
  -h, --help                Show this help.

Defaults can also come from ROOTAIKA_SERVER_URL, ROOTAIKA_CLIENT_USERNAME,
ROOTAIKA_CLIENT_PASSWORD, and ROOTAIKA_VERSION.
EOF
}

json_escape() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/\\r}"
  s="${s//$'\t'/\\t}"
  printf '%s' "$s"
}

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CLIENT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REQUEST_DIR="$CLIENT_ROOT/.vbox-launch"
REQUEST_PATH="$REQUEST_DIR/request.json"

SERVER_URL="${ROOTAIKA_SERVER_URL:-http://192.168.68.199:8080}"
CLIENT_USERNAME="${ROOTAIKA_CLIENT_USERNAME:-client}"
CLIENT_PASSWORD="${ROOTAIKA_CLIENT_PASSWORD:-client}"
VERSION="${ROOTAIKA_VERSION:-dev}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server-url)
      SERVER_URL="${2:?missing value for --server-url}"
      shift 2
      ;;
    --client-username)
      CLIENT_USERNAME="${2:?missing value for --client-username}"
      shift 2
      ;;
    --client-password)
      CLIENT_PASSWORD="${2:?missing value for --client-password}"
      shift 2
      ;;
    --version)
      VERSION="${2:?missing value for --version}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

REQUEST_ID="$(date -u '+%Y%m%dT%H%M%SZ')-$$"
CREATED_AT_UTC="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

echo "Building Windows client on this machine..."
SHA256="$("$CLIENT_ROOT/scripts/build.sh" "$VERSION")"

mkdir -p "$REQUEST_DIR"
TMP_REQUEST="$REQUEST_DIR/request.$REQUEST_ID.tmp"
cat >"$TMP_REQUEST" <<EOF
{
  "request_id": "$(json_escape "$REQUEST_ID")",
  "created_at_utc": "$(json_escape "$CREATED_AT_UTC")",
  "version": "$(json_escape "$VERSION")",
  "artifact": "rootaika.exe",
  "sha256": "$(json_escape "$SHA256")",
  "server_url": "$(json_escape "$SERVER_URL")",
  "client_username": "$(json_escape "$CLIENT_USERNAME")",
  "client_password": "$(json_escape "$CLIENT_PASSWORD")"
}
EOF
mv "$TMP_REQUEST" "$REQUEST_PATH"

cat <<EOF
Wrote VirtualBox launch request:
  $REQUEST_PATH

The Windows watcher should restart rootaika from:
  $CLIENT_ROOT/dist/rootaika.exe

If the watcher is not running in Windows yet, start it once from the shared repo:
  powershell -ExecutionPolicy Bypass -File .\\client-windows\\scripts\\vbox-windows-watch.ps1

For autostart inside the VM:
  powershell -ExecutionPolicy Bypass -File .\\client-windows\\scripts\\vbox-windows-watch.ps1 -InstallAutostart
EOF
