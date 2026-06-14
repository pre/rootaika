# E-ink panel diagnosis (Waveshare 4.2" Rev2.1, 400x300)

Date: 2026-06-13 / 2026-06-14
Hardware: Raspberry Pi Zero (armv6l, Raspbian trixie, Python 3.13) +
Waveshare 4.2inch e-Paper Module Rev 2.1 (400x300, B/W, SSD1683 controller).
Pi host: `admin@192.168.68.107`.

## Verdict

**The panel is faulty (FPC ribbon / on-glass controller). Replace / RMA it.**
The Raspberry Pi side is fully provisioned and proven working. A known-good
4.2" panel should paint on the first run, no further setup needed.

## Pi setup performed (all working, reusable for a new panel)

- Enabled SPI (`raspi-config nonint do_spi 0`); `/dev/spidev0.0` present after reboot.
- Installed `python3-pil python3-numpy fonts-dejavu-core git` via apt.
- Cloned Waveshare lib to `~/e-Paper` (driver `epd4in2_V2` matches Rev2.1).
- Passwordless sudo for `admin` (`/etc/sudoers.d/010_admin-nopasswd`).
- Test script `~/eink_hello.py`; diagnostic suite `~/eink-tests/` (01-06 + README).
- Wiring per client-waveshare/README.md (8 wires, VCC 3.3V). PWR pin (BCM18/phys12)
  unused, normal for the bare module.

## Evidence gathered

Proven good (Pi side):
- Full framebuffer leaves the Pi on SPI: `display()` clocks **30000 bytes**
  (15000 B/plane x 2 = 400*300/8 *2) every call. The Pi *does* try to paint.
- Control GPIOs RST/DC/PWR toggle high/low correctly.
- VCC measured **3.27V at the panel**. Power reaches the board.
- Continuity passed on all 8 wires (user tested).
- Correct driver confirmed by panel sticker (Rev2.1 -> `epd4in2_V2`).

Panel behaviour (the fault):
- Controller partially responds: after a hardware reset BUSY goes high; sending
  SWRESET (0x12) drops it -> the controller accepts some SPI commands.
- On a refresh trigger (master activation 0x20) the panel does **not** reliably
  assert BUSY, and the **glass never paints**, even with BUSY ignored entirely
  (pure fixed-timing "blind" paint in `05`/`06`).
- BUSY measured at the panel is unstable across runs: sometimes ~3V for ~2s,
  but usually **stuck at ~1.55V** and never moving. 1.55V on a 3.3V CMOS line
  is the invalid/high-impedance region: the controller is **not driving BUSY**.
  That is also why the Pi's GPIO24 reads the signal unreliably.

## Why this points to the panel, not software/wiring

The SSD1683 controller is bonded to the glass and reachable only through the
FPC ribbon (which also carries BUSY and the high-voltage traces that flip the
ink). A controller that resets/ACKs commands but never drives BUSY and never
paints, while the Pi faithfully sends every byte, is a damaged FPC / on-glass
controller. Reseating the dupont wires and the FPC ribbon did not change it.
Driver version (V2 and old `epd4in2`), SPI speed (4M/1M/500k), and reset-pulse
timing made no difference.

## Things ruled out (with evidence)

| Hypothesis | Result |
|---|---|
| SPI disabled / missing libs | Fixed; SPI live, PIL/numpy/fonts installed |
| Wrong driver for revision | Rev2.1 -> `epd4in2_V2`, confirmed by sticker |
| BUSY pin floating (gpiozero pull-down) | Red herring; pull-down is by design |
| Reset pulse too short for Rev2.1 | Lengthened pulse, no change |
| SPI signal integrity on jumpers | Lowered to 1M/500k, no change |
| No power to panel | VCC 3.27V at panel |
| Framebuffer not actually sent | Disproven: 30000 bytes/display() clocked |
| Loose dupont / FPC contact | Reseated multiple times, no change |

## Note on a measurement artifact (corrected)

An earlier byte counter reported "1 data byte" for `display()`. That was an
instrumentation bug: `display()` sends the framebuffer via the bulk path
`send_data2()` (not the per-byte `send_data()` that was hooked). The corrected
counter hooks `send_data2` and shows the full 30000 bytes. The framebuffer was
always being transmitted.

## Diagnostic scripts (on the Pi, `~/eink-tests/`)

- `01_busy_level.py` - BUSY pull-up/down read
- `02_gpio_toggle.py` - RST/DC/PWR outputs toggle (Pi side OK)
- `03_reset_busy_response.py` - reset + SWRESET -> controller accepts SPI
- `04_refresh_busy_watch.py` - refresh trigger -> does Pi see BUSY high?
- `05_blind_paint.py` - paint with NO BUSY reads (fixed timing), watch glass
- `06_proof_paint.py` - same, plus proves 30000 bytes clocked per display()
- `README.md` - run order + observations

These live only on the Pi currently. Not yet committed to the repo.

## Next step

Swap in a known-good 4.2" panel and rerun `sudo python3 ~/eink_hello.py`
(or `~/eink-tests/05_blind_paint.py`). If it paints, the original unit is
confirmed defective.
