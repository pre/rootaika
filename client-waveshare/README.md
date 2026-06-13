# client-waveshare

Shows today's screen time on an external e-ink display. A Raspberry Pi fetches
the per-device minutes from the rootaika server and draws them on a Waveshare
4.2" e-Paper panel. The refresh interval is set from the server admin UI (the
"Taulun päivitysväli, s" setting) and arrives in the JSON response, so the Pi
needs no local interval configuration.

Hardware: Raspberry Pi Zero W 1.1 + Waveshare 4.2" e-Paper Module (400x300,
Rev 2.1, black and white).

## Wiring (SPI)

Connect the Waveshare module's wires to the Raspberry Pi's GPIO header. The pin
numbers in the table are **physical** pins (header order); the BCM number is in
parentheses. A clear diagram of the pin layout is available at
[pinout.xyz](https://pinout.xyz/).

| Waveshare | Wire color | Pi pin (physical) | Signal (BCM) |
|---|---|---|---|
| VCC | gray | 1 | 3.3 V |
| GND | brown | 6 | GND |
| DIN | blue | 19 | GPIO10 / MOSI |
| CLK | yellow | 23 | GPIO11 / SCLK |
| CS | orange | 24 | GPIO8 / CE0 |
| DC | green | 22 | GPIO25 |
| RST | white | 11 | GPIO17 |
| BUSY | purple | 18 | GPIO24 |

Important:

- **Connect VCC to 3.3 V (pin 1), not 5 V.** The module uses 3.3 V logic.
- The pins (RST=17, DC=25, CS=CE0/8, BUSY=24) are the Waveshare driver's
  defaults, so the stock library works without any custom pin configuration.

## Enabling SPI

```sh
sudo raspi-config   # Interface Options -> SPI -> Enable
sudo reboot
```

## Installing dependencies

```sh
sudo apt update
sudo apt install -y python3-pip python3-pil fonts-dejavu-core
cd client-waveshare
pip3 install -r requirements.txt
```

### Installing the Waveshare driver (if the pip package does not work)

The `waveshare-epd` pip package does not always cover the Rev 2.1 panel. If the
display does not refresh, clone the official library and add it to the Python
path:

```sh
git clone https://github.com/waveshareteam/e-Paper.git ~/e-Paper
export PYTHONPATH="$HOME/e-Paper/RaspberryPi_JetsonNano/python/lib:$PYTHONPATH"
```

This code uses the `epd4in2_V2` driver. If your panel is an older revision,
change the import in `board_display.py` to the matching module (`epd4in2`).

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `ROOTAIKA_SERVER_URL` | (required) | Server address, e.g. `http://192.168.1.10:8080` |
| `ROOTAIKA_CLIENT_USERNAME` | `client` | Client API user |
| `ROOTAIKA_CLIENT_PASSWORD` | (empty) | Client API password |

## Running manually

```sh
ROOTAIKA_SERVER_URL=http://192.168.1.10:8080 \
ROOTAIKA_CLIENT_PASSWORD=change-me \
python3 board_display.py
```

## Starting on boot (systemd)

```sh
sudo cp rootaika-board.service /etc/systemd/system/
sudoedit /etc/systemd/system/rootaika-board.service   # set the path, URL, and password
sudo systemctl daemon-reload
sudo systemctl enable --now rootaika-board.service
sudo systemctl status rootaika-board.service
```

The service restarts automatically if it crashes (`Restart=always`).

## Notes

- A full e-ink refresh takes a few seconds and wears the panel, so keep the
  refresh interval moderate (default 60 s). Adjust it from the admin UI.
- If the server does not respond, the display shows "No connection to server"
  and the Pi retries after the default interval (60 s).
