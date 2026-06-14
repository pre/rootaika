/*
  rootaika server on iLabs Challenger RP2040 WiFi (ESP-AT WiFi).

  This board IS the rootaika screen-time server (a hardware build alongside the
  Go server/). Clients (macOS / Windows agents) talk to it over the rootaika
  client API; the Go server stays as the reference implementation.

  Implemented endpoints (HTTP Basic Auth):
    POST /api/v1/events/batch        client/admin  ingest activity_observed -> LittleFS log
    GET  /api/v1/client/config       client/admin  per-device config + lock + categories + sound ver
    GET  /api/v1/warning-sound       client/admin  stream the admin-uploaded MP3 (404 if none)
    GET  /api/v1/board/today          client/admin  per-device today minutes + refresh interval
    GET  /api/v1/lock                client/admin  global lock status {locked,locked_count,total_count}
    POST /api/v1/lock                client/admin  toggle all assigned devices (board button)
    POST /api/v1/unlock              client/admin  release all assigned devices
    GET  /settings                   client/admin  admin Settings page (read-only for client)
    POST /admin/*                    admin only    settings page mutations (urlencoded + MP3 multipart)
    GET  /                           client/admin  live dashboard (today minutes, lock state)

  Lock is driven by the PHYSICAL BUTTON (GPIO2): short press = lock all assigned,
  hold >=1s = unlock all. RGB NeoPixel (GPIO11) breathes red=locked / green=open.

  NOTE ON ESP-AT LIMIT: max 5 simultaneous TCP connections, so /client/config is
  SHORT-poll (answers immediately, ignores wait=). Statistics views (week/month/
  charts) are intentionally NOT ported. Build with a LittleFS partition, e.g.
  fqbn ...:flash=8388608_4194304 (4MB FS).
*/

#include <WiFiEspAT.h>
#include <Adafruit_NeoPixel.h>
#include <LittleFS.h>
#include <ArduinoJson.h>
#include "wifi.h"
#include "storage.h"
#include "html.h"

const char* WIFI_SSID = SECRET_SSID;
const char* WIFI_PASS = SECRET_PASS;

const char* HOSTNAME   = "rootaika";   // -> rootaika.local
const int   BUTTON_PIN = 2;         // GPIO2 = Feather "D5". Button between pin and GND.

// HTTP Basic Auth expected header values (client:client / admin:admin -> base64).
const char* AUTH_CLIENT = "Basic Y2xpZW50OmNsaWVudA==";
const char* AUTH_ADMIN  = "Basic YWRtaW46YWRtaW4=";

WiFiServer server(80);

// ---- button debounce ----
bool     buttonPressed   = false;
int      lastReading     = HIGH;
int      stableState     = HIGH;
uint32_t lastDebounceMs  = 0;
const uint32_t DEBOUNCE_MS = 25;
bool     prevPressed  = false;
uint32_t pressStartMs = 0;
bool     longFired    = false;
const uint32_t LONG_PRESS_MS = 1000;
// Pre-lock warning countdown applied by the POST /api/v1/lock toggle and the admin
// "Lock all" button, so a client shows a warning (banner + sound) before the screen
// locks. The physical button short press locks immediately (no warning) by design.
const int LOCK_TOGGLE_WARN_SECONDS = 60;
// Default lock-screen message for the POST /api/v1/lock toggle.
const char LOCK_TOGGLE_MESSAGE[] = "rootaika";

// ---- RGB NeoPixel: breathing, red=locked / green=open ----
Adafruit_NeoPixel pixel(1, NEOPIXEL, NEO_GRB + NEO_KHZ800);
uint8_t  ledR = 0, ledG = 0, ledB = 0;
const uint32_t BREATH_MS = 4000;
uint32_t lastLedFrameMs = 0;

void ledSet(uint8_t r, uint8_t g, uint8_t b) { ledR = r; ledG = g; ledB = b; }
void applyLockLed() {
  bool locked; int lc, tc;
  globalLockState(&locked, &lc, &tc);
  if (locked) ledSet(255, 0, 0); else ledSet(0, 255, 0);
}

void updateLed() {
  if (millis() - lastLedFrameMs < 20) return;
  lastLedFrameMs = millis();
  float phase = (millis() % BREATH_MS) / (float)BREATH_MS;
  float level = 0.08f + 0.92f * (1.0f - cos(phase * 2.0f * PI)) / 2.0f;
  pixel.setPixelColor(0, pixel.Color((uint8_t)(ledR * level),
                                     (uint8_t)(ledG * level),
                                     (uint8_t)(ledB * level)));
  pixel.show();
}

void updateButton() {
  int reading = digitalRead(BUTTON_PIN);
  if (reading != lastReading) { lastDebounceMs = millis(); lastReading = reading; }
  if (millis() - lastDebounceMs > DEBOUNCE_MS && reading != stableState) {
    stableState = reading;
    buttonPressed = (stableState == LOW);     // pull-up: pressed = LOW
  }
}

// ---------- HTTP response helpers ----------
void send401(WiFiClient& c) {
  c.print(F("HTTP/1.1 401 Unauthorized\r\n"
            "WWW-Authenticate: Basic realm=\"rootaika\"\r\n"
            "Content-Type: text/plain\r\nConnection: close\r\n\r\nunauthorized"));
  c.flush(); c.stop();
}
void send403(WiFiClient& c) {
  c.print(F("HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\nforbidden"));
  c.flush(); c.stop();
}
void send405(WiFiClient& c) {
  c.print(F("HTTP/1.1 405 Method Not Allowed\r\nConnection: close\r\n\r\n"));
  c.flush(); c.stop();
}
void sendJsonHead(WiFiClient& c, int status) {
  c.print(F("HTTP/1.1 "));
  c.print(status);
  c.print(status == 200 ? F(" OK") : F(" Bad Request"));
  c.print(F("\r\nContent-Type: application/json\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n"));
}
void sendApiError(WiFiClient& c, int status, const char* msg) {
  sendJsonHead(c, status);
  c.print(F("{\"error\":\"")); c.print(msg); c.print(F("\"}"));
  c.flush(); c.stop();
}
// 303 redirect back to the settings page after an admin POST (PRG pattern).
void sendRedirect(WiFiClient& c, const char* loc) {
  c.print(F("HTTP/1.1 303 See Other\r\nLocation: "));
  c.print(loc);
  c.print(F("\r\nConnection: close\r\n\r\n"));
  c.flush(); c.stop();
}

// ---------- query / form parsing ----------
// getParam extracts a query parameter value from a path like /x?client_id=abc.
bool getParam(const char* path, const char* key, char* out, int outsz) {
  const char* q = strchr(path, '?');
  if (!q) return false;
  char needle[40];
  snprintf(needle, sizeof(needle), "%s=", key);
  const char* p = strstr(q, needle);
  // ensure the match begins right after '?' or '&', not mid-token
  while (p && p != q + 1 && *(p - 1) != '&' && *(p - 1) != '?')
    p = strstr(p + 1, needle);
  if (!p) return false;
  p += strlen(needle);
  int i = 0;
  while (*p && *p != '&' && i < outsz - 1) out[i++] = *p++;
  out[i] = 0;
  return true;
}

// urlDecode decodes %XX and + from in into out.
void urlDecode(const char* in, char* out, int outsz) {
  int o = 0;
  for (const char* p = in; *p && o < outsz - 1; p++) {
    if (*p == '+') out[o++] = ' ';
    else if (*p == '%' && p[1] && p[2]) {
      auto hex = [](char ch) -> int {
        if (ch >= '0' && ch <= '9') return ch - '0';
        if (ch >= 'a' && ch <= 'f') return ch - 'a' + 10;
        if (ch >= 'A' && ch <= 'F') return ch - 'A' + 10;
        return -1;
      };
      int hi = hex(p[1]), lo = hex(p[2]);
      if (hi >= 0 && lo >= 0) { out[o++] = (char)(hi * 16 + lo); p += 2; }
      else out[o++] = *p;
    } else out[o++] = *p;
  }
  out[o] = 0;
}

// formField pulls key from a urlencoded body and URL-decodes it into out.
bool formField(const char* body, const char* key, char* out, int outsz) {
  char needle[40];
  snprintf(needle, sizeof(needle), "%s=", key);
  const char* p = strstr(body, needle);
  while (p && p != body && *(p - 1) != '&')
    p = strstr(p + 1, needle);
  if (!p) { out[0] = 0; return false; }
  p += strlen(needle);
  char raw[256];
  int i = 0;
  while (*p && *p != '&' && i < (int)sizeof(raw) - 1) raw[i++] = *p++;
  raw[i] = 0;
  urlDecode(raw, out, outsz);
  return true;
}

int formInt(const char* body, const char* key, int fallback) {
  char buf[32];
  if (!formField(body, key, buf, sizeof(buf))) return fallback;
  char* end;
  long v = strtol(buf, &end, 10);
  if (end == buf) return fallback;
  return (int)v;
}

bool formCheckbox(const char* body, const char* key) {
  char buf[16];
  if (!formField(body, key, buf, sizeof(buf))) return false;
  return !strcmp(buf, "on") || !strcmp(buf, "1") || !strcmp(buf, "true") || !strcmp(buf, "yes");
}

// ---------- auth ----------
bool authIsClientOrAdmin(const char* hdr) {
  return strcmp(hdr, AUTH_CLIENT) == 0 || strcmp(hdr, AUTH_ADMIN) == 0;
}
bool authIsAdmin(const char* hdr) {
  return strcmp(hdr, AUTH_ADMIN) == 0;
}

// ===================== API endpoints =====================

void handleClientConfig(WiFiClient& c, const char* path) {
  char clientId[40] = "";
  getParam(path, "client_id", clientId, sizeof(clientId));
  if (clientId[0] == 0) { sendApiError(c, 400, "client_id is required"); return; }

  Device* d = ensureDevice(clientId);
  if (!d) { sendApiError(c, 400, "device table full"); return; }

  // The client reports its own state in the same poll; record it if valid.
  char status[12] = "";
  if (getParam(path, "status", status, sizeof(status))) {
    if (!strcmp(status, "active") || !strcmp(status, "idle") || !strcmp(status, "locked"))
      recordDeviceStatus(d, status);
  }

  // The client also reports its running version in the same poll (OTA update).
  char clientVer[24] = "";
  if (getParam(path, "version", clientVer, sizeof(clientVer)) && clientVer[0])
    recordDeviceVersion(d, clientVer);

  char ver[20]; configVersionFor(*d, ver, sizeof(ver));
  char sv[16];  soundVersionStr(sv, sizeof(sv));
  // Effective desired-version triple the client should update to ("" = none).
  const char *dVer, *dArtifact, *dSha;
  resolveDesiredVersion(*d, &dVer, &dArtifact, &dSha);

  sendJsonHead(c, 200);
  c.print(F("{\"client_id\":\"")); jsonEscape(c, clientId);
  c.print(F("\",\"config_version\":\"")); c.print(ver);
  c.print(F("\",\"idle_threshold_seconds\":")); c.print(d->idle);
  c.print(F(",\"upload_interval_seconds\":")); c.print(d->upload);
  c.print(F(",\"poll_interval_seconds\":")); c.print(d->poll);
  c.print(F(",\"max_countable_gap_seconds\":")); c.print(g_settings.maxGap);
  c.print(F(",\"debug_mode\":")); c.print(debugFor(*d) ? F("true") : F("false"));
  c.print(F(",\"locked\":")); c.print(d->locked ? F("true") : F("false"));
  c.print(F(",\"lock_message\":\"")); if (d->locked) jsonEscape(c, d->lockMsg);
  c.print(F("\",\"warning_seconds\":")); c.print(d->locked ? d->warnSeconds : 0);
  c.print(F(",\"warning_sound_version\":\"")); c.print(sv);
  c.print(F("\",\"desired_version\":\"")); jsonEscape(c, dVer);
  c.print(F("\",\"artifact_name\":\"")); jsonEscape(c, dArtifact);
  c.print(F("\",\"sha256\":\"")); jsonEscape(c, dSha);
  c.print(F("\",\"categories\":["));
  for (int i = 0; i < g_categoryCount; i++) {
    if (i) c.print(',');
    c.print(F("{\"match_type\":\"")); jsonEscape(c, g_categories[i].type);
    c.print(F("\",\"pattern\":\"")); jsonEscape(c, g_categories[i].pattern);
    c.print(F("\",\"category\":\"")); jsonEscape(c, g_categories[i].cat);
    c.print(F("\"}"));
  }
  c.print(F("]}"));
  c.flush(); c.stop();
}

void handleEventsBatch(WiFiClient& c, const char* body, int bodyLen) {
  JsonDocument doc;
  DeserializationError err = deserializeJson(doc, body, bodyLen);
  if (err) { sendApiError(c, 400, "invalid json"); return; }

  const char* clientId = doc["client_id"] | "";
  if (clientId[0] == 0) { sendApiError(c, 400, "client_id is required"); return; }
  JsonArray events = doc["events"].as<JsonArray>();
  if (events.isNull() || events.size() == 0) { sendApiError(c, 400, "events is required"); return; }

  Device* d = ensureDevice(clientId);
  if (!d) { sendApiError(c, 400, "device table full"); return; }
  int devId = d->id;
  int accepted = 0;

  File f = LittleFS.open("/events.jsonl", "a");
  for (JsonObject ev : events) {
    const char* eventId = ev["event_id"] | "";
    const char* type    = ev["type"] | "";
    const char* occ     = ev["occurred_at"] | "";
    const char* state   = ev["state"] | "";
    const char* proc    = ev["process_name"] | "";
    long seq            = ev["sequence"] | 0;
    if (eventId[0] == 0 || occ[0] == 0) continue;
    if (strcmp(type, "activity_observed") != 0) continue;
    if (strcmp(state, "active") && strcmp(state, "idle") && strcmp(state, "locked")) continue;
    if (seenEventId(eventId)) continue;       // idempotent re-send: drop duplicates
    if (strcmp(state, "active") == 0 && proc[0] == 0) proc = "unknown";
    if (strcmp(state, "active") != 0) proc = "";
    if (f) {
      f.print(F("{\"d\":")); f.print(devId);
      f.print(F(",\"id\":\"")); jsonEscape(f, eventId);
      f.print(F("\",\"t\":\"")); jsonEscape(f, occ);
      f.print(F("\",\"s\":\"")); jsonEscape(f, state);
      f.print(F("\",\"p\":\"")); jsonEscape(f, proc);
      f.print(F("\",\"seq\":")); f.print(seq);
      f.println(F("}"));
    }
    rememberEventId(eventId);
    updateDeviceLastSeen(d, occ);
    accepted++;
  }
  if (f) f.close();
  if (accepted) saveDevices();   // persist updated lastSeen

  sendJsonHead(c, 200);
  c.print(F("{\"accepted\":")); c.print(accepted);
  c.print(F(",\"duplicate_or_ignored\":")); c.print((int)events.size() - accepted);
  c.print(F(",\"device_id\":")); c.print(devId);
  c.print(F("}"));
  c.flush(); c.stop();
}

void handleWarningSound(WiFiClient& c) {
  if (g_settings.soundVer <= 0 || !LittleFS.exists("/warning.mp3")) {
    c.print(F("HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n"));
    c.flush(); c.stop();
    return;
  }
  File f = LittleFS.open("/warning.mp3", "r");
  if (!f) { c.print(F("HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n")); c.flush(); c.stop(); return; }
  c.print(F("HTTP/1.1 200 OK\r\nContent-Type: audio/mpeg\r\nETag: \""));
  c.print(g_settings.soundVer);
  c.print(F("\"\r\nContent-Disposition: attachment; filename=\""));
  jsonEscape(c, g_settings.soundName[0] ? g_settings.soundName : "warning.mp3");
  c.print(F("\"\r\nContent-Length: "));
  c.print((long)f.size());
  c.print(F("\r\nConnection: close\r\n\r\n"));
  uint8_t buf[512];
  while (f.available()) {
    int n = f.read(buf, sizeof(buf));
    if (n <= 0) break;
    c.write(buf, n);
  }
  f.close();
  c.flush(); c.stop();
}

void handleBoardToday(WiFiClient& c) {
  long sec[MAX_DEVICES]; char maxDate[11];
  computeTodaySeconds(sec, maxDate);
  sendJsonHead(c, 200);
  c.print(F("{\"now\":\"")); c.print(maxDate);
  c.print(F("\",\"refresh_seconds\":")); c.print(g_settings.boardRefresh);
  c.print(F(",\"devices\":["));
  for (int i = 0; i < g_deviceCount; i++) {
    if (i) c.print(',');
    int minutes = (int)((sec[i] + 30) / 60);
    c.print(F("{\"name\":\"")); jsonEscape(c, g_devices[i].name);
    c.print(F("\",\"minutes\":")); c.print(minutes); c.print(F("}"));
  }
  c.print(F("]}"));
  c.flush(); c.stop();
}

void handleLockStatus(WiFiClient& c) {
  bool locked; int lc, tc;
  globalLockState(&locked, &lc, &tc);
  sendJsonHead(c, 200);
  c.print(F("{\"locked\":")); c.print(locked ? F("true") : F("false"));
  c.print(F(",\"locked_count\":")); c.print(lc);
  c.print(F(",\"total_count\":")); c.print(tc);
  c.print(F("}"));
  c.flush(); c.stop();
}

void handleLockToggle(WiFiClient& c) {
  int affected = 0;
  // The toggle locks with a default "rootaika" message and a 60 s pre-lock
  // warning, so clients show the warning banner + sound before locking.
  bool locked = toggleAllLocks(LOCK_TOGGLE_MESSAGE, LOCK_TOGGLE_WARN_SECONDS, &affected);
  applyLockLed();
  sendJsonHead(c, 200);
  c.print(F("{\"locked\":")); c.print(locked ? F("true") : F("false"));
  c.print(F(",\"affected\":")); c.print(affected);
  c.print(F("}"));
  c.flush(); c.stop();
}

void handleUnlock(WiFiClient& c) {
  int affected = unlockAllLocks();
  applyLockLed();
  sendJsonHead(c, 200);
  c.print(F("{\"locked\":false,\"affected\":")); c.print(affected); c.print(F("}"));
  c.flush(); c.stop();
}

// ===================== admin POST handlers =====================
// Each takes the already-read urlencoded body and 303-redirects to /settings.

void adminSettings(WiFiClient& c, const char* body) {
  Settings s;
  s.idle         = formInt(body, "idle_threshold_seconds", 0);
  s.upload       = formInt(body, "upload_interval_seconds", 0);
  s.poll         = formInt(body, "poll_interval_seconds", 0);
  s.maxGap       = formInt(body, "max_countable_gap_seconds", 0);
  s.chartYMax    = formInt(body, "chart_y_max_minutes", 0);
  s.boardRefresh = formInt(body, "board_refresh_seconds", 0);
  if (s.idle <= 0 || s.upload <= 0 || s.poll <= 0 || s.maxGap <= 0 || s.chartYMax <= 0 || s.boardRefresh <= 0) {
    sendApiError(c, 400, "settings must be positive integers");
    return;
  }
  s.debug = formCheckbox(body, "debug_mode");
  s.debugUnassigned = formCheckbox(body, "debug_unassigned_clients");
  updateSettings(s);

  // Global OTA version selection: the id of a registered version record (0 =
  // none). Independent of the numeric validation above, applied separately.
  setGlobalSelectedVersion(formInt(body, "selected_version_id", 0));

  sendRedirect(c, "/settings#settings");
}

// adminLockAll saves the lock-all message (when provided) and locks every
// assigned device with it. Mirrors the physical button's short press, but the
// message is admin-supplied instead of the stored default.
void adminLockAll(WiFiClient& c, const char* body) {
  char msg[64]; formField(body, "message", msg, sizeof(msg));
  setLockAllMessage(msg);
  lockAllAssigned(g_settings.lockAllMessage, LOCK_TOGGLE_WARN_SECONDS);
  applyLockLed();
  sendRedirect(c, "/settings#lockall");
}

// adminUnlockAll releases every assigned device (settings-page counterpart to the
// physical button's long press).
void adminUnlockAll(WiFiClient& c) {
  unlockAllLocks();
  applyLockLed();
  sendRedirect(c, "/settings#lockall");
}

void adminCreateUser(WiFiClient& c, const char* body) {
  char name[40]; formField(body, "name", name, sizeof(name));
  if (name[0]) createUser(name);
  sendRedirect(c, "/settings#users");
}

void adminRenameUser(WiFiClient& c, int id, const char* body) {
  char name[40]; formField(body, "name", name, sizeof(name));
  renameUser(id, name);
  sendRedirect(c, "/settings#users");
}

void adminAssignDevice(WiFiClient& c, int id, const char* body) {
  char name[40]; formField(body, "display_name", name, sizeof(name));
  int userId = formInt(body, "user_id", 0);
  updateDevice(id, name, userId);
  applyLockLed();
  sendRedirect(c, "/settings#devices");
}

void adminDeviceLock(WiFiClient& c, int id, const char* body) {
  char msg[64]; formField(body, "message", msg, sizeof(msg));
  int warn = formInt(body, "warning_seconds", 0);
  if (warn < 0) warn = 0;
  if (warn > 600) warn = 600;
  setDeviceLock(id, true, msg, warn);
  applyLockLed();
  sendRedirect(c, "/settings#devices");
}

void adminDeviceUnlock(WiFiClient& c, int id) {
  setDeviceLock(id, false, "", 0);
  applyLockLed();
  sendRedirect(c, "/settings#devices");
}

void adminDeviceDelete(WiFiClient& c, int id) {
  deleteDevice(id);
  applyLockLed();
  sendRedirect(c, "/settings#devices");
}

// adminDeviceVersion points a device at a registered version id, or 0 to inherit
// the global selection.
void adminDeviceVersion(WiFiClient& c, int id, const char* body) {
  setDeviceSelectedVersion(id, formInt(body, "selected_version_id", 0));
  sendRedirect(c, "/settings#devices");
}

// adminCreateVersion registers a new selectable version triple (version required).
void adminCreateVersion(WiFiClient& c, const char* body) {
  char ver[24], artifact[64], sha[72];
  formField(body, "version", ver, sizeof(ver));
  formField(body, "artifact", artifact, sizeof(artifact));
  formField(body, "sha256", sha, sizeof(sha));
  createVersion(ver, artifact, sha);
  sendRedirect(c, "/settings#versions");
}

// adminEditVersion updates a registered version record in place.
void adminEditVersion(WiFiClient& c, int id, const char* body) {
  char ver[24], artifact[64], sha[72];
  formField(body, "version", ver, sizeof(ver));
  formField(body, "artifact", artifact, sizeof(artifact));
  formField(body, "sha256", sha, sizeof(sha));
  editVersion(id, ver, artifact, sha);
  sendRedirect(c, "/settings#versions");
}

// adminDeleteVersion removes a registered version; selections pointing at it reset.
void adminDeleteVersion(WiFiClient& c, int id) {
  deleteVersion(id);
  sendRedirect(c, "/settings#versions");
}

void adminCreateCategory(WiFiClient& c, const char* body) {
  char type[16], pat[64], cat[40];
  formField(body, "match_type", type, sizeof(type));
  formField(body, "pattern", pat, sizeof(pat));
  formField(body, "category", cat, sizeof(cat));
  createCategory(type, pat, cat);
  sendRedirect(c, "/settings#categories");
}

void adminDeleteCategory(WiFiClient& c, int id) {
  deleteCategory(id);
  sendRedirect(c, "/settings#categories");
}

// ---------- multipart MP3 upload (streamed to LittleFS) ----------
// The body is far larger than g_body, so this reads the socket directly: skip the
// multipart preamble through the blank line after the part headers, then write
// bytes to /warning.mp3 until the closing boundary. A held-back window of the
// last delimLen bytes lets a boundary that spans two reads still be detected.
void handleWarningSoundUpload(WiFiClient& c, const char* contentType, long contentLength) {
  const char* b = strstr(contentType, "boundary=");
  if (!b) { sendApiError(c, 400, "no boundary"); return; }
  b += 9;
  char boundary[80];
  int bi = 0;
  if (*b == '"') b++;
  while (*b && *b != '"' && *b != ';' && bi < (int)sizeof(boundary) - 1) boundary[bi++] = *b++;
  boundary[bi] = 0;
  // the file part is terminated by CRLF + "--" + boundary
  char delim[88];
  snprintf(delim, sizeof(delim), "\r\n--%s", boundary);
  int delimLen = strlen(delim);
  if (delimLen >= 96) { sendApiError(c, 400, "boundary too long"); return; }

  File out = LittleFS.open("/warning.mp3.tmp", "w");
  if (!out) { sendApiError(c, 500, "fs open failed"); return; }

  long remaining = contentLength;
  uint32_t t0 = millis();

  // Phase 1: read part headers up to and including "\r\n\r\n", buffering them so
  // the Content-Disposition filename can be parsed out.
  int hdrMatch = 0;
  const char* hdrSeq = "\r\n\r\n";
  bool headersDone = false;
  char hdrs[256];
  int hdrLen = 0;
  while (remaining > 0 && millis() - t0 < 15000) {
    if (!c.available()) { updateButton(); continue; }
    char ch = c.read(); remaining--;
    if (hdrLen < (int)sizeof(hdrs) - 1) hdrs[hdrLen++] = ch;
    if (ch == hdrSeq[hdrMatch]) { hdrMatch++; if (hdrMatch == 4) { headersDone = true; break; } }
    else hdrMatch = (ch == '\r') ? 1 : 0;
  }
  if (!headersDone) { out.close(); LittleFS.remove("/warning.mp3.tmp"); sendApiError(c, 400, "malformed multipart"); return; }
  hdrs[hdrLen] = 0;

  // Extract the upload's original filename from filename="..." (best-effort; an
  // absent or unparseable name falls back to the on-disk "warning.mp3").
  char fileName[64] = "warning.mp3";
  const char* fn = strstr(hdrs, "filename=\"");
  if (fn) {
    fn += 10;
    int i = 0;
    while (*fn && *fn != '"' && i < (int)sizeof(fileName) - 1) fileName[i++] = *fn++;
    fileName[i] = 0;
    if (i == 0) strcpy(fileName, "warning.mp3");
  }

  // Phase 2: stream the file body, watching for the closing delimiter.
  char window[96];
  int winLen = 0;
  long written = 0;
  bool foundDelim = false;
  t0 = millis();
  while (remaining > 0 && millis() - t0 < 30000) {
    if (!c.available()) { updateButton(); continue; }
    char ch = c.read(); remaining--;
    t0 = millis();
    window[winLen++] = ch;
    if (winLen == delimLen) {
      if (memcmp(window, delim, delimLen) == 0) { foundDelim = true; break; }
      out.write((uint8_t)window[0]);     // confirmed non-delimiter: flush oldest byte
      written++;
      memmove(window, window + 1, --winLen);
    }
    if (written > maxWarningSoundBytesRP) {
      out.close(); LittleFS.remove("/warning.mp3.tmp");
      sendApiError(c, 400, "file too large");
      return;
    }
  }
  out.close();

  if (!foundDelim || written == 0) {
    LittleFS.remove("/warning.mp3.tmp");
    sendApiError(c, 400, foundDelim ? "empty file" : "upload truncated");
    return;
  }
  LittleFS.remove("/warning.mp3");
  LittleFS.rename("/warning.mp3.tmp", "/warning.mp3");
  recordSoundUpload(fileName, written);
  sendRedirect(c, "/settings#settings");
}

// ===================== dashboard (live status) =====================
void sendRootPage(WiFiClient& c) {
  long sec[MAX_DEVICES]; char maxDate[11];
  computeTodaySeconds(sec, maxDate);
  bool locked; int lc, tc;
  globalLockState(&locked, &lc, &tc);

  c.print(F("HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nConnection: close\r\n\r\n"));
  c.print(F("<!doctype html><html lang=fi><head><meta charset=utf-8>"
            "<meta name=viewport content='width=device-width,initial-scale=1'>"
            "<meta http-equiv=refresh content=10><title>ruutuaika</title><style>"
            "body{font-family:system-ui;background:#0f172a;color:#e2e8f0;margin:0;padding:2rem}"
            "h1{font-size:1.4rem;margin:0 0 .3rem}.sub{opacity:.6;font-size:.85rem;margin-bottom:1.5rem}"
            "a{color:#5eead4}"
            ".lock{display:inline-block;padding:.2rem .7rem;border-radius:.5rem;font-weight:700}"
            ".on{background:#dc2626}.off{background:#16a34a}"
            "table{width:100%;border-collapse:collapse;max-width:640px}"
            "td,th{padding:.6rem .4rem;border-bottom:1px solid #1e293b;text-align:left}"
            "th{opacity:.6;font-weight:600;font-size:.8rem}"
            ".min{font-size:1.6rem;font-weight:800;font-variant-numeric:tabular-nums;text-align:right}"
            ".name{font-family:ui-monospace,monospace;font-size:.8rem;opacity:.85}"
            "</style></head><body>"));
  c.print(F("<h1>ruutuaika <span class='lock "));
  c.print(locked ? F("on'>LUKITTU") : F("off'>auki"));
  c.print(F("</span></h1><div class=sub>"));
  c.print(g_deviceCount); c.print(F(" laitetta \xC2\xB7 p\xC3\xA4iv\xC3\xA4 "));
  c.print(maxDate[0] ? maxDate : "\xE2\x80\x94");
  c.print(F(" \xC2\xB7 <a href='/settings'>Asetukset</a>"));
  c.print(F("</div><table><tr><th>laite</th><th style=text-align:right>t\xC3\xA4n\xC3\xA4\xC3\xA4n</th></tr>"));
  for (int i = 0; i < g_deviceCount; i++) {
    int minutes = (int)((sec[i] + 30) / 60);
    c.print(F("<tr><td class=name>")); htmlEscape(c, g_devices[i].name);
    c.print(F("</td><td class=min>")); c.print(minutes); c.print(F("<span style='font-size:.7rem;opacity:.5'> min</span></td></tr>"));
  }
  if (g_deviceCount == 0) c.print(F("<tr><td colspan=2 style=opacity:.5>ei laitteita viel\xC3\xA4</td></tr>"));
  c.print(F("</table></body></html>"));
  c.flush(); c.stop();
}

// ===================== request parser + router =====================
char g_body[8192];

// pathStarts matches a route prefix followed by end-of-path, '/', or '?'.
bool pathStarts(const char* path, const char* prefix) {
  int n = strlen(prefix);
  if (strncmp(path, prefix, n) != 0) return false;
  char after = path[n];
  return after == 0 || after == '/' || after == '?';
}

// routeAdmin dispatches POST /admin/* using the urlencoded body already read.
void routeAdmin(WiFiClient& c, const char* path, const char* body) {
  const char* p = path + 7;          // strip "/admin/"
  char seg0[24] = "", seg2[24] = "";
  int id = 0;
  int i = 0; while (p[i] && p[i] != '/' && p[i] != '?' && i < 23) { seg0[i] = p[i]; i++; } seg0[i] = 0;
  const char* rest = p + i;
  if (*rest == '/') {
    rest++;
    id = atoi(rest);
    while (*rest && *rest != '/') rest++;
    if (*rest == '/') { rest++; int j = 0; while (*rest && *rest != '/' && *rest != '?' && j < 23) { seg2[j] = *rest; rest++; j++; } seg2[j] = 0; }
  }

  if (!strcmp(seg0, "settings") && seg2[0] == 0)                         { adminSettings(c, body); return; }
  if (!strcmp(seg0, "lock-all") && id == 0)                              { adminLockAll(c, body); return; }
  if (!strcmp(seg0, "unlock-all") && id == 0)                            { adminUnlockAll(c); return; }
  if (!strcmp(seg0, "users") && id == 0)                                 { adminCreateUser(c, body); return; }
  if (!strcmp(seg0, "users") && id > 0 && !strcmp(seg2, "rename"))       { adminRenameUser(c, id, body); return; }
  if (!strcmp(seg0, "devices") && id > 0 && !strcmp(seg2, "assign"))     { adminAssignDevice(c, id, body); return; }
  if (!strcmp(seg0, "devices") && id > 0 && !strcmp(seg2, "lock"))       { adminDeviceLock(c, id, body); return; }
  if (!strcmp(seg0, "devices") && id > 0 && !strcmp(seg2, "unlock"))     { adminDeviceUnlock(c, id); return; }
  if (!strcmp(seg0, "devices") && id > 0 && !strcmp(seg2, "delete"))     { adminDeviceDelete(c, id); return; }
  if (!strcmp(seg0, "devices") && id > 0 && !strcmp(seg2, "version"))    { adminDeviceVersion(c, id, body); return; }
  if (!strcmp(seg0, "categories") && id == 0)                           { adminCreateCategory(c, body); return; }
  if (!strcmp(seg0, "categories") && id > 0 && !strcmp(seg2, "delete"))  { adminDeleteCategory(c, id); return; }
  if (!strcmp(seg0, "versions") && id == 0)                             { adminCreateVersion(c, body); return; }
  if (!strcmp(seg0, "versions") && id > 0 && !strcmp(seg2, "edit"))      { adminEditVersion(c, id, body); return; }
  if (!strcmp(seg0, "versions") && id > 0 && !strcmp(seg2, "delete"))    { adminDeleteVersion(c, id); return; }

  c.print(F("HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n"));
  c.flush(); c.stop();
}

void handleClient(WiFiClient& client) {
  char line[300];
  int  len = 0;
  bool firstLine = true;
  char method[8] = "";
  char path[260] = "/";
  char authHdr[80] = "";
  char contentType[120] = "";
  long contentLength = 0;
  bool headersDone = false;
  uint32_t start = millis();

  while (client.connected() && millis() - start < 1500) {
    if (client.available()) {
      char ch = client.read();
      if (ch == '\r') continue;
      if (ch == '\n') {
        line[len] = 0;
        if (firstLine) {
          char* sp = strchr(line, ' ');
          if (sp) {
            int ml = sp - line; if (ml > 7) ml = 7;
            memcpy(method, line, ml); method[ml] = 0;
            char* p = sp + 1;
            char* sp2 = strchr(p, ' ');
            int n = sp2 ? (int)(sp2 - p) : (int)strlen(p);
            if (n > (int)sizeof(path) - 1) n = sizeof(path) - 1;
            memcpy(path, p, n); path[n] = 0;
          }
          firstLine = false;
        } else if (len == 0) {
          headersDone = true;
          break;
        } else {
          if (strncasecmp(line, "Authorization:", 14) == 0) {
            char* v = line + 14; while (*v == ' ') v++;
            strncpy(authHdr, v, sizeof(authHdr) - 1);
          } else if (strncasecmp(line, "Content-Length:", 15) == 0) {
            contentLength = atol(line + 15);
          } else if (strncasecmp(line, "Content-Type:", 13) == 0) {
            char* v = line + 13; while (*v == ' ') v++;
            strncpy(contentType, v, sizeof(contentType) - 1);
          }
        }
        len = 0;
      } else if (len < (int)sizeof(line) - 1) {
        line[len++] = ch;
      }
    } else {
      updateButton();
    }
  }
  if (!headersDone) { client.stop(); return; }

  bool isGet  = strcmp(method, "GET") == 0;
  bool isPost = strcmp(method, "POST") == 0;
  bool isMultipart = strncasecmp(contentType, "multipart/form-data", 19) == 0;

  bool apiPath      = strncmp(path, "/api/v1/", 8) == 0;
  bool adminPath    = strncmp(path, "/admin/", 7) == 0;
  bool settingsPath = pathStarts(path, "/settings");

  // ---- auth gating by route ----
  if (apiPath || settingsPath) {
    if (!authIsClientOrAdmin(authHdr)) { send401(client); return; }
  } else if (adminPath) {
    if (!authIsClientOrAdmin(authHdr)) { send401(client); return; }
    if (!authIsAdmin(authHdr)) { send403(client); return; }   // mutations require admin
  }

  // ---- multipart upload streams straight from the socket (bypasses g_body) ----
  if (adminPath && isPost && isMultipart) {
    if (pathStarts(path, "/admin/settings/warning-sound"))
      handleWarningSoundUpload(client, contentType, contentLength);
    else { client.print(F("HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n")); client.flush(); client.stop(); }
    return;
  }

  // ---- read body for non-multipart POSTs ----
  int bodyLen = 0;
  if (contentLength > 0) {
    long n = contentLength;
    if (n > (long)sizeof(g_body) - 1) { sendApiError(client, 400, "body too large"); return; }
    uint32_t t0 = millis();
    while (bodyLen < n && client.connected() && millis() - t0 < 4000) {
      if (client.available()) g_body[bodyLen++] = client.read();
      else updateButton();
    }
    g_body[bodyLen] = 0;
  } else {
    g_body[0] = 0;
  }

  // ---- routing ----
  if (pathStarts(path, "/api/v1/events/batch")) {
    if (isPost) handleEventsBatch(client, g_body, bodyLen); else send405(client);
  } else if (pathStarts(path, "/api/v1/client/config")) {
    if (isGet) handleClientConfig(client, path); else send405(client);
  } else if (pathStarts(path, "/api/v1/warning-sound")) {
    if (isGet) handleWarningSound(client); else send405(client);
  } else if (pathStarts(path, "/api/v1/board/today")) {
    if (isGet) handleBoardToday(client); else send405(client);
  } else if (pathStarts(path, "/api/v1/unlock")) {
    if (isPost) handleUnlock(client); else send405(client);
  } else if (pathStarts(path, "/api/v1/lock")) {
    if (isGet) handleLockStatus(client);
    else if (isPost) handleLockToggle(client);
    else send405(client);
  } else if (settingsPath) {
    if (isGet) renderSettingsPage(client, authIsAdmin(authHdr)); else send405(client);
  } else if (adminPath) {
    if (isPost) routeAdmin(client, path, g_body); else send405(client);
  } else if (strncmp(path, "/favicon", 8) == 0) {
    client.print(F("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"));
    client.flush(); client.stop();
  } else {
    sendRootPage(client);
  }
}

void setup() {
  pinMode(BUTTON_PIN, INPUT_PULLUP);
  pinMode(LED_BUILTIN, OUTPUT);
  pixel.begin();
  pixel.setBrightness(120);
  pixel.clear();
  pixel.show();

  Serial.begin(115200);
  uint32_t t0 = millis();
  while (!Serial && millis() - t0 < 1500) {}
  Serial.println(F("\n[rootaika] booting (rootaika server)"));

  if (!LittleFS.begin()) {
    Serial.println(F("[rootaika] LittleFS mount failed -> formatting"));
    LittleFS.format();
    LittleFS.begin();
  }
  storageBegin();
  Serial.print(F("[rootaika] devices=")); Serial.print(g_deviceCount);
  Serial.print(F(" users=")); Serial.print(g_userCount);
  Serial.print(F(" categories=")); Serial.println(g_categoryCount);
  applyLockLed();

  Serial2.begin(115200);
  delay(50);
  WiFi.init(Serial2);
  if (WiFi.status() == WL_NO_MODULE)
    Serial.println(F("[rootaika] ERROR: ESP-AT module not found on Serial2"));

  Serial.print(F("[rootaika] connecting WiFi: ")); Serial.println(WIFI_SSID);
  WiFi.begin(WIFI_SSID, WIFI_PASS);
  for (int i = 0; i < 40 && WiFi.status() != WL_CONNECTED; i++) { delay(500); Serial.print('.'); }
  Serial.println();

  if (WiFi.status() == WL_CONNECTED) {
    Serial.print(F("[rootaika] connected, IP: ")); Serial.println(WiFi.localIP());
    if (WiFi.startMDNS(HOSTNAME, "http", 80))
      Serial.println(F("[rootaika] mDNS -> http://rootaika.local/"));
    // SNTP for version-report timestamps (the board has no RTC). Best-effort:
    // give the ESP-AT firmware a few seconds to fetch time, then carry on.
    WiFi.sntp("pool.ntp.org", "time.nist.gov");
    for (int i = 0; i < 10 && !clockSync(); i++) delay(500);
    if (g_clockEpochBase) { char t[24]; nowUtcString(t, sizeof(t)); Serial.print(F("[rootaika] clock synced: ")); Serial.println(t); }
    else                    Serial.println(F("[rootaika] clock NOT synced (version timestamps blank until NTP responds)"));
    server.begin(5, 3);
    Serial.println(F("[rootaika] rootaika server on :80"));
  } else {
    Serial.println(F("[rootaika] WiFi FAILED - check SSID/password"));
  }
}

void loop() {
  updateButton();
  digitalWrite(LED_BUILTIN, buttonPressed ? HIGH : LOW);

  // physical button: short press = lock all assigned, hold >=1s = unlock all
  if (buttonPressed && !prevPressed) {
    pressStartMs = millis();
    longFired = false;
  } else if (buttonPressed && !longFired && millis() - pressStartMs >= LONG_PRESS_MS) {
    unlockAllLocks();
    applyLockLed();
    longFired = true;
  } else if (!buttonPressed && prevPressed) {
    if (!longFired) { lockAllAssigned(g_settings.lockAllMessage, 0); applyLockLed(); }
  }
  prevPressed = buttonPressed;

  updateLed();

  // Keep the NTP clock fresh: re-sync hourly once synced, retry every 30 s while
  // still unsynced. Gated on millis() so we never spam the ESP-AT per loop.
  static uint32_t lastClockAttemptMs = 0;
  uint32_t clockInterval = g_clockEpochBase ? CLOCK_RESYNC_MS : 30000UL;
  if (millis() - lastClockAttemptMs >= clockInterval) {
    lastClockAttemptMs = millis();
    clockSync();
  }

  WiFiClient client = server.available();
  if (client) handleClient(client);
}
