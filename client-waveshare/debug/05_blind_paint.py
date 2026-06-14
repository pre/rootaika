#!/usr/bin/env python3
"""Paint WITHOUT reading BUSY at all - pure fixed-timing delays.

If the panel hardware works, the glass should cycle black -> white ->
Hello World even with BUSY disconnected or unreadable. If the glass stays
blank here, the panel's display side (FPC / booster / glass) is faulty, not
the BUSY wire.

Watch the GLASS, not the multimeter: the screen is the ground truth.

Run with sudo.
"""

import time

from eink_common import FONT_BOLD, FONT_REGULAR, add_waveshare_to_path

add_waveshare_to_path()
from PIL import Image, ImageDraw, ImageFont  # noqa: E402
from waveshare_epd import epd4in2_V2, epdconfig  # noqa: E402


def main() -> int:
    epd = epd4in2_V2.EPD()
    epd.ReadBusy = lambda *a, **k: epdconfig.delay_ms(5000)  # never read BUSY

    print("init (blind, 5s waits)...")
    epd.init()
    print("init done")

    print(">>> WATCH GLASS: full BLACK <<<")
    epd.display(epd.getbuffer(Image.new("1", (epd.width, epd.height), 0)))
    time.sleep(6)

    print(">>> WATCH GLASS: full WHITE <<<")
    epd.display(epd.getbuffer(Image.new("1", (epd.width, epd.height), 255)))
    time.sleep(6)

    print(">>> WATCH GLASS: Hello World <<<")
    img = Image.new("1", (epd.width, epd.height), 255)
    draw = ImageDraw.Draw(img)
    big = ImageFont.truetype(FONT_BOLD, 48)
    small = ImageFont.truetype(FONT_REGULAR, 20)
    draw.text((40, 100), "Hello World", font=big, fill=0)
    draw.text((40, 170), "rootaika e-ink test", font=small, fill=0)
    draw.rectangle((10, 10, epd.width - 10, epd.height - 10), outline=0)
    epd.display(epd.getbuffer(img))
    time.sleep(6)

    epd.sleep()
    print("END")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
