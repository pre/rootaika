#pragma once
// storage.h — rootaika server data model + LittleFS persistence + lock/usage
// logic, ported from the Go server's store.go. Header-only: included once by the
// .ino, so the globals live here. All state is kept in RAM and mirrored to small
// JSON files on LittleFS so it survives reboots.

#include <LittleFS.h>
#include <ArduinoJson.h>

// ---- fixed capacities (home LAN scale: a handful of PCs) ----
#define MAX_DEVICES   16
#define MAX_USERS     16
#define MAX_CATEGORIES 32
#define DEDUP_RING    256   // recent event_ids kept in RAM to drop retried duplicates

// Cap the uploadable warning MP3 (matches the Go server's 10 MB limit), so an
// oversized upload cannot fill LittleFS.
static const long maxWarningSoundBytesRP = 10L << 20;

// ---------- data model ----------
struct Settings {
  int  idle = 60, upload = 60, poll = 30, maxGap = 300, chartYMax = 720, boardRefresh = 60;
  bool debug = false, debugUnassigned = false;
  int  soundVer = 0;                 // bumped on every MP3 upload; 0 = none uploaded
  int  nextDeviceId = 1, nextUserId = 1, nextCategoryId = 1;
};

struct Device {
  int  id = 0;
  char uuid[40] = "";
  char name[40] = "";
  int  userId = 0;                   // 0 = unassigned
  bool locked = false;
  char lockMsg[64] = "";
  int  warnSeconds = 0;
  char lastStatus[8] = "";           // active/idle/locked, client-reported
  int  idle = 60, upload = 60, poll = 30;
  char lastSeen[24] = "";            // newest event occurred_at (no RTC on board)
};

struct User {
  int  id = 0;
  char name[40] = "";
};

struct Category {
  int  id = 0;
  char type[10] = "";                // exact/prefix/contains
  char pattern[64] = "";
  char cat[40] = "";
};

// ---------- globals ----------
Settings g_settings;
Device   g_devices[MAX_DEVICES];
int      g_deviceCount = 0;
User     g_users[MAX_USERS];
int      g_userCount = 0;
Category g_categories[MAX_CATEGORIES];
int      g_categoryCount = 0;

// recent event_id ring for idempotent re-send (RAM only, resets on reboot)
char     g_dedup[DEDUP_RING][40];
int      g_dedupHead = 0;
bool     g_dedupFull = false;

// ---------- small helpers ----------
static bool isAssigned(const Device& d) { return d.userId > 0; }

static Device* deviceById(int id) {
  for (int i = 0; i < g_deviceCount; i++) if (g_devices[i].id == id) return &g_devices[i];
  return nullptr;
}
static Device* deviceByUuid(const char* uuid) {
  for (int i = 0; i < g_deviceCount; i++) if (strcmp(g_devices[i].uuid, uuid) == 0) return &g_devices[i];
  return nullptr;
}
static User* userById(int id) {
  for (int i = 0; i < g_userCount; i++) if (g_users[i].id == id) return &g_users[i];
  return nullptr;
}
static const char* userName(int id) {
  User* u = userById(id);
  return u ? u->name : "";
}

// jsonEscapeTo writes s into a WiFiClient/File-like sink with JSON string escaping
// for the characters that can appear in admin-entered names/messages.
template <class Sink>
static void jsonEscape(Sink& out, const char* s) {
  for (const char* p = s; *p; p++) {
    char ch = *p;
    if (ch == '"' || ch == '\\') { out.print('\\'); out.print(ch); }
    else if (ch == '\n') out.print(F("\\n"));
    else if (ch == '\r') out.print(F("\\r"));
    else if (ch == '\t') out.print(F("\\t"));
    else if ((uint8_t)ch < 0x20) { /* drop other control chars */ }
    else out.print(ch);
  }
}

// ---------- persistence: load ----------
static void loadSettings() {
  File f = LittleFS.open("/settings.json", "r");
  if (!f) return;
  JsonDocument doc;
  if (deserializeJson(doc, f) == DeserializationError::Ok) {
    g_settings.idle            = doc["idle"]            | g_settings.idle;
    g_settings.upload          = doc["upload"]          | g_settings.upload;
    g_settings.poll            = doc["poll"]            | g_settings.poll;
    g_settings.maxGap          = doc["maxGap"]          | g_settings.maxGap;
    g_settings.chartYMax       = doc["chartYMax"]       | g_settings.chartYMax;
    g_settings.boardRefresh    = doc["boardRefresh"]    | g_settings.boardRefresh;
    g_settings.debug           = doc["debug"]           | g_settings.debug;
    g_settings.debugUnassigned = doc["debugUnassigned"] | g_settings.debugUnassigned;
    g_settings.soundVer        = doc["soundVer"]        | g_settings.soundVer;
    g_settings.nextDeviceId    = doc["nextDeviceId"]    | g_settings.nextDeviceId;
    g_settings.nextUserId      = doc["nextUserId"]      | g_settings.nextUserId;
    g_settings.nextCategoryId  = doc["nextCategoryId"]  | g_settings.nextCategoryId;
  }
  f.close();
}

static void loadDevices() {
  g_deviceCount = 0;
  File f = LittleFS.open("/devices.json", "r");
  if (!f) return;
  JsonDocument doc;
  if (deserializeJson(doc, f) == DeserializationError::Ok) {
    for (JsonObject o : doc.as<JsonArray>()) {
      if (g_deviceCount >= MAX_DEVICES) break;
      Device& d = g_devices[g_deviceCount];
      d = Device{};
      d.id = o["id"] | 0;
      strncpy(d.uuid, o["uuid"] | "", sizeof(d.uuid) - 1);
      strncpy(d.name, o["name"] | "", sizeof(d.name) - 1);
      d.userId = o["userId"] | 0;
      d.locked = o["locked"] | false;
      strncpy(d.lockMsg, o["lockMsg"] | "", sizeof(d.lockMsg) - 1);
      d.warnSeconds = o["warnSeconds"] | 0;
      strncpy(d.lastStatus, o["lastStatus"] | "", sizeof(d.lastStatus) - 1);
      d.idle   = o["idle"]   | g_settings.idle;
      d.upload = o["upload"] | g_settings.upload;
      d.poll   = o["poll"]   | g_settings.poll;
      strncpy(d.lastSeen, o["lastSeen"] | "", sizeof(d.lastSeen) - 1);
      if (d.id > 0 && d.uuid[0]) g_deviceCount++;
    }
  }
  f.close();
}

static void loadUsers() {
  g_userCount = 0;
  File f = LittleFS.open("/users.json", "r");
  if (!f) return;
  JsonDocument doc;
  if (deserializeJson(doc, f) == DeserializationError::Ok) {
    for (JsonObject o : doc.as<JsonArray>()) {
      if (g_userCount >= MAX_USERS) break;
      User& u = g_users[g_userCount];
      u.id = o["id"] | 0;
      strncpy(u.name, o["name"] | "", sizeof(u.name) - 1);
      u.name[sizeof(u.name) - 1] = 0;
      if (u.id > 0 && u.name[0]) g_userCount++;
    }
  }
  f.close();
}

static void loadCategories() {
  g_categoryCount = 0;
  File f = LittleFS.open("/categories.json", "r");
  if (!f) return;
  JsonDocument doc;
  if (deserializeJson(doc, f) == DeserializationError::Ok) {
    for (JsonObject o : doc.as<JsonArray>()) {
      if (g_categoryCount >= MAX_CATEGORIES) break;
      Category& c = g_categories[g_categoryCount];
      c.id = o["id"] | 0;
      strncpy(c.type, o["type"] | "", sizeof(c.type) - 1);
      strncpy(c.pattern, o["pattern"] | "", sizeof(c.pattern) - 1);
      strncpy(c.cat, o["cat"] | "", sizeof(c.cat) - 1);
      if (c.id > 0 && c.type[0]) g_categoryCount++;
    }
  }
  f.close();
}

// ---------- persistence: save ----------
static void saveSettings() {
  File f = LittleFS.open("/settings.json", "w");
  if (!f) return;
  f.print(F("{\"idle\":"));            f.print(g_settings.idle);
  f.print(F(",\"upload\":"));          f.print(g_settings.upload);
  f.print(F(",\"poll\":"));            f.print(g_settings.poll);
  f.print(F(",\"maxGap\":"));          f.print(g_settings.maxGap);
  f.print(F(",\"chartYMax\":"));       f.print(g_settings.chartYMax);
  f.print(F(",\"boardRefresh\":"));    f.print(g_settings.boardRefresh);
  f.print(F(",\"debug\":"));           f.print(g_settings.debug ? F("true") : F("false"));
  f.print(F(",\"debugUnassigned\":")); f.print(g_settings.debugUnassigned ? F("true") : F("false"));
  f.print(F(",\"soundVer\":"));        f.print(g_settings.soundVer);
  f.print(F(",\"nextDeviceId\":"));    f.print(g_settings.nextDeviceId);
  f.print(F(",\"nextUserId\":"));      f.print(g_settings.nextUserId);
  f.print(F(",\"nextCategoryId\":"));  f.print(g_settings.nextCategoryId);
  f.print(F("}"));
  f.close();
}

static void saveDevices() {
  File f = LittleFS.open("/devices.json", "w");
  if (!f) return;
  f.print('[');
  for (int i = 0; i < g_deviceCount; i++) {
    Device& d = g_devices[i];
    if (i) f.print(',');
    f.print(F("{\"id\":")); f.print(d.id);
    f.print(F(",\"uuid\":\"")); jsonEscape(f, d.uuid);
    f.print(F("\",\"name\":\"")); jsonEscape(f, d.name);
    f.print(F("\",\"userId\":")); f.print(d.userId);
    f.print(F(",\"locked\":")); f.print(d.locked ? F("true") : F("false"));
    f.print(F(",\"lockMsg\":\"")); jsonEscape(f, d.lockMsg);
    f.print(F("\",\"warnSeconds\":")); f.print(d.warnSeconds);
    f.print(F(",\"lastStatus\":\"")); jsonEscape(f, d.lastStatus);
    f.print(F("\",\"idle\":")); f.print(d.idle);
    f.print(F(",\"upload\":")); f.print(d.upload);
    f.print(F(",\"poll\":")); f.print(d.poll);
    f.print(F(",\"lastSeen\":\"")); jsonEscape(f, d.lastSeen);
    f.print(F("\"}"));
  }
  f.print(']');
  f.close();
}

static void saveUsers() {
  File f = LittleFS.open("/users.json", "w");
  if (!f) return;
  f.print('[');
  for (int i = 0; i < g_userCount; i++) {
    if (i) f.print(',');
    f.print(F("{\"id\":")); f.print(g_users[i].id);
    f.print(F(",\"name\":\"")); jsonEscape(f, g_users[i].name);
    f.print(F("\"}"));
  }
  f.print(']');
  f.close();
}

static void saveCategories() {
  File f = LittleFS.open("/categories.json", "w");
  if (!f) return;
  f.print('[');
  for (int i = 0; i < g_categoryCount; i++) {
    Category& c = g_categories[i];
    if (i) f.print(',');
    f.print(F("{\"id\":")); f.print(c.id);
    f.print(F(",\"type\":\"")); jsonEscape(f, c.type);
    f.print(F("\",\"pattern\":\"")); jsonEscape(f, c.pattern);
    f.print(F("\",\"cat\":\"")); jsonEscape(f, c.cat);
    f.print(F("\"}"));
  }
  f.print(']');
  f.close();
}

static void storageBegin() {
  loadSettings();
  loadUsers();
  loadDevices();
  loadCategories();
}

// ---------- device lifecycle ----------
// ensureDevice returns the device for uuid, auto-creating an unassigned one with
// config defaulted from the global settings (mirrors Go's EnsureDevice).
static Device* ensureDevice(const char* uuid) {
  Device* d = deviceByUuid(uuid);
  if (d) return d;
  if (g_deviceCount >= MAX_DEVICES) return nullptr;
  d = &g_devices[g_deviceCount];
  *d = Device{};
  d->id = g_settings.nextDeviceId++;
  strncpy(d->uuid, uuid, sizeof(d->uuid) - 1);
  // default display name: "Laite <last 8 of uuid>"
  size_t ul = strlen(uuid);
  if (ul <= 8) strncpy(d->name, uuid, sizeof(d->name) - 1);
  else snprintf(d->name, sizeof(d->name), "Laite %s", uuid + ul - 8);
  d->idle = g_settings.idle; d->upload = g_settings.upload; d->poll = g_settings.poll;
  g_deviceCount++;
  saveDevices();
  saveSettings();
  return d;
}

static void recordDeviceStatus(Device* d, const char* status) {
  if (!d) return;
  strncpy(d->lastStatus, status, sizeof(d->lastStatus) - 1);
  d->lastStatus[sizeof(d->lastStatus) - 1] = 0;
  saveDevices();
}

static void updateDeviceLastSeen(Device* d, const char* occurredAt) {
  if (!d || !occurredAt[0]) return;
  // keep the lexicographically-latest RFC3339 timestamp (UTC, fixed width)
  if (strncmp(occurredAt, d->lastSeen, sizeof(d->lastSeen) - 1) > 0) {
    strncpy(d->lastSeen, occurredAt, sizeof(d->lastSeen) - 1);
    d->lastSeen[sizeof(d->lastSeen) - 1] = 0;
  }
}

// ---------- admin mutations ----------
static int createUser(const char* name) {
  if (!name[0] || g_userCount >= MAX_USERS) return 0;
  for (int i = 0; i < g_userCount; i++) if (strcmp(g_users[i].name, name) == 0) return g_users[i].id;
  User& u = g_users[g_userCount];
  u.id = g_settings.nextUserId++;
  strncpy(u.name, name, sizeof(u.name) - 1);
  u.name[sizeof(u.name) - 1] = 0;
  g_userCount++;
  saveUsers();
  saveSettings();
  return u.id;
}

static bool renameUser(int id, const char* name) {
  if (!name[0]) return false;
  User* u = userById(id);
  if (!u) return false;
  strncpy(u->name, name, sizeof(u->name) - 1);
  u->name[sizeof(u->name) - 1] = 0;
  saveUsers();
  return true;
}

static bool updateDevice(int id, const char* displayName, int userId) {
  Device* d = deviceById(id);
  if (!d) return false;
  if (displayName[0]) { strncpy(d->name, displayName, sizeof(d->name) - 1); d->name[sizeof(d->name) - 1] = 0; }
  d->userId = userId;  // assigned status derives from userId
  saveDevices();
  return true;
}

// purgeDeviceEvents rewrites events.jsonl dropping the deleted device's lines so
// its events do not resurface if a new device later reuses an array slot.
static void purgeDeviceEvents(int deviceId) {
  File in = LittleFS.open("/events.jsonl", "r");
  if (!in) return;
  File out = LittleFS.open("/events.tmp", "w");
  if (!out) { in.close(); return; }
  char needle[24];
  snprintf(needle, sizeof(needle), "\"d\":%d,", deviceId);
  while (in.available()) {
    String ln = in.readStringUntil('\n');
    if (ln.length() == 0) continue;
    if (strstr(ln.c_str(), needle)) continue;  // drop this device's events
    out.print(ln); out.print('\n');
  }
  in.close();
  out.close();
  LittleFS.remove("/events.jsonl");
  LittleFS.rename("/events.tmp", "/events.jsonl");
}

static bool deleteDevice(int id) {
  int idx = -1;
  for (int i = 0; i < g_deviceCount; i++) if (g_devices[i].id == id) { idx = i; break; }
  if (idx < 0) return false;
  purgeDeviceEvents(id);
  for (int i = idx; i < g_deviceCount - 1; i++) g_devices[i] = g_devices[i + 1];
  g_deviceCount--;
  saveDevices();
  return true;
}

static bool setDeviceLock(int id, bool locked, const char* msg, int warnSeconds) {
  Device* d = deviceById(id);
  if (!d) return false;
  d->locked = locked;
  if (locked) {
    strncpy(d->lockMsg, msg, sizeof(d->lockMsg) - 1); d->lockMsg[sizeof(d->lockMsg) - 1] = 0;
    d->warnSeconds = warnSeconds;
  } else {
    d->lockMsg[0] = 0; d->warnSeconds = 0;
  }
  saveDevices();
  return true;
}

// toggleAllLocks flips the lock of every ASSIGNED device: if any assigned device
// is locked, all unlock; otherwise all lock with msg. Mirrors Go's ToggleAllLocks
// and backs the physical button. Returns the resulting state and affected count.
static bool toggleAllLocks(const char* msg, int* affectedOut) {
  int lockedCount = 0, total = 0;
  for (int i = 0; i < g_deviceCount; i++)
    if (isAssigned(g_devices[i])) { total++; if (g_devices[i].locked) lockedCount++; }
  bool lock = (lockedCount == 0);
  int affected = 0;
  for (int i = 0; i < g_deviceCount; i++) {
    if (!isAssigned(g_devices[i])) continue;
    g_devices[i].locked = lock;
    if (lock) { strncpy(g_devices[i].lockMsg, msg, sizeof(g_devices[i].lockMsg) - 1); g_devices[i].lockMsg[63] = 0; }
    else g_devices[i].lockMsg[0] = 0;
    g_devices[i].warnSeconds = 0;
    affected++;
  }
  if (affectedOut) *affectedOut = affected;
  saveDevices();
  return lock;
}

// lockAllAssigned force-locks every assigned device with msg (no warning delay).
// Backs the physical button's short press, whose contract is "lock", not "toggle".
static int lockAllAssigned(const char* msg) {
  int affected = 0;
  for (int i = 0; i < g_deviceCount; i++) {
    if (!isAssigned(g_devices[i])) continue;
    g_devices[i].locked = true;
    strncpy(g_devices[i].lockMsg, msg, sizeof(g_devices[i].lockMsg) - 1);
    g_devices[i].lockMsg[sizeof(g_devices[i].lockMsg) - 1] = 0;
    g_devices[i].warnSeconds = 0;
    affected++;
  }
  saveDevices();
  return affected;
}

static int unlockAllLocks() {
  int affected = 0;
  for (int i = 0; i < g_deviceCount; i++) {
    if (!isAssigned(g_devices[i])) continue;
    g_devices[i].locked = false;
    g_devices[i].lockMsg[0] = 0;
    g_devices[i].warnSeconds = 0;
    affected++;
  }
  saveDevices();
  return affected;
}

static void globalLockState(bool* locked, int* lockedCount, int* total) {
  int lc = 0, tc = 0;
  for (int i = 0; i < g_deviceCount; i++)
    if (isAssigned(g_devices[i])) { tc++; if (g_devices[i].locked) lc++; }
  *lockedCount = lc; *total = tc; *locked = lc > 0;
}

static int createCategory(const char* type, const char* pattern, const char* cat) {
  if (!type[0] || !pattern[0] || !cat[0] || g_categoryCount >= MAX_CATEGORIES) return 0;
  if (strcmp(type, "exact") && strcmp(type, "prefix") && strcmp(type, "contains")) return 0;
  for (int i = 0; i < g_categoryCount; i++)
    if (!strcmp(g_categories[i].type, type) && !strcmp(g_categories[i].pattern, pattern) && !strcmp(g_categories[i].cat, cat))
      return g_categories[i].id;
  Category& c = g_categories[g_categoryCount];
  c.id = g_settings.nextCategoryId++;
  strncpy(c.type, type, sizeof(c.type) - 1);
  strncpy(c.pattern, pattern, sizeof(c.pattern) - 1);
  strncpy(c.cat, cat, sizeof(c.cat) - 1);
  g_categoryCount++;
  saveCategories();
  saveSettings();
  return c.id;
}

static bool deleteCategory(int id) {
  int idx = -1;
  for (int i = 0; i < g_categoryCount; i++) if (g_categories[i].id == id) { idx = i; break; }
  if (idx < 0) return false;
  for (int i = idx; i < g_categoryCount - 1; i++) g_categories[i] = g_categories[i + 1];
  g_categoryCount--;
  saveCategories();
  return true;
}

// updateSettings applies new global settings and pushes idle/upload/poll onto
// every device config (Go's UpdateSettings does the same). Caller validates.
static void updateSettings(const Settings& s) {
  g_settings.idle = s.idle; g_settings.upload = s.upload; g_settings.poll = s.poll;
  g_settings.maxGap = s.maxGap; g_settings.chartYMax = s.chartYMax; g_settings.boardRefresh = s.boardRefresh;
  g_settings.debug = s.debug; g_settings.debugUnassigned = s.debugUnassigned;
  for (int i = 0; i < g_deviceCount; i++) {
    g_devices[i].idle = s.idle; g_devices[i].upload = s.upload; g_devices[i].poll = s.poll;
  }
  saveSettings();
  saveDevices();
}

static void bumpSoundVersion() {
  g_settings.soundVer++;
  saveSettings();
}

// soundVersionStr returns the warning-sound version a client compares against: a
// decimal counter when an MP3 is present, empty otherwise.
static void soundVersionStr(char* out, int outsz) {
  if (g_settings.soundVer > 0 && LittleFS.exists("/warning.mp3"))
    snprintf(out, outsz, "%d", g_settings.soundVer);
  else
    out[0] = 0;
}

// ---------- effective config + version ----------
// debugFor mirrors Go: global debug, or debug-unassigned for an unassigned device.
static bool debugFor(const Device& d) {
  return g_settings.debug || (g_settings.debugUnassigned && !isAssigned(d));
}

// configVersionFor is an FNV-1a fingerprint of the fields a client acts on, byte
// for byte equivalent to the Go server's configVersion so clients see a new
// version exactly when their effective config changes.
static void configVersionFor(const Device& d, char* out, int outsz) {
  uint64_t h = 1469598103934665603ULL;
  const uint64_t prime = 1099511628211ULL;
  auto mix = [&](const char* s) { for (const char* p = s; *p; p++) { h ^= (uint8_t)*p; h *= prime; } };
  char buf[96];
  char sv[16]; soundVersionStr(sv, sizeof(sv));
  // i=%d;u=%d;p=%d;g=%d;d=%t;l=%t;m=%q;w=%d;s=%q;
  snprintf(buf, sizeof(buf), "i=%d;u=%d;p=%d;g=%d;d=%s;l=%s;m=",
           d.idle, d.upload, d.poll, g_settings.maxGap,
           debugFor(d) ? "true" : "false", d.locked ? "true" : "false");
  mix(buf);
  // Go uses %q (quoted) for strings; replicate the surrounding quotes.
  mix("\""); mix(d.locked ? d.lockMsg : ""); mix("\"");
  snprintf(buf, sizeof(buf), ";w=%d;s=", d.locked ? d.warnSeconds : 0);
  mix(buf);
  mix("\""); mix(sv); mix("\""); mix(";");
  for (int i = 0; i < g_categoryCount; i++) {
    mix("c=\""); mix(g_categories[i].type); mix("\",\""); mix(g_categories[i].pattern);
    mix("\",\""); mix(g_categories[i].cat); mix("\";");
  }
  snprintf(out, outsz, "%016llx", (unsigned long long)h);
}

// ---------- event dedup ----------
static bool seenEventId(const char* id) {
  int n = g_dedupFull ? DEDUP_RING : g_dedupHead;
  for (int i = 0; i < n; i++) if (strcmp(g_dedup[i], id) == 0) return true;
  return false;
}
static void rememberEventId(const char* id) {
  strncpy(g_dedup[g_dedupHead], id, 39);
  g_dedup[g_dedupHead][39] = 0;
  g_dedupHead = (g_dedupHead + 1) % DEDUP_RING;
  if (g_dedupHead == 0) g_dedupFull = true;
}

// ---------- usage computation (board "today") ----------
// Parse RFC3339 "YYYY-MM-DDTHH:MM:SS..." -> Unix epoch seconds (UTC). -1 on fail.
static long epochFromRFC3339(const char* s) {
  int Y, Mo, D, h, mi, se;
  if (sscanf(s, "%4d-%2d-%2dT%2d:%2d:%2d", &Y, &Mo, &D, &h, &mi, &se) != 6) return -1;
  int y = Y - (Mo <= 2);
  long era = (y >= 0 ? y : y - 399) / 400;
  unsigned yoe = (unsigned)(y - era * 400);
  unsigned doy = (153 * (Mo + (Mo > 2 ? -3 : 9)) + 2) / 5 + D - 1;
  unsigned doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
  long days = era * 146097 + (long)doe - 719468;
  return days * 86400L + h * 3600L + mi * 60L + se;
}

// computeTodaySeconds fills secByIndex[i] with active seconds for g_devices[i] on
// the newest event date in the log (the board has no RTC, so "today" = latest
// observed day). Each gap after an 'active' event is attributed to that device,
// capped at the global maxGap. maxDate[11] receives the day used.
static void computeTodaySeconds(long secByIndex[], char* maxDate) {
  for (int i = 0; i < g_deviceCount; i++) secByIndex[i] = 0;
  maxDate[0] = 0;

  File f = LittleFS.open("/events.jsonl", "r");
  if (!f) return;
  while (f.available()) {
    String ln = f.readStringUntil('\n');
    const char* t = strstr(ln.c_str(), "\"t\":\"");
    if (!t) continue;
    char date[11]; strncpy(date, t + 5, 10); date[10] = 0;
    if (strcmp(date, maxDate) > 0) strcpy(maxDate, date);
  }
  f.close();
  if (maxDate[0] == 0) return;

  long lastEpoch[MAX_DEVICES];
  bool lastActive[MAX_DEVICES], lastOnDay[MAX_DEVICES];
  for (int i = 0; i < g_deviceCount; i++) { lastEpoch[i] = -1; lastActive[i] = false; lastOnDay[i] = false; }

  f = LittleFS.open("/events.jsonl", "r");
  while (f.available()) {
    String ln = f.readStringUntil('\n');
    const char* p = ln.c_str();
    const char* pd = strstr(p, "\"d\":");
    const char* pt = strstr(p, "\"t\":\"");
    const char* ps = strstr(p, "\"s\":\"");
    if (!pd || !pt || !ps) continue;
    int devId = atoi(pd + 4);
    int idx = -1;
    for (int i = 0; i < g_deviceCount; i++) if (g_devices[i].id == devId) { idx = i; break; }
    if (idx < 0) continue;
    const char* tt = pt + 5;
    bool onDay = (strncmp(tt, maxDate, 10) == 0);
    long ep = epochFromRFC3339(tt);
    bool active = (*(ps + 5) == 'a');
    if (lastEpoch[idx] >= 0 && lastActive[idx] && lastOnDay[idx] && onDay && ep >= 0) {
      long gap = ep - lastEpoch[idx];
      if (gap > 0) { if (gap > g_settings.maxGap) gap = g_settings.maxGap; secByIndex[idx] += gap; }
    }
    lastEpoch[idx] = ep; lastActive[idx] = active; lastOnDay[idx] = onDay;
  }
  f.close();
}
