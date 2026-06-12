# rootaika Windows client

MVP-client koostuu kahdesta Go-binääristä:

- `rootaika-service`: service-tyyppinen prosessi, joka säilyttää configin, puskuroi eventit paikalliseen SQLiteen, lähettää batchit serverille, pollaa configin ja komennot sekä pitää agentin käynnissä.
- `rootaika-agent`: käyttäjäsessiossa ajettava agentti, joka lukee Windowsissa idle-ajan ja aktiivisen prosessin, lähettää `activity_observed`-eventit servicelle ja toteuttaa lock/unlock-pohjan.

## Konfiguraatio

Oletuspolku Windowsissa on:

```powershell
C:\ProgramData\rootaika\client.json
```

Ensimmäinen käynnistys luo configin ja pysyvän `client_id`-UUID:n. Tärkeimmät kentät:

```json
{
  "server_url": "http://127.0.0.1:8080",
  "client_username": "client",
  "client_password": "vaihda-tama",
  "agent_listen_address": "127.0.0.1:48611"
}
```

Polun voi antaa molemmille binääreille:

```powershell
.\rootaika-service.exe -config C:\ProgramData\rootaika\client.json
.\rootaika-agent.exe -config C:\ProgramData\rootaika\client.json
```

Ympäristömuuttujat `ROOTAIKA_SERVER_URL`, `ROOTAIKA_CLIENT_USERNAME`, `ROOTAIKA_CLIENT_PASSWORD` ja `ROOTAIKA_AGENT_LISTEN_ADDRESS` ohittavat tiedoston arvot ajonaikaisesti.

## Build

Kehityskoneella:

```sh
go test ./...
GOOS=windows GOARCH=amd64 go build ./cmd/rootaika-service ./cmd/rootaika-agent
```

Windowsissa:

```powershell
go build .\cmd\rootaika-service
go build .\cmd\rootaika-agent
```

## Asennus PowerShell-skriptillä

Hakemistossa `scripts/` on asennusautomaatio. Aja kaikki adminina (paitsi build).

1. Käännä binäärit:

```powershell
.\scripts\build.ps1
```

   Tämä tuottaa `dist\rootaika-service.exe` ja `dist\rootaika-agent.exe`. Agentti linkataan `-H=windowsgui`-lipulla, joten sillä ei ole konsoli-ikkunaa oletuksena. Debug-tila avaa konsolin ajonaikaisesti.

2. Asenna (avaa PowerShell adminina):

```powershell
.\scripts\install.ps1 -ServerUrl http://192.168.1.10:8080 -ClientPassword vaihda-tama
```

   Skripti:
   - kopioi binäärit hakemistoon `C:\Program Files\rootaika`,
   - kirjoittaa configin polkuun `C:\ProgramData\rootaika\client.json` (server-URL ja client-salasana mukana),
   - rekisteröi `rootaika-service`-servicen auto-startilla ja crash recoveryllä (`restart/5000` kolmesti),
   - rekisteröi agentin käynnistymään käyttäjän kirjautuessa (HKLM `Run`),
   - käynnistää servicen ja agentin heti.

3. Poista asennus:

```powershell
.\scripts\uninstall.ps1          # poistaa servicen ja autostartin
.\scripts\uninstall.ps1 -Purge   # poistaa myös binäärit ja configin/puskurin
```

Service ajetaan LocalSystem-tilillä ja avaa vain localhostiin sidotun agentti-endpointin. Agentti ajetaan käyttäjäsessiossa (service toimii watchdogina ja yrittää käynnistää agentin uudelleen, jos se sammuu).

### Verkon katkokset ja serverin uudelleenkäynnistys

Client kestää lyhyet verkkokatkokset ja serverin uudelleenkäynnistykset:

- Eventit puskuroidaan paikalliseen SQLite-tiedostoon (`rootaika-client.db`), ja merkitään lähetetyiksi vasta onnistuneen lähetyksen jälkeen. Lähettämättömät eventit jäävät jonoon, kunnes server vastaa.
- HTTP-kutsut (event-lähetys, config- ja komento-polling) yrittävät uudelleen exponentiaalisella backoffilla (oletuksena 4 yritystä, 0,5 s → 5 s) transienteissa virheissä: verkkovirheet, 5xx ja 429. 4xx-virheitä ei yritetä uudelleen.
- Tämä silloittaa serverin uudelleenkäynnistyksen sekunneissa sen sijaan, että odotettaisiin seuraavaa upload-sykliä.

## Server API

Client käyttää suunnitelman endpointteja Basic Authilla:

- `POST /api/v1/events/batch`
- `GET /api/v1/client/config?client_id=...`
- `GET /api/v1/client/commands?client_id=...`
- `POST /api/v1/client/commands/{command_id}/ack`

Agentin ja servicen välinen paikallinen API on suojattu configiin generoidulla `X-Rootaika-Agent-Token`-headerilla.
