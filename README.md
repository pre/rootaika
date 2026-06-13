# rootaika

A screen-time tracking and enforcement system. It has two parts:

- **`server/`** – A Go HTTP server that receives client events, stores them in SQLite, and serves an HTML admin UI for reports, settings, and lock/unlock commands.
- **`client-windows/`** – A Windows client made of two binaries: a background `rootaika-service` and a user-session `rootaika-agent`.

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

The client consists of two binaries:

- `rootaika-service`: a background process that holds the config, buffers events into a local SQLite database, sends batches to the server, polls for config and commands, and keeps the agent running (watchdog).
- `rootaika-agent`: a process that runs in the user session, reads idle time and the active process, sends `activity_observed` events to the service, and enforces lock/unlock.

### Cross-build (Linux/macOS)

```sh
cd client-windows
go test ./...
GOOS=windows GOARCH=amd64 go build ./cmd/rootaika-service ./cmd/rootaika-agent
```

### Build on Windows

Use the PowerShell script, which produces both binaries into `dist/`:

```powershell
cd client-windows
.\scripts\build.ps1
```

The agent is linked with the `-H=windowsgui` flag, so it has no console window by default (debug mode allocates a console at runtime). Alternatively, build manually:

```powershell
go build .\cmd\rootaika-service
go build -ldflags "-H=windowsgui" .\cmd\rootaika-agent
```

### Install (PowerShell as administrator)

```powershell
.\scripts\install.ps1 -ServerUrl http://192.168.1.10:8080 -ClientPassword change-me
```

The script:

- copies the binaries to `C:\Program Files\rootaika`,
- writes the config to `C:\ProgramData\rootaika\client.json`,
- registers the `rootaika-service` service with auto-start and crash recovery (`restart/5000` three times),
- registers the agent to start on user logon (HKLM `Run`),
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
  "server_url": "http://127.0.0.1:8080",
  "client_username": "client",
  "client_password": "change-me",
  "agent_listen_address": "127.0.0.1:48611"
}
```

The environment variables `ROOTAIKA_SERVER_URL`, `ROOTAIKA_CLIENT_USERNAME`, `ROOTAIKA_CLIENT_PASSWORD`, and `ROOTAIKA_AGENT_LISTEN_ADDRESS` override the file values at runtime.

### Running in debug mode

Debug mode is read only from the config file's `debug_mode` field (or from the config the server pushes down). There is no command-line flag or environment variable for it. The binaries accept only `-config`. When the agent is built with `-H=windowsgui`, `debug_mode: true` opens a console window at runtime.

To run against a specific server (for example `192.168.68.126:8080`) without touching the installed `C:\ProgramData\rootaika\client.json`, create a separate config such as `debug.json`:

```json
{
  "server_url": "http://192.168.68.126:8080",
  "client_username": "client",
  "client_password": "change-me",
  "agent_listen_address": "127.0.0.1:48611",
  "debug_mode": true
}
```

Then start both binaries against it in two separate windows:

```powershell
# Window 1: service
.\rootaika-service.exe -config .\debug.json

# Window 2: agent (debug_mode opens a console at runtime)
.\rootaika-agent.exe -config .\debug.json
```

The service fills in any missing fields (`client_id`, `agent_token`, intervals, db path) and saves them back on first run. Notes:

- If `rootaika-service` is already installed as a Windows service, stop it first (`sc stop rootaika-service` or `scripts\uninstall.ps1`) so two instances do not fight over the agent port (`48611`).
- `ROOTAIKA_SERVER_URL` can override the server URL at runtime, but `debug_mode` has no environment override and must be set in the config file.

For more detail, see [`client-windows/README.md`](client-windows/README.md) and [`server/README.md`](server/README.md).

---

## End-to-end setup

1. Start the server (Docker or binary) and set strong `ROOTAIKA_ADMIN_PASSWORD` and `ROOTAIKA_CLIENT_PASSWORD` values.
2. Build the client with the `scripts\build.ps1` script.
3. Install the client on each Windows machine with `scripts\install.ps1`, providing the server URL and client password.
4. Manage machines, reports, and locks from the admin UI at `http://<server>:8080/`.

## Server API

The client uses these endpoints with Basic Auth (the `client` credentials):

- `POST /api/v1/events/batch`
- `GET /api/v1/client/config?client_id=<uuid>`
- `GET /api/v1/client/commands?client_id=<uuid>`
- `POST /api/v1/client/commands/{command_id}/ack?client_id=<uuid>`

The local API between the agent and the service is protected by an `X-Rootaika-Agent-Token` header generated into the config.

## License

MIT, see [LICENSE](LICENSE).
