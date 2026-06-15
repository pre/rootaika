#!/usr/bin/env bash
# flash-sound.sh — put the warning MP3 onto the board's LittleFS WITHOUT going
# over WiFi, preserving the existing data (devices, events, settings).
#
# Why this exists: the RP2040<->ESP-AT WiFi link is ~11 KB/s and overruns on a
# large multipart upload, so POST /admin/settings/warning-sound is unreliable
# above a few tens of KB. Flashing the file straight into the LittleFS partition
# over USB bypasses the radio entirely.
#
# What it does:
#   1. Reads the board's current 4 MB LittleFS partition over USB (picotool save).
#   2. Unpacks it, drops in warning.mp3, and bumps settings.json (soundVer etc.)
#      so the firmware serves it. Existing devices/events/settings are kept.
#   3. Repacks the image and writes it back to the FS partition (picotool load).
#
# The firmware UF2 is NOT touched, so this is independent of `make flash`; the
# MP3 survives every later `make flash` (the sketch lives at the start of flash,
# the FS partition lives near the end before the Arduino EEPROM reserve).
#
# REQUIREMENTS:
#   - macOS/Linux with the rp2040 Arduino core installed (picotool + mklittlefs
#     ship with it); this script finds them automatically.
#   - The board in BOOTSEL mode: unplug USB, hold the BOOTSEL button, replug, so
#     an "RPI-RP2" disk appears. picotool only sees the flash in this mode.
#
# USAGE:
#   scripts/flash-sound.sh /path/to/liike.mp3
#   FS=2097152 scripts/flash-sound.sh sound.mp3      # 2 MB FS layout instead of 4 MB
#
set -euo pipefail

MP3="${1:-}"
FS="${FS:-4194304}"                 # LittleFS partition size, must match build FQBN
FLASH_TOTAL="${FLASH:-8388608}"     # total addressed flash
EEPROM_BYTES="${EEPROM:-4096}"      # RP2040 Arduino core reserves this at flash end
# The FS partition sits before the Arduino EEPROM reserve (XIP base 0x10000000).
FS_OFFSET=$(printf '0x%x' $((0x10000000 + FLASH_TOTAL - EEPROM_BYTES - FS)))

# Firmware's cap (storage.h maxWarningSoundBytesRP = 10 MB) and the filename it
# serves from (handleWarningSound opens /warning.mp3).
SOUND_NAME_ON_FS="warning.mp3"

if [ -z "$MP3" ] || [ ! -f "$MP3" ]; then
  echo "usage: $0 /path/to/sound.mp3" >&2
  exit 1
fi

# --- locate picotool + mklittlefs from the rp2040 core (or PATH) ---------------
find_tool() {
  local name="$1"
  if command -v "$name" >/dev/null 2>&1; then command -v "$name"; return; fi
  local hit
  hit=$(find "$HOME/Library/Arduino15/packages/rp2040/tools" \
             "$HOME/.arduino15/packages/rp2040/tools" \
             -name "$name" -type f 2>/dev/null | sort -V | tail -1)
  [ -n "$hit" ] && { echo "$hit"; return; }
  echo "error: $name not found (install the rp2040 Arduino core)" >&2
  exit 1
}
PICOTOOL=$(find_tool picotool)
MKLITTLEFS=$(find_tool mklittlefs)
echo "picotool:   $PICOTOOL"
echo "mklittlefs: $MKLITTLEFS"

# --- require BOOTSEL ----------------------------------------------------------
if ! "$PICOTOOL" info >/dev/null 2>&1; then
  echo >&2
  echo "error: no RP2040 in BOOTSEL mode." >&2
  echo "       Unplug USB, HOLD the BOOTSEL button, replug (an RPI-RP2 disk appears)," >&2
  echo "       then re-run this script." >&2
  exit 1
fi

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
CUR_IMG="$WORK/current-fs.bin"
NEW_IMG="$WORK/new-fs.bin"
UNPACK="$WORK/fs"
mkdir -p "$UNPACK"

echo
echo "=== 1/4 reading current LittleFS ($FS bytes @ $FS_OFFSET) ==="
# picotool save -r <start> <end> reads a flash range into a .bin.
FS_END=$(printf '0x%x' $((FS_OFFSET + FS)))
"$PICOTOOL" save -r "$FS_OFFSET" "$FS_END" "$CUR_IMG"

echo
echo "=== 2/4 unpacking + injecting $SOUND_NAME_ON_FS ==="
if "$MKLITTLEFS" -u "$UNPACK" -s "$FS" "$CUR_IMG" >/dev/null 2>&1; then
  echo "unpacked existing image (data preserved)"
else
  echo "WARNING: could not unpack current image (corrupt/empty?); starting fresh." >&2
  rm -rf "$UNPACK"; mkdir -p "$UNPACK"
fi

cp "$MP3" "$UNPACK/$SOUND_NAME_ON_FS"
SIZE=$(wc -c < "$UNPACK/$SOUND_NAME_ON_FS" | tr -d ' ')
echo "copied $MP3 -> $SOUND_NAME_ON_FS ($SIZE bytes)"

# Bump settings.json so the firmware serves the sound. soundVer must be > 0;
# soundName/soundSize feed the Settings page. We do a minimal JSON edit with
# python3 so the rest of settings (id counters etc.) is preserved.
SETTINGS="$UNPACK/settings.json"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
python3 - "$SETTINGS" "$SIZE" "$NOW" "$(basename "$MP3")" <<'PY'
import json, sys, os
path, size, now, name = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4]
s = {}
if os.path.exists(path):
    try:
        with open(path) as f: s = json.load(f)
    except Exception:
        s = {}
s["soundVer"]  = int(s.get("soundVer", 0)) + 1
s["soundName"] = name
s["soundSize"] = size
s["soundAt"]   = now
with open(path, "w") as f:
    json.dump(s, f)
print(f"settings.json: soundVer={s['soundVer']} soundName={name} soundSize={size}")
PY

echo
echo "=== 3/4 repacking image ==="
"$MKLITTLEFS" -c "$UNPACK" -s "$FS" "$NEW_IMG"
echo "built $NEW_IMG ($(wc -c < "$NEW_IMG" | tr -d ' ') bytes)"

echo
echo "=== 4/4 writing image back to FS partition @ $FS_OFFSET ==="
# load -o <offset> writes the .bin at an absolute flash offset.
"$PICOTOOL" load -o "$FS_OFFSET" "$NEW_IMG"
"$PICOTOOL" reboot

echo
echo "DONE. The board is rebooting with the new warning sound."
echo "Verify once it is back on the LAN:"
echo "  curl -s -u admin:admin http://rootaika.local/api/v1/warning-sound -o back.mp3 -w '%{size_download}\\n'"
echo "  # expect $SIZE"
