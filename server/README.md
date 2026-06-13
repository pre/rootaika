# rootaika server

A Go HTTP server that receives events from Windows clients, stores them in SQLite, and provides a small HTML admin view.

## Running locally

```sh
cd server
go run ./cmd/rootaika-server
```

The default address is `http://localhost:8080/` and the database is `rootaika.db`.

Default credentials:

- admin: `admin` / `admin`
- client: `client` / `client`

Change the credentials in production with environment variables:

```sh
ROOTAIKA_ADMIN_USER=admin \
ROOTAIKA_ADMIN_PASSWORD='change-me' \
ROOTAIKA_CLIENT_USER=client \
ROOTAIKA_CLIENT_PASSWORD='change-me-too' \
ROOTAIKA_ADDR=:8080 \
ROOTAIKA_DB_PATH=rootaika.db \
go run ./cmd/rootaika-server
```

## Docker

```sh
cd server
docker build -t rootaika-server .
docker run --rm -p 8080:8080 -v rootaika-data:/data \
  -e ROOTAIKA_ADMIN_PASSWORD='change-me' \
  -e ROOTAIKA_CLIENT_PASSWORD='change-me-too' \
  rootaika-server
```

## Client API

The client uses the Basic Auth credentials for `client`.

- `POST /api/v1/events/batch`
- `GET /api/v1/client/config?client_id=<uuid>`
- `GET /api/v1/client/commands?client_id=<uuid>`
- `POST /api/v1/client/commands/{command_id}/ack?client_id=<uuid>`

The admin lock/unlock and the settings and category changes are available in the web UI at `/`.
