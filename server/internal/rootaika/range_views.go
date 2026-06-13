package rootaika

import (
	"html/template"
	"net/http"
	"time"
)

type rangeViewData struct {
	Role     Role
	Title    string
	Subtitle string
	Range    string
}

func (a *App) handleWeek(w http.ResponseWriter, r *http.Request) {
	a.handleRange(w, r, string(RangeWeek), "Viikko", "Aktiivinen käyttö per laite, viimeiset 7 päivää.")
}

func (a *App) handleMonth(w http.ResponseWriter, r *http.Request) {
	a.handleRange(w, r, string(RangeMonth), "Kuukausi", "Aktiivinen käyttö per laite, viimeiset 30 päivää.")
}

func (a *App) handleRange(w http.ResponseWriter, r *http.Request, chartRange, title, subtitle string) {
	role, ok := a.requireRole(w, r, RoleAdmin, RoleClient)
	if !ok {
		return
	}

	data := rangeViewData{Role: role, Title: title, Subtitle: subtitle, Range: chartRange}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := rangeTemplate.Execute(w, data); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
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

var rangeTemplate = template.Must(template.New("range").Parse(`<!doctype html>
<html lang="fi">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>rootaika · {{.Title}}</title>
  <style>` + chartBaseCSS + `</style>
</head>
<body>
  <main>
    <header>
      <div>
        <span class="pill">rootaika</span>
        <h1>{{.Title}}</h1>
        <p class="muted">{{.Subtitle}} Rooli {{.Role}}.</p>
      </div>
      ` + chartNav + `
    </header>

    <section class="card">
      <div class="legend" id="device-legend"></div>
      <div id="usage-chart"></div>
      <p class="muted" id="usage-empty" hidden>Ei laitteita.</p>
    </section>
  </main>
  <script>` + chartScript + `
  (function(){
    var range = ` + "`{{.Range}}`" + `;
    function load(){
      fetchJSON('/api/v1/charts/usage?range=' + range).then(function(data){
        renderUsageChart(document.getElementById('usage-chart'),
          document.getElementById('device-legend'), data);
        document.getElementById('usage-empty').hidden = data.devices.length > 0;
      }).catch(function(){});
    }
    load();
  })();
  </script>
</body>
</html>`))
