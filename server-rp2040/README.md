# server-rp2040

A bare-metal **RP2040 build of the rootaika server** — an alternative to the Go
`server/` for a hardware deployment with a physical lock button. The board *is*
the server: clients (`client-macos`, `client-windows`) talk to it directly over
the rootaika client API; there is no PC, OS, or database engine in between.

Target hardware: **iLabs Challenger RP2040 WiFi** (RP2040 + ESP-AT WiFi
co-processor on `Serial2`). A momentary push button on `GPIO2` (to GND) locks /
unlocks all devices; the onboard RGB NeoPixel (`GPIO11`) breathes red when
locked, green when unlocked.

## What it implements

HTTP on **port 80**, HTTP Basic Auth (`client:client` / `admin:admin`), with the
client-role endpoints the agents need:

| Endpoint | Notes |
|---|---|
| `POST /api/v1/events/batch` | Ingest `activity_observed` events → append to a LittleFS JSONL log |
| `GET  /api/v1/client/config` | Returns config + lock state. **Short-poll** (responds immediately, see below) |
| `GET  /api/v1/board/today` | Per-device active minutes for today |
| `GET  /api/v1/lock` | Global lock status `{locked,locked_count,total_count}` |
| `POST /api/v1/lock` / `POST /api/v1/unlock` | Toggle / release all locks (same path the physical button drives) |
| `GET  /` | Live dashboard (per-device today minutes, lock state, 10s refresh) |

Usage is computed from the event log at query time (gap after each `active`
event attributed to its process, capped at `max_countable_gap_seconds`). "Today"
is taken from the newest event's date, so the board needs no real-time clock —
clients supply real timestamps.

## Deliberate deviations from the Go server

These come from the ESP-AT WiFi firmware's hard limit of **5 simultaneous TCP
connections** and a ~5–8 KB/s link:

- **Short-poll, not long-poll.** `GET /api/v1/client/config` answers immediately
  and ignores `wait=`. Holding long-poll connections open would exhaust the 5
  links with only a few PCs. Clients poll on an interval (lock latency ≈ poll
  interval, fine for screen-time). Clients must pace themselves and use **one
  connection per host** (URLSession's default of 6 wedges the pool).
- **No SQLite engine.** Events are an append-only JSONL file on LittleFS; usage
  is recomputed per request. Storage is ~6–7 MB — effectively unlimited for this
  data volume.
- **No admin HTML UI / charts / week / month** (yet). Lock control is the
  physical button + the `POST /lock|/unlock` endpoints.

## Build & flash

Requires `arduino-cli`, the `rp2040` core, and libraries **WiFiEspAT**,
**Adafruit NeoPixel**, **ArduinoJson**. Copy `RootaikaServer/wifi.h.example` to
`RootaikaServer/wifi.h` and fill in your WiFi credentials (gitignored).

A **LittleFS partition is required** — select an FS size in the flash layout:

```sh
FQBN="rp2040:rp2040:challenger_2040_wifi:flash=8388608_4194304"   # 4 MB FS
arduino-cli compile --fqbn "$FQBN" RootaikaServer
arduino-cli upload  --fqbn "$FQBN" -p /dev/cu.usbmodemXXXX RootaikaServer
```

The server advertises mDNS as `nappi.local`, so clients can target
`http://nappi.local`.
