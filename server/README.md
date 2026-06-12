# rootaika server

Go HTTP server, joka vastaanottaa Windows-clienttien tapahtumat, tallentaa ne SQLiteen ja tarjoaa pienen HTML-adminnäkymän.

## Ajo paikallisesti

```sh
cd server
go run ./cmd/rootaika-server
```

Oletusosoite on `http://localhost:8080/` ja tietokanta `rootaika.db`.

Oletustunnukset:

- admin: `admin` / `admin`
- client: `client` / `client`

Vaihda tunnukset tuotannossa ympäristömuuttujilla:

```sh
ROOTAIKA_ADMIN_USER=admin \
ROOTAIKA_ADMIN_PASSWORD='vaihda-tama' \
ROOTAIKA_CLIENT_USER=client \
ROOTAIKA_CLIENT_PASSWORD='vaihda-tama-myos' \
ROOTAIKA_ADDR=:8080 \
ROOTAIKA_DB_PATH=rootaika.db \
go run ./cmd/rootaika-server
```

## Docker

```sh
cd server
docker build -t rootaika-server .
docker run --rm -p 8080:8080 -v rootaika-data:/data \
  -e ROOTAIKA_ADMIN_PASSWORD='vaihda-tama' \
  -e ROOTAIKA_CLIENT_PASSWORD='vaihda-tama-myos' \
  rootaika-server
```

## Client API

Client käyttää Basic Auth -tunnusta `client`.

- `POST /api/v1/events/batch`
- `GET /api/v1/client/config?client_id=<uuid>`
- `GET /api/v1/client/commands?client_id=<uuid>`
- `POST /api/v1/client/commands/{command_id}/ack?client_id=<uuid>`

Adminin lock/unlock ja asetusten sekä kategorioiden muutokset löytyvät web-UI:sta osoitteesta `/`.
