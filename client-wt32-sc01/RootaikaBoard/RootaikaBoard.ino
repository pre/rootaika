#define LGFX_USE_V1
#include <LovyanGFX.hpp>
#include <WiFi.h>
#include <HTTPClient.h>
#include <ArduinoJson.h>
#include "config.h"  // WIFI_SSID, WIFI_PASS, SERVER_URL, BOARD_USER, BOARD_PASS; not committed

const char* BOARD_URL = SERVER_URL "/api/v1/board/today";
const char* CHART_URL = SERVER_URL "/api/v1/charts/usage?range=day";

class LGFX : public lgfx::LGFX_Device {
  lgfx::Panel_ST7796  _panel_instance;
  lgfx::Bus_Parallel8 _bus_instance;
  lgfx::Light_PWM     _light_instance;
  lgfx::Touch_FT5x06  _touch_instance;
public:
  LGFX(void) {
    {
      auto cfg = _bus_instance.config();
      cfg.port=0; cfg.freq_write=20000000; cfg.pin_wr=47; cfg.pin_rd=-1; cfg.pin_rs=0;
      cfg.pin_d0=9; cfg.pin_d1=46; cfg.pin_d2=3; cfg.pin_d3=8;
      cfg.pin_d4=18; cfg.pin_d5=17; cfg.pin_d6=16; cfg.pin_d7=15;
      _bus_instance.config(cfg); _panel_instance.setBus(&_bus_instance);
    }
    {
      auto cfg = _panel_instance.config();
      cfg.pin_cs=-1; cfg.pin_rst=4; cfg.pin_busy=-1;
      cfg.panel_width=320; cfg.panel_height=480;
      cfg.readable=true; cfg.invert=true; cfg.rgb_order=false; cfg.bus_shared=false;
      _panel_instance.config(cfg);
    }
    {
      auto cfg = _light_instance.config();
      cfg.pin_bl=45; cfg.freq=44100; cfg.pwm_channel=7;
      _light_instance.config(cfg); _panel_instance.setLight(&_light_instance);
    }
    {
      auto cfg = _touch_instance.config();
      cfg.x_min=0; cfg.x_max=319; cfg.y_min=0; cfg.y_max=479;
      cfg.pin_int=7; cfg.bus_shared=true; cfg.offset_rotation=0;
      cfg.i2c_port=1; cfg.i2c_addr=0x38; cfg.pin_sda=6; cfg.pin_scl=5; cfg.freq=400000;
      _touch_instance.config(cfg); _panel_instance.setTouch(&_touch_instance);
    }
    setPanel(&_panel_instance);
  }
};

static LGFX gfx;

#define C_BG      0x0841
#define C_HEADER  0x6B5E
#define C_CARD    0x18E3
#define C_ACCENT  0x07FF
#define C_TEXT    0xFFFF
#define C_DIM     0x8410
#define C_GRID    0x2945

enum View { VIEW_LIST, VIEW_DETAIL };
View view = VIEW_LIST;
String selectedName = "";

int refreshSeconds = 60;
uint32_t lastFetch = 0;

const int MAX_CARDS = 6;
struct CardHit { int top; int bottom; String name; };
CardHit cardHits[MAX_CARDS];
int cardHitCount = 0;

void drawHeader(const char* title, bool back) {
  gfx.fillRect(0, 0, gfx.width(), 44, C_HEADER);
  gfx.setTextColor(C_TEXT, C_HEADER);
  gfx.setTextDatum(middle_left);
  if (back) {
    gfx.setTextSize(2);
    gfx.drawString("<", 14, 22);
    gfx.drawString(title, 40, 22);
  } else {
    gfx.setTextSize(2);
    gfx.drawString(title, 14, 22);
  }
}

void drawCard(int idx, const char* name, int minutes) {
  int top = 56 + idx * 64;
  int h = 56;
  int w = gfx.width() - 24;
  int x = 12;
  gfx.fillRoundRect(x, top, w, h, 8, C_CARD);

  gfx.setTextColor(C_TEXT, C_CARD);
  gfx.setTextDatum(top_left);
  gfx.setTextSize(2);
  gfx.drawString(name, x + 14, top + 9);

  char buf[32];
  int hrs = minutes / 60;
  int mins = minutes % 60;
  if (hrs > 0) snprintf(buf, sizeof(buf), "%dh %02dmin", hrs, mins);
  else         snprintf(buf, sizeof(buf), "%dmin", mins);

  gfx.setTextColor(C_ACCENT, C_CARD);
  gfx.setTextDatum(bottom_right);
  gfx.setTextSize(3);
  gfx.drawString(buf, x + w - 14, top + h - 8);

  if (idx < MAX_CARDS) {
    cardHits[idx].top = top;
    cardHits[idx].bottom = top + h;
    cardHits[idx].name = String(name);
  }
}

void drawFooter(const char* now) {
  gfx.setTextColor(C_DIM, C_BG);
  gfx.setTextDatum(bottom_center);
  gfx.setTextSize(1);
  gfx.drawString(now, gfx.width()/2, gfx.height() - 6);
}

void showMessage(const char* msg, uint32_t color) {
  gfx.fillScreen(C_BG);
  drawHeader("Rootaika", false);
  gfx.setTextColor(color, C_BG);
  gfx.setTextDatum(middle_center);
  gfx.setTextSize(2);
  gfx.drawString(msg, gfx.width()/2, gfx.height()/2);
}

bool fetchAndDraw() {
  cardHitCount = 0;
  if (WiFi.status() != WL_CONNECTED) { showMessage("Ei WiFi-yhteytta", C_ACCENT); return false; }
  HTTPClient http;
  http.begin(BOARD_URL);
  http.setAuthorization(BOARD_USER, BOARD_PASS);
  http.setConnectTimeout(5000);
  int code = http.GET();
  if (code != 200) {
    char buf[40]; snprintf(buf, sizeof(buf), "HTTP %d", code);
    showMessage(buf, C_ACCENT); http.end(); return false;
  }
  String payload = http.getString();
  http.end();

  JsonDocument doc;
  if (deserializeJson(doc, payload)) { showMessage("JSON virhe", C_ACCENT); return false; }

  refreshSeconds = doc["refresh_seconds"] | 60;
  const char* now = doc["now"] | "";
  JsonArray devices = doc["devices"].as<JsonArray>();

  gfx.startWrite();
  gfx.fillScreen(C_BG);
  drawHeader("Rootaika", false);
  int idx = 0;
  int maxCards = (gfx.height() - 56 - 24) / 64;
  if (maxCards > MAX_CARDS) maxCards = MAX_CARDS;
  for (JsonObject dev : devices) {
    if (idx >= maxCards) break;
    drawCard(idx, dev["name"] | "?", dev["minutes"] | 0);
    idx++;
  }
  cardHitCount = idx;
  if (idx == 0) {
    gfx.setTextColor(C_DIM, C_BG);
    gfx.setTextDatum(middle_center);
    gfx.setTextSize(2);
    gfx.drawString("Ei dataa tanaan", gfx.width()/2, gfx.height()/2);
  }
  drawFooter(now);
  gfx.endWrite();
  return true;
}

bool fetchAndDrawDetail() {
  if (WiFi.status() != WL_CONNECTED) { showMessage("Ei WiFi-yhteytta", C_ACCENT); return false; }
  HTTPClient http;
  http.begin(CHART_URL);
  http.setAuthorization(BOARD_USER, BOARD_PASS);
  http.setConnectTimeout(5000);
  int code = http.GET();
  if (code != 200) {
    char buf[40]; snprintf(buf, sizeof(buf), "HTTP %d", code);
    showMessage(buf, C_ACCENT); http.end(); return false;
  }
  String payload = http.getString();
  http.end();

  JsonDocument doc;
  if (deserializeJson(doc, payload)) { showMessage("JSON virhe", C_ACCENT); return false; }

  refreshSeconds = doc["refresh_seconds"] | 60;
  int yMax = doc["y_max_minutes"] | 240;
  if (yMax <= 0) yMax = 240;
  JsonArray labels = doc["labels"].as<JsonArray>();
  JsonArray devices = doc["devices"].as<JsonArray>();

  JsonArray points;
  bool found = false;
  for (JsonObject dev : devices) {
    const char* nm = dev["name"] | "";
    if (selectedName == nm) { points = dev["points"].as<JsonArray>(); found = true; break; }
  }

  gfx.startWrite();
  gfx.fillScreen(C_BG);
  drawHeader(selectedName.c_str(), true);

  if (!found || points.size() == 0) {
    gfx.setTextColor(C_DIM, C_BG);
    gfx.setTextDatum(middle_center);
    gfx.setTextSize(2);
    gfx.drawString("Ei dataa tanaan", gfx.width()/2, gfx.height()/2);
    gfx.endWrite();
    return true;
  }

  // Plot area
  int plotX = 56;
  int plotY = 58;
  int plotW = gfx.width() - plotX - 16;
  int plotH = gfx.height() - plotY - 40;

  // Y grid + labels (0, 1/2, max)
  gfx.setTextColor(C_DIM, C_BG);
  gfx.setTextDatum(middle_right);
  gfx.setTextSize(1);
  for (int g = 0; g <= 2; g++) {
    int val = yMax * g / 2;
    int yy = plotY + plotH - (plotH * g / 2);
    gfx.drawLine(plotX, yy, plotX + plotW, yy, C_GRID);
    char lab[12]; snprintf(lab, sizeof(lab), "%d", val);
    gfx.drawString(lab, plotX - 6, yy);
  }

  // X labels: every whole hour that fits, plus vertical grid
  int n = points.size();
  int lsz = labels.size();
  gfx.setTextColor(C_DIM, C_BG);
  gfx.setTextSize(1);
  int lastLabelX = -1000;
  const int MIN_LABEL_GAP = 22;
  for (int i = 0; i < lsz; i++) {
    const char* s = labels[i] | "";
    if (strlen(s) < 5) continue;
    bool wholeHour = (s[3] == 0 && s[4] == 0);
    if (!wholeHour) continue;
    float fx = (lsz > 1) ? (float)i / (lsz - 1) : 0.0f;
    int px = plotX + (int)(fx * plotW);
    if (px - lastLabelX < MIN_LABEL_GAP) continue;
    lastLabelX = px;
    gfx.drawLine(px, plotY, px, plotY + plotH, C_GRID);
    char hh[3] = { s[0], s[1], 0 };
    gfx.setTextDatum(top_center);
    gfx.drawString(hh, px, plotY + plotH + 6);
  }
  // Always mark current time at the right edge
  if (lsz > 0) {
    const char* last = labels[lsz-1] | "";
    int px = plotX + plotW;
    if (px - lastLabelX >= MIN_LABEL_GAP) {
      gfx.setTextColor(C_TEXT, C_BG);
      gfx.setTextDatum(top_right);
      gfx.drawString(last, px, plotY + plotH + 6);
    }
  }

  // Curve
  float lastVal = 0;
  int prevX = 0, prevY = 0;
  for (int i = 0; i < n; i++) {
    float v = points[i].as<float>();
    lastVal = v;
    float fx = (n > 1) ? (float)i / (n - 1) : 0.0f;
    int px = plotX + (int)(fx * plotW);
    float fy = v / yMax; if (fy > 1.0f) fy = 1.0f; if (fy < 0) fy = 0;
    int py = plotY + plotH - (int)(fy * plotH);
    if (i > 0) {
      gfx.drawLine(prevX, prevY, px, py, C_ACCENT);
      gfx.drawLine(prevX, prevY+1, px, py+1, C_ACCENT);
    }
    prevX = px; prevY = py;
  }
  // Last point marker
  gfx.fillCircle(prevX, prevY, 3, C_TEXT);

  // Current cumulative value, big
  char buf[32];
  int total = (int)(lastVal + 0.5f);
  int hrs = total / 60, mins = total % 60;
  if (hrs > 0) snprintf(buf, sizeof(buf), "%dh %02dmin", hrs, mins);
  else         snprintf(buf, sizeof(buf), "%dmin", mins);
  gfx.setTextColor(C_ACCENT, C_BG);
  gfx.setTextDatum(top_right);
  gfx.setTextSize(2);
  gfx.drawString(buf, gfx.width() - 16, plotY + 2);

  gfx.endWrite();
  return true;
}

void refreshCurrent() {
  if (view == VIEW_LIST) fetchAndDraw();
  else fetchAndDrawDetail();
  lastFetch = millis();
}

void setup(void) {
  Serial.begin(115200);
  gfx.init();
  gfx.setRotation(1);
  gfx.setBrightness(128);

  showMessage("Yhdistetaan WiFi...", C_TEXT);
  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASS);
  uint32_t start = millis();
  while (WiFi.status() != WL_CONNECTED && millis() - start < 20000) delay(300);

  refreshCurrent();
}

void loop(void) {
  int32_t tx, ty;
  if (gfx.getTouch(&tx, &ty)) {
    if (view == VIEW_LIST) {
      int hit = -1;
      for (int i = 0; i < cardHitCount; i++) {
        if (ty >= cardHits[i].top && ty <= cardHits[i].bottom) { hit = i; break; }
      }
      if (hit >= 0) {
        gfx.fillRect(cardHits[hit].top >= 0 ? 0 : 0, cardHits[hit].top, gfx.width(), cardHits[hit].bottom - cardHits[hit].top, C_ACCENT);
        delay(80);
        selectedName = cardHits[hit].name;
        view = VIEW_DETAIL;
        refreshCurrent();
      }
    } else {
      gfx.fillRect(0, 0, gfx.width(), 44, C_ACCENT);
      delay(80);
      view = VIEW_LIST;
      refreshCurrent();
    }
    delay(300);
  }

  if (millis() - lastFetch >= (uint32_t)refreshSeconds * 1000UL) {
    refreshCurrent();
  }
  delay(20);
}
