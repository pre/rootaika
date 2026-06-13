#!/usr/bin/env python3
"""Rootaika e-ink board for the Waveshare 4.2" (400x300) on a Raspberry Pi.

Fetches today's per-device screen-time totals from the rootaika server and
renders them as a simple table on the e-paper display. The refresh interval is
controlled from the server admin UI and arrives in the JSON payload, so the Pi
needs no local interval configuration.
"""

import os
import sys
import time
import logging

import requests
from PIL import Image, ImageDraw, ImageFont

from waveshare_epd import epd4in2_V2

WIDTH = 400
HEIGHT = 300

# Fallback sleep when the server is unreachable or omits a refresh interval.
DEFAULT_REFRESH_SECONDS = 60

# Network timeout for a single poll. Kept short so a dead server doesn't wedge
# the loop for long before we draw the offline screen.
HTTP_TIMEOUT_SECONDS = 10

FONT_PATH = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
FONT_BOLD_PATH = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger("rootaika-board")


def env(name: str, default: str = "") -> str:
    return os.environ.get(name, default).strip()


def load_fonts():
    title = ImageFont.truetype(FONT_BOLD_PATH, 28)
    row = ImageFont.truetype(FONT_PATH, 22)
    small = ImageFont.truetype(FONT_PATH, 16)
    return title, row, small


def format_minutes(minutes: int) -> str:
    hours, mins = divmod(max(minutes, 0), 60)
    if hours:
        return f"{hours}h {mins}min"
    return f"{mins}min"


def fetch_board(server_url: str, auth) -> dict:
    url = server_url.rstrip("/") + "/api/v1/board/today"
    response = requests.get(url, auth=auth, timeout=HTTP_TIMEOUT_SECONDS)
    response.raise_for_status()
    return response.json()


def render(board: dict, fonts) -> Image.Image:
    title_font, row_font, small_font = fonts
    # Mode "1": 1-bit image. 1 = white background, 0 = black ink.
    image = Image.new("1", (WIDTH, HEIGHT), 255)
    draw = ImageDraw.Draw(image)

    draw.text((10, 6), "Päivän ruutuaika", font=title_font, fill=0)
    draw.line((10, 42, WIDTH - 10, 42), fill=0)

    devices = board.get("devices", [])
    y = 54
    line_height = 30
    if not devices:
        draw.text((10, y), "Ei laitteita", font=row_font, fill=0)
    for device in devices:
        if y > HEIGHT - 40:
            break
        name = str(device.get("name", "?"))
        minutes = int(device.get("minutes", 0))
        draw.text((10, y), name, font=row_font, fill=0)
        value = format_minutes(minutes)
        value_width = draw.textlength(value, font=row_font)
        draw.text((WIDTH - 10 - value_width, y), value, font=row_font, fill=0)
        y += line_height

    updated = str(board.get("now", ""))
    draw.text((10, HEIGHT - 22), f"Päivitetty {updated}", font=small_font, fill=0)
    return image


def render_offline(message: str, fonts) -> Image.Image:
    title_font, row_font, _ = fonts
    image = Image.new("1", (WIDTH, HEIGHT), 255)
    draw = ImageDraw.Draw(image)
    draw.text((10, 6), "Päivän ruutuaika", font=title_font, fill=0)
    draw.line((10, 42, WIDTH - 10, 42), fill=0)
    draw.text((10, 60), "No connection to server", font=row_font, fill=0)
    draw.text((10, 90), message[:36], font=row_font, fill=0)
    return image


def main() -> int:
    server_url = env("ROOTAIKA_SERVER_URL")
    username = env("ROOTAIKA_CLIENT_USERNAME", "client")
    password = env("ROOTAIKA_CLIENT_PASSWORD")
    if not server_url:
        log.error("ROOTAIKA_SERVER_URL is required")
        return 2
    auth = (username, password)

    fonts = load_fonts()
    epd = epd4in2_V2.EPD()
    epd.init()
    epd.Clear()

    try:
        while True:
            refresh_seconds = DEFAULT_REFRESH_SECONDS
            try:
                board = fetch_board(server_url, auth)
                refresh_seconds = int(board.get("refresh_seconds") or DEFAULT_REFRESH_SECONDS)
                image = render(board, fonts)
            except Exception as exc:  # network, decode, or HTTP error
                log.warning("poll failed: %s", exc)
                image = render_offline(str(exc), fonts)

            epd.display(epd.getbuffer(image))
            if refresh_seconds < 1:
                refresh_seconds = DEFAULT_REFRESH_SECONDS
            time.sleep(refresh_seconds)
    except KeyboardInterrupt:
        log.info("stopping")
        epd.init()
        epd.Clear()
        epd.sleep()
        return 0


if __name__ == "__main__":
    sys.exit(main())
