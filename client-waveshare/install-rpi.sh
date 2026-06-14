#!/usr/bin/env bash
#
# Provision a Raspberry Pi for the rootaika Waveshare e-ink board.
#
# This is the Raspberry Pi variant of the board client (the directory may later
# gain an ESP32 variant, hence the -rpi suffix).
#
# Idempotent: safe to re-run. Enables SPI, installs the Python imaging stack
# and fonts, and clones the Waveshare e-Paper driver library. Does NOT install
# the rootaika board service itself (see README.md "Starting on boot").
#
# Copy this whole directory to the Pi and run it there:
#   scp -r client-waveshare admin@<pi>:~/
#   ssh admin@<pi> 'cd ~/client-waveshare && ./install-rpi.sh'
#
# A reboot is required the first time SPI is enabled (the script tells you).

set -euo pipefail

WAVESHARE_REPO="https://github.com/waveshareteam/e-Paper.git"
WAVESHARE_DIR="${HOME}/e-Paper"
FONT_PATH="/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"

log() { printf '\n=== %s ===\n' "$1"; }

if [ "$(id -u)" -eq 0 ]; then
  echo "Run as a normal user (it calls sudo itself), not as root." >&2
  exit 1
fi

log "Enabling SPI"
spi_was_off=0
if [ -e /dev/spidev0.0 ]; then
  echo "SPI already active (/dev/spidev0.0 present)."
else
  sudo raspi-config nonint do_spi 0
  spi_was_off=1
  echo "SPI enabled in config. A reboot is needed to create /dev/spidev*."
fi

log "Installing apt packages (PIL, numpy, fonts, git)"
sudo apt-get update -qq
sudo apt-get install -y python3-pil python3-numpy fonts-dejavu-core git

log "Cloning Waveshare e-Paper driver library"
if [ -d "${WAVESHARE_DIR}" ]; then
  echo "Already present at ${WAVESHARE_DIR}, skipping clone."
else
  git clone --depth 1 "${WAVESHARE_REPO}" "${WAVESHARE_DIR}"
fi

log "Verifying Python imports"
python3 - <<'PY'
import sys
sys.path.insert(0, f"{__import__('os').path.expanduser('~')}/e-Paper/RaspberryPi_JetsonNano/python/lib")
import PIL, numpy
print("PIL", PIL.__version__, "/ numpy", numpy.__version__)
from waveshare_epd import epd4in2_V2  # noqa: F401  (import only verifies presence)
print("waveshare_epd.epd4in2_V2 import OK")
PY

log "Verifying fonts"
if [ -f "${FONT_PATH}" ]; then
  echo "DejaVu font present: ${FONT_PATH}"
else
  echo "WARNING: ${FONT_PATH} missing" >&2
fi

log "Done"
echo "Driver library: ${WAVESHARE_DIR}/RaspberryPi_JetsonNano/python/lib"
echo
echo "Next:"
echo "  - Wire the panel per client-waveshare/README.md (VCC to 3.3V)."
if [ "${spi_was_off}" -eq 1 ]; then
  echo "  - REBOOT now to activate SPI:  sudo reboot"
fi
echo "  - Smoke-test the panel:        sudo python3 debug/run_all.py"
echo "  - Install the board service:   see README.md"
