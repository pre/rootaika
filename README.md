# rootaika

A screen-time tracking and enforcement system. It has three parts:

- **`server/`** – A Go HTTP server that receives client events, stores them in SQLite, and serves an HTML admin UI for reports, settings, and lock/unlock commands.
- **`client-windows/`** – A Windows client built as a single `rootaika.exe` that runs both a background service and a user-session agent (`rootaika.exe service` / `rootaika.exe agent`). It updates itself over the air from a public GitHub release.
- **`client-waveshare/`** – A Raspberry Pi + Waveshare e-ink display that shows today's per-device screen-time totals, fetched from the server as JSON.
- **`client-rp2040-button/`** – An RP2040 board with a physical button that locks (short press) or unlocks (long press) all devices via the server API.

The client sends activity events to the server, polls for config and commands, and enforces screen locking.

## Prerequisites

- Go 1.22 or newer (the server image build uses Go 1.24).
- Docker (optional, for running the server in a container).
- Windows + PowerShell 5.1 (for building and installing the client).

Both modules use pure-Go SQLite (`modernc.org/sqlite`), so **CGO is not required**.

---

## Server

### Build and run locally

```sh
cd server
go test ./...
go run ./cmd/rootaika-server
```

The default address is `http://localhost:8080/` and the database is `rootaika.db`. The admin UI is served at the root `/`.

Compiled binary:

```sh
cd server
go build -o rootaika-server ./cmd/rootaika-server
./rootaika-server
```

### Configuration (environment variables)

| Variable | Default | Description |
|---|---|---|
| `ROOTAIKA_ADDR` | `:8080` | Listen address/port |
| `ROOTAIKA_DB_PATH` | `rootaika.db` | SQLite file path |
| `ROOTAIKA_DATA_DIR` | dir of `ROOTAIKA_DB_PATH` | Directory for filesystem assets (uploaded lock-warning MP3) |
| `ROOTAIKA_ADMIN_USER` | `admin` | Admin UI user |
| `ROOTAIKA_ADMIN_PASSWORD` | `admin` | Admin UI password |
| `ROOTAIKA_CLIENT_USER` | `client` | Client API user |
| `ROOTAIKA_CLIENT_PASSWORD` | `client` | Client API password |

Change the credentials in production:

```sh
ROOTAIKA_ADMIN_PASSWORD='change-me' \
ROOTAIKA_CLIENT_PASSWORD='change-me-too' \
ROOTAIKA_ADDR=:8080 \
ROOTAIKA_DB_PATH=rootaika.db \
go run ./cmd/rootaika-server
```

### Docker

```sh
cd server
docker build -t rootaika-server .
docker run --rm -p 8080:8080 -v rootaika-data:/data \
  -e ROOTAIKA_ADMIN_PASSWORD='change-me' \
  -e ROOTAIKA_CLIENT_PASSWORD='change-me-too' \
  rootaika-server
```

The image builds a static binary (`CGO_ENABLED=0`), runs as a non-root user, and persists the database in the `/data` volume.

---

## Windows client

The client is a single `rootaika.exe` dispatched on its first argument, so one file swap updates everything:

- `rootaika.exe service`: a background process that holds the config, buffers events into a local SQLite database, sends batches to the server, polls for config and commands, keeps the agent running (watchdog), and performs over-the-air updates.
- `rootaika.exe agent`: a process that runs in the user session, reads idle time and the active process, sends `activity_observed` events to the service, and enforces lock/unlock.
- `rootaika.exe apply-update`: a transient helper the service launches to swap the binary and restart the service during an OTA update.

### Cross-build (Linux/macOS)

```sh
cd client-windows
go test ./...
GOOS=windows GOARCH=amd64 go build ./cmd/rootaika
```

### Build on Windows

Use the PowerShell script, which produces `rootaika.exe` into `dist/`:

```powershell
cd client-windows
.\scripts\build.ps1 -Version v1.2.0
```

The binary is linked with the `-H=windowsgui` flag, so neither process shows a console by default (debug mode allocates a console at runtime). Alternatively, build manually:

```powershell
go build -ldflags "-H=windowsgui -X rootaika/client-windows/internal/version.Version=v1.2.0" .\cmd\rootaika
```

### Over-the-air updates

The server declares a desired client version as a triple `(version, artifact_name, sha256)`. Releases are registered once in the admin UI's *Versiot* section, then the deployed version is *selected* from that registry: globally in settings, or as a per-device override (test one machine first). The client reports its own version on the config poll (`client_version`); when the server's desired version differs, the service downloads the named asset from the fixed public repo `github.com/pre/rootaika`, verifies its SHA256, and launches `apply-update` to swap the file and restart, without rebooting Windows. A failed version is not retried for 30 minutes, so a bad release cannot spin.

### Publishing a release

Push a version tag; GitHub Actions (`.github/workflows/release.yml`) builds `rootaika.exe` inside the pinned Go image and publishes the release with the asset, its `.sha256` file, and `install.ps1`. The release notes contain the triple to paste into the admin UI.

```sh
git tag v1.2.0 && git push origin v1.2.0
```

The build is reproducible: the same commit built in the same image yields a bit-identical exe, so anyone can verify a published release locally:

```sh
git checkout v1.2.0
make -C client-windows docker-build VERSION=v1.2.0
cat client-windows/dist/rootaika.exe.sha256   # must equal the release asset digest
```

The release is authored by the `rootaika-bot` machine account: it is a collaborator with write access, and its PAT (classic, `public_repo` scope only) is stored as the `RELEASE_BOT_TOKEN` Actions secret. The workflow's own `GITHUB_TOKEN` is read-only. Manual fallback from any box with `gh` authenticated: `cd client-windows && scripts/release.sh v1.2.0`.

### Install (PowerShell as administrator)

```powershell
.\scripts\install.ps1 -ClientPassword change-me
```

The script:

- copies `rootaika.exe` to `C:\Program Files\rootaika`,
- writes the config to `C:\ProgramData\rootaika\client.json`,
- registers the `rootaika-service` service (`rootaika.exe service`) with auto-start and crash recovery (`restart/5000` three times),
- registers the agent (`rootaika.exe agent`) to start on user logon (HKLM `Run`),
- starts the service and agent immediately.

### Uninstall

```powershell
.\scripts\uninstall.ps1          # removes the service and autostart
.\scripts\uninstall.ps1 -Purge   # also removes the binaries and config/buffer
```

### Configuration

The default path is `C:\ProgramData\rootaika\client.json`. The first launch creates the config and a persistent `client_id` UUID.

```json
{
  "server_url": "http://192.168.68.199:8080",
  "client_username": "client",
  "client_password": "change-me",
  "agent_listen_address": "127.0.0.1:48611"
}
```

The default server URL uses the direct LAN IP `http://192.168.68.199:8080` until DNS is available. Existing configs that still contain the old loopback default `http://127.0.0.1:8080` are migrated to the LAN IP on load. The environment variables `ROOTAIKA_SERVER_URL`, `ROOTAIKA_CLIENT_USERNAME`, `ROOTAIKA_CLIENT_PASSWORD`, and `ROOTAIKA_AGENT_LISTEN_ADDRESS` override the file values at runtime.

### Running in debug mode

Debug mode is read only from the config file's `debug_mode` field (or from the config the server pushes down). There is no command-line flag or environment variable for it. Each subcommand accepts only `-config`. When built with `-H=windowsgui`, `debug_mode: true` opens a console window at runtime.

To run against a specific server without touching the installed `C:\ProgramData\rootaika\client.json`, create a separate config such as `debug.json`:

```json
{
  "server_url": "http://192.168.68.199:8080",
  "client_username": "client",
  "client_password": "change-me",
  "agent_listen_address": "127.0.0.1:48611",
  "debug_mode": true
}
```

Then start the service against it (its watchdog launches the agent from the same exe):

```powershell
.\rootaika.exe service -config .\debug.json
```

The service fills in any missing fields (`client_id`, `agent_token`, intervals, db path) and saves them back on first run. Notes:

- If `rootaika-service` is already installed as a Windows service, stop it first (`sc stop rootaika-service` or `scripts\uninstall.ps1`) so two instances do not fight over the agent port (`48611`).
- `ROOTAIKA_SERVER_URL` can override the server URL at runtime, but `debug_mode` has no environment override and must be set in the config file.

For more detail, see [`client-windows/README.md`](client-windows/README.md) and [`server/README.md`](server/README.md).

---

## End-to-end setup

1. Start the server (Docker or binary) and set strong `ROOTAIKA_ADMIN_PASSWORD` and `ROOTAIKA_CLIENT_PASSWORD` values.
2. Build the client (`rootaika.exe`) with the `scripts\build.ps1` script.
3. Install the client on each Windows machine with `scripts\install.ps1`, providing the client password and overriding `-ServerUrl` only if needed.
4. Manage machines, reports, and locks from the admin UI at `http://<server>:8080/`.

## Server API

The client uses these endpoints with Basic Auth (the `client` credentials):

- `POST /api/v1/events/batch`
- `GET /api/v1/client/config?client_id=<uuid>`
- `GET /api/v1/client/commands?client_id=<uuid>`
- `POST /api/v1/client/commands/{command_id}/ack?client_id=<uuid>`

The e-ink board client (`client-waveshare/`) uses the `client` credentials to read:

- `GET /api/v1/board/today` – today's per-device active minutes plus the admin-configured refresh interval, as compact JSON.

The local API between the agent and the service is protected by an `X-Rootaika-Agent-Token` header generated into the config.

## License

MIT, see [LICENSE](LICENSE).
