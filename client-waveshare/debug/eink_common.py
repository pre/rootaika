"""Shared helpers for the e-ink debug scripts.

Keeps the Waveshare lib path and font lookup in one place so each diagnostic
stays short. These scripts need sudo (GPIO/SPI access).
"""

import os
import sys

WAVESHARE_LIB = os.path.expanduser(
    "~/e-Paper/RaspberryPi_JetsonNano/python/lib"
)

FONT_BOLD = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
FONT_REGULAR = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"

# BCM pin numbers used by the Waveshare RaspberryPi driver.
RST_BCM = 17
DC_BCM = 25
BUSY_BCM = 24
PWR_BCM = 18


def add_waveshare_to_path() -> None:
    """Put the cloned Waveshare driver library on sys.path."""
    if WAVESHARE_LIB not in sys.path:
        sys.path.insert(0, WAVESHARE_LIB)
