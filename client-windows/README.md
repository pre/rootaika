# rootaika Windows client

The MVP client is one combined Go binary:

- `rootaika.exe service`: a service-style process that holds the config, buffers events in a local SQLite database, sends batches to the server, polls for config and commands, and keeps the agent running.
- `rootaika.exe agent`: an agent that runs in the user session, reads idle time and the active process on Windows, sends `activity_observed` events to the service, and implements the lock/unlock base.
- `rootaika.exe apply-update`: a short-lived helper used by the self-update flow.

## Configuration

The default path on Windows is:

```powershell
C:\ProgramData\rootaika\client.json
```

The first launch creates the config and a persistent `client_id` UUID. The key fields:

```json
{
  "server_url": "http://192.168.68.199:8080",
  "client_username": "client",
  "client_password": "change-me",
  "agent_listen_address": "127.0.0.1:48611"
}
```

The path can be passed to both binaries:

```powershell
.\rootaika.exe service -config C:\ProgramData\rootaika\client.json
.\rootaika.exe agent -config C:\ProgramData\rootaika\client.json
```

The environment variables `ROOTAIKA_SERVER_URL`, `ROOTAIKA_CLIENT_USERNAME`, `ROOTAIKA_CLIENT_PASSWORD`, and `ROOTAIKA_AGENT_LISTEN_ADDRESS` override the file values at runtime.
An existing config that still has the old loopback default
`http://127.0.0.1:8080` is migrated to the LAN IP on load.

## Build

On a development machine:

```sh
go test ./...
GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui" -o dist/rootaika.exe ./cmd/rootaika
```

On Windows:

```powershell
.\scripts\build.ps1
```

## Installation with the PowerShell script

The `scripts/` directory contains installation automation. Run everything as admin (except the build).

1. Build the binaries:

```powershell
.\scripts\build.ps1
```

   This produces `dist\rootaika.exe`. The binary is linked with the `-H=windowsgui` flag, so it has no console window by default. Debug mode opens a console at runtime.

2. Install (open PowerShell as admin):

```powershell
.\scripts\install.ps1 -ClientPassword change-me
```

   The script:
   - copies the binaries to `C:\Program Files\rootaika`,
   - writes the config to `C:\ProgramData\rootaika\client.json` (including the server URL and client password),
   - registers the `rootaika-service` service with auto-start and crash recovery (`restart/5000` three times),
   - registers the agent to start when the user logs in (HKLM `Run`),
   - starts the service and the agent immediately.

3. Uninstall:

```powershell
.\scripts\uninstall.ps1          # removes the service and the autostart entry
.\scripts\uninstall.ps1 -Purge   # also removes the binaries and the config/buffer
```

The service runs under the LocalSystem account and only opens an agent endpoint bound to localhost. The agent runs in the user session (the service acts as a watchdog and tries to restart the agent if it stops).

## Lock warning and the warning sound

When an admin locks a device, they can set a warning time in seconds (`warning_seconds`, default 60 in the admin UI). If it is greater than zero, the agent first runs a warning countdown before the screen locks:

- The screen stays fully usable for the whole countdown, so a running game can be played to the end of the warning. The played time is still counted as usage.
- A click-through countdown banner floats on top (it does not steal focus or input) showing the time remaining and the admin's lock message.
- If a warning sound (MP3) is configured on the server, the agent loops it for the whole countdown. If no sound is configured, the warning is silent, the banner still shows.
- When the countdown ends, the normal full-screen lock overlay takes over. Unlocking during the countdown cancels both the banner and the sound.

A true exclusive-fullscreen DirectX game will not show the banner, but the looping sound still carries.

### Warning sound MP3

The warning sound is a single MP3 uploaded by an admin on the server's settings page (`/settings`). The server stores it on the filesystem (under `ROOTAIKA_DATA_DIR`, which defaults to the database directory) and serves it at `GET /api/v1/warning-sound`. Each client downloads and caches the file (`warning.mp3` next to `client.json`) when the server reports a new version, and plays it with the built-in Windows Media Player COM component, so no extra codec or voice package needs to be installed.

## Test run with the PowerShell script

The `scripts\test-run.ps1` script runs a lightweight test session with a single command, without an actual installation (no service, no registrations). It copies the binaries into a local temp directory, sets hard-coded environment variables, and starts the service, which starts the agent via its watchdog. The server address and credentials are hard-coded inside the script.

1. Cross-compile the Windows binaries in WSL first:

```sh
cd client-windows
GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui" -o dist/rootaika.exe ./cmd/rootaika
```

2. Run the script on the Windows side in PowerShell as a **normal user** (not as admin), so the agent sees the same session as your browser:

```powershell
& "\\wsl.localhost\Ubuntu-24.04\home\prepo\dev\oma\rootaika\client-windows\scripts\test-run.ps1"
```

   If the execution policy blocks it:

```powershell
powershell -ExecutionPolicy Bypass -File "\\wsl.localhost\Ubuntu-24.04\home\prepo\dev\oma\rootaika\client-windows\scripts\test-run.ps1"
```

By default the script uses the temp directory `%TEMP%\rootaika-test` and the direct LAN server URL `http://192.168.68.199:8080` until DNS is available. Using the same directory on every run means a persistent `client_id`, so the server sees the same device across reruns. Stop with `Ctrl+C`. The server and credential defaults can be changed by editing the variables at the top of the script; `-DistDir` and `-WorkDir` can be passed as parameters if your WSL distro or working directory differs from the default.

## VirtualBox test run from macOS

Use this when the Windows 11 VM runs on macOS and cannot build the client in WSL. The repository must be shared into the VM with VirtualBox Shared Folders.

1. Start the Windows-side watcher once from the shared repository path:

```powershell
powershell -ExecutionPolicy Bypass -File .\client-windows\scripts\vbox-windows-watch.ps1
```

   To start the watcher automatically when the current Windows user logs in:

```powershell
powershell -ExecutionPolicy Bypass -File .\client-windows\scripts\vbox-windows-watch.ps1 -InstallAutostart
```

2. From macOS, rebuild and restart the Windows test client:

```sh
client-windows/scripts/vbox-macos-launch.sh
```

The macOS script cross-compiles `client-windows/dist/rootaika.exe`, writes a launch request under `client-windows/.vbox-launch/`, and exits. The Windows watcher sees the request, verifies the SHA256, stops old `rootaika` processes, copies the exe to `%TEMP%\rootaika-vbox`, and starts `rootaika.exe service`; the service starts the agent from the same exe.

By default the macOS script sends `http://<current Mac default-network IP>:8080` as the server URL. This avoids the stale hard-coded LAN IP problem when the Mac gets a new address.

Useful overrides:

```sh
ROOTAIKA_SERVER_URL=http://192.168.68.199:8080 \
ROOTAIKA_CLIENT_USERNAME=client \
ROOTAIKA_CLIENT_PASSWORD=client \
client-windows/scripts/vbox-macos-launch.sh --version dev
```

If only the port differs:

```sh
client-windows/scripts/vbox-macos-launch.sh --server-port 8081
```

### Network outages and server restarts

The client tolerates short network outages and server restarts:

- Events are buffered in a local SQLite file (`rootaika-client.db`) and marked as sent only after a successful upload. Unsent events stay queued until the server responds.
- HTTP calls (event upload, config and command polling) retry with exponential backoff (4 attempts by default, 0.5 s → 5 s) on transient errors: network errors, 5xx, and 429. 4xx errors are not retried.
- This bridges a server restart in seconds instead of waiting for the next upload cycle.

## Server API

The client uses the planned endpoints with Basic Auth:

- `POST /api/v1/events/batch`
- `GET /api/v1/client/config?client_id=...`
- `GET /api/v1/client/commands?client_id=...`
- `POST /api/v1/client/commands/{command_id}/ack`

The local API between the agent and the service is protected with an `X-Rootaika-Agent-Token` header generated into the config.
