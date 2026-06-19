#!/usr/bin/env bash
# Builds rootaika.exe on macOS/Linux and asks the Windows VirtualBox watcher to
# restart the test client from the shared repository folder.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/vbox-macos-launch.sh [options]

Options:
  --server-url URL          Server URL for the Windows client.
  --server-port PORT        Port used when auto-detecting the Mac host URL.
  --client-username USER    Client Basic Auth username.
  --client-password PASS    Client Basic Auth password.
  --version VERSION         Version string baked into rootaika.exe.
  -h, --help                Show this help.

Defaults can also come from ROOTAIKA_SERVER_URL, ROOTAIKA_SERVER_PORT,
ROOTAIKA_CLIENT_USERNAME, ROOTAIKA_CLIENT_PASSWORD, and ROOTAIKA_VERSION.
EOF
}

detect_host_ip() {
  local iface=""
  local ip=""

  if command -v route >/dev/null 2>&1; then
    iface="$(route -n get default 2>/dev/null | awk '/interface:/{print $2; exit}')"
  fi
  if [[ -n "$iface" ]] && command -v ipconfig >/dev/null 2>&1; then
    ip="$(ipconfig getifaddr "$iface" 2>/dev/null || true)"
  fi
  if [[ -z "$ip" ]] && command -v ifconfig >/dev/null 2>&1; then
    ip="$(ifconfig 2>/dev/null | awk '/inet / && $2 != "127.0.0.1" && $2 !~ /^169[.]254[.]/{print $2; exit}')"
  fi
  printf '%s' "$ip"
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

SERVER_URL="${ROOTAIKA_SERVER_URL:-}"
SERVER_PORT="${ROOTAIKA_SERVER_PORT:-8080}"
CLIENT_USERNAME="${ROOTAIKA_CLIENT_USERNAME:-client}"
CLIENT_PASSWORD="${ROOTAIKA_CLIENT_PASSWORD:-client}"
VERSION="${ROOTAIKA_VERSION:-dev}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server-url)
      SERVER_URL="${2:?missing value for --server-url}"
      shift 2
      ;;
    --server-port)
      SERVER_PORT="${2:?missing value for --server-port}"
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

if [[ -z "$SERVER_URL" ]]; then
  HOST_IP="$(detect_host_ip)"
  if [[ -z "$HOST_IP" ]]; then
    echo "could not auto-detect the Mac host IP, pass --server-url http://IP:PORT" >&2
    exit 2
  fi
  SERVER_URL="http://$HOST_IP:$SERVER_PORT"
fi

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

Server URL sent to Windows:
  $SERVER_URL

If the watcher is not running in Windows yet, start it once from the shared repo:
  powershell -ExecutionPolicy Bypass -File .\\client-windows\\scripts\\vbox-windows-watch.ps1

For autostart inside the VM:
  powershell -ExecutionPolicy Bypass -File .\\client-windows\\scripts\\vbox-windows-watch.ps1 -InstallAutostart
EOF
