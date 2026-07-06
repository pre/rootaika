#!/usr/bin/env bash
# Install the rootaika macOS client: a root LaunchDaemon (config, buffering,
# upload, watchdog) plus a per-user LaunchAgent (observation + lock overlay),
# mirroring the Windows service/agent split. Run as a normal user; sudo is
# used inline for the privileged steps.
#
# Usage:
#   ./packaging/install.sh            # build, install binary + daemon + agent
#   ./packaging/install.sh uninstall  # unload + remove
set -euo pipefail

DAEMON_LABEL="com.rootaika.daemon"
AGENT_LABEL="com.rootaika.agent"
LEGACY_LABEL="com.rootaika.macclient"
BIN_DST="/usr/local/bin/rootaika-mac"
PKG_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DAEMON_PLIST_SRC="${PKG_DIR}/packaging/${DAEMON_LABEL}.plist"
AGENT_PLIST_SRC="${PKG_DIR}/packaging/${AGENT_LABEL}.plist"
DAEMON_PLIST_DST="/Library/LaunchDaemons/${DAEMON_LABEL}.plist"
AGENT_PLIST_DST="/Library/LaunchAgents/${AGENT_LABEL}.plist"
DATA_DIR="/Library/Application Support/rootaika"
LEGACY_CONFIG="${HOME}/Library/Application Support/rootaika/config.json"
GUI_DOMAIN="gui/$(id -u)"

uninstall() {
    echo "Unloading ${DAEMON_LABEL} and ${AGENT_LABEL}..."
    sudo launchctl bootout "system/${DAEMON_LABEL}" 2>/dev/null || true
    launchctl bootout "${GUI_DOMAIN}/${AGENT_LABEL}" 2>/dev/null || true
    sudo rm -f "${DAEMON_PLIST_DST}" "${AGENT_PLIST_DST}" "${BIN_DST}"
    echo "Uninstalled. (config/db under ${DATA_DIR} left intact)"
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

echo "Preparing ${DATA_DIR}..."
sudo mkdir -p "${DATA_DIR}"
sudo chmod 755 "${DATA_DIR}"
# Preserve the device identity from a pre-split per-user install.
if sudo test ! -f "${DATA_DIR}/config.json" && [[ -f "${LEGACY_CONFIG}" ]]; then
    echo "Migrating existing config (client_id) from ${LEGACY_CONFIG}..."
    sudo cp "${LEGACY_CONFIG}" "${DATA_DIR}/config.json"
    sudo chown root:wheel "${DATA_DIR}/config.json"
    sudo chmod 600 "${DATA_DIR}/config.json"
fi

echo "Removing legacy per-user LaunchAgent (${LEGACY_LABEL}) if present..."
launchctl bootout "${GUI_DOMAIN}/${LEGACY_LABEL}" 2>/dev/null || true
rm -f "${HOME}/Library/LaunchAgents/${LEGACY_LABEL}.plist"

echo "Installing LaunchDaemon + LaunchAgent plists..."
sudo cp "${DAEMON_PLIST_SRC}" "${DAEMON_PLIST_DST}"
sudo cp "${AGENT_PLIST_SRC}" "${AGENT_PLIST_DST}"
sudo chown root:wheel "${DAEMON_PLIST_DST}" "${AGENT_PLIST_DST}"
sudo chmod 644 "${DAEMON_PLIST_DST}" "${AGENT_PLIST_DST}"

echo "Loading daemon (system) and agent (${GUI_DOMAIN})..."
sudo launchctl bootout "system/${DAEMON_LABEL}" 2>/dev/null || true
sudo launchctl bootstrap system "${DAEMON_PLIST_DST}"
launchctl bootout "${GUI_DOMAIN}/${AGENT_LABEL}" 2>/dev/null || true
sudo launchctl bootstrap "${GUI_DOMAIN}" "${AGENT_PLIST_DST}"

cat <<'EOF'

Installed.
  - com.rootaika.daemon (root):   reporting, config, event buffer, watchdog
  - com.rootaika.agent (user):    idle/frontmost observation + lock overlay

Grant permissions in System Settings > Privacy & Security:
  - Accessibility   (and/or Input Monitoring) for system idle detection
Then restart the agent:
  launchctl kickstart -k "gui/$(id -u)/com.rootaika.agent"

Config lives at: /Library/Application Support/rootaika/config.json (root-only).
Server URL/credentials: edit that file with sudo, then
  sudo launchctl kickstart -k system/com.rootaika.daemon
EOF
