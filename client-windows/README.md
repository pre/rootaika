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

## Käyttö MVP:ssä

1. Käännä `rootaika-service.exe` ja `rootaika-agent.exe` samaan hakemistoon.
2. Luo tai käynnistä kerran config, lisää `client_password` vastaamaan serverin client Basic Auth -salasanaa.
3. Aja service admin-oikeuksilla. Service avaa vain localhostiin sidotun agentti-endpointin ja puskuroi eventit SQLiteen.
4. Agentti voidaan käynnistää manuaalisesti käyttäjäsessiossa; service yrittää myös käynnistää samasta hakemistosta löytyvän `rootaika-agent.exe`-prosessin.

Windows Service -asennuksen luonnos:

```powershell
sc.exe create rootaika-service binPath= "C:\Program Files\rootaika\rootaika-service.exe -config C:\ProgramData\rootaika\client.json" start= auto
sc.exe failure rootaika-service reset= 60 actions= restart/5000/restart/5000/restart/5000
sc.exe start rootaika-service
```

Service-agent-session integraatio on MVP-tasoinen: LocalSystem-servicen käynnistämä agentti ei vielä tee varsinaista aktiivisen käyttäjäsession prosessinluontia. Käytännön ensimmäisessä asennuksessa agentti kannattaa käynnistää kirjautumisen yhteydessä, ja service toimii watchdogina niissä ympäristöissä, joissa child-prosessi syntyy oikeaan sessioon.

## Server API

Client käyttää suunnitelman endpointteja Basic Authilla:

- `POST /api/v1/events/batch`
- `GET /api/v1/client/config?client_id=...`
- `GET /api/v1/client/commands?client_id=...`
- `POST /api/v1/client/commands/{command_id}/ack`

Agentin ja servicen välinen paikallinen API on suojattu configiin generoidulla `X-Rootaika-Agent-Token`-headerilla.
