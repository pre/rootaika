#!/usr/bin/env python3
"""Trigger a real refresh (master activation 0x20) and sample BUSY at the Pi.

A working SSD1683 holds BUSY HIGH for ~2-3s during a refresh. If the Pi reports
0 high samples while a multimeter shows BUSY moving at the panel, the BUSY
wire / contact is bad (the panel works but the Pi can't read the signal).

Measure BUSY vs GND at the panel during the printed window.

Run with sudo.
"""

import time

from eink_common import add_waveshare_to_path

add_waveshare_to_path()
from PIL import Image  # noqa: E402
from waveshare_epd import epd4in2_V2, epdconfig  # noqa: E402


def main() -> int:
    epd = epd4in2_V2.EPD()

    # Bound the pre-SWRESET ReadBusy so init never hangs: this panel holds BUSY
    # high until SWRESET, but the stock driver waits on BUSY before sending it.
    def bounded_readbusy(limit=8):
        start = time.time()
        while epdconfig.digital_read(epd.busy_pin) == 1:
            if time.time() - start > limit:
                return
            epdconfig.delay_ms(20)

    epd.ReadBusy = bounded_readbusy

    print("init...")
    epd.init()
    print("init done")

    buf = epd.getbuffer(Image.new("1", (epd.width, epd.height), 0))  # all black
    epd.send_command(0x24)
    for byte in buf:
        epd.send_data(byte)

    print(">>> triggering refresh, sampling BUSY 8s - MEASURE AT PANEL NOW <<<")
    epd.send_command(0x22)
    epd.send_data(0xF7)
    epd.send_command(0x20)
    start = time.time()
    high = 0
    while time.time() - start < 8:
        if epdconfig.digital_read(epd.busy_pin) == 1:
            high += 1
        time.sleep(0.01)
    print(f"Pi saw BUSY-high {high} times in 8s")

    epd.sleep()
    print("END")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
