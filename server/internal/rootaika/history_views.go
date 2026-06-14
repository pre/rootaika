package rootaika

import (
	"html/template"
	"net/http"
	"strconv"
	"time"
)

// historyEventLimit caps how many rows each history table shows. The system
// targets a handful of home PCs, so a few hundred rows is plenty of history
// while keeping the page light.
const historyEventLimit = 200

type historyData struct {
	Role        Role
	Now         time.Time
	Transitions []lockTransitionView
	AuditLog    []lockAuditView
}

type lockTransitionView struct {
	OccurredAt time.Time
	DeviceName string
	Locked     bool
}

type lockAuditView struct {
	OccurredAt time.Time
	Target     string
	Locked     bool
	Source     string
	Affected   int
}

func (a *App) handleHistory(w http.ResponseWriter, r *http.Request) {
	role, ok := a.requireRole(w, r, RoleAdmin, RoleClient)
	if !ok {
		return
	}

	data, err := a.historyViewData(r, role)
	if err != nil {
		http.Error(w, "history failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := historyTemplate.Execute(w, data); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func (a *App) historyViewData(r *http.Request, role Role) (historyData, error) {
	ctx := r.Context()
	transitions, err := a.store.LockTransitions(ctx, historyEventLimit)
	if err != nil {
		return historyData{}, err
	}
	audit, err := a.store.LockAuditEntries(ctx, historyEventLimit)
	if err != nil {
		return historyData{}, err
	}

	transitionViews := make([]lockTransitionView, 0, len(transitions))
	for _, t := range transitions {
		transitionViews = append(transitionViews, lockTransitionView{
			OccurredAt: t.OccurredAt,
			DeviceName: transitionDeviceName(t),
			Locked:     t.Locked,
		})
	}

	auditViews := make([]lockAuditView, 0, len(audit))
	for _, entry := range audit {
		auditViews = append(auditViews, lockAuditView{
			OccurredAt: entry.OccurredAt,
			Target:     auditTarget(entry),
			Locked:     entry.Locked,
			Source:     auditSourceLabel(entry.Source),
			Affected:   entry.Affected,
		})
	}

	return historyData{
		Role:        role,
		Now:         a.now().In(a.location),
		Transitions: transitionViews,
		AuditLog:    auditViews,
	}, nil
}

func transitionDeviceName(t LockTransition) string {
	if t.DeviceName != "" {
		return t.DeviceName
	}
	return "Laite " + strconv.FormatInt(t.DeviceID, 10)
}

// auditTarget names what an audit entry acted on: a single device by name, or
// all devices for the board-wide actions.
func auditTarget(entry LockAuditEntry) string {
	if entry.DeviceID == nil && entry.DeviceName == "" {
		return "Kaikki laitteet"
	}
	if entry.DeviceName != "" {
		return entry.DeviceName
	}
	return "Laite " + strconv.FormatInt(*entry.DeviceID, 10)
}

func auditSourceLabel(source string) string {
	switch source {
	case LockSourceAdmin:
		return "Admin"
	case LockSourceBoardButton:
		return "Taulun nappi"
	case LockSourceBoardUnlock:
		return "Taulun avaus"
	default:
		return source
	}
}

func lockLabel(locked bool) string {
	if locked {
		return "Lukittu"
	}
	return "Avattu"
}

var historyTemplate = template.Must(template.New("history").Funcs(template.FuncMap{
	"formatTime": formatLocal,
	"lockLabel":  lockLabel,
}).Parse(`<!doctype html>
<html lang="fi">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>rootaika · Historia</title>
  <style>
    :root { color-scheme: light; --bg:#f6f7f9; --surface:#fff; --text:#1f2933; --muted:#5b6673; --border:#d8dee6; --accent:#0f766e; --lock:#9a3412; --lock-bg:#fdeee6; --open:#0f766e; --open-bg:#e6f4f1; }
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
    .muted { color:var(--muted); }
    .pill { display:inline-block; padding:2px 8px; border-radius:999px; background:#e6f4f1; color:var(--accent); font-size:.86rem; font-weight:650; }
    .state { display:inline-block; padding:2px 9px; border-radius:999px; font-size:.86rem; font-weight:650; }
    .state.lock { background:var(--lock-bg); color:var(--lock); }
    .state.open { background:var(--open-bg); color:var(--open); }
    nav.inline { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
    .compact th, .compact td { padding:7px 8px; }
    @media (max-width: 760px) {
      header { display:block; }
      table { display:block; overflow-x:auto; }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <span class="pill">rootaika</span>
        <h1>Historia</h1>
        <p class="muted">Lukitusten historia. Rooli {{.Role}}. Päivitetty {{formatTime .Now}}.</p>
      </div>
      ` + chartNav + `
    </header>

    <section id="reported">
      <h2>Laitteiden raportoima lukitustila</h2>
      <p class="muted">Clientin itse raportoima tila tapahtumavirrasta: milloin näyttö oikeasti lukittui tai avautui.</p>
      <table class="compact">
        <thead><tr><th>Aika</th><th>Laite</th><th>Tila</th></tr></thead>
        <tbody>
        {{range .Transitions}}
          <tr>
            <td>{{formatTime .OccurredAt}}</td>
            <td>{{.DeviceName}}</td>
            <td><span class="state {{if .Locked}}lock{{else}}open{{end}}">{{lockLabel .Locked}}</span></td>
          </tr>
        {{else}}
          <tr><td colspan="3" class="muted">Ei lukitustapahtumia.</td></tr>
        {{end}}
        </tbody>
      </table>
    </section>

    <section id="actions">
      <h2>Admin- ja taulukomennot</h2>
      <p class="muted">Adminin ja taulun napin tekemät lukitus- ja avauskomennot.</p>
      <table class="compact">
        <thead><tr><th>Aika</th><th>Kohde</th><th>Toiminto</th><th>Lähde</th><th>Laitteita</th></tr></thead>
        <tbody>
        {{range .AuditLog}}
          <tr>
            <td>{{formatTime .OccurredAt}}</td>
            <td>{{.Target}}</td>
            <td><span class="state {{if .Locked}}lock{{else}}open{{end}}">{{lockLabel .Locked}}</span></td>
            <td>{{.Source}}</td>
            <td>{{.Affected}}</td>
          </tr>
        {{else}}
          <tr><td colspan="5" class="muted">Ei komentoja.</td></tr>
        {{end}}
        </tbody>
      </table>
    </section>

  </main>
</body>
</html>`))
