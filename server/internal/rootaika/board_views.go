package rootaika

import (
	"html/template"
	"net/http"
)

// boardPollSeconds is how often the dedicated board screen refreshes its data.
// Matches the client's default upload cadence so there is fresh data to show.
const boardPollSeconds = 30

type boardViewData struct {
	Role        Role
	PollSeconds int
}

func (a *App) handleBoard(w http.ResponseWriter, r *http.Request) {
	role, ok := a.requireRole(w, r, RoleAdmin, RoleClient)
	if !ok {
		return
	}
	data := boardViewData{Role: role, PollSeconds: boardPollSeconds}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := boardTemplate.Execute(w, data); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

var boardTemplate = template.Must(template.New("board").Parse(`<!doctype html>
<html lang="fi">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>rootaika · Taulu</title>
  <style>` + chartBaseCSS + `
    body { background:#0f1720; color:#e8edf2; }
    main { max-width:1500px; padding:24px 26px 48px; }
    h1 { font-size:2.4rem; }
    h2 { font-size:1.5rem; color:#e8edf2; }
    .card { background:#16212e; border-color:#26323f; }
    .muted { color:#93a1b0; }
    .pill { background:#11332f; color:#5eead4; }
    :root { --grid:#26323f; --axis:#3a4756; }
    .ax { fill:#93a1b0; }
    .barlabel { fill:#e8edf2; }
    .barval { fill:#93a1b0; }
    select { background:#0f1720; color:#e8edf2; border-color:#3a4756; }
    nav.inline a { color:#5eead4; }
    .updated { font-size:.95rem; }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <span class="pill">rootaika · taulu</span>
        <h1>Päivän ruutuaika</h1>
        <p class="muted updated">Päivittyy {{.PollSeconds}} s välein. Päivitetty <span id="updated">-</span>.</p>
      </div>
      ` + chartNav + `
    </header>

    <section class="card">
      <h2>Kaikki laitteet, kumulatiivinen tänään</h2>
      <div class="legend" id="device-legend"></div>
      <div id="usage-chart"></div>
      <p class="muted" id="usage-empty" hidden>Ei laitteita.</p>
    </section>
  </main>
  <script>` + chartScript + `
  initDayDashboard({
    usageChart:'usage-chart', usageLegend:'device-legend', usageEmpty:'usage-empty',
    updated:'updated', pollSeconds:{{.PollSeconds}}
  });
  </script>
</body>
</html>`))
