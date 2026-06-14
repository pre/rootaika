#!/usr/bin/env python3
"""Read BUSY (GPIO24) with the internal pull-up, then the pull-down.

A pin actively driven by the panel ignores the pull and reads the same value
both ways. A floating / disconnected pin follows the pull (up -> 1, down -> 0).

NOTE: the Waveshare driver itself configures BUSY with a pull-down, so an idle
panel that is not currently driving BUSY will look "floating" here. This test
is a coarse wiring check, not a verdict on its own.

Run with sudo.
"""

import time

import gpiozero

from eink_common import BUSY_BCM


def main() -> int:
    for pull in ("up", "down"):
        pin = gpiozero.Button(BUSY_BCM, pull_up=(pull == "up"))
        vals = [pin.pin.state for _ in range(5) if not time.sleep(0.02)]
        print(f"BUSY pull_{pull}: {vals}")
        pin.close()
        time.sleep(0.1)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
