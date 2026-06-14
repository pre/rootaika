#pragma once
// html.h — server-side rendering of the Settings admin page, ported from the Go
// server's settings_views.go. Writes directly to the WiFiClient. The `readOnly`
// flag hides mutating controls for the client role (admin role gets full forms).

#include "storage.h"

// htmlEscape writes s into the client with HTML entity escaping for the few
// characters that matter in attribute/text contexts (admin-entered names etc).
static void htmlEscape(WiFiClient& c, const char* s) {
  for (const char* p = s; *p; p++) {
    char ch = *p;
    switch (ch) {
      case '&': c.print(F("&amp;")); break;
      case '<': c.print(F("&lt;"));  break;
      case '>': c.print(F("&gt;"));  break;
      case '"': c.print(F("&quot;")); break;
      case '\'': c.print(F("&#39;")); break;
      default: c.print(ch);
    }
  }
}

// lockStateText mirrors Go's lockState(): a device is "lukittu" only once the
// client reports state=locked, otherwise it is pending (with the warning delay).
static const char* lockStateText(const Device& d) {
  static char buf[40];
  if (!d.locked) return "avattu";
  if (strcmp(d.lastStatus, "locked") == 0) return "lukittu";
  if (d.warnSeconds > 0) { snprintf(buf, sizeof(buf), "lukitaan (%d s varoitus)", d.warnSeconds); return buf; }
  return "lukitaan\xE2\x80\xA6";  // "lukitaan…"
}

// formatBytes renders a byte count as a human-readable "kB"/"MB" string (decimal
// units, one decimal place; bytes shown raw under 1 kB).
static void formatBytes(char* out, int outsz, long bytes) {
  if (bytes >= 1000000L)      snprintf(out, outsz, "%.1f MB", bytes / 1000000.0);
  else if (bytes >= 1000L)    snprintf(out, outsz, "%.1f kB", bytes / 1000.0);
  else                        snprintf(out, outsz, "%ld B", bytes);
}

// versionOptionLabel writes a registered version as "tag (artifact)", or just
// "tag" when no artifact is set.
static void versionOptionLabel(WiFiClient& c, const VersionRecord& v) {
  htmlEscape(c, v.version);
  if (v.artifact[0]) { c.print(F(" (")); htmlEscape(c, v.artifact); c.print(F(")")); }
}

// renderVersionSelect emits a <select name=...> over the registry. selectedId is
// pre-selected; inheritLabel (non-null) adds a leading 0-value option used by the
// per-device dropdown ("inherit global"); the global dropdown passes "Ei versiota".
static void renderVersionSelect(WiFiClient& c, const char* name, int selectedId, const __FlashStringHelper* zeroLabel) {
  c.print(F("<select name=")); c.print(name); c.print(F(" aria-label='Versio'>"));
  c.print(F("<option value=0")); if (selectedId == 0) c.print(F(" selected")); c.print(F(">")); c.print(zeroLabel); c.print(F("</option>"));
  for (int i = 0; i < g_versionCount; i++) {
    c.print(F("<option value=")); c.print(g_versions[i].id);
    if (g_versions[i].id == selectedId) c.print(F(" selected"));
    c.print(F(">")); versionOptionLabel(c, g_versions[i]); c.print(F("</option>"));
  }
  c.print(F("</select>"));
}

static void sendSettingsHead(WiFiClient& c) {
  c.print(F("HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nConnection: close\r\n\r\n"));
  c.print(F(
    "<!doctype html><html lang=fi><head><meta charset=utf-8>"
    "<meta name=viewport content='width=device-width,initial-scale=1'>"
    "<title>rootaika \xC2\xB7 Asetukset</title><style>"
    ":root{color-scheme:light;--bg:#f6f7f9;--surface:#fff;--text:#1f2933;--muted:#5b6673;--border:#d8dee6;--accent:#0f766e;--warn:#8a5a00;--warn-bg:#fff5d6}"
    "*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;line-height:1.45}"
    "main{max-width:1180px;margin:0 auto;padding:28px 18px 56px}"
    "header{display:flex;justify-content:space-between;gap:16px;align-items:flex-start;margin-bottom:18px}"
    "h1{margin:0;font-size:2rem}h2{margin:30px 0 10px;font-size:1.25rem}h3{margin:18px 0 8px}p{margin:0 0 8px}"
    "table{width:100%;border-collapse:collapse;background:var(--surface);border:1px solid var(--border);border-radius:8px;overflow:hidden}"
    "th,td{padding:9px 10px;border-bottom:1px solid var(--border);text-align:left;vertical-align:top}"
    "th{background:#edf2f7;font-weight:650}tr:last-child td{border-bottom:0}"
    "input,select,button{font:inherit}input,select{min-height:34px;padding:5px 8px;border:1px solid var(--border);border-radius:6px;background:#fff}"
    "button{min-height:34px;padding:5px 10px;border:1px solid #0c5f59;border-radius:6px;background:var(--accent);color:#fff;cursor:pointer}"
    "button.secondary{border-color:var(--border);background:#fff;color:var(--text)}"
    "button.warn{border-color:#7a5100;background:var(--warn);color:#fff}"
    "code{padding:1px 4px;border-radius:4px;background:#eef2f7}.muted{color:var(--muted)}"
    ".pill{display:inline-block;padding:2px 8px;border-radius:999px;background:#e6f4f1;color:var(--accent);font-size:.86rem;font-weight:650}"
    ".notice{padding:10px 12px;border:1px solid #f0d98b;border-radius:8px;background:var(--warn-bg);color:var(--warn);margin:12px 0}"
    ".grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:12px}"
    ".inline{display:flex;flex-wrap:wrap;gap:8px;align-items:center}.stack{display:grid;gap:8px}"
    ".actions form{margin:0 0 6px}.compact th,.compact td{padding:7px 8px}"
    "nav.inline a{color:var(--accent);text-decoration:none;font-weight:600}"
    "@media(max-width:760px){header{display:block}table{display:block;overflow-x:auto}.inline{display:grid;align-items:stretch}}"
    "</style></head><body><main>"));
}

// renderSettingsPage writes the full Settings page. `admin` true = admin role
// (full controls); false = client role (read-only).
static void renderSettingsPage(WiFiClient& c, bool admin) {
  bool readOnly = !admin;
  sendSettingsHead(c);

  // header
  c.print(F("<header><div><span class=pill>rootaika</span><h1>Asetukset</h1>"
            "<p class=muted>Rooli "));
  c.print(admin ? F("admin") : F("client"));
  c.print(F(".</p></div><nav class=inline><a href='/'>Etusivu</a></nav></header>"));

  if (readOnly)
    c.print(F("<div class=notice>Client-tunnuksella n\xC3\xA4kym\xC3\xA4 on read-only. Muutokset vaativat admin-tunnuksen.</div>"));

  // ---- lock all ----
  // Locks every assigned device at once with an admin-set message. The message is
  // persisted (g_settings.lockAllMessage) and reused by the physical button too.
  c.print(F("<section id=lockall><h2>Lukitse kaikki</h2>"));
  {
    bool locked; int lc, tc;
    globalLockState(&locked, &lc, &tc);
    c.print(F("<p class=muted>Tila: "));
    c.print(locked ? F("lukittu") : F("auki"));
    c.print(F(" (")); c.print(lc); c.print('/'); c.print(tc);
    c.print(F(" laitetta).</p>"));
  }
  if (readOnly) {
    c.print(F("<p class=muted>Viesti lukitusruudulle: <code>"));
    htmlEscape(c, g_settings.lockAllMessage);
    c.print(F("</code></p>"));
  } else {
    c.print(F("<form method=post action='/admin/lock-all' class=inline>"
              "<input name=message value='"));
    htmlEscape(c, g_settings.lockAllMessage);
    c.print(F("' placeholder='Viesti lukitusruudulle' aria-label='Lukitusviesti' style='min-width:260px'>"
              "<button class=warn type=submit>Lukitse kaikki</button></form>"
              "<form method=post action='/admin/unlock-all' class=inline>"
              "<button class=secondary type=submit>Avaa kaikki</button></form>"));
  }
  c.print(F("</section>"));

  // ---- devices ----
  c.print(F("<section id=devices><h2>Laitteet</h2><table><thead><tr>"
            "<th>ID</th><th>Nimi</th><th>UUID</th><th>K\xC3\xA4ytt\xC3\xA4j\xC3\xA4</th><th>Tila</th><th>Viimeksi</th><th>Versio</th><th>Admin</th>"
            "</tr></thead><tbody>"));
  if (g_deviceCount == 0)
    c.print(F("<tr><td colspan=8 class=muted>Ei laitteita.</td></tr>"));
  for (int i = 0; i < g_deviceCount; i++) {
    Device& d = g_devices[i];
    c.print(F("<tr><td>")); c.print(d.id);
    c.print(F("</td><td>")); htmlEscape(c, d.name);
    c.print(F("</td><td><code>")); htmlEscape(c, d.uuid);
    c.print(F("</code></td><td>"));
    if (isAssigned(d)) htmlEscape(c, userName(d.userId));
    else c.print(F("<span class=muted>ei liitetty</span>"));
    c.print(F("</td><td>")); c.print(isAssigned(d) ? F("assigned") : F("unassigned"));
    c.print(F("<br><span class=muted>")); c.print(lockStateText(d)); c.print(F("</span>"));
    c.print(F("</td><td>")); htmlEscape(c, d.lastSeen[0] ? d.lastSeen : "-");
    // reported running version + (per-device override indicator)
    c.print(F("</td><td>"));
    if (d.lastVersion[0]) {
      c.print(F("<code>")); htmlEscape(c, d.lastVersion); c.print(F("</code>"));
      if (d.lastVersionAt[0]) { c.print(F("<br><span class=muted>")); htmlEscape(c, d.lastVersionAt); c.print(F("</span>")); }
    } else {
      c.print(F("<span class=muted>-</span>"));
    }
    // target version the device resolves to (per-device selection or global),
    // shown as "→ tag", with "(laite)" when it is a per-device override.
    {
      VersionRecord* tgt = versionById(effectiveVersionId(d));
      if (tgt) {
        c.print(F("<br><span class=muted>\xE2\x86\x92 ")); htmlEscape(c, tgt->version);
        if (d.selectedVersionId > 0) c.print(F(" (laite)"));
        c.print(F("</span>"));
      }
    }
    c.print(F("</td><td class=actions>"));
    if (readOnly) {
      c.print(F("<span class=muted>read-only</span>"));
    } else {
      // assign form
      c.print(F("<form method=post action='/admin/devices/")); c.print(d.id);
      c.print(F("/assign' class=stack><input name=display_name value='")); htmlEscape(c, d.name);
      c.print(F("' aria-label='Laitteen nimi'><select name=user_id aria-label='K\xC3\xA4ytt\xC3\xA4j\xC3\xA4'>"
                "<option value=0>Ei k\xC3\xA4ytt\xC3\xA4j\xC3\xA4\xC3\xA4</option>"));
      for (int u = 0; u < g_userCount; u++) {
        c.print(F("<option value=")); c.print(g_users[u].id);
        if (g_users[u].id == d.userId) c.print(F(" selected"));
        c.print(F(">")); htmlEscape(c, g_users[u].name); c.print(F("</option>"));
      }
      c.print(F("</select><button class=secondary type=submit>Tallenna</button></form>"));
      // lock form
      c.print(F("<form method=post action='/admin/devices/")); c.print(d.id);
      c.print(F("/lock' class=stack><input name=message placeholder='Viesti lukitusruudulle' aria-label='Lukitusviesti'>"
                "<input name=warning_seconds type=number min=0 max=600 value=60 aria-label='Varoitusaika'>"
                "<button class=warn type=submit>Lock</button></form>"));
      // unlock + delete
      c.print(F("<form method=post action='/admin/devices/")); c.print(d.id);
      c.print(F("/unlock'><button type=submit>Unlock</button></form>"));
      c.print(F("<form method=post action='/admin/devices/")); c.print(d.id);
      c.print(F("/delete'><button class=secondary type=submit onclick=\"return confirm('Poistetaanko laite ja sen tapahtumat pysyv\xC3\xA4sti?')\">Poista</button></form>"));
      // per-device OTA version selection (0 = inherit the global selection)
      c.print(F("<form method=post action='/admin/devices/")); c.print(d.id);
      c.print(F("/version' class=stack>"));
      renderVersionSelect(c, "selected_version_id", d.selectedVersionId, F("Globaali oletus"));
      c.print(F("<button class=secondary type=submit>Aseta versio</button></form>"));
    }
    c.print(F("</td></tr>"));
  }
  c.print(F("</tbody></table></section>"));

  // ---- users ----
  c.print(F("<section id=users><h2>K\xC3\xA4ytt\xC3\xA4j\xC3\xA4t</h2><table class=compact><thead><tr>"
            "<th>ID</th><th>Nimi</th><th>Admin</th></tr></thead><tbody>"));
  if (g_userCount == 0) c.print(F("<tr><td colspan=3 class=muted>Ei k\xC3\xA4ytt\xC3\xA4ji\xC3\xA4.</td></tr>"));
  for (int u = 0; u < g_userCount; u++) {
    c.print(F("<tr><td>")); c.print(g_users[u].id);
    c.print(F("</td><td>")); htmlEscape(c, g_users[u].name);
    c.print(F("</td><td class=actions>"));
    if (readOnly) c.print(F("<span class=muted>read-only</span>"));
    else {
      c.print(F("<form method=post action='/admin/users/")); c.print(g_users[u].id);
      c.print(F("/rename' class=inline><input name=name value='")); htmlEscape(c, g_users[u].name);
      c.print(F("' required><button class=secondary type=submit>Tallenna</button></form>"));
    }
    c.print(F("</td></tr>"));
  }
  c.print(F("</tbody></table>"));
  if (!readOnly)
    c.print(F("<form method=post action='/admin/users' class=inline>"
              "<input name=name placeholder='K\xC3\xA4ytt\xC3\xA4j\xC3\xA4n nimi' required>"
              "<button type=submit>Luo k\xC3\xA4ytt\xC3\xA4j\xC3\xA4</button></form>"));
  c.print(F("</section>"));

  // ---- settings ----
  c.print(F("<section id=settings><h2>Asetukset</h2>"
            "<form method=post action='/admin/settings' class=grid>"));
  auto numField = [&](const __FlashStringHelper* label, const char* name, int value) {
    c.print(F("<label class=stack>")); c.print(label);
    c.print(F("<input name=")); c.print(name);
    c.print(F(" type=number min=1 value=")); c.print(value);
    if (readOnly) c.print(F(" disabled"));
    c.print(F("></label>"));
  };
  numField(F("Idle-raja, s"),            "idle_threshold_seconds",   g_settings.idle);
  numField(F("Upload-v\xC3\xA4li, s"),   "upload_interval_seconds",  g_settings.upload);
  numField(F("Polling-v\xC3\xA4li, s"),  "poll_interval_seconds",    g_settings.poll);
  numField(F("Maksimilaskentav\xC3\xA4li, s"), "max_countable_gap_seconds", g_settings.maxGap);
  numField(F("Kuvaajan y-maksimi, min"), "chart_y_max_minutes",      g_settings.chartYMax);
  numField(F("Taulun p\xC3\xA4ivitysv\xC3\xA4li, s"), "board_refresh_seconds", g_settings.boardRefresh);
  c.print(F("<label class=inline><input name=debug_mode type=checkbox value=on"));
  if (g_settings.debug) c.print(F(" checked"));
  if (readOnly) c.print(F(" disabled"));
  c.print(F("> Debug-tila (n\xC3\xA4yt\xC3\xA4 clientin konsoli)</label>"));
  c.print(F("<label class=inline><input name=debug_unassigned_clients type=checkbox value=on"));
  if (g_settings.debugUnassigned) c.print(F(" checked"));
  if (readOnly) c.print(F(" disabled"));
  c.print(F("> Debug-tila rekister\xC3\xB6im\xC3\xA4tt\xC3\xB6mille</label>"));
  // global OTA version selection (client auto-update). Versions are registered in
  // the Versiot section below; per-device overrides live in the devices table.
  // "Ei versiota" = no update wanted.
  c.print(F("<label class=stack>Haluttu client-versio (globaali)"));
  if (readOnly) {
    VersionRecord* gv = versionById(g_settings.selectedVersionId);
    c.print(F("<input value='"));
    if (gv) versionOptionLabel(c, *gv); else c.print(F("Ei versiota"));
    c.print(F("' disabled>"));
  } else {
    renderVersionSelect(c, "selected_version_id", g_settings.selectedVersionId, F("Ei versiota"));
  }
  c.print(F("</label>"));
  if (!readOnly) c.print(F("<div><button type=submit>Tallenna asetukset</button></div>"));
  c.print(F("</form>"));

  // ---- warning sound ----
  c.print(F("<h3>Lukitusvaroituksen \xC3\xA4\xC3\xA4ni (MP3)</h3>"));
  char sv[16]; soundVersionStr(sv, sizeof(sv));
  if (sv[0]) {
    // Show the uploaded filename and size (plus upload time) instead of a bare
    // version counter.
    c.print(F("<p class=muted>Tiedosto: <code>"));
    htmlEscape(c, g_settings.soundName[0] ? g_settings.soundName : "warning.mp3");
    c.print(F("</code> ("));
    char sz[24]; formatBytes(sz, sizeof(sz), g_settings.soundSize);
    c.print(sz);
    c.print(F(")"));
    if (g_settings.soundAt[0]) { c.print(F(", ladattu ")); htmlEscape(c, g_settings.soundAt); }
    c.print(F(". Client soittaa t\xC3\xA4m\xC3\xA4n varoituksen aikana.</p>"));
  } else {
    c.print(F("<p class=muted>Ei \xC3\xA4\xC3\xA4nt\xC3\xA4 asetettu. Varoitus n\xC3\xA4kyy ruudulla mutta \xC3\xA4\xC3\xA4nt\xC3\xA4 ei soiteta.</p>"));
  }
  if (!readOnly)
    c.print(F("<form method=post action='/admin/settings/warning-sound' enctype=multipart/form-data class=inline>"
              "<input name=sound type=file accept='audio/mpeg,.mp3' required aria-label='MP3-tiedosto'>"
              "<button type=submit>Lataa \xC3\xA4\xC3\xA4ni</button></form>"));
  c.print(F("</section>"));

  // ---- versions (OTA registry) ----
  // The selectable client releases: each is the (tag, artifact, sha256) triple
  // entered once here, then chosen from the global/per-device dropdowns above.
  c.print(F("<section id=versions><h2>Versiot</h2>"
            "<p class=muted>Rekister\xC3\xB6idyt client-versiot. Valitse k\xC3\xA4ytt\xC3\xB6\xC3\xB6n otettava versio Asetukset-osiossa (globaali) tai laitekohtaisesti.</p>"
            "<table class=compact><thead><tr>"
            "<th>Versio</th><th>Artifakti</th><th>SHA256</th><th></th></tr></thead><tbody>"));
  if (g_versionCount == 0) c.print(F("<tr><td colspan=4 class=muted>Ei rekister\xC3\xB6ityj\xC3\xA4 versioita.</td></tr>"));
  for (int i = 0; i < g_versionCount; i++) {
    VersionRecord& v = g_versions[i];
    if (readOnly) {
      c.print(F("<tr><td><code>")); htmlEscape(c, v.version);
      c.print(F("</code></td><td>")); htmlEscape(c, v.artifact);
      c.print(F("</td><td><code>")); htmlEscape(c, v.sha256);
      c.print(F("</code></td><td></td></tr>"));
    } else {
      // editable in place: one row-spanning form keyed by version id
      c.print(F("<tr><td colspan=4><form method=post action='/admin/versions/")); c.print(v.id);
      c.print(F("/edit' class=inline>"
                "<input name=version value='")); htmlEscape(c, v.version);
      c.print(F("' placeholder='v1.2.0' required aria-label='Versio' style='min-width:110px'>"
                "<input name=artifact value='")); htmlEscape(c, v.artifact);
      c.print(F("' placeholder='rootaika.exe' aria-label='Artifakti' style='min-width:150px'>"
                "<input name=sha256 value='")); htmlEscape(c, v.sha256);
      c.print(F("' placeholder='sha256' aria-label='SHA256' style='min-width:260px'>"
                "<button class=secondary type=submit>Tallenna</button></form>"
                "<form method=post action='/admin/versions/")); c.print(v.id);
      c.print(F("/delete' style='display:inline'><button class=secondary type=submit "
                "onclick=\"return confirm('Poistetaanko versio? Valinnat palautuvat oletukseen.')\">Poista</button></form>"
                "</td></tr>"));
    }
  }
  c.print(F("</tbody></table>"));
  if (!readOnly)
    c.print(F("<form method=post action='/admin/versions' class=inline>"
              "<input name=version placeholder='v1.2.0' required aria-label='Versio'>"
              "<input name=artifact placeholder='rootaika.exe' aria-label='Artifakti'>"
              "<input name=sha256 placeholder='64 heksamerkki\xC3\xA4' aria-label='SHA256' style='min-width:260px'>"
              "<button type=submit>Lis\xC3\xA4\xC3\xA4 versio</button></form>"));
  c.print(F("</section>"));

  // ---- categories ----
  c.print(F("<section id=categories><h2>Kategoriat</h2><table class=compact><thead><tr>"
            "<th>Tyyppi</th><th>Pattern</th><th>Kategoria</th><th></th></tr></thead><tbody>"));
  if (g_categoryCount == 0) c.print(F("<tr><td colspan=4 class=muted>Ei kategorias\xC3\xA4\xC3\xA4nt\xC3\xB6j\xC3\xA4.</td></tr>"));
  for (int i = 0; i < g_categoryCount; i++) {
    Category& cat = g_categories[i];
    c.print(F("<tr><td>")); htmlEscape(c, cat.type);
    c.print(F("</td><td><code>")); htmlEscape(c, cat.pattern);
    c.print(F("</code></td><td>")); htmlEscape(c, cat.cat);
    c.print(F("</td><td>"));
    if (!readOnly) {
      c.print(F("<form method=post action='/admin/categories/")); c.print(cat.id);
      c.print(F("/delete'><button class=secondary type=submit>Poista</button></form>"));
    }
    c.print(F("</td></tr>"));
  }
  c.print(F("</tbody></table>"));
  if (!readOnly)
    c.print(F("<form method=post action='/admin/categories' class=inline>"
              "<select name=match_type><option value=exact>exact</option>"
              "<option value=contains>contains</option><option value=prefix>prefix</option></select>"
              "<input name=pattern placeholder='steam.exe' required>"
              "<input name=category placeholder='pelit' required>"
              "<button type=submit>Lis\xC3\xA4\xC3\xA4 kategoria</button></form>"));
  c.print(F("</section>"));

  c.print(F("</main></body></html>"));
  c.flush();
  c.stop();
}
