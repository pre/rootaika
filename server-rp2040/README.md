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
| `GET  /api/v1/client/config` | client/admin | Per-device config + lock state + categories + `warning_sound_version` + the OTA `desired_version`/`artifact_name`/`sha256` triple. The client reports its running version back via `&version=`. `config_version` is the same FNV-1a fingerprint the Go server emits. **Short-poll** (responds immediately, see below) |
| `GET  /api/v1/warning-sound` | client/admin | Streams the admin-uploaded MP3 (`404` when none set) |
| `GET  /api/v1/board/today` | client/admin | Per-device active minutes for today + `refresh_seconds` |
| `GET  /api/v1/lock` | client/admin | Global lock status `{locked,locked_count,total_count}` over assigned devices |
| `POST /api/v1/lock` / `POST /api/v1/unlock` | client/admin | Toggle / release all assigned-device locks (same path the physical button drives) |
| `GET  /settings` | client/admin | Admin Settings page (read-only for the client role) |
| `POST /admin/*` | admin only | Settings mutations: devices (assign/lock/unlock/delete), users (create/rename), global settings, categories, MP3 upload |
| `GET  /` | client/admin | Live dashboard (per-device today minutes, lock state, 10s refresh) |

Server state (settings, users, devices with per-device config/lock/status,
categories, OTA version registry) lives in RAM and is mirrored to small JSON
files on LittleFS (`settings.json`, `users.json`, `devices.json`,
`categories.json`, `versions.json`), so it survives reboots. Lock is **per device** and acts only on *assigned* devices,
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

### Client OTA auto-update (server side)

The board is the server half of the Windows client's over-the-air update (see
`plans/auto-update-suunnitelma.html`). It does **not** download or build
anything; it only *declares* the version the client should run and *records*
what the client reports:

- **Version management and version selection are separate.** A version is the
  triple `(version, artifact, sha256)` entered **once** in the Settings page's
  *Versiot* registry (`versions.json`); the download origin (GitHub owner/repo)
  is fixed in the client binary and is never server-controlled. Fixing a wrong
  sha256/artifact is an in-place edit of the record, so every selection pointing
  at it updates automatically.
- **Selection chooses a registered version by id**, not by re-typing the triple.
  Set the **global** default from a dropdown in the Settings section, and an
  optional **per-device** override from a dropdown in the devices table (the
  per-device dropdown's "Globaali oletus" inherits the global selection). A
  device resolves to its own selection if set, otherwise the global one;
  "Ei versiota" / no selection means no update is offered (clients keep running).
  Deleting a registered version resets any global/device selection that pointed
  at it back to none.
- **Travels on the existing config poll** — no new endpoint. The client sends
  its running version as `GET /api/v1/client/config?...&version=v1.2.0`; the
  board stores it as the device's `lastVersion`/`lastVersionAt` and returns the
  resolved triple (`desired_version`/`artifact_name`/`sha256`) in the same JSON
  response, unchanged on the wire. The client compares and self-swaps.
- **Reported version is shown** in the devices table (`Versio` column) with the
  time it was last reported and a `→ <tag>` marker for the resolved target
  (`(laite)` when a per-device override is active).

### Deliberate deviation: NTP clock

The Go server timestamps the reported version with its own wall clock. The board
has **no RTC**, so it pulls UTC from the ESP-AT firmware's SNTP client
(`WiFi.sntp()` / `WiFi.getTime()`) once at boot and re-syncs hourly, then
extrapolates with `millis()`. `lastVersionAt` uses this clock; until NTP first
responds it is left blank rather than guessed. (Usage/"today" still derive from
client-supplied event timestamps, not this clock.)

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

The server advertises mDNS as `rootaika.local`, so clients can target
`http://rootaika.local`.
