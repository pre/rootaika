package rootaika

import (
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"time"
)

type dashboardData struct {
	Role       Role
	ReadOnly   bool
	Now        time.Time
	TodayLabel string
	Devices    []deviceView
	Users      []User
	Settings   Settings
	Categories []ProgramCategory
}

type deviceView struct {
	Device       Device
	TotalSeconds int64
	Processes    []processView
	LockState    string
}

type processView struct {
	Name    string
	Seconds int64
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	role, ok := a.requireRole(w, r, RoleAdmin, RoleClient)
	if !ok {
		return
	}

	data, err := a.dashboardData(r, role)
	if err != nil {
		http.Error(w, "dashboard failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTemplate.Execute(w, data); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func (a *App) dashboardData(r *http.Request, role Role) (dashboardData, error) {
	ctx := r.Context()
	now := a.now()
	localNow := now.In(a.location)
	startLocal := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, a.location)
	start := startLocal.UTC()
	end := now.UTC()

	settings, err := a.store.Settings(ctx)
	if err != nil {
		return dashboardData{}, err
	}
	users, err := a.store.Users(ctx)
	if err != nil {
		return dashboardData{}, err
	}
	devices, err := a.store.Devices(ctx)
	if err != nil {
		return dashboardData{}, err
	}
	categories, err := a.store.Categories(ctx)
	if err != nil {
		return dashboardData{}, err
	}

	deviceViews := make([]deviceView, 0, len(devices))
	for _, device := range devices {
		events, err := a.store.ReportEvents(ctx, device.ID, start, end)
		if err != nil {
			return dashboardData{}, err
		}
		report := CalculateUsage(events, start, end, now, time.Duration(settings.MaxCountableGapSeconds)*time.Second)
		view := deviceView{
			Device:       device,
			TotalSeconds: report.TotalSeconds,
			Processes:    processViews(report.ByProcess),
			LockState:    lockState(device),
		}
		deviceViews = append(deviceViews, view)
	}

	return dashboardData{
		Role:       role,
		ReadOnly:   role != RoleAdmin,
		Now:        localNow,
		TodayLabel: startLocal.Format("2006-01-02"),
		Devices:    deviceViews,
		Users:      users,
		Settings:   settings,
		Categories: categories,
	}, nil
}

func processViews(byProcess map[string]int64) []processView {
	processes := make([]processView, 0, len(byProcess))
	for process, seconds := range byProcess {
		processes = append(processes, processView{Name: process, Seconds: seconds})
	}
	sort.Slice(processes, func(i, j int) bool {
		if processes[i].Seconds == processes[j].Seconds {
			return processes[i].Name < processes[j].Name
		}
		return processes[i].Seconds > processes[j].Seconds
	})
	return processes
}

// lockState describes the lock status shown in the admin UI. When the admin
// requests a lock, the device is not "lukittu" yet: it becomes locked only once
// the client reports state=locked back during a config poll. Until then it shows
// as pending, including the configured warning delay, so the admin sees the
// device is on its way to locking rather than already locked.
func lockState(device Device) string {
	if !device.Locked {
		return "avattu"
	}
	if device.LastStatus == StateLocked {
		return "lukittu"
	}
	if device.WarningSeconds > 0 {
		return "lukitaan (" + strconv.Itoa(device.WarningSeconds) + " s varoitus)"
	}
	return "lukitaan…"
}

func humanSeconds(seconds int64) string {
	if seconds <= 0 {
		return "0 min"
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	if hours > 0 {
		if minutes == 0 {
			return strconv.FormatInt(hours, 10) + " h"
		}
		return strconv.FormatInt(hours, 10) + " h " + strconv.FormatInt(minutes, 10) + " min"
	}
	if minutes > 0 {
		return strconv.FormatInt(minutes, 10) + " min"
	}
	return strconv.FormatInt(seconds, 10) + " s"
}

func formatLocal(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.In(templateLocation).Format("2006-01-02 15:04:05")
}

func formatLocalPtr(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return formatLocal(*t)
}

func selectedUser(userID int64, deviceUserID *int64) bool {
	return deviceUserID != nil && *deviceUserID == userID
}

// deviceLabel renders a collapsible section title as "User name (device id)",
// falling back to the device display name when no user is assigned yet.
func deviceLabel(device Device) string {
	name := device.UserName
	if name == "" {
		name = device.DisplayName
	}
	return name + " (" + strconv.FormatInt(device.ID, 10) + ")"
}

var templateLocation = func() *time.Location {
	location, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		return time.Local
	}
	return location
}()

var dashboardTemplate = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"human":         humanSeconds,
	"formatTime":    formatLocal,
	"formatTimePtr": formatLocalPtr,
	"selectedUser":  selectedUser,
	"deviceLabel":   deviceLabel,
}).Parse(`<!doctype html>
<html lang="fi">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>rootaika</title>
  <style>
    :root { color-scheme: light; --bg:#f6f7f9; --surface:#fff; --text:#1f2933; --muted:#5b6673; --border:#d8dee6; --accent:#0f766e; --warn:#8a5a00; --warn-bg:#fff5d6; }
    * { box-sizing: border-box; }
    body { margin:0; background:var(--bg); color:var(--text); font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; line-height:1.45; }
    main { max-width:1180px; margin:0 auto; padding:28px 18px 56px; }
    header { display:flex; justify-content:space-between; gap:16px; align-items:flex-start; margin-bottom:18px; }
    h1 { margin:0; font-size:2rem; letter-spacing:0; }
    h2 { margin:30px 0 10px; font-size:1.25rem; letter-spacing:0; }
    h3 { margin:16px 0 8px; font-size:1rem; letter-spacing:0; }
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
    .card { padding:14px; border:1px solid var(--border); border-radius:8px; background:var(--surface); }
    .card strong { display:block; margin-bottom:6px; color:var(--accent); }
    .inline { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
    .stack { display:grid; gap:8px; }
    .actions form { margin:0 0 6px; }
    .compact th, .compact td { padding:7px 8px; }
    details.device { border:1px solid var(--border); border-radius:8px; background:var(--surface); overflow:hidden; }
    details.device > summary { display:flex; justify-content:space-between; align-items:center; gap:12px; padding:11px 14px; cursor:pointer; list-style:none; font-weight:650; }
    details.device > summary::-webkit-details-marker { display:none; }
    details.device > summary::before { content:"\25B8"; color:var(--muted); font-weight:400; margin-right:2px; transition:transform .12s ease; }
    details.device[open] > summary::before { transform:rotate(90deg); }
    details.device > summary:hover { background:#f0f4f8; }
    .device-title { flex:1; color:var(--accent); }
    .device-total { color:var(--muted); font-weight:600; }
    .device-body { padding:0 14px 14px; }
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
        <h1>Ruutuaika</h1>
        <p class="muted">Tänään {{.TodayLabel}}, rooli {{.Role}}. Päivitetty {{formatTime .Now}}.</p>
      </div>
      ` + chartNav + `
    </header>

    {{if .ReadOnly}}<div class="notice">Client-tunnuksella näkymä on read-only. Muutokset vaativat admin-tunnuksen.</div>{{end}}

    <section id="today">
      <h2>Tänään, ohjelmat per laite</h2>
      <div class="stack">
        {{range .Devices}}
        <details class="device">
          <summary>
            <span class="device-title">{{deviceLabel .Device}}</span>
            <span class="device-total">{{human .TotalSeconds}}</span>
          </summary>
          <div class="device-body">
            <p class="muted">Viimeksi nähty {{formatTime .Device.LastSeenAt}} · {{.LockState}}</p>
            {{if .Processes}}
            <table class="compact">
              <thead><tr><th>Ohjelma</th><th>Aika</th></tr></thead>
              <tbody>
                {{range .Processes}}<tr><td><code>{{.Name}}</code></td><td>{{human .Seconds}}</td></tr>{{end}}
              </tbody>
            </table>
            {{else}}<p class="muted">Ei aktiivisia havaintoja tälle päivälle.</p>{{end}}
          </div>
        </details>
        {{else}}
        <article class="card"><strong>Ei laitteita</strong><p class="muted">Ensimmäinen client luodaan automaattisesti, kun se hakee configin tai lähettää eventtejä.</p></article>
        {{end}}
      </div>
    </section>

    <section id="devices">
      <h2>Laitteet</h2>
      <table>
        <thead><tr><th>ID</th><th>Nimi</th><th>UUID</th><th>Käyttäjä</th><th>Tila</th><th>Viimeksi nähty</th><th>Admin</th></tr></thead>
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
              <form method="post" action="/admin/devices/{{.Device.ID}}/delete"><button class="secondary" type="submit" onclick="return confirm('Poistetaanko laite ja sen tapahtumat pysyvästi?')">Poista</button></form>
              {{end}}
            </td>
          </tr>
        {{else}}
          <tr><td colspan="7" class="muted">Ei laitteita.</td></tr>
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
        <label class="inline"><input name="debug_mode" type="checkbox" value="on" {{if .Settings.DebugMode}}checked{{end}} {{if .ReadOnly}}disabled{{end}}> Debug-tila (näytä clientin konsoli)</label>
        <label class="inline"><input name="debug_unassigned_clients" type="checkbox" value="on" {{if .Settings.DebugUnassignedClients}}checked{{end}} {{if .ReadOnly}}disabled{{end}}> Debug-tila rekisteröimättömille clienteille</label>
        {{if not .ReadOnly}}<div><button type="submit">Tallenna asetukset</button></div>{{end}}
      </form>
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
