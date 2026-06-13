package rootaika

import (
	"html/template"
	"net/http"
	"time"
)

type rangeData struct {
	Role     Role
	ReadOnly bool
	Title    string
	Subtitle string
	Days     []DaySpan
	Rows     []rangeRow
}

type rangeRow struct {
	Device       Device
	DailySeconds []int64
	TotalSeconds int64
}

func (a *App) handleWeek(w http.ResponseWriter, r *http.Request) {
	a.handleRange(w, r, 7, "Viikko", "Aktiivinen käyttö per laite, viimeiset 7 päivää.")
}

func (a *App) handleMonth(w http.ResponseWriter, r *http.Request) {
	a.handleRange(w, r, 30, "Kuukausi", "Aktiivinen käyttö per laite, viimeiset 30 päivää.")
}

func (a *App) handleRange(w http.ResponseWriter, r *http.Request, dayCount int, title, subtitle string) {
	role, ok := a.requireRole(w, r, RoleAdmin, RoleClient)
	if !ok {
		return
	}

	data, err := a.rangeData(r, role, dayCount, title, subtitle)
	if err != nil {
		http.Error(w, "range failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := rangeTemplate.Execute(w, data); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func (a *App) rangeData(r *http.Request, role Role, dayCount int, title, subtitle string) (rangeData, error) {
	ctx := r.Context()
	now := a.now()
	days := dailySpans(now, a.location, dayCount)

	settings, err := a.store.Settings(ctx)
	if err != nil {
		return rangeData{}, err
	}
	devices, err := a.store.Devices(ctx)
	if err != nil {
		return rangeData{}, err
	}

	maxGap := time.Duration(settings.MaxCountableGapSeconds) * time.Second
	rangeStart := days[0].Start
	rangeEnd := days[len(days)-1].End

	rows := make([]rangeRow, 0, len(devices))
	for _, device := range devices {
		events, err := a.store.ReportEvents(ctx, device.ID, rangeStart, rangeEnd)
		if err != nil {
			return rangeData{}, err
		}
		daily := DailyUsage(events, days, now, maxGap)
		var total int64
		for _, seconds := range daily {
			total += seconds
		}
		rows = append(rows, rangeRow{Device: device, DailySeconds: daily, TotalSeconds: total})
	}

	return rangeData{
		Role:     role,
		ReadOnly: role != RoleAdmin,
		Title:    title,
		Subtitle: subtitle,
		Days:     days,
		Rows:     rows,
	}, nil
}

// dailySpans returns dayCount consecutive day windows ending with the day that
// contains now, each expressed as a UTC start/end derived from local midnight.
func dailySpans(now time.Time, location *time.Location, dayCount int) []DaySpan {
	if dayCount < 1 {
		dayCount = 1
	}
	localNow := now.In(location)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)

	days := make([]DaySpan, 0, dayCount)
	for offset := dayCount - 1; offset >= 0; offset-- {
		dayStart := todayStart.AddDate(0, 0, -offset)
		dayEnd := dayStart.AddDate(0, 0, 1)
		days = append(days, DaySpan{
			Start: dayStart.UTC(),
			End:   dayEnd.UTC(),
			Label: dayStart.Format("02.01"),
		})
	}
	return days
}

var rangeTemplate = template.Must(template.New("range").Funcs(template.FuncMap{
	"human": humanSeconds,
	"index": func(values []int64, i int) int64 {
		if i < 0 || i >= len(values) {
			return 0
		}
		return values[i]
	},
}).Parse(`<!doctype html>
<html lang="fi">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>rootaika · {{.Title}}</title>
  <style>
    :root { color-scheme: light; --bg:#f6f7f9; --surface:#fff; --text:#1f2933; --muted:#5b6673; --border:#d8dee6; --accent:#0f766e; }
    * { box-sizing: border-box; }
    body { margin:0; background:var(--bg); color:var(--text); font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; line-height:1.45; }
    main { max-width:1180px; margin:0 auto; padding:28px 18px 56px; }
    header { display:flex; justify-content:space-between; gap:16px; align-items:flex-start; margin-bottom:18px; }
    h1 { margin:0; font-size:2rem; letter-spacing:0; }
    p { margin:0 0 8px; }
    table { width:100%; border-collapse:collapse; background:var(--surface); border:1px solid var(--border); border-radius:8px; overflow:hidden; }
    th, td { padding:8px 10px; border-bottom:1px solid var(--border); text-align:right; vertical-align:top; white-space:nowrap; }
    th:first-child, td:first-child { text-align:left; }
    th { background:#edf2f7; font-weight:650; }
    tr:last-child td { border-bottom:0; }
    .muted { color:var(--muted); }
    .pill { display:inline-block; padding:2px 8px; border-radius:999px; background:#e6f4f1; color:var(--accent); font-size:.86rem; font-weight:650; }
    .total { font-weight:650; }
    nav.inline { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
    @media (max-width: 760px) { header { display:block; } table { display:block; overflow-x:auto; } }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <span class="pill">rootaika</span>
        <h1>{{.Title}}</h1>
        <p class="muted">{{.Subtitle}} Rooli {{.Role}}.</p>
      </div>
      <nav class="inline">
        <a href="/">Tänään</a>
        <a href="/week">Viikko</a>
        <a href="/month">Kuukausi</a>
        <a href="/#devices">Laitteet</a>
        <a href="/#users">Käyttäjät</a>
        <a href="/#settings">Asetukset</a>
        <a href="/#commands">Komennot</a>
      </nav>
    </header>

    <table>
      <thead>
        <tr>
          <th>Laite</th>
          {{range .Days}}<th>{{.Label}}</th>{{end}}
          <th>Yhteensä</th>
        </tr>
      </thead>
      <tbody>
      {{range .Rows}}
        {{$row := .}}
        <tr>
          <td>{{.Device.DisplayName}}</td>
          {{range $i, $day := $.Days}}<td>{{human (index $row.DailySeconds $i)}}</td>{{end}}
          <td class="total">{{human .TotalSeconds}}</td>
        </tr>
      {{else}}
        <tr><td colspan="{{len .Days}}" class="muted">Ei laitteita.</td><td class="muted">-</td></tr>
      {{end}}
      </tbody>
    </table>
  </main>
</body>
</html>`))
