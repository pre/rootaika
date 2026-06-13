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
	devices, err := a.store.Devices(ctx)
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
	"human":       humanSeconds,
	"formatTime":  formatLocal,
	"deviceLabel": deviceLabel,
}).Parse(`<!doctype html>
<html lang="fi">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>rootaika</title>
  <style>
    :root { color-scheme: light; --bg:#f6f7f9; --surface:#fff; --text:#1f2933; --muted:#5b6673; --border:#d8dee6; --accent:#0f766e; --warn:#8a5a00; --warn-bg:#fff5d6; --grid:#e6eaef; --axis:#b6bfca; }
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
    select { min-height:34px; padding:5px 8px; border:1px solid var(--border); border-radius:6px; background:#fff; font:inherit; }
    code { padding:1px 4px; border-radius:4px; background:#eef2f7; }
    .muted { color:var(--muted); }
    .pill { display:inline-block; padding:2px 8px; border-radius:999px; background:#e6f4f1; color:var(--accent); font-size:.86rem; font-weight:650; }
    .notice { padding:10px 12px; border:1px solid #f0d98b; border-radius:8px; background:var(--warn-bg); color:var(--warn); margin:12px 0; }
    .card { padding:14px; border:1px solid var(--border); border-radius:8px; background:var(--surface); }
    .card strong { display:block; margin-bottom:6px; color:var(--accent); }
    .inline { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
    .stack { display:grid; gap:8px; }
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
    .stacked { display:grid; gap:18px; }
    @media (max-width: 760px) {
      header { display:block; }
      table { display:block; overflow-x:auto; }
      .inline { display:grid; align-items:stretch; }
    }
  ` + chartElementsCSS + `</style>
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

    <section class="card">
      <h2>Kaikki laitteet, kumulatiivinen tänään</h2>
      <div class="legend" id="device-legend"></div>
      <div id="usage-chart"></div>
      <p class="muted" id="usage-empty" hidden>Ei laitteita.</p>
    </section>

    <section class="card">
      <div class="inline" style="justify-content:space-between">
        <h2 style="margin:0">Ohjelmat laitteittain</h2>
        <label class="inline">Laite: <select id="device-select"></select></label>
      </div>
      <div class="stacked" style="margin-top:14px">
        <div>
          <p class="muted">Kehitys tänään</p>
          <div id="program-line"></div>
        </div>
        <div>
          <p class="muted">Yhteensä tänään</p>
          <div id="program-bar"></div>
        </div>
      </div>
      <p class="muted" id="program-empty" hidden>Valitse laite.</p>
    </section>

  </main>
  <script>` + chartScript + `
  initDayDashboard({
    usageChart:'usage-chart', usageLegend:'device-legend', usageEmpty:'usage-empty',
    deviceSelect:'device-select', programLine:'program-line', programBar:'program-bar', programEmpty:'program-empty'
  });
  </script>
</body>
</html>`))
