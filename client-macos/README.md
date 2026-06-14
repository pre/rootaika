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
swift run RootaikaMac --server http://192.168.1.10:8080
```

Environment overrides (applied after defaults, persisted on first load):
`ROOTAIKA_SERVER_URL`, `ROOTAIKA_CLIENT_USERNAME`, `ROOTAIKA_CLIENT_PASSWORD`,
`ROOTAIKA_HOME`.

## Install

```sh
./packaging/install.sh            # build release, install /usr/local/bin/rootaika-mac, load LaunchAgent
./packaging/install.sh uninstall  # unload + remove
```

## LaunchAgent vs root LaunchDaemon (durability)

This client installs as a **per-user LaunchAgent**, because the things it must do
— read idle time, observe the frontmost app, draw the shielding overlay, hide the
cursor, speak via AVSpeechSynthesizer — all require an **interactive GUI (Aqua)
session**. A root `LaunchDaemon` runs at boot with no GUI session and cannot do
these things.

The tradeoff is durability. `KeepAlive=true` in the LaunchAgent relaunches the
agent if it crashes or is killed, but a **determined user with their own login
session can still kill or unload a user-session agent** (e.g. `launchctl unload`).
The Windows design sidesteps this with a LocalSystem service + a user-session
agent; the durable, non-user-killable half is the service. The kid-proof macOS
equivalent is a two-process split:

- a root **LaunchDaemon** owning the network/storage/coordination tier (no UI), and
- a per-user **LaunchAgent** doing activity sampling + the overlay/TTS,

with the daemon (re)launching and supervising the agent. This scaffold ships the
single LaunchAgent; the hardened daemon split is future work.

## Required macOS permissions

Grant in **System Settings > Privacy & Security**:

- **Accessibility** and/or **Input Monitoring** — needed for reliable
  system-wide idle detection. (`CGEventSourceSecondsSinceLastEventType` works
  without entitlements in most cases, but Input Monitoring/Accessibility makes it
  robust and is required if input is captured.)
- A **Finnish system voice** (System Settings > Accessibility > Spoken Content >
  System Voices / Manage Voices) for the spoken `fi-FI` lock countdown; falls back
  to the default voice if absent.

Reading the frontmost application name via `NSWorkspace.frontmostApplication`
needs no special entitlement.
