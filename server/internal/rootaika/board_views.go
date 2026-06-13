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
    .stacked { display:grid; gap:18px; }
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
      <nav class="inline">
        <a href="/">Tänään</a>
        <a href="/week">Viikko</a>
        <a href="/month">Kuukausi</a>
      </nav>
    </header>

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
  (function(){
    var range='day';
    var selectEl=document.getElementById('device-select');
    var selectedDevice=null, knownDevices='';

    function loadPrograms(){
      if(!selectedDevice){ document.getElementById('program-empty').hidden=false; return; }
      document.getElementById('program-empty').hidden=true;
      fetchJSON('/api/v1/charts/programs?range='+range+'&device_id='+selectedDevice).then(function(data){
        renderProgramCharts(document.getElementById('program-line'),
          document.getElementById('program-bar'), data);
      }).catch(function(){});
    }

    function syncDeviceSelect(devices){
      var sig=devices.map(function(d){ return d.device_id+':'+d.name; }).join('|');
      if(sig===knownDevices) return;
      knownDevices=sig;
      selectEl.innerHTML='';
      devices.forEach(function(d){
        var o=document.createElement('option'); o.value=d.device_id; o.textContent=d.name; selectEl.appendChild(o);
      });
      if(devices.length && !devices.some(function(d){ return String(d.device_id)===String(selectedDevice); })){
        selectedDevice=devices[0].device_id; selectEl.value=selectedDevice;
      }
    }

    selectEl.addEventListener('change', function(){ selectedDevice=selectEl.value; loadPrograms(); });

    function loadUsage(){
      fetchJSON('/api/v1/charts/usage?range='+range).then(function(data){
        renderUsageChart(document.getElementById('usage-chart'),
          document.getElementById('device-legend'), data);
        document.getElementById('usage-empty').hidden = data.devices.length>0;
        document.getElementById('updated').textContent = data.now;
        syncDeviceSelect(data.devices);
        loadPrograms();
      }).catch(function(){});
    }

    loadUsage();
    setInterval(loadUsage, {{.PollSeconds}}*1000);
  })();
  </script>
</body>
</html>`))
