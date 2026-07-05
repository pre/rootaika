package rootaika

import (
	"html/template"
	"net/http"
	"time"
)

type settingsData struct {
	Role                Role
	ReadOnly            bool
	Now                 time.Time
	Devices             []deviceView
	Users               []User
	Settings            Settings
	Categories          []ProgramCategory
	WarningSoundEnabled bool
	WarningSoundVersion string
	Versions            []ClientVersion
	// GlobalVersionID mirrors Settings.SelectedVersionID as int64 so the
	// template can compare it against ClientVersion.ID with eq.
	GlobalVersionID int64
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	role, ok := a.requireRole(w, r, RoleAdmin, RoleClient)
	if !ok {
		return
	}

	data, err := a.settingsViewData(r, role)
	if err != nil {
		http.Error(w, "settings failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := settingsTemplate.Execute(w, data); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func (a *App) settingsViewData(r *http.Request, role Role) (settingsData, error) {
	ctx := r.Context()
	settings, err := a.store.Settings(ctx)
	if err != nil {
		return settingsData{}, err
	}
	users, err := a.store.Users(ctx)
	if err != nil {
		return settingsData{}, err
	}
	devices, err := a.store.Devices(ctx)
	if err != nil {
		return settingsData{}, err
	}
	categories, err := a.store.Categories(ctx)
	if err != nil {
		return settingsData{}, err
	}
	versions, err := a.store.Versions(ctx)
	if err != nil {
		return settingsData{}, err
	}

	versionByID := make(map[int64]ClientVersion, len(versions))
	for _, v := range versions {
		versionByID[v.ID] = v
	}

	deviceViews := make([]deviceView, 0, len(devices))
	for _, device := range devices {
		view := deviceView{Device: device, LockState: lockState(device)}
		// The OTA target this device resolves to: its own selection when set,
		// otherwise the global selection. Empty when neither points at a
		// registered version.
		effectiveID := int64(settings.SelectedVersionID)
		if device.SelectedVersionID != nil {
			effectiveID = *device.SelectedVersionID
			view.TargetIsOverride = true
		}
		if target, ok := versionByID[effectiveID]; ok {
			view.TargetVersion = target.Version
		}
		deviceViews = append(deviceViews, view)
	}

	return settingsData{
		Role:                role,
		ReadOnly:            role != RoleAdmin,
		Now:                 a.now().In(a.location),
		Devices:             deviceViews,
		Users:               users,
		Settings:            settings,
		Categories:          categories,
		WarningSoundEnabled: a.warningSound.enabled(),
		WarningSoundVersion: a.warningSound.version(),
		Versions:            versions,
		GlobalVersionID:     int64(settings.SelectedVersionID),
	}, nil
}

var settingsTemplate = template.Must(template.New("settings").Funcs(template.FuncMap{
	"formatTime":      formatLocal,
	"selectedUser":    selectedUser,
	"selectedVersion": selectedVersion,
}).Parse(`<!doctype html>
<html lang="fi">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>rootaika · Asetukset</title>
  <style>
    :root { color-scheme: light; --bg:#f6f7f9; --surface:#fff; --text:#1f2933; --muted:#5b6673; --border:#d8dee6; --accent:#0f766e; --warn:#8a5a00; --warn-bg:#fff5d6; }
    * { box-sizing: border-box; }
    body { margin:0; background:var(--bg); color:var(--text); font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; line-height:1.45; }
    main { max-width:1180px; margin:0 auto; padding:28px 18px 56px; }
    header { display:flex; justify-content:space-between; gap:16px; align-items:flex-start; margin-bottom:18px; }
    h1 { margin:0; font-size:2rem; letter-spacing:0; }
    h2 { margin:30px 0 10px; font-size:1.25rem; letter-spacing:0; }
    p { margin:0 0 8px; }
    table { width:100%; border-collapse:collapse; background:var(--surface); border:1px solid var(--border); border-radius:8px; overflow:hidden; }
    th, td { padding:9px 10px; border-bottom:1px solid var(--border); text-align:left; vertical-align:top; }
    th { background:#edf2f7; font-weight:650; }
    tr:last-child td { border-bottom:0; }
    input, select, button { font:inherit; }
    input, select { min-height:34px; padding:5px 8px; border:1px solid var(--border); border-radius:6px; background:#fff; }
    button { min-height:34px; padding:5px 10px; border:1px solid #0c5f59; border-radius:6px; background:var(--accent); color:#fff; cursor:pointer; }
    button.secondary { border-color:var(--border); background:#fff; color:var(--text); }
    button.warn { border-color:#7a5100; background:var(--warn); color:#fff; }
    code { padding:1px 4px; border-radius:4px; background:#eef2f7; }
    .muted { color:var(--muted); }
    .pill { display:inline-block; padding:2px 8px; border-radius:999px; background:#e6f4f1; color:var(--accent); font-size:.86rem; font-weight:650; }
    .notice { padding:10px 12px; border:1px solid #f0d98b; border-radius:8px; background:var(--warn-bg); color:var(--warn); margin:12px 0; }
    .grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(230px,1fr)); gap:12px; }
    nav.inline { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
    .inline { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
    .stack { display:grid; gap:8px; }
    .actions form { margin:0 0 6px; }
    .compact th, .compact td { padding:7px 8px; }
    @media (max-width: 760px) {
      header { display:block; }
      table { display:block; overflow-x:auto; }
      .inline { display:grid; align-items:stretch; }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <span class="pill">rootaika</span>
        <h1>Asetukset</h1>
        <p class="muted">Rooli {{.Role}}. Päivitetty {{formatTime .Now}}.</p>
      </div>
      ` + chartNav + `
    </header>

    {{if .ReadOnly}}<div class="notice">Client-tunnuksella näkymä on read-only. Muutokset vaativat admin-tunnuksen.</div>{{end}}

    <section id="devices">
      <h2>Laitteet</h2>
      <table>
        <thead><tr><th>ID</th><th>Nimi</th><th>UUID</th><th>Käyttäjä</th><th>Tila</th><th>Viimeksi nähty</th><th>Versio</th><th>Admin</th></tr></thead>
        <tbody>
        {{range .Devices}}
        {{$device := .}}
          <tr>
            <td>{{.Device.ID}}</td>
            <td>{{.Device.DisplayName}}</td>
            <td><code>{{.Device.ClientUUID}}</code></td>
            <td>{{if .Device.UserName}}{{.Device.UserName}}{{else}}<span class="muted">ei liitetty</span>{{end}}</td>
            <td>{{.Device.RegistrationStatus}}<br><span class="muted">{{.LockState}}</span></td>
            <td>{{formatTime .Device.LastSeenAt}}</td>
            <td>
              {{if .Device.LastClientVersion}}<code>{{.Device.LastClientVersion}}</code><br><span class="muted">{{formatTime .Device.LastClientVersionAt}}</span>{{else}}<span class="muted">-</span>{{end}}
              {{if .TargetVersion}}<br><span class="muted">→ {{.TargetVersion}}{{if .TargetIsOverride}} (laite){{end}}</span>{{end}}
            </td>
            <td class="actions">
              {{if $.ReadOnly}}<span class="muted">read-only</span>{{else}}
              <form method="post" action="/admin/devices/{{.Device.ID}}/assign" class="stack">
                <input name="display_name" value="{{.Device.DisplayName}}" aria-label="Laitteen nimi">
                <select name="user_id" aria-label="Käyttäjä">
                  <option value="">Ei käyttäjää</option>
                  {{range $.Users}}<option value="{{.ID}}" {{if selectedUser .ID $device.Device.UserID}}selected{{end}}>{{.Name}}</option>{{end}}
                </select>
                <button class="secondary" type="submit">Tallenna</button>
              </form>
              <form method="post" action="/admin/devices/{{.Device.ID}}/lock" class="stack">
                <input name="message" placeholder="Viesti lukitusruudulle" aria-label="Lukitusviesti">
                <input name="warning_seconds" type="number" min="0" max="600" value="60" aria-label="Varoitusaika sekunteina" title="Varoitusaika sekunteina ennen lukitusta (0 = lukitse heti)">
                <button class="warn" type="submit">Lock</button>
              </form>
              <form method="post" action="/admin/devices/{{.Device.ID}}/unlock"><button type="submit">Unlock</button></form>
              <form method="post" action="/admin/devices/{{.Device.ID}}/version" class="stack">
                <select name="selected_version_id" aria-label="Versio">
                  <option value="0">Globaali oletus</option>
                  {{range $.Versions}}<option value="{{.ID}}" {{if selectedVersion .ID $device.Device.SelectedVersionID}}selected{{end}}>{{.Version}}{{if .ArtifactName}} ({{.ArtifactName}}){{end}}</option>{{end}}
                </select>
                <button class="secondary" type="submit">Aseta versio</button>
              </form>
              <form method="post" action="/admin/devices/{{.Device.ID}}/delete"><button class="secondary" type="submit" onclick="return confirm('Poistetaanko laite ja sen tapahtumat pysyvästi?')">Poista</button></form>
              {{end}}
            </td>
          </tr>
        {{else}}
          <tr><td colspan="8" class="muted">Ei laitteita.</td></tr>
        {{end}}
        </tbody>
      </table>
    </section>

    <section id="users">
      <h2>Käyttäjät</h2>
      <table class="compact">
        <thead><tr><th>ID</th><th>Nimi</th><th>Luotu</th><th>Admin</th></tr></thead>
        <tbody>
        {{range .Users}}
          <tr>
            <td>{{.ID}}</td>
            <td>{{.Name}}</td>
            <td>{{formatTime .CreatedAt}}</td>
            <td class="actions">
              {{if $.ReadOnly}}<span class="muted">read-only</span>{{else}}
              <form method="post" action="/admin/users/{{.ID}}/rename" class="inline">
                <input name="name" value="{{.Name}}" aria-label="Käyttäjän nimi" required>
                <button class="secondary" type="submit">Tallenna</button>
              </form>
              {{end}}
            </td>
          </tr>
        {{else}}
          <tr><td colspan="4" class="muted">Ei käyttäjiä.</td></tr>
        {{end}}
        </tbody>
      </table>
      {{if not .ReadOnly}}
      <form method="post" action="/admin/users" class="inline">
        <input name="name" placeholder="Käyttäjän nimi" required>
        <button type="submit">Luo käyttäjä</button>
      </form>
      {{end}}
    </section>

    <section id="settings">
      <h2>Asetukset</h2>
      <form method="post" action="/admin/settings" class="grid">
        <label class="stack">Idle-raja, s<input name="idle_threshold_seconds" type="number" min="1" value="{{.Settings.IdleThresholdSeconds}}" {{if .ReadOnly}}disabled{{end}}></label>
        <label class="stack">Upload-väli, s<input name="upload_interval_seconds" type="number" min="1" value="{{.Settings.UploadIntervalSeconds}}" {{if .ReadOnly}}disabled{{end}}></label>
        <label class="stack">Polling-väli, s<input name="poll_interval_seconds" type="number" min="1" value="{{.Settings.PollIntervalSeconds}}" {{if .ReadOnly}}disabled{{end}}></label>
        <label class="stack">Maksimilaskentaväli, s<input name="max_countable_gap_seconds" type="number" min="1" value="{{.Settings.MaxCountableGapSeconds}}" {{if .ReadOnly}}disabled{{end}}></label>
        <label class="stack">Kuvaajan y-maksimi, min<input name="chart_y_max_minutes" type="number" min="1" value="{{.Settings.ChartYMaxMinutes}}" {{if .ReadOnly}}disabled{{end}}></label>
        <label class="stack">Taulun päivitysväli, s<input name="board_refresh_seconds" type="number" min="1" value="{{.Settings.BoardRefreshSeconds}}" {{if .ReadOnly}}disabled{{end}}></label>
        <label class="inline"><input name="debug_mode" type="checkbox" value="on" {{if .Settings.DebugMode}}checked{{end}} {{if .ReadOnly}}disabled{{end}}> Debug-tila (näytä clientin konsoli)</label>
        <label class="inline"><input name="debug_unassigned_clients" type="checkbox" value="on" {{if .Settings.DebugUnassignedClients}}checked{{end}} {{if .ReadOnly}}disabled{{end}}> Debug-tila rekisteröimättömille clienteille</label>
        <label class="stack">Haluttu client-versio (globaali)
          <select name="selected_version_id" {{if .ReadOnly}}disabled{{end}}>
            <option value="0">Ei versiota</option>
            {{range .Versions}}<option value="{{.ID}}" {{if eq .ID $.GlobalVersionID}}selected{{end}}>{{.Version}}{{if .ArtifactName}} ({{.ArtifactName}}){{end}}</option>{{end}}
          </select>
        </label>
        {{if not .ReadOnly}}<div><button type="submit">Tallenna asetukset</button></div>{{end}}
      </form>

      <h3>Lukitusvaroituksen ääni (MP3)</h3>
      {{if .WarningSoundEnabled}}
        {{if .WarningSoundVersion}}
        <p class="muted">Ääni asetettu (versio {{.WarningSoundVersion}}). Client soittaa tämän varoituksen aikana.</p>
        {{else}}
        <p class="muted">Ei ääntä asetettu. Varoitus näkyy ruudulla mutta ääntä ei soiteta.</p>
        {{end}}
        {{if not .ReadOnly}}
        <form method="post" action="/admin/settings/warning-sound" enctype="multipart/form-data" class="inline">
          <input name="sound" type="file" accept="audio/mpeg,.mp3" required aria-label="MP3-tiedosto">
          <button type="submit">Lataa ääni</button>
        </form>
        {{end}}
      {{else}}
        <p class="muted">Ääntä ei voi tallentaa: palvelimen data-hakemistoa ei ole määritetty.</p>
      {{end}}
    </section>

    <section id="versions">
      <h2>Versiot</h2>
      <p class="muted">Rekisteröidyt client-versiot. Valitse käyttöön otettava versio Asetukset-osiossa (globaali) tai laitekohtaisesti Laitteet-taulukosta.</p>
      <table class="compact">
        <thead><tr><th>Versio</th><th>Artifakti</th><th>SHA256</th><th></th></tr></thead>
        <tbody>
        {{range .Versions}}
          {{if $.ReadOnly}}
          <tr><td><code>{{.Version}}</code></td><td>{{.ArtifactName}}</td><td><code>{{.SHA256}}</code></td><td></td></tr>
          {{else}}
          <tr><td colspan="4">
            <form method="post" action="/admin/versions/{{.ID}}/edit" class="inline">
              <input name="version" value="{{.Version}}" placeholder="v1.2.0" required aria-label="Versio" style="min-width:110px">
              <input name="artifact" value="{{.ArtifactName}}" placeholder="rootaika.exe" aria-label="Artifakti" style="min-width:150px">
              <input name="sha256" value="{{.SHA256}}" placeholder="sha256" aria-label="SHA256" style="min-width:260px">
              <button class="secondary" type="submit">Tallenna</button>
            </form>
            <form method="post" action="/admin/versions/{{.ID}}/delete" style="display:inline"><button class="secondary" type="submit" onclick="return confirm('Poistetaanko versio? Valinnat palautuvat oletukseen.')">Poista</button></form>
          </td></tr>
          {{end}}
        {{else}}<tr><td colspan="4" class="muted">Ei rekisteröityjä versioita.</td></tr>{{end}}
        </tbody>
      </table>
      {{if not .ReadOnly}}
      <form method="post" action="/admin/versions" class="inline">
        <input name="version" placeholder="v1.2.0" required aria-label="Versio">
        <input name="artifact" placeholder="rootaika.exe" aria-label="Artifakti">
        <input name="sha256" placeholder="sha256" aria-label="SHA256">
        <button type="submit">Lisää versio</button>
      </form>
      {{end}}
    </section>

    <section id="categories">
      <h2>Kategoriat</h2>
      <table class="compact">
        <thead><tr><th>Tyyppi</th><th>Pattern</th><th>Kategoria</th><th></th></tr></thead>
        <tbody>
        {{range .Categories}}
          <tr>
            <td>{{.MatchType}}</td><td><code>{{.Pattern}}</code></td><td>{{.Category}}</td>
            <td>{{if $.ReadOnly}}{{else}}<form method="post" action="/admin/categories/{{.ID}}/delete"><button class="secondary" type="submit">Poista</button></form>{{end}}</td>
          </tr>
        {{else}}<tr><td colspan="4" class="muted">Ei kategoriasääntöjä.</td></tr>{{end}}
        </tbody>
      </table>
      {{if not .ReadOnly}}
      <form method="post" action="/admin/categories" class="inline">
        <select name="match_type">
          <option value="exact">exact</option>
          <option value="contains">contains</option>
          <option value="prefix">prefix</option>
        </select>
        <input name="pattern" placeholder="steam.exe" required>
        <input name="category" placeholder="pelit" required>
        <button type="submit">Lisää kategoria</button>
      </form>
      {{end}}
    </section>

  </main>
</body>
</html>`))
