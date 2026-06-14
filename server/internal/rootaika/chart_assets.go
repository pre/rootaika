package rootaika

// chartNav is the shared top navigation, identical on every page. Links are
// absolute so they work from any view. Laitteet, Käyttäjät, Asetukset and
// Kategoriat all live on the /settings page. Historia shows the lock/unlock
// history.
const chartNav = `<nav class="inline">
        <a href="/">Tänään</a>
        <a href="/week">Viikko</a>
        <a href="/month">Kuukausi</a>
        <a href="/board">Taulu</a>
        <a href="/history">Historia</a>
        <a href="/settings">Asetukset</a>
      </nav>`

// chartElementsCSS styles the SVG charts and legend. It is shared by the chart
// pages (via chartBaseCSS) and the dashboard, so the cumulative-today charts
// look identical wherever they are embedded. It depends on the --grid, --axis,
// --muted and --text custom properties being defined by the host stylesheet.
const chartElementsCSS = `
.legend { display:flex; flex-wrap:wrap; gap:14px; margin:4px 0 12px; }
.legend label { display:inline-flex; align-items:center; gap:6px; font-size:.92rem; cursor:pointer; user-select:none; }
.legend .sw { width:14px; height:14px; border-radius:3px; display:inline-block; flex:none; }
.legend input { margin:0; }
.legend label.off { opacity:.4; }
svg { display:block; width:100%; height:auto; }
.ax { font-size:11px; fill:var(--muted); }
.gl { stroke:var(--grid); stroke-width:1; }
.axline { stroke:var(--axis); stroke-width:1; }
.barlabel { fill:var(--text); }
.barval { fill:var(--muted); }
`

// chartBaseCSS is the shared stylesheet for the chart views. It mirrors the
// palette used by the dashboard so the chart pages feel part of the same app.
const chartBaseCSS = `
:root { color-scheme: light; --bg:#f6f7f9; --surface:#fff; --text:#1f2933; --muted:#5b6673; --border:#d8dee6; --accent:#0f766e; --grid:#e6eaef; --axis:#b6bfca; }
* { box-sizing: border-box; }
body { margin:0; background:var(--bg); color:var(--text); font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; line-height:1.45; }
main { max-width:1180px; margin:0 auto; padding:28px 18px 56px; }
header { display:flex; justify-content:space-between; gap:16px; align-items:flex-start; margin-bottom:18px; }
h1 { margin:0; font-size:2rem; letter-spacing:0; }
h2 { margin:0 0 8px; font-size:1.2rem; }
p { margin:0 0 8px; }
.muted { color:var(--muted); }
.pill { display:inline-block; padding:2px 8px; border-radius:999px; background:#e6f4f1; color:var(--accent); font-size:.86rem; font-weight:650; }
.card { background:var(--surface); border:1px solid var(--border); border-radius:10px; padding:18px 20px; margin-top:14px; }
nav.inline { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
.inline { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
select { min-height:34px; padding:5px 8px; border:1px solid var(--border); border-radius:6px; background:#fff; font:inherit; }
@media (max-width: 760px) { header { display:block; } }
` + chartElementsCSS

// chartScript holds the shared client-side drawing code. It exposes
// fetchJSON, renderUsageChart and renderProgramCharts on the page. All drawing
// is hand-rolled SVG so the views need no third-party assets and work on a LAN
// with no internet access. The y-axis maximum comes from the server payload
// (y_max_minutes) and stays fixed across views and reloads.
const chartScript = `
var CHART_PALETTE = ['#0f766e','#d4760a','#2f7d32','#5865f2','#1f6feb','#b4257f','#0e7490','#9a3412','#4d7c0f','#7c3aed','#be123c','#0369a1'];
function colorFor(i){ return CHART_PALETTE[i % CHART_PALETTE.length]; }
function fetchJSON(url){ return fetch(url, {credentials:'same-origin'}).then(function(r){ if(!r.ok){ throw new Error('http '+r.status); } return r.json(); }); }
function svgEl(name, attrs){ var e=document.createElementNS('http://www.w3.org/2000/svg', name); for(var k in attrs){ e.setAttribute(k, attrs[k]); } return e; }

// niceStep picks a round gridline step so a fixed yMax gets ~4-8 lines.
function niceStep(yMax){
  var targets=[15,30,60,120,180,240,360,480,720,1440];
  for(var i=0;i<targets.length;i++){ if(yMax/targets[i] <= 8) return targets[i]; }
  return Math.ceil(yMax/8);
}
function fmtMinutes(min){
  min=Math.round(min);
  if(min<60) return min+' min';
  var h=Math.floor(min/60), m=min%60;
  return m===0 ? h+' h' : h+' h '+m+' min';
}

// drawLineChart renders a multi-series line chart with a fixed y-axis into host.
// series: [{name, points:[min...], color, hidden}]. labels: x-axis labels.
function drawLineChart(host, labels, series, yMax){
  host.innerHTML='';
  var W=860, H=340, padL=52, padR=16, padT=14, padB=30;
  var plotW=W-padL-padR, plotH=H-padT-padB;
  var n=labels.length;
  var svg=svgEl('svg', {viewBox:'0 0 '+W+' '+H, 'aria-label':'kuvaaja'});
  var xAt=function(i){ return n<=1 ? padL+plotW/2 : padL + plotW*i/(n-1); };
  var yAt=function(v){ var c=v>yMax?yMax:v; return padT + plotH*(1 - c/yMax); };

  var step=niceStep(yMax);
  for(var g=0; g<=yMax+0.001; g+=step){
    var gy=yAt(g);
    svg.appendChild(svgEl('line', {class:'gl', x1:padL, y1:gy, x2:W-padR, y2:gy}));
    var t=svgEl('text', {class:'ax', x:padL-6, y:gy+4, 'text-anchor':'end'}); t.textContent=String(g); svg.appendChild(t);
  }
  svg.appendChild(svgEl('line', {class:'axline', x1:padL, y1:padT, x2:padL, y2:H-padB}));
  svg.appendChild(svgEl('line', {class:'axline', x1:padL, y1:H-padB, x2:W-padR, y2:H-padB}));
  var yl=svgEl('text', {class:'ax', x:padL-34, y:padT+plotH/2, 'text-anchor':'middle', transform:'rotate(-90 '+(padL-34)+' '+(padT+plotH/2)+')'}); yl.textContent='minuuttia'; svg.appendChild(yl);

  // x labels: thin out when there are many points.
  var maxLabels=12, every=Math.ceil(n/maxLabels);
  for(var i=0;i<n;i++){
    if(i%every!==0 && i!==n-1) continue;
    var tx=svgEl('text', {class:'ax', x:xAt(i), y:H-padB+16, 'text-anchor':'middle'}); tx.textContent=labels[i]; svg.appendChild(tx);
  }

  series.forEach(function(s){
    if(s.hidden) return;
    var pts=s.points.map(function(v,i){ return xAt(i)+','+yAt(v); }).join(' ');
    svg.appendChild(svgEl('polyline', {fill:'none', stroke:s.color, 'stroke-width':2.5, 'stroke-linejoin':'round', points:pts}));
    if(n<=31){
      s.points.forEach(function(v,i){ svg.appendChild(svgEl('circle', {cx:xAt(i), cy:yAt(v), r:3, fill:s.color})); });
    }
  });
  host.appendChild(svg);
}

// drawBarChart renders horizontal bars (program totals) with a fixed x scale.
function drawBarChart(host, items, xMax){
  host.innerHTML='';
  if(!items.length){ var p=document.createElement('p'); p.className='muted'; p.textContent='Ei ohjelmahavaintoja.'; host.appendChild(p); return; }
  var W=860, rowH=42, padL=150, padR=90, top=10;
  var barW=W-padL-padR, H=items.length*rowH+top*2;
  var svg=svgEl('svg', {viewBox:'0 0 '+W+' '+H});
  items.forEach(function(it, i){
    var y=top+i*rowH;
    var w=xMax>0 ? barW*Math.min(it.minutes,xMax)/xMax : 0;
    var lab=svgEl('text', {class:'barlabel', x:padL-10, y:y+rowH/2+4, 'text-anchor':'end', 'font-size':13}); lab.textContent=it.program; svg.appendChild(lab);
    svg.appendChild(svgEl('rect', {x:padL, y:y+6, width:Math.max(w,1), height:rowH-16, rx:5, fill:it.color}));
    var val=svgEl('text', {class:'barval', x:padL+Math.max(w,1)+8, y:y+rowH/2+4, 'font-size':12}); val.textContent=fmtMinutes(it.minutes); svg.appendChild(val);
  });
  host.appendChild(svg);
}

// renderUsageChart draws the all-devices chart and a checkbox legend that
// toggles device visibility. Hidden devices are remembered across reloads.
var usageHidden = {};
function renderUsageChart(chartHost, legendHost, data){
  var series=data.devices.map(function(d,i){
    return { id:d.device_id, name:d.name, points:d.points, color:colorFor(i), hidden:!!usageHidden[d.device_id] };
  });
  legendHost.innerHTML='';
  series.forEach(function(s){
    var label=document.createElement('label');
    if(s.hidden) label.className='off';
    var cb=document.createElement('input'); cb.type='checkbox'; cb.checked=!s.hidden;
    cb.addEventListener('change', function(){
      s.hidden=!cb.checked; usageHidden[s.id]=s.hidden;
      label.className=s.hidden?'off':'';
      drawLineChart(chartHost, data.labels, series, data.y_max_minutes);
    });
    var sw=document.createElement('span'); sw.className='sw'; sw.style.background=s.color;
    var txt=document.createTextNode(s.name);
    label.appendChild(cb); label.appendChild(sw); label.appendChild(txt);
    legendHost.appendChild(label);
  });
  drawLineChart(chartHost, data.labels, series, data.y_max_minutes);
}

// renderProgramCharts draws one device's per-program time series and totals bar.
function renderProgramCharts(lineHost, barHost, data){
  var series=data.series.map(function(s,i){ return { name:s.program, points:s.points, color:colorFor(i), hidden:false }; });
  drawLineChart(lineHost, data.labels, series, data.y_max_minutes);
  var items=data.totals.map(function(t,i){ return { program:t.program, minutes:t.minutes, color:colorFor(i) }; });
  var xMax=0; items.forEach(function(it){ if(it.minutes>xMax) xMax=it.minutes; });
  drawBarChart(barHost, items, xMax || 1);
}

// initDayDashboard wires the cumulative "today" usage chart plus the per-device
// program charts against the chart JSON endpoints. It is the single source of
// truth shared by the Tänään page and the Taulu screen, so the two views never
// drift apart. The argument is a map of element ids; every id is optional, so a
// page can embed only the usage chart (Taulu) or the full set (Tänään). When
// pollSeconds > 0 it refreshes on that interval, otherwise it loads once.
function initDayDashboard(opts){
  opts = opts || {};
  var range='day';
  var byId=function(id){ return id ? document.getElementById(id) : null; };
  var chartHost=byId(opts.usageChart), legendHost=byId(opts.usageLegend), usageEmpty=byId(opts.usageEmpty);
  var selectEl=byId(opts.deviceSelect), lineHost=byId(opts.programLine), barHost=byId(opts.programBar);
  var programEmpty=byId(opts.programEmpty), updatedEl=byId(opts.updated);
  var hasPrograms=selectEl && lineHost && barHost;
  var selectedDevice=null, knownDevices='';

  function loadPrograms(){
    if(!hasPrograms) return;
    if(!selectedDevice){ if(programEmpty) programEmpty.hidden=false; return; }
    if(programEmpty) programEmpty.hidden=true;
    fetchJSON('/api/v1/charts/programs?range='+range+'&device_id='+selectedDevice).then(function(data){
      renderProgramCharts(lineHost, barHost, data);
    }).catch(function(){});
  }

  function syncDeviceSelect(devices){
    if(!hasPrograms) return;
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

  if(hasPrograms){
    selectEl.addEventListener('change', function(){ selectedDevice=selectEl.value; loadPrograms(); });
  }

  function loadUsage(){
    fetchJSON('/api/v1/charts/usage?range='+range).then(function(data){
      if(chartHost) renderUsageChart(chartHost, legendHost, data);
      if(usageEmpty) usageEmpty.hidden = data.devices.length>0;
      if(updatedEl) updatedEl.textContent = data.now;
      syncDeviceSelect(data.devices);
      loadPrograms();
    }).catch(function(){});
  }

  loadUsage();
  if(opts.pollSeconds>0) setInterval(loadUsage, opts.pollSeconds*1000);
}
`
