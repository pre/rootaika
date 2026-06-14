# e-ink debug scripts

Diagnostics for the Waveshare 4.2" e-Paper Rev2.1 (400x300, SSD1683) on a
Raspberry Pi. Use these to localise a "nothing shows on the panel" fault to a
specific layer: Pi GPIO/SPI, the wiring, or the panel itself.

All scripts need `sudo` (GPIO/SPI access) and import `eink_common.py`, so run
them from this directory. They assume the Waveshare driver lib is at
`~/e-Paper` (the `install-rpi.sh` script puts it there).

## Run order

```sh
cd client-waveshare/debug
sudo python3 02_gpio_toggle.py          # Pi outputs work?
sudo python3 01_busy_level.py           # coarse BUSY wiring read
sudo python3 03_reset_busy_response.py  # controller accepts SPI commands?
sudo timeout 30 python3 04_refresh_busy_watch.py  # does Pi see BUSY during refresh?
sudo timeout 60 python3 05_blind_paint.py         # paint w/o BUSY - watch the glass
sudo timeout 60 python3 06_proof_paint.py         # prove 30000 bytes/display() clocked
```

Or run the lot with section headers:

```sh
sudo python3 run_all.py
```

## What each script proves

| Script | Question it answers |
|---|---|
| `01_busy_level.py` | Is BUSY following the internal pull (coarse wiring check)? |
| `02_gpio_toggle.py` | Do the Pi's RST/DC/PWR outputs toggle? (Pi side OK) |
| `03_reset_busy_response.py` | Does the controller react to reset + SWRESET? |
| `04_refresh_busy_watch.py` | Does the Pi see BUSY go high during a refresh? |
| `05_blind_paint.py` | Does the glass paint with BUSY ignored (fixed timing)? |
| `06_proof_paint.py` | Is the full framebuffer actually clocked out on SPI? |

## Interpreting results

- **02 passes, 03 reacts, but 05/06 leave the glass blank** -> the Pi and SPI
  work and the controller takes commands, but the display side never paints.
  Points to the panel (FPC ribbon / booster / glass), not software or the Pi.
- **04 reports 0 BUSY-high at the Pi while a multimeter shows BUSY moving at the
  panel** -> bad BUSY wire/contact: panel refreshes, Pi can't read the signal.
- **A steady ~1.55V on BUSY** (neither 0V nor 3.3V) is an invalid/high-impedance
  level: the controller is not driving BUSY at all.
- **06 prints 30000 bytes/display()** -> the Pi is sending the whole frame; if
  the glass is still blank, the fault is downstream of the Pi.

## Findings from the 2026-06 bring-up (original panel was faulty)

See `../EINK_DIAGNOSIS.md` for the full write-up. Summary: Pi side proven good
(GPIOs toggle, 30000 bytes/frame clocked, VCC 3.27V at the panel), controller
partially responded (reset + SWRESET), but the glass never painted and BUSY sat
at an invalid ~1.55V. Conclusion: faulty panel (FPC/controller), RMA it. A
known-good panel should paint with `05_blind_paint.py` or `../board_display.py`.
