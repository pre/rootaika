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

HTTP on **port 80**, HTTP Basic Auth (`client:client` / `admin:admin`). The board
now mirrors the Go server's full client API plus the admin Settings page:

| Endpoint | Role | Notes |
|---|---|---|
| `POST /api/v1/events/batch` | client/admin | Ingest `activity_observed` events → append to a LittleFS JSONL log (a RAM dedup ring drops retried `event_id`s) |
| `GET  /api/v1/client/config` | client/admin | Per-device config + lock state + categories + `warning_sound_version`. `config_version` is the same FNV-1a fingerprint the Go server emits. **Short-poll** (responds immediately, see below) |
| `GET  /api/v1/warning-sound` | client/admin | Streams the admin-uploaded MP3 (`404` when none set) |
| `GET  /api/v1/board/today` | client/admin | Per-device active minutes for today + `refresh_seconds` |
| `GET  /api/v1/lock` | client/admin | Global lock status `{locked,locked_count,total_count}` over assigned devices |
| `POST /api/v1/lock` / `POST /api/v1/unlock` | client/admin | Toggle / release all assigned-device locks (same path the physical button drives) |
| `GET  /settings` | client/admin | Admin Settings page (read-only for the client role) |
| `POST /admin/*` | admin only | Settings mutations: devices (assign/lock/unlock/delete), users (create/rename), global settings, categories, MP3 upload |
| `GET  /` | client/admin | Live dashboard (per-device today minutes, lock state, 10s refresh) |

Server state (settings, users, devices with per-device config/lock/status,
categories) lives in RAM and is mirrored to small JSON files on LittleFS
(`settings.json`, `users.json`, `devices.json`, `categories.json`), so it
survives reboots. Lock is **per device** and acts only on *assigned* devices,
matching the Go server: a device shows `lukittu` only once its client reports
`state=locked` back, `lukitaan…` until then.

Usage is computed from the event log at query time (gap after each `active`
event attributed to its process, capped at `max_countable_gap_seconds`). "Today"
is taken from the newest event's date, so the board needs no real-time clock —
clients supply real timestamps.

### Admin Settings page parity

The `/settings` page ports the Go server's `settings_views.go`: the same
devices / users / settings / warning-sound / categories sections, rendered
server-side and driven by form POSTs to `/admin/*` (PRG redirect back to
`/settings`). The MP3 upload is `multipart/form-data` streamed straight to
LittleFS (`/warning.mp3`, ≤10 MB), bypassing the request buffer; every upload
bumps an integer `soundVer` that becomes the `warning_sound_version` clients see.

The code is split into `RootaikaServer.ino` (HTTP routing + handlers),
`storage.h` (data model, LittleFS persistence, lock/usage/config-version logic),
and `html.h` (Settings page rendering).

## Deliberate deviations from the Go server

These come from the ESP-AT WiFi firmware's hard limit of **5 simultaneous TCP
connections** and a ~5–8 KB/s link:

- **Short-poll, not long-poll.** `GET /api/v1/client/config` answers immediately
  and ignores `wait=`. Holding long-poll connections open would exhaust the 5
  links with only a few PCs. Clients poll on an interval (lock latency ≈ poll
  interval, fine for screen-time). Clients must pace themselves and use **one
  connection per host** (URLSession's default of 6 wedges the pool).
- **No SQLite engine.** Events are an append-only JSONL file on LittleFS; the
  rest of the state is small JSON files. Usage is recomputed per request. Storage
  is ~6–7 MB — effectively unlimited for this data volume.
- **No statistics views.** The `/week`, `/month`, and chart endpoints
  (`/api/v1/charts/*`) and the rich dashboard charts are intentionally not
  ported. The board keeps a minimal today-only dashboard; full history/charts
  stay in the Go server.
- **No config long-poll, no in-process notifier.** Config changes reach clients
  on their next short poll, not instantly.
- **Fixed capacities** (RAM-resident): up to 16 devices, 16 users, 32 category
  rules, and a 256-entry event dedup ring (resets on reboot). Fine at the home
  LAN scale this targets.

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
