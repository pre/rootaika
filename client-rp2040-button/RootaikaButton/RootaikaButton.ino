/*
  RootaikaButton — a physical lock button for the rootaika Go server.

  Board: iLabs Challenger RP2040 WiFi (RP2040 + ESP-AT WiFi co-processor on
  Serial2). A momentary push button on GPIO2 (to GND):

    short press      -> POST /api/v1/lock    (locks all assigned devices,
                        clients get a warning countdown before the screen locks)
    hold >= 1 second -> POST /api/v1/unlock  (releases all locks)

  The onboard RGB NeoPixel (GPIO11) shows the LAST SUCCESSFUL CALL made from
  this button: red after lock, green after unlock, off at boot or after a
  failed call.

  ponytail: the LED does not poll the server, so its color can disagree with
  the real lock state (e.g. after an admin lock/unlock from the web UI or a
  reboot). Known limitation; add a GET /api/v1/lock status poll when it bites.
*/

#include <WiFiEspAT.h>
#include <Adafruit_NeoPixel.h>
#include "wifi.h"

const int      BUTTON_PIN    = 2;     // GPIO2 = Feather "D5". Button between pin and GND.
const uint32_t DEBOUNCE_MS   = 25;
const uint32_t LONG_PRESS_MS = 1000;  // hold this long to unlock
const uint32_t HTTP_TIMEOUT_MS    = 8000;
const uint32_t WIFI_RECONNECT_MS  = 30000;

Adafruit_NeoPixel pixel(1, NEOPIXEL, NEO_GRB + NEO_KHZ800);

void ledShow(uint8_t r, uint8_t g, uint8_t b) {
  pixel.setPixelColor(0, pixel.Color(r, g, b));
  pixel.show();
}

// ---- button debounce + short/long press ----
int      lastReading    = HIGH;
int      stableState    = HIGH;
uint32_t lastDebounceMs = 0;
bool     buttonPressed  = false;
bool     prevPressed    = false;
uint32_t pressStartMs   = 0;
bool     longFired      = false;

void updateButton() {
  int reading = digitalRead(BUTTON_PIN);
  if (reading != lastReading) { lastDebounceMs = millis(); lastReading = reading; }
  if (millis() - lastDebounceMs > DEBOUNCE_MS && reading != stableState) {
    stableState = reading;
    buttonPressed = (stableState == LOW);     // pull-up: pressed = LOW
  }
}

// sendCall POSTs an empty body to the given API path with Basic auth and
// returns true on any 2xx response.
bool sendCall(const char* path) {
  if (WiFi.status() != WL_CONNECTED) return false;

  WiFiClient client;
  if (!client.connect(ROOTAIKA_HOST, ROOTAIKA_PORT)) {
    Serial.print(F("[button] connect failed: ")); Serial.println(ROOTAIKA_HOST);
    return false;
  }
  client.print(F("POST "));
  client.print(path);
  client.print(F(" HTTP/1.1\r\nHost: "));
  client.print(ROOTAIKA_HOST);
  client.print(F("\r\nAuthorization: Basic "));
  client.print(ROOTAIKA_AUTH_B64);
  client.print(F("\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"));
  client.flush();

  // Read just the status line: "HTTP/1.1 200 OK".
  char line[32];
  int  len = 0;
  uint32_t start = millis();
  while (millis() - start < HTTP_TIMEOUT_MS) {
    if (!client.available()) {
      if (!client.connected()) break;
      delay(5);
      continue;
    }
    char ch = client.read();
    if (ch == '\n') break;
    if (ch != '\r' && len < (int)sizeof(line) - 1) line[len++] = ch;
  }
  line[len] = 0;
  client.stop();

  int status = 0;
  const char* sp = strchr(line, ' ');
  if (sp) status = atoi(sp + 1);
  Serial.print(F("[button] ")); Serial.print(path);
  Serial.print(F(" -> ")); Serial.println(status);
  return status >= 200 && status < 300;
}

void pressLock() {
  ledShow(40, 40, 40);                               // dim white: sending
  if (sendCall("/api/v1/lock")) ledShow(255, 0, 0);  // red: locked
  else                          ledShow(0, 0, 0);    // off: unknown
}

void pressUnlock() {
  ledShow(40, 40, 40);
  if (sendCall("/api/v1/unlock")) ledShow(0, 255, 0); // green: unlocked
  else                            ledShow(0, 0, 0);
}

void setup() {
  pinMode(BUTTON_PIN, INPUT_PULLUP);
  pinMode(LED_BUILTIN, OUTPUT);
  pixel.begin();
  pixel.setBrightness(120);
  ledShow(0, 0, 0);

  Serial.begin(115200);
  uint32_t t0 = millis();
  while (!Serial && millis() - t0 < 1500) {}
  Serial.println(F("\n[button] booting (rootaika lock button)"));

  // The RP2040<->ESP-AT link stays at 115200; higher baud breaks WiFi
  // association on this wiring (verified on the real board).
  Serial2.begin(115200);
  delay(50);
  WiFi.init(Serial2);
  if (WiFi.status() == WL_NO_MODULE)
    Serial.println(F("[button] ERROR: ESP-AT module not found on Serial2"));

  Serial.print(F("[button] connecting WiFi: ")); Serial.println(SECRET_SSID);
  WiFi.begin(SECRET_SSID, SECRET_PASS);
  for (int i = 0; i < 40 && WiFi.status() != WL_CONNECTED; i++) { delay(500); Serial.print('.'); }
  Serial.println();
  if (WiFi.status() == WL_CONNECTED) {
    Serial.print(F("[button] connected, IP: ")); Serial.println(WiFi.localIP());
  } else {
    Serial.println(F("[button] WiFi FAILED - check SSID/password"));
  }
}

void loop() {
  updateButton();
  digitalWrite(LED_BUILTIN, buttonPressed ? HIGH : LOW);

  // short press = lock on release, hold >= LONG_PRESS_MS = unlock while held
  if (buttonPressed && !prevPressed) {
    pressStartMs = millis();
    longFired = false;
  } else if (buttonPressed && !longFired && millis() - pressStartMs >= LONG_PRESS_MS) {
    pressUnlock();
    longFired = true;
  } else if (!buttonPressed && prevPressed) {
    if (!longFired) pressLock();
  }
  prevPressed = buttonPressed;

  // Reconnect WiFi if the association drops, gated so we never spam the ESP-AT.
  static uint32_t lastReconnectMs = 0;
  if (WiFi.status() != WL_CONNECTED && millis() - lastReconnectMs >= WIFI_RECONNECT_MS) {
    lastReconnectMs = millis();
    Serial.println(F("[button] WiFi lost, reconnecting"));
    WiFi.begin(SECRET_SSID, SECRET_PASS);
  }
}
