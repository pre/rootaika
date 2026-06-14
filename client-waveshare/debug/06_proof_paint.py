#!/usr/bin/env python3
"""Prove the framebuffer is clocked out on SPI, ignoring BUSY (fixed delays).

Hooks the BULK SPI path (send_data2) that display() actually uses, so the byte
count is real. A full frame is 400*300/8 = 15000 bytes/plane, sent on two
planes (0x24 + 0x26) = 30000 bytes per display() call.

If this prints 30000 bytes/display() but the glass stays blank, the Pi is
sending everything correctly and the panel is not acting on it -> panel fault.

(History: an earlier version hooked the per-byte send_data() and wrongly
reported "1 byte", because display() bypasses it via send_data2. Hooking the
bulk path is the correct measurement.)

Run with sudo.
"""

import time

from eink_common import FONT_BOLD, add_waveshare_to_path

add_waveshare_to_path()
from PIL import Image, ImageDraw, ImageFont  # noqa: E402
from waveshare_epd import epd4in2_V2, epdconfig  # noqa: E402


def main() -> int:
    epd = epd4in2_V2.EPD()

    count = {"bulk_bytes": 0, "cmds": 0}
    orig_cmd = epd.send_command
    orig_data2 = epd.send_data2

    def counting_cmd(x):
        count["cmds"] += 1
        return orig_cmd(x)

    def counting_data2(x):
        try:
            count["bulk_bytes"] += len(x)
        except TypeError:
            count["bulk_bytes"] += 1
        return orig_data2(x)

    epd.send_command = counting_cmd
    epd.send_data2 = counting_data2
    epd.ReadBusy = lambda *a, **k: epdconfig.delay_ms(6000)

    fbsize = epd.width * epd.height // 8
    print("init...")
    epd.init()

    print(">>> WATCH GLASS: full BLACK, ~7s <<<")
    before = count["bulk_bytes"]
    epd.display(epd.getbuffer(Image.new("1", (epd.width, epd.height), 0)))
    print(
        "  bulk bytes clocked this display():",
        count["bulk_bytes"] - before,
        f"(framebuffer {fbsize} x2 planes = {fbsize * 2})",
    )
    time.sleep(7)

    print(">>> WATCH GLASS: Hello World, ~7s <<<")
    img = Image.new("1", (epd.width, epd.height), 255)
    draw = ImageDraw.Draw(img)
    big = ImageFont.truetype(FONT_BOLD, 48)
    draw.text((40, 110), "Hello World", font=big, fill=0)
    draw.rectangle((10, 10, epd.width - 10, epd.height - 10), outline=0)
    epd.display(epd.getbuffer(img))
    time.sleep(7)

    print("TOTAL bulk bytes:", count["bulk_bytes"], "commands:", count["cmds"])
    epd.sleep()
    print("END")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
