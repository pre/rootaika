# rootaika macOS client

A native macOS client for the rootaika LAN screen-time system. It is the macOS
equivalent of the Windows Go agent: it samples local activity (idle time +
frontmost app), reports `activity_observed` events to the server, long-polls for
config + lock state, and draws a full-screen lock overlay with a spoken Finnish
warning countdown.

This is a SwiftPM executable targeting macOS 13+.

## Layout

- `Sources/RootaikaMac/Models.swift` — Codable wire types (`Event`, `EventBatch`, `ClientConfig`, `ActivityState`).
- `Sources/RootaikaMac/Protocols.swift` — `BoardClienting`, `ActivityProbing`, `LockControlling` (the cross-module contract).
- `Sources/RootaikaMac/Config.swift` — config load/save under `~/Library/Application Support/rootaika/config.json`.
- `Sources/RootaikaMac/Core.swift` — the agent loop (observe -> state machine -> buffer/report -> long-poll -> lock). Depends only on the protocols.
- `Sources/RootaikaMac/Networking.swift` — `NetworkBoardClient` (server HTTP client).
- `Sources/RootaikaMac/Activity.swift` — `MacActivityProbe` (idle + frontmost app).
- `Sources/RootaikaMac/LockOverlay.swift` — `MacLockController` (shielding overlay + kiosk mode).
- `Sources/RootaikaMac/main.swift` — flag parsing + dependency wiring.
- `packaging/` — LaunchAgent plist + install script.

> The three concrete classes (`NetworkBoardClient(config:)`, `MacActivityProbe()`,
> `MacLockController()`) currently ship as stubs and are filled in by separate
> module agents. Their names and initializers are a fixed contract.

## Build

```sh
cd client-macos
swift build
```

The SwiftPM product (and the built binary under `.build/<config>/`) is named
`RootaikaMac`. The install script copies it to `/usr/local/bin/rootaika-mac`.

## Run

```sh
# No-GUI self test: config load + JSON round-trip + stub event build. Prints OK, exits 0.
swift run RootaikaMac --selftest
# (equivalently: swift build && .build/debug/RootaikaMac --selftest)

# Show the lock overlay for 5 seconds then exit.
swift run RootaikaMac --test-lock 5

# Run the agent (accessory app, no Dock icon). Optional server override.
swift run RootaikaMac --server http://192.168.68.199:8080

# Run with the debug console forced on for this run (see below).
swift run RootaikaMac --debug
```

## Debug mode

Like the Windows client, the macOS agent has a debug mode that opens an on-screen
terminal-style console window and traces, line by line, what it observes and
exchanges with the server: each observation (idle seconds, frontmost app, derived
state), every queued event, every upload with the server's accept/duplicate
counts, every config long-poll with the returned settings, plus lock-state and
warning-sound changes. Every line is also mirrored to stderr (the LaunchAgent
captures it at `/tmp/rootaika-mac.err.log`).

Debug mode turns on either when the server sets `debug_mode` for the device (the
console appears/disappears live as the setting changes) or when the binary is
started with `--debug` for a local diagnosis session. This is the macOS
equivalent of the Windows client's runtime console.

The default server URL is `http://192.168.68.199:8080`, using the direct LAN IP
until DNS is available. Environment overrides (applied after defaults, persisted
on first load):
`ROOTAIKA_SERVER_URL`, `ROOTAIKA_CLIENT_USERNAME`, `ROOTAIKA_CLIENT_PASSWORD`,
`ROOTAIKA_HOME`.
An existing config that still has the old loopback default
`http://127.0.0.1:8080` is migrated to the LAN IP on load.

## Install

```sh
./packaging/install.sh            # build release, install /usr/local/bin/rootaika-mac, load LaunchAgent
./packaging/install.sh uninstall  # unload + remove
```

## LaunchAgent vs root LaunchDaemon (durability)

This client installs as a **per-user LaunchAgent**, because the things it must do
— read idle time, observe the frontmost app, draw the shielding overlay, hide the
cursor, play the warning sound — all require an **interactive GUI (Aqua)
session**. A root `LaunchDaemon` runs at boot with no GUI session and cannot do
these things.

The tradeoff is durability. `KeepAlive=true` in the LaunchAgent relaunches the
agent if it crashes or is killed, but a **determined user with their own login
session can still kill or unload a user-session agent** (e.g. `launchctl unload`).
The Windows design sidesteps this with a LocalSystem service + a user-session
agent; the durable, non-user-killable half is the service. The kid-proof macOS
equivalent is a two-process split:

- a root **LaunchDaemon** owning the network/storage/coordination tier (no UI), and
- a per-user **LaunchAgent** doing activity sampling + the overlay/warning sound,

with the daemon (re)launching and supervising the agent. This scaffold ships the
single LaunchAgent; the hardened daemon split is future work.

## Required macOS permissions

Grant in **System Settings > Privacy & Security**:

- **Accessibility** and/or **Input Monitoring** — needed for reliable
  system-wide idle detection. (`CGEventSourceSecondsSinceLastEventType` works
  without entitlements in most cases, but Input Monitoring/Accessibility makes it
  robust and is required if input is captured.)
No extra audio setup is required. The pre-lock warning sound is an MP3 the admin
uploads on the server; the client downloads it from `GET /api/v1/warning-sound`,
caches it at `~/Library/Application Support/rootaika/warning-sound.mp3`, and
re-downloads only when the server's `warning_sound_version` changes. If no sound
is configured the warning is silent.

Reading the frontmost application name via `NSWorkspace.frontmostApplication`
needs no special entitlement.

## Lock and pre-lock warning

The lock overlay is a full-screen green shield (matching the Windows client) with
"rootaika" and the admin message centered in white. When the server requests a
lock with a `warning_seconds` countdown, the client first shows a translucent,
click-through banner counting down "X sekuntia/minuuttia jäljellä ennen lukitusta"
while the screen stays usable, and loops the cached warning MP3 for the duration.
The sound plays only during this countdown, never at the lock moment. An unlock
during the countdown cancels both the banner and the sound.
