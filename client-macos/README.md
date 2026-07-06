# rootaika macOS client

A native macOS client for the rootaika LAN screen-time system, the macOS
equivalent of the Windows client's service/agent pair. One SwiftPM binary
(macOS 13+), two long-lived roles:

- **`rootaika-mac daemon`** — a root **LaunchDaemon** (`com.rootaika.daemon`).
  Owns `config.json` and the persistent SQLite event buffer under
  `/Library/Application Support/rootaika`, uploads `activity_observed`
  batches, long-polls the server for config + lock state, caches the warning
  MP3, self-updates over the air, and serves a loopback HTTP endpoint
  (`127.0.0.1:48611`, `X-Rootaika-Agent-Token` auth) for the agent. Its
  watchdog re-bootstraps the agent every 15 s if a user boots it out.
- **`rootaika-mac agent`** — a per-user **LaunchAgent** (`com.rootaika.agent`,
  root-owned plist in `/Library/LaunchAgents`). Runs in the GUI session:
  samples idle time + frontmost app, reports events to the daemon, and draws
  the full-screen lock overlay with the Finnish warning countdown. It holds no
  server credentials.

## Layout

- `Sources/RootaikaMac/Models.swift` — Codable wire types (`Event`, `EventBatch`, `ClientConfig`, `ActivityState`).
- `Sources/RootaikaMac/Protocols.swift` — `BoardClienting`, `ActivityProbing`, `LockControlling` (the cross-module contract).
- `Sources/RootaikaMac/Config.swift` — config load/save (daemon: `/Library/Application Support/rootaika/config.json`).
- `Sources/RootaikaMac/Daemon.swift` — the daemon: upload + poll + watchdog loops, agent endpoint wiring, OTA check.
- `Sources/RootaikaMac/Core.swift` — the agent loop (daemon state -> lock overlay/warning -> observe -> report).
- `Sources/RootaikaMac/EventBuffer.swift` — persistent SQLite buffer (system libsqlite3), mark-sent after upload.
- `Sources/RootaikaMac/IPC.swift` / `HTTPServer.swift` — the loopback daemon<->agent bridge (same port, header and JSON shapes as Windows).
- `Sources/RootaikaMac/Networking.swift` — `NetworkBoardClient` (server HTTP client, retry/backoff).
- `Sources/RootaikaMac/Updater.swift` / `Version.swift` — OTA self-update (GitHub release asset + SHA256 verify).
- `Sources/RootaikaMac/Activity.swift` — `MacActivityProbe` (idle + frontmost app).
- `Sources/RootaikaMac/LockOverlay.swift` — `MacLockController` (shielding overlay + kiosk mode).
- `Sources/RootaikaMac/main.swift` — subcommand/flag parsing + dependency wiring.
- `packaging/` — LaunchDaemon + LaunchAgent plists and the install script.

## Build

```sh
cd client-macos
swift build
```

The SwiftPM product (and the built binary under `.build/<config>/`) is named
`RootaikaMac`. The install script copies it to `/usr/local/bin/rootaika-mac`.
`scripts/build.sh v1.2.0` builds a release binary into `dist/` with the version
label injected into `Version.swift` for the build (`--version` prints it).

## Run

```sh
# No-GUI self test: config load + JSON round-trips. Prints OK, exits 0.
swift run RootaikaMac --selftest

# Show the lock overlay for 5 seconds then exit.
swift run RootaikaMac --test-lock 5

# Run the daemon in the foreground (dev: ROOTAIKA_HOME avoids needing root).
ROOTAIKA_HOME=/tmp/rootaika-dev swift run RootaikaMac daemon --server http://192.168.68.199:8080

# Run the agent (accessory app, no Dock icon); it talks to the local daemon.
ROOTAIKA_HOME=/tmp/rootaika-dev swift run RootaikaMac agent

# Force the debug console on for this run (see below).
ROOTAIKA_HOME=/tmp/rootaika-dev swift run RootaikaMac agent --debug
```

## Debug mode

Like the Windows client, the agent has a debug mode that opens an on-screen
terminal-style console window and traces, line by line, what it observes and
exchanges with the daemon: each observation (idle seconds, frontmost app,
derived state), every reported event, every daemon state fetch, plus
lock-state changes. Every line is also mirrored to stderr (the LaunchAgent
captures it at `/tmp/rootaika-agent.err.log`; the daemon logs to
`/var/log/rootaika-daemon.log`).

Debug mode turns on either when the server sets `debug_mode` for the device
(the console appears/disappears live as the setting changes) or when the agent
is started with `--debug` for a local diagnosis session.

The default server URL is `http://192.168.68.199:8080`, using the direct LAN IP
until DNS is available. Environment overrides (applied after defaults,
persisted on first load): `ROOTAIKA_SERVER_URL`, `ROOTAIKA_CLIENT_USERNAME`,
`ROOTAIKA_CLIENT_PASSWORD`, `ROOTAIKA_HOME`. An existing config that still has
the old loopback default `http://127.0.0.1:8080` is migrated to the LAN IP on
load.

## Install

```sh
./packaging/install.sh            # build release, install binary + daemon + agent (uses sudo)
./packaging/install.sh uninstall  # unload + remove
```

The installer migrates a pre-split per-user config
(`~/Library/Application Support/rootaika/config.json`) to
`/Library/Application Support/rootaika/config.json` so the device keeps its
`client_id`, and removes the legacy `com.rootaika.macclient` LaunchAgent.
Server URL/credentials live in the root-only config file; edit with sudo, then
`sudo launchctl kickstart -k system/com.rootaika.daemon`.

## Durability (why a daemon/agent split)

The GUI-facing work — idle time, frontmost app, the shielding overlay, the
warning sound — requires an interactive Aqua session, so it lives in a
per-user LaunchAgent. Everything durable lives in the root LaunchDaemon, which
a non-admin user cannot unload. If the user kills either process, launchd's
`KeepAlive` relaunches it; if the user boots the agent out of their session,
the daemon's watchdog bootstraps it back within 15 s. This mirrors the Windows
LocalSystem-service + user-agent design. (Hard limits remain: an admin user,
single-user mode, or physically powering off.)

## OTA self-update

`scripts/release.sh v1.2.0` publishes the binary as a GitHub release asset and
prints a `version / artifact / sha256` triple. Enter that triple as a client
version in the server admin UI and assign it to the mac devices; the daemon
notices the differing `desired_version` on its next config poll, downloads the
asset from the compile-time-fixed `github.com/pre/rootaika` (SHA256-verified),
atomically replaces `/usr/local/bin/rootaika-mac`, and restarts both halves
via launchd. A failed version is retried after a 30-minute cooldown.

## Required macOS permissions

Grant in **System Settings > Privacy & Security**:

- **Accessibility** and/or **Input Monitoring** — needed for reliable
  system-wide idle detection. (`CGEventSourceSecondsSinceLastEventType` works
  without entitlements in most cases, but Input Monitoring/Accessibility makes it
  robust and is required if input is captured.)

No extra audio setup is required. The pre-lock warning sound is an MP3 the
admin uploads on the server; the daemon downloads it from
`GET /api/v1/warning-sound`, caches it at
`/Library/Application Support/rootaika/warning-sound.mp3` (world-readable so
the agent can play it), and re-downloads only when the server's
`warning_sound_version` changes. If no sound is configured the warning is
silent.

Reading the frontmost application name via `NSWorkspace.frontmostApplication`
needs no special entitlement.

## Lock and pre-lock warning

The lock overlay is a full-screen green shield (matching the Windows client) with
"rootaika" and the admin message centered in white. When the server requests a
lock with a `warning_seconds` countdown, the agent first shows a translucent,
click-through banner counting down "X sekuntia/minuuttia jäljellä ennen lukitusta"
while the screen stays usable, and loops the cached warning MP3 for the duration.
The sound plays only during this countdown, never at the lock moment. An unlock
during the countdown cancels both the banner and the sound.
