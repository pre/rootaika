# client-rp2040-button

A physical **lock button client** for the rootaika Go server (`server/`), built
on the **iLabs Challenger RP2040 WiFi** (RP2040 + ESP-AT WiFi co-processor on
`Serial2`).

> **History:** this directory used to be `server-rp2040`, a full bare-metal
> RP2040 port of the rootaika server. Maintaining feature parity with the Go
> server (statistics, history, long-poll) on a microcontroller was not worth
> it, so the board was reduced to the one thing only hardware can do: a button
> on the table. The old server firmware lives in git history.

## What it does

A momentary push button on `GPIO2` (to GND):

| Action | API call | Effect |
|---|---|---|
| Short press | `POST /api/v1/lock` | Locks all assigned devices. Clients play the warning sound and show a countdown before the screen locks. |
| Hold ≥ 1 s | `POST /api/v1/unlock` | Releases all locks. |

Both endpoints are idempotent, so the button never needs to know the current
state: pressing lock while already locked simply stays locked.

The onboard RGB NeoPixel (`GPIO11`) shows the **last successful call made from
this button**: red after lock, green after unlock, off at boot or after a
failed call (dim white while a call is in flight).

### Known limitation (documented for later)

**The LED color does not necessarily match the server's lock state.** The
button does not poll `GET /api/v1/lock`; it only remembers its own last
successful call. An admin lock/unlock from the web UI or a board reboot leaves
the LED stale until the next press. If this starts to matter, add a periodic
`GET /api/v1/lock` status poll and drive the LED from the response.

## Configuration

Copy `RootaikaButton/wifi.h.example` to `RootaikaButton/wifi.h` (gitignored)
and fill in the WiFi credentials, the Go server address/port, and the
base64-encoded `client` role credentials (`echo -n 'client:client' | base64`).

## Build & flash

Requires `arduino-cli`, the `rp2040` core, and libraries **WiFiEspAT**,
**Adafruit NeoPixel**:

```sh
make build   # compile
make flash   # upload to the board (autodetects the port, or PORT=/dev/...)
```

The WiFi/HTTP hardware path does not run under CI; verify on the real board
(same manual-verification stance as the Windows `*_windows.go` files).
