# client-macos-screentime-collector

Kerää perheenjäsenten (lasten iOS-laitteiden) ruutuajan rootaikaan ilman
lasten laitteille asennettavaa ohjelmistoa: skriptaa macOS:n
Järjestelmäasetukset ▸ Näyttöaika -näkymää saavutettavuuspuun (AX) kautta ja
lähettää per-appi-päiväsummat synteettisinä `activity_observed`-eventteinä
palvelimen normaaliin `/api/v1/events/batch`-rajapintaan. Ei
serverimuutoksia. Suunnitelma ja prototyyppitulokset:
`plans/ios-screen-time-plan.md`.

Toimii taustaan fast-user-switchatussa sessiossa, myös kansi kiinni (AC-virta
+ `pmset disablesleep`) — todennettu 2026-07-10, macOS 26.5.1.

## Tiedostot

- `collector.py` — kerääjä (python3, vain stdlib; ajaa `osascript`-skrapen).
- `bin/start.sh` — asentaa ja käynnistää kerääjän yhdellä komennolla.
- `config.env.example` — asetuspohja; kopioi nimellä `config.env` ja täytä.
  `config.env` on .gitignoressa: henkilönimet ja salasanat eivät päädy repoon.
- `ax-probe.sh` — prototyypin skrape-sykli, pidetty vianetsintätyökaluna.

## Vaatimukset (kerääjä-Mac)

- `python3` (Xcode Command Line Tools — `xcode-select --install` jos
  `/usr/bin/python3` ei toimi). Vain stdlib, ei paketteja.
- Paikallinen käyttäjä `rootaika-scraper`, kirjattuna sisään huoltajan Apple
  ID:llä (Family Sharing), **käyttöliittymän kieli englanniksi** (kestoparsinta
  olettaa "N hours M minutes" -muodot). Tarkista että kaikkien lasten käyttö
  näkyy sen Näyttöaika-paneelissa.
- TCC-luvat scraper-tilissä (kertaluonteinen): Accessibility + Automation
  ("System Events"). Helpoin tapa: aja alla oleva käsiajo kerran terminaalista
  scraper-tilissä ja hyväksy kyselyt.
- Kansi-kiinni-käyttöön (vain kerääjä-Macilla, AC-virta):
  `sudo pmset -a disablesleep 1`.

## Käyttöönotto — yksi komento

Kertaalleen: kopioi `config.env.example` → `config.env` ja täytä jäsenten
nimet, palvelimen osoite ja client-tunnukset. Sitten scraper-tilissä:

```sh
cd client-macos-screentime-collector
bin/start.sh
```

Skripti lukee `config.env`:n (se yliajaa ympäristömuuttujat) ja generoi
LaunchAgentin
(`~/Library/LaunchAgents/fi.rootaika.ios-screentime.plist`), lataa sen ja
kerääjä käynnistyy heti — sen jälkeen 30 min välein. Konfiguraation vaihto:
aja `bin/start.sh` uudestaan uusilla arvoilla. Loki:
`~/Library/Logs/rootaika-ios-screentime.log`.

Pysäytys: `launchctl bootout gui/$(id -u)/fi.rootaika.ios-screentime`
(ja poista plist jos lopullinen).

Ensimmäisen ajon jälkeen laitteet ilmestyvät palvelimelle
`unassigned`-tilassa (yksi rootaika-laite per lapsi, iPhone+iPad yhdessä);
nimeä ja kytke ne lapsiin admin-UI:ssa.

**Joka uudelleenkäynnistyksen jälkeen:** kirjaa scraper-käyttäjä kerran
sisään ja fast-switchaa pois (tai tee siitä auto-login-tili) — LaunchAgent
elää vain kirjautuneessa sessiossa.

## Konfiguraatio (`config.env` / ympäristömuuttujat)

`bin/start.sh` lukee `config.env`:n ja leipoo arvot LaunchAgentin plistiin.
Samat muuttujat kelpaavat myös ympäristömuuttujina `collector.py`:n käsiajolle.

| Muuttuja | Oletus | Kuvaus |
|---|---|---|
| `ROOTAIKA_SERVER_URL` | `http://192.168.68.199:8080` | Palvelimen osoite |
| `ROOTAIKA_CLIENT_USERNAME` | `client` | Client-roolin tunnus |
| `ROOTAIKA_CLIENT_PASSWORD` | `client` | Client-roolin salasana |
| `ROOTAIKA_MEMBERS` | *(pakollinen)* | Jäsenten etunimet pilkuilla, esim. `Etunimi1,Etunimi2` — käytetään popupin type-selectiin JA laite-UUID:n johtamiseen (nimen vaihto luo uuden laitteen) |
| `ROOTAIKA_DAYS` | `7` | Luettava ikkuna päivinä (tänään + N−1 taaksepäin) |
| `ROOTAIKA_STATE_FILE` | `~/Library/Application Support/rootaika-ios-screentime/state.json` | Lähetyskirjanpito (vain käsiajossa; LaunchAgent käyttää oletusta) |

## Ajo käsin (testaus ja TCC-lupien hyväksyntä)

```sh
(set -a; . ./config.env; set +a; python3 collector.py --dry-run)  # skrapea + parsi, ei lähetä eikä kirjaa stateen
(set -a; . ./config.env; set +a; python3 collector.py)            # oikea ajo kerran
python3 collector.py --selftest                                   # parsinta- ja synteesitarkistukset ilman UI:ta
```

Skrape ottaa System Settingsin hetkeksi haltuun (näppäilee ja klikkaa) —
taustasessiossa tästä ei ole häiriötä.

## Miten se toimii

Joka ajo lukee jokaiselta jäseneltä trailing-ikkunan per-appi-päiväsummat
UI:sta. Päivän käyttö ladotaan "nauhalle" joka alkaa paikallisesta
keskiyöstä; state-tiedosto muistaa per (jäsen, päivä, appi) jo lähetetyt
sekunnit ja vain **kasvudelta** lähetetään nauhan jatkoksi (active-heartbeat
240 s välein + päättävä idle, jotta palvelimen 300 s gap cap ei katkaise).
Eventtien UUID:t johdetaan deterministisesti nauhaofsetista, joten
uudelleenlähetys kaatumisen tai kadonneen state-tiedoston jälkeen
deduplikoituu palvelimella eikä koskaan vääristä lukuja — state-tiedoston
saa turvallisesti poistaa (pakottaa täyden uudelleenlähetyksen, jonka
palvelin ohittaa duplikaatteina).

## Rajoitukset

- Päivänsisäinen sijoittelu on likimääräinen (summat per appi per päivä
  täsmäävät; kellonajat eivät vastaa todellista käyttöä). Sama hyväksytty
  linjaus kuin suunnitelmassa.
- UI näyttää kestot minuutin tarkkuudella; per-appi-päivävirhe ≤ 1 min.
- Skrape rikkoutuu macOS-päivityksen muuttaessa System Settingsin
  AX-rakennetta — tarkista `ax-probe.sh`:lla ison päivityksen jälkeen.
- Sovellus voi ensirenderöinnillä näkyä bundle-id:nä (`com.example.app`)
  ennen kuin näyttönimi latautuu; viiveet kattavat tämän yleensä, mutta
  harvinainen kaksoiskirjaus on mahdollinen (näkyy lokissa uutena appina).
- Jos jäsenvalinnan type-select osuu väärään nimeen (esim. samat alkukirjaimet),
  kerääjä huomaa sen popupin arvosta ja keskeyttää jäsenen äänekkäästi —
  käytä yksiselitteisiä etunimialkuja `ROOTAIKA_MEMBERS`issa.
