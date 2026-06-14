#!/usr/bin/env python3
"""Hardware reset + SWRESET, watching whether the panel drives BUSY.

Expected on a healthy Rev2.1 panel: BUSY goes HIGH after the hardware reset,
then drops once SWRESET (0x12) is received. That proves the controller is
powered and accepting SPI commands, independent of whether a full refresh works.

Run with sudo.
"""

import time

from eink_common import add_waveshare_to_path

add_waveshare_to_path()
from waveshare_epd import epdconfig  # noqa: E402


def busy() -> int:
    return epdconfig.digital_read(epdconfig.BUSY_PIN)


def main() -> int:
    print("module_init:", epdconfig.module_init())
    print("BUSY before reset:", busy())

    epdconfig.digital_write(epdconfig.RST_PIN, 1)
    time.sleep(0.02)
    epdconfig.digital_write(epdconfig.RST_PIN, 0)
    time.sleep(0.005)
    epdconfig.digital_write(epdconfig.RST_PIN, 1)
    time.sleep(0.02)
    print("BUSY right after reset:", busy())

    epdconfig.digital_write(epdconfig.DC_PIN, 0)  # command mode
    epdconfig.spi_writebyte([0x12])  # SWRESET
    print("sent SWRESET, watching BUSY for 1s:")
    seen = []
    for _ in range(50):
        seen.append(busy())
        time.sleep(0.02)
    print("BUSY samples:", "".join(str(x) for x in seen))
    print(f"BUSY high {sum(seen)}/50 samples")

    epdconfig.module_exit()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
