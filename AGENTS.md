# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`rootaika` is a LAN screen-time tracking system for Windows gaming machines (target: 5-10 home PCs, scale is not a concern). A headless Windows client reports active usage to a Go HTTP server, which stores raw events in SQLite and renders an HTML admin/viewing UI plus a client API. The authoritative design and an "implementation vs. plan deviations" table live in `plans/ruutuaika-suunnitelma.html` (Finnish). Keep that deviations table updated when the implementation diverges from the plan.

This is a monorepo with one directory per top-level component: `server/` and `client-windows/`. The plan, READMEs, and UI strings are in Finnish; code identifiers are English.

## Commands

For build, run, Docker, cross-compile, and Windows install/uninstall commands, see [README.md](README.md) (the single source of truth for setup). Dev-workflow commands not covered there:

```sh
go test -race -cover ./...                    # race detector + coverage (run in server/ or client-windows/)
go test ./internal/rootaika -run TestName     # single test (server/)
```

Windows-specific syscall wrappers (`*_windows.go`) do not compile/run under Linux CI. Logic is deliberately isolated into testable functions; the platform files use `_windows.go` / `_other.go` build-tag pairs. Verify real Windows behavior manually, not via CI.

## Architecture

### Server (`server/internal/rootaika`)
- **No router framework.** `App.ServeHTTP` (`app.go`) is a hand-written `switch` on path + method. Path-suffix helpers like `isCommandAckPath` handle parameterized routes. When adding an endpoint, wire it here.
- **Roles via Basic Auth** (`auth.go`): `admin` (mutating admin actions) and `client` (event ingest, config/command polling, read-only UI). Every handler calls `requireRole`. Credentials live in the `auth_credentials` table, seeded from env (`ROOTAIKA_ADMIN_USER/PASSWORD`, `ROOTAIKA_CLIENT_USER/PASSWORD`); MVP stores passwords in plaintext by design.
- **Store layer** (`store.go`): all SQL and the embedded schema migration/seed live here. `OpenStore` runs `migrate` then `seed` on startup. Tables: `users`, `devices`, `events`, `device_config`, `commands`, `program_categories`, `settings`, `auth_credentials`. Unknown `client_uuid`s auto-create a device in `registration_status='unassigned'`.
- **Events are append-only observations.** The client sends timestamped `activity_observed` events (`state` = active/idle/locked, `process_name` only when active, per-event UUID for idempotent re-send). The server does NOT keep a start/stop state machine.
- **Usage is computed at query time** (`reports.go`, `range_views.go`): order a device's events by `occurred_at_utc, sequence`; attribute the gap after an `active` event to its `process_name`; skip gaps after idle/locked; cap any gap at the global `max_countable_gap_seconds` setting so sleep/shutdown/network loss doesn't inflate totals. This means calculation logic can be fixed server-side without a client update.
- **Admin actions are HTML form POSTs + redirect**, not JSON REST (see `handleAdmin` dispatch). Views (`views.go`, `range_views.go`) render server-side HTML for `/` (today), `/week`, `/month`.
- All timestamps stored as UTC text; presentation uses `Europe/Helsinki` (`app.go` `location`, with `time/tzdata` embedded).

### Windows client (`client-windows`) — two binaries
- **`rootaika-service`** (`internal/serviceapp`): the durable component. Holds the persistent `client_id` UUID + config (`C:\ProgramData\rootaika\client.json`), buffers events in local SQLite (`internal/buffer`), and runs three goroutines: `uploadLoop` (batch POST), `pollLoop` (config + commands), `watchdogLoop` (restarts the agent). Runs as LocalSystem; non-admin users cannot stop it.
- **`rootaika-agent`** (`internal/serviceapp` agent side + `internal/activity`, `internal/lock`, `internal/consolewin`): runs in the user session. Reads idle time (`GetLastInputInfo`) and foreground process name, emits `activity_observed` to the service, and shows a black fullscreen topmost overlay when locked. Built with `-H=windowsgui` (no console); `debug_mode` from server config opens a console at runtime.
- **Service↔agent IPC** is loopback HTTP on `127.0.0.1` (`agent_listen_address`), authenticated with a config-generated `X-Rootaika-Agent-Token` header. (The plan left the mechanism unnamed; this is the chosen refinement.)
- **State-change reporting:** the agent compares against the previous observation and sends immediately on change, otherwise at heartbeat intervals.
- **Resilience:** events are marked sent only after a successful upload; HTTP calls (`internal/api`) retry transient failures (network, 5xx, 429) with exponential backoff (~4 tries, 0.5s→5s), never 4xx. This bridges server restarts in seconds.

## Conventions specific to this repo
- Server has no third-party HTTP/router/ORM deps; SQLite via `modernc.org/sqlite` (pure Go, no cgo). Keep that constraint, it's what lets the server cross-compile and containerize cleanly.
- LAN-only, plain HTTP, no TLS by design. Don't add auth hardening or TLS without checking the plan's accepted security limitations.
- New device-tunable settings default to the global `settings` table unless a per-device override is genuinely needed (`max_countable_gap_seconds` is global, not in `device_config`).
