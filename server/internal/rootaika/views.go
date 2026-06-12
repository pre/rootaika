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
	Commands   []Command
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
	commands, err := a.store.RecentCommands(ctx, 50)
	if err != nil {
		return dashboardData{}, err
	}
	categories, err := a.store.Categories(ctx)
	if err != nil {
		return dashboardData{}, err
	}

	latestCommandByDevice := map[int64]Command{}
	for _, command := range commands {
		if _, ok := latestCommandByDevice[command.DeviceID]; !ok {
			latestCommandByDevice[command.DeviceID] = command
		}
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
			LockState:    lockState(latestCommandByDevice[device.ID]),
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
		Commands:   commands,
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

func lockState(command Command) string {
	if command.ID == 0 {
		return "ei komentoa"
	}
	if command.Status == CommandStatusPending {
		return command.Type + " odottaa"
	}
	if command.Type == CommandLock {
		return "lukittu"
	}
	if command.Type == CommandUnlock {
		return "avattu"
	}
	return command.Status
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
      <nav class="inline">
        <a href="#today">Tänään</a>
        <a href="#devices">Laitteet</a>
        <a href="#users">Käyttäjät</a>
        <a href="#settings">Asetukset</a>
        <a href="#commands">Komennot</a>
      </nav>
    </header>

    {{if .ReadOnly}}<div class="notice">Client-tunnuksella näkymä on read-only. Muutokset vaativat admin-tunnuksen.</div>{{end}}

    <section id="today">
      <h2>Tänään</h2>
      <div class="grid">
        {{range .Devices}}
        <article class="card">
          <strong>{{.Device.DisplayName}}</strong>
          <p>{{human .TotalSeconds}} aktiivista käyttöä</p>
          <p class="muted">Viimeksi nähty {{formatTime .Device.LastSeenAt}} · {{.LockState}}</p>
          {{if .Processes}}
          <table class="compact">
            <thead><tr><th>Ohjelma</th><th>Aika</th></tr></thead>
            <tbody>
              {{range .Processes}}<tr><td><code>{{.Name}}</code></td><td>{{human .Seconds}}</td></tr>{{end}}
            </tbody>
          </table>
          {{else}}<p class="muted">Ei aktiivisia havaintoja tälle päivälle.</p>{{end}}
        </article>
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
              <form method="post" action="/admin/devices/{{.Device.ID}}/lock"><button class="warn" type="submit">Lock</button></form>
              <form method="post" action="/admin/devices/{{.Device.ID}}/unlock"><button type="submit">Unlock</button></form>
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
        <thead><tr><th>ID</th><th>Nimi</th><th>Luotu</th></tr></thead>
        <tbody>
        {{range .Users}}<tr><td>{{.ID}}</td><td>{{.Name}}</td><td>{{formatTime .CreatedAt}}</td></tr>{{else}}<tr><td colspan="3" class="muted">Ei käyttäjiä.</td></tr>{{end}}
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

    <section id="commands">
      <h2>Komennot</h2>
      <table class="compact">
        <thead><tr><th>ID</th><th>Laite</th><th>Tyyppi</th><th>Status</th><th>Luotu</th><th>Ack</th></tr></thead>
        <tbody>
        {{range .Commands}}<tr><td>{{.ID}}</td><td>{{.Device}}</td><td>{{.Type}}</td><td>{{.Status}}</td><td>{{formatTime .CreatedAt}}</td><td>{{formatTimePtr .AckAt}}</td></tr>{{else}}<tr><td colspan="6" class="muted">Ei komentoja.</td></tr>{{end}}
        </tbody>
      </table>
    </section>
  </main>
</body>
</html>`))
