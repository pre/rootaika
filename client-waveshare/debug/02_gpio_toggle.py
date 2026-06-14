#!/usr/bin/env python3
"""Verify the control GPIOs (RST/DC/PWR) can be driven high and low.

This isolates the Pi side: if these toggle, the Pi's outputs and the GPIO stack
work, and any remaining fault is the panel or the wiring to it.

Run with sudo.
"""

import time

import gpiozero

from eink_common import DC_BCM, PWR_BCM, RST_BCM


def main() -> int:
    ok = True
    for name, bcm in [("RST", RST_BCM), ("DC", DC_BCM), ("PWR", PWR_BCM)]:
        led = gpiozero.LED(bcm)
        led.on()
        time.sleep(0.05)
        hi = led.value
        led.off()
        time.sleep(0.05)
        lo = led.value
        passed = hi == 1 and lo == 0
        ok = ok and passed
        print(f"{name} (BCM{bcm}): on->{hi} off->{lo} {'OK' if passed else 'FAIL'}")
        led.close()
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
