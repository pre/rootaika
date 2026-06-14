#!/usr/bin/env bash
# Install the rootaika macOS client as a per-user LaunchAgent.
#
# Usage:
#   ./packaging/install.sh            # build, install binary + LaunchAgent, load it
#   ./packaging/install.sh uninstall  # unload + remove
set -euo pipefail

LABEL="com.rootaika.macclient"
BIN_DST="/usr/local/bin/rootaika-mac"
PLIST_SRC="$(cd "$(dirname "$0")" && pwd)/${LABEL}.plist"
PLIST_DST="${HOME}/Library/LaunchAgents/${LABEL}.plist"
PKG_DIR="$(cd "$(dirname "$0")/.." && pwd)"

uninstall() {
    echo "Unloading LaunchAgent ${LABEL}..."
    launchctl unload "${PLIST_DST}" 2>/dev/null || true
    rm -f "${PLIST_DST}"
    sudo rm -f "${BIN_DST}"
    echo "Uninstalled. (config/db under ~/Library/Application Support/rootaika left intact)"
}

if [[ "${1:-}" == "uninstall" ]]; then
    uninstall
    exit 0
fi

echo "Building release binary..."
( cd "${PKG_DIR}" && swift build -c release )
BUILT="${PKG_DIR}/.build/release/RootaikaMac"

echo "Installing binary to ${BIN_DST} (needs sudo)..."
sudo mkdir -p "$(dirname "${BIN_DST}")"
sudo cp "${BUILT}" "${BIN_DST}"
sudo chmod 755 "${BIN_DST}"

echo "Installing LaunchAgent to ${PLIST_DST}..."
mkdir -p "${HOME}/Library/LaunchAgents"
cp "${PLIST_SRC}" "${PLIST_DST}"

echo "Loading LaunchAgent..."
launchctl unload "${PLIST_DST}" 2>/dev/null || true
launchctl load "${PLIST_DST}"

cat <<'EOF'

Installed. The agent runs in your GUI login session (LaunchAgent) so it can
read idle time, see the frontmost app, and draw the lock overlay.

Grant permissions in System Settings > Privacy & Security:
  - Accessibility   (and/or Input Monitoring) for system idle detection
Then restart the agent:
  launchctl kickstart -k "gui/$(id -u)/com.rootaika.macclient"

Config lives at: ~/Library/Application Support/rootaika/config.json
EOF
