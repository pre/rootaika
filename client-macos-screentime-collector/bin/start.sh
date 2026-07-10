#!/bin/sh
# Installs and starts the collector as a LaunchAgent for the current user:
# generates the plist from the environment, (re)loads it and runs immediately
# (RunAtLoad), then every 30 minutes. Re-run after changing configuration.
set -eu

LABEL=fi.rootaika.ios-screentime
DIR=$(cd "$(dirname "$0")/.." && pwd)
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
LOG="$HOME/Library/Logs/rootaika-ios-screentime.log"

# config.env (gitignored: member names + credentials) overrides the environment
[ -f "$DIR/config.env" ] && . "$DIR/config.env"

if [ -z "${ROOTAIKA_MEMBERS:-}" ]; then
	echo "usage: copy $DIR/config.env.example to config.env, fill it in and re-run $0" >&2
	exit 2
fi

xml() { printf '%s' "$1" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g'; }

mkdir -p "$HOME/Library/LaunchAgents" "$HOME/Library/Logs"

cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>/usr/bin/caffeinate</string>
		<string>-i</string>
		<string>/usr/bin/python3</string>
		<string>$(xml "$DIR/collector.py")</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>ROOTAIKA_SERVER_URL</key>
		<string>$(xml "${ROOTAIKA_SERVER_URL:-http://192.168.68.199:8080}")</string>
		<key>ROOTAIKA_CLIENT_USERNAME</key>
		<string>$(xml "${ROOTAIKA_CLIENT_USERNAME:-client}")</string>
		<key>ROOTAIKA_CLIENT_PASSWORD</key>
		<string>$(xml "${ROOTAIKA_CLIENT_PASSWORD:-client}")</string>
		<key>ROOTAIKA_MEMBERS</key>
		<string>$(xml "$ROOTAIKA_MEMBERS")</string>
		<key>ROOTAIKA_DAYS</key>
		<string>$(xml "${ROOTAIKA_DAYS:-7}")</string>
	</dict>
	<key>StartInterval</key>
	<integer>1800</integer>
	<key>RunAtLoad</key>
	<true/>
	<key>StandardOutPath</key>
	<string>$(xml "$LOG")</string>
	<key>StandardErrorPath</key>
	<string>$(xml "$LOG")</string>
	<key>LimitLoadToSessionType</key>
	<string>Aqua</string>
</dict>
</plist>
EOF

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"

echo "loaded $LABEL (runs now, then every 30 min)"
echo "log:    tail -f $LOG"
echo "status: launchctl print gui/$(id -u)/$LABEL"
echo "stop:   launchctl bootout gui/$(id -u)/$LABEL"
