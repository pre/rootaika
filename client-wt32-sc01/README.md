# client-wt32-sc01

Shows today's screen time on a WT32-SC01 Plus touchscreen. The ESP32-S3 fetches
the per-device minutes from the rootaika server and draws them as a card list.
Touching a device card opens a cumulative usage chart for that device (today,
x = time of day, y = accumulated active minutes). The refresh interval is set
from the server admin UI (the "Taulun paivitysvali, s" setting) and arrives in
the JSON response, so the device needs no local interval configuration.

Hardware: Panlee WT32-SC01 Plus (ZX3D50CE08S), ESP32-S3 (WT32-S3-WROVER), 3.5"
480x320 ST7796 display, 8-bit parallel (8080) bus, capacitive FT6336 touch over
I2C.

## Views

- **List**: one card per device with today's total active time. Auto-refreshes
  on the server-provided interval; touch anywhere also refreshes.
- **Detail**: touch a card to open that device's cumulative usage curve for
  today. The y-axis maximum comes from the server (`y_max_minutes`); the x-axis
  shows whole-hour ticks that fit. Touch to return to the list.

## Server endpoints used

Both require HTTP Basic Auth with the `client` credentials.

- `GET /api/v1/board/today` - compact per-device today totals (list view).
- `GET /api/v1/charts/usage?range=day` - cumulative per-device time series
  (detail chart). The device is matched by name from the touched card.

## Configuration

Configuration lives in `RootaikaBoard/config.h`, which is gitignored. Copy the
example and fill it in:

```sh
cp client-wt32-sc01/RootaikaBoard/config.h.example \
   client-wt32-sc01/RootaikaBoard/config.h
```

`config.h` defines:

- `WIFI_SSID`, `WIFI_PASS` - 2.4 GHz network (ESP32 has no 5 GHz radio).
- `SERVER_URL` - rootaika server base URL on the LAN (e.g. `http://192.168.1.10:8080`).
- `BOARD_USER`, `BOARD_PASS` - rootaika server `client` credentials.

The endpoint paths are appended to `SERVER_URL` at the top of
`RootaikaBoard/RootaikaBoard.ino`.

## Build and flash

The board exposes a native USB CDC serial port, so no external USB-serial
adapter is needed.

Prerequisites:

```sh
arduino-cli config add board_manager.additional_urls \
  https://espressif.github.io/arduino-esp32/package_esp32_index.json
arduino-cli core update-index
arduino-cli core install esp32:esp32@2.0.17
arduino-cli lib install LovyanGFX ArduinoJson
```

Note: esp32 core 2.0.17 is used because newer 3.x cores have known LovyanGFX
build issues.

Build and flash:

```sh
cd client-wt32-sc01
make PORT=/dev/cu.usbmodem1101
```

The Makefile auto-detects a USB serial port when possible, so `make` is enough
when only the board's USB port is visible. Use `make build` to compile only,
`make flash PORT=/dev/cu.usbmodem1101` to flash a previously built firmware,
`make clean` to remove build artifacts, and `make help` to show the available
commands and overrides.

If upload fails in a connect/disconnect loop, force the bootloader: hold
BOOT/GPIO0, tap RESET, release GPIO0, then upload again.
