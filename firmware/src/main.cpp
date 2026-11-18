// Today firmware for the Waveshare ESP32-S3-RLCD-4.2.
//
// Always-on state machine: Wi-Fi stays up (modem sleep), a frame is fetched
// every 60 s with an ETag, and the device draws its own cursor, local
// strike-through and the four widgets. Keys never depend on the network: the
// screen updates from the cached bitmap immediately, acks go out when a link
// exists and queue in NVS otherwise.
//
// The wire format is defined by internal/frame/frame.go (fmt 3); do not
// change one side without the other.

#include <Arduino.h>
#include <ArduinoJson.h>
#include <WiFi.h>

#include "board.h"
#include "display.h"
#include "net.h"
#include "night.h"
#include "portal.h"
#include "sensors.h"
#include "storage.h"
#include "tbf.h"
#include "widgets.h"

namespace {

storage::Config cfg;
tbf::Frame frame;
uint8_t base[(board::kWidth / 8) * board::kHeight];   // server bitmap
uint8_t work[(board::kWidth / 8) * board::kHeight];   // base + local overlays
tbf::Row rows[tbf::kMaxRows];
uint8_t rowCount = 0;
uint8_t cursorRow = 0;
uint32_t doneMask = 0;   // local optimistic state, 1 bit per cursor row
bool haveFrame = false;

uint32_t lastFrameAt = 0;
uint32_t lastClimateAt = 0;
uint32_t lastRetryAt = 0;
uint32_t lastKeyAt = 0;
int lastClockMinute = -1;
uint8_t wifiFailures = 0;
bool portalPending = false;

float temperatureC = 0;
float humidity = 0;
bool climateOk = false;
bool timeOk = false;
int batteryMv = 0;

bool nightEnabled = false;
int nightStart = 0;
int nightEnd = 0;
bool screenOff = false;   // DISPOFF has been sent
bool nightDark = false;   // inside the night window: pushes keep the panel off

int utcOffset = 0;
String etag;
String availableFirmware;

void DrawWidgets();

void CaptureFrame() {
  // Remember the focused item before the row table is replaced: the server
  // sinks done rows, so the old index can point at a different item.
  uint8_t previous[16] = {};
  const bool hadCursor = haveFrame && rowCount > 0 && cursorRow < rowCount;
  if (hadCursor) memcpy(previous, rows[cursorRow].id, sizeof(previous));

  rowCount = frame.rowCount;
  for (uint8_t i = 0; i < rowCount; i++) rows[i] = frame.rows[i];
  memcpy(base, frame.bitmap, sizeof(base));
  haveFrame = true;

  // Follow the item by id; fall back to clamping when it disappeared (done
  // items stay listed, but a deleted item or a +N summary row may not).
  if (hadCursor && rowCount > 0) {
    for (uint8_t i = 0; i < rowCount; i++) {
      if (tbf::SameID(rows[i].id, previous)) {
        cursorRow = i;
        break;
      }
    }
  }
  if (rowCount == 0) {
    cursorRow = 0;
  } else if (cursorRow >= rowCount) {
    cursorRow = rowCount - 1;
  }
  storage::SaveCursor(cursorRow);
  // A fetched frame is authoritative for every row: once the server reflects
  // the toggle, the optimistic bit has done its job.
  uint32_t serverDone = 0;
  for (uint8_t i = 0; i < rowCount && i < 32; i++) {
    if (rows[i].flags & tbf::kRowDone) serverDone |= (1UL << i);
  }
  doneMask = serverDone;
}

uint32_t RowBit(int index) { return index >= 0 && index < 32 ? (1UL << index) : 0; }

// ComposeLocal rebuilds `work` from the cached server bitmap and applies the
// local overlays. It never touches the network.
void ComposeLocal() {
  if (!haveFrame) return;
  memcpy(work, base, sizeof(work));
  for (uint8_t i = 0; i < rowCount; i++) {
    if (doneMask & RowBit(i)) widgets::StrikeRow(rows[i].y, board::kRowHeight);
  }
  if (rowCount > 0 && cursorRow < rowCount) {
    widgets::InvertRow(rows[cursorRow].y, board::kRowHeight);
  }
}

// PushPanel sends the composed bitmap and the widgets. In night mode the
// contents still go to the controller RAM, but the panel stays dark: no
// DISPON and `screenOff` is left untouched.
void PushPanel() {
  if (!haveFrame) return;
  display::ShowFrame(work);
  DrawWidgets();
  display::Flush();
  if (!nightDark) display::On();
}

void DrawWidgets() {
  struct tm now {};
  const bool haveTime = timeOk && getLocalTime(&now, 0);
  if (const tbf::Widget* w = tbf::Find(frame, tbf::kWidgetClock)) {
    if (haveTime) {
      widgets::DrawClock(*w, now.tm_hour, now.tm_min);
    } else {
      widgets::DrawClockUnknown(*w);
    }
  }
  if (const tbf::Widget* w = tbf::Find(frame, tbf::kWidgetWifi)) {
    widgets::DrawWifi(*w, net::Rssi(), !net::Connected() && wifiFailures > 0,
                      portal::Active(), net::Connected());
  }
  if (const tbf::Widget* w = tbf::Find(frame, tbf::kWidgetClimate)) {
    widgets::DrawClimate(*w, temperatureC, humidity, climateOk);
  }
  if (const tbf::Widget* w = tbf::Find(frame, tbf::kWidgetPower)) {
    // No VBUS/CHG GPIO on this board: charging can only be guessed, so the
    // widget never claims it. A reading below 2.5 V means "no battery".
    widgets::DrawPower(*w, batteryMv, false, false);
  }
}

// Paint repaints from cache (no fetch) and lights the panel. Only the key
// wake-up and the end of the night window call it.
void Paint() {
  if (!haveFrame) return;
  nightDark = false;
  screenOff = false;
  ComposeLocal();
  PushPanel();
}

void PaintMessage(const char* line1, const char* line2, const char* line3) {
  memset(work, 0, sizeof(work));
  display::ShowFrame(work);
  widgets::DrawString(16, 28, line1, 2);
  widgets::DrawString(16, 76, line2, 1);
  widgets::DrawString(16, 96, line3, 1);
  display::Flush();
  display::On();
  nightDark = false;
  screenOff = false;
  // The cached frame stays in `base`: a later 304 repaints the board instead
  // of leaving this message on screen forever.
}

uint32_t BatteryMillivolts() {
  uint16_t samples[7];
  for (uint16_t& s : samples) {
    s = static_cast<uint16_t>(analogReadMilliVolts(board::kPinBattery));
    delay(2);
  }
  // Median of seven rejects USB noise and panel-switching spikes.
  for (size_t i = 1; i < 7; i++) {
    for (size_t j = i; j > 0 && samples[j - 1] > samples[j]; j--) {
      const uint16_t t = samples[j - 1];
      samples[j - 1] = samples[j];
      samples[j] = t;
    }
  }
  return static_cast<uint32_t>(samples[3]) * board::kBatteryScale;
}

void ReadClimate() {
  float t = 0;
  float h = 0;
  climateOk = sensors::ReadClimate(&t, &h);
  if (climateOk) {
    temperatureC = t;
    humidity = h;
  }
  lastClimateAt = millis();
}

int PressedKey() {
  if (digitalRead(board::kPinKey) == board::kButtonActiveLevel) return board::kPinKey;
  if (digitalRead(board::kPinBoot) == board::kButtonActiveLevel) return board::kPinBoot;
  return -1;
}

void WaitRelease(int pin) {
  const uint32_t start = millis();
  while (digitalRead(pin) == board::kButtonActiveLevel && millis() - start < 4000) delay(10);
}

void SyncClock(int64_t serverNow, int offset) {
  if (serverNow <= 0) return;
  utcOffset = offset;
  storage::SaveUtcOffset(offset);
  const time_t local = static_cast<time_t>(serverNow) + offset;
  struct timeval tv = {local, 0};
  settimeofday(&tv, nullptr);
  timeOk = true;
  struct tm localTm {};
  gmtime_r(&local, &localTm);
  sensors::RtcWrite(localTm);
}

String BuildAcksJson(const char* power) {
  JsonDocument doc;
  doc["frame_seq"] = static_cast<uint32_t>(storage::LoadSeq());
  if (batteryMv > 0) doc["battery_mv"] = batteryMv;
  if (power != nullptr) doc["power"] = power;
  JsonArray ops = doc["ops"].to<JsonArray>();
  const uint8_t count = storage::OutboxCount();
  for (uint8_t i = 0; i < count; i++) {
    storage::OutboxOp op{};
    if (!storage::LoadOutbox(i, &op)) break;
    char itemId[37];
    tbf::FormatID(op.id, itemId);
    char opId[48];
    snprintf(opId, sizeof(opId), "esp-%08lx-%lu",
             static_cast<unsigned long>(ESP.getEfuseMac() & 0xFFFFFFFFUL),
             static_cast<unsigned long>(op.seq));
    JsonObject entry = ops.add<JsonObject>();
    entry["op_id"] = opId;
    entry["item_id"] = itemId;
    entry["status"] = op.status != 0 ? "done" : "open";
  }
  String out;
  serializeJson(doc, out);
  return out;
}

// SendAcks posts queued ops plus the battery reading. Empty ops are valid and
// keep the settings page's battery figure fresh.
void SendAcks() {
  if (!net::Connected()) return;
  const uint8_t count = storage::OutboxCount();
  batteryMv = static_cast<int>(BatteryMillivolts());
  const String payload = BuildAcksJson(nullptr);
  const int code = net::PostAcks(cfg, payload);
  if (code >= 200 && code < 300) {
    if (count > 0) storage::ClearOutbox();
    Serial.printf("acks: sent %u\n", count);
  } else {
    Serial.printf("acks: failed %d, kept %u\n", code, count);
  }
}

void MaybeUpdate(const String& version) {
  if (version.isEmpty() || version == board::kFirmwareVersion) return;
  Serial.printf("ota: %s -> %s\n", board::kFirmwareVersion, version.c_str());
  PaintMessage("UPDATING", version.c_str(), "KEEP POWER ON");
  if (net::DownloadFirmware(cfg)) {
    delay(800);
    ESP.restart();
  }
  Serial.println("ota: failed, keeping current firmware");
}

void ApplyFrame(const net::FetchResult& res) {
  if (res.changed) {
    CaptureFrame();
    storage::SaveFrame(base, sizeof(base));
    storage::SaveRows(rows, rowCount);
    storage::SaveSeq(res.seq);
    etag = res.etag;
    storage::SaveETag(etag);
    availableFirmware = res.firmwareVersion;
  }
  if (res.serverNow > 0) SyncClock(res.serverNow, res.utcOffset);
  nightEnabled = res.night;
  nightStart = res.nightStart;
  nightEnd = res.nightEnd;
}

// Fetch refreshes from the server. During night mode the fresh bitmap is
// composed into RAM but the panel is not lit.
void Fetch(bool repaintAlways) {
  const bool force = storage::TakeForceFull();
  net::FetchResult res = net::FetchFrame(cfg, etag, force, work, sizeof(work), &frame);
  lastFrameAt = millis();
  if (res.status != 304 && res.status != 200) {
    Serial.printf("fetch: %d\n", res.status);
    return;
  }
  wifiFailures = 0;
  ApplyFrame(res);
  if (!haveFrame) return;
  if (repaintAlways || res.changed || res.refresh || res.forceFull || force) {
    ComposeLocal();
    PushPanel();
    return;
  }
  // 304: only the clock and the other widgets can have moved.
  ComposeLocal();
  PushPanel();
}

void HandleKeys() {
  const int key = PressedKey();
  if (key < 0) return;

  // Measure the hold once: 0.6 s is the toggle threshold, a full 3 s on KEY
  // opens the setup portal instead (so the two gestures cannot collide).
  const uint32_t start = millis();
  uint32_t held = 0;
  while (digitalRead(key) == board::kButtonActiveLevel) {
    delay(10);
    held = millis() - start;
    if (key == board::kPinKey && held >= board::kProvisionHoldMs) break;
    if (held >= 4000) break;
  }
  if (digitalRead(key) == board::kButtonActiveLevel) WaitRelease(key);

  if (key == board::kPinKey && held >= board::kProvisionHoldMs) {
    Serial.println("key: setup portal");
    portalPending = true;
    return;
  }

  // Night: the first press only lights the board. In the dark the same press
  // must not also move the cursor or toggle a row.
  if (screenOff) {
    Paint();
    lastKeyAt = millis();
    return;
  }
  lastKeyAt = millis();

  if (key == board::kPinBoot) {
    storage::SetForceFull(true);
    SendAcks();
    Fetch(true);
    return;
  }

  const bool longPress = held >= board::kLongPressMs;
  if (longPress) {
    if (rowCount == 0) return;
    tbf::Row& row = rows[cursorRow];
    const bool wasDone = (doneMask & RowBit(cursorRow)) != 0 ||
                         (row.flags & tbf::kRowDone) != 0;
    const uint32_t seq = storage::LoadOpSeq() + 1;
    if (!storage::AppendOutbox(row.id, wasDone ? 0 : 1, seq)) return;
    storage::SaveOpSeq(seq);
    // Optimistic local strike, independent of the network.
    if (wasDone) {
      doneMask &= ~RowBit(cursorRow);
    } else {
      doneMask |= RowBit(cursorRow);
    }
    storage::SaveDoneMask(doneMask);
    ComposeLocal();
    PushPanel();
    // Then let the server catch up; the frame it returns is authoritative.
    SendAcks();
    Fetch(false);
    return;
  }

  if (rowCount == 0) return;
  cursorRow = static_cast<uint8_t>((cursorRow + 1) % rowCount);
  storage::SaveCursor(cursorRow);
  // Local only: moving the cursor must not need a request or touch the ETag.
  ComposeLocal();
  PushPanel();
}

void SerialCommand(const String& line) {
  String command = line;
  String rest;
  const int space = line.indexOf(' ');
  if (space >= 0) {
    command = line.substring(0, space);
    rest = line.substring(space + 1);
  }
  if (command == "wifi") {
    const int split = rest.indexOf(' ');
    if (split < 0) {
      Serial.println("usage: wifi <ssid> <password>");
      return;
    }
    cfg.wifiSsid = rest.substring(0, split);
    cfg.wifiPass = rest.substring(split + 1);
    storage::SaveConfig(cfg);
    Serial.println("wifi saved");
  } else if (command == "setup") {
    portalPending = true;
  } else if (command == "server") {
    cfg.server = rest;
    storage::SaveConfig(cfg);
    Serial.println("server saved");
  } else if (command == "token") {
    cfg.token = rest;
    storage::SaveConfig(cfg);
    Serial.println("token saved");
  } else if (command == "device") {
    cfg.device = rest;
    storage::SaveConfig(cfg);
    Serial.println("device saved");
  } else if (command == "show") {
    Serial.printf("ssid=%s server=%s device=%s token=%s\n", cfg.wifiSsid.c_str(),
                  cfg.server.c_str(), cfg.device.c_str(),
                  cfg.token.isEmpty() ? "(empty)" : "(set)");
  } else if (command == "reboot") {
    ESP.restart();
  } else {
    Serial.println("wifi/setup/server/token/device/show/reboot");
  }
}

void SerialWindow(uint32_t windowMs) {
  Serial.println();
  Serial.println("Today firmware " + String(board::kFirmwareVersion));
  Serial.println(
      "wifi <ssid> <password> | setup | server <url> | token <token> | device <id> | show | reboot");
  String line;
  const uint32_t start = millis();
  while (millis() - start < windowMs) {
    while (Serial.available()) {
      const char c = static_cast<char>(Serial.read());
      if (c == '\n') {
        SerialCommand(line);
        line = "";
      } else if (c != '\r') {
        line += c;
      }
    }
    delay(10);
  }
}

void RunPortal() {
  const uint8_t failures = wifiFailures;
  portal::Start();
  char line[24];
  snprintf(line, sizeof(line), "SSID %s", portal::Ssid().c_str());
  PaintMessage("WIFI SETUP", line, portal::Password().c_str());
  widgets::DrawString(16, 128, "http://192.168.4.1", 1);
  display::Flush();
  const uint32_t start = millis();
  while (!portal::Submitted() && millis() - start < board::kPortalIdleMs) {
    portal::Loop();
    delay(5);
  }
  portal::Stop();
  if (portal::Submitted()) {
    delay(400);
    ESP.restart();
  }
  Serial.println("portal: closed by timeout");
  wifiFailures = failures;  // do not loop straight back into the portal
  PaintMessage("NO WIFI", "KEY 3S OR 'setup' OVER SERIAL", "");
}

}  // namespace

void setup() {
  Serial.begin(115200);
  delay(50);
  storage::Begin();
  cfg = storage::LoadConfig();
  sensors::Begin();
  display::Begin();
  widgets::Begin();

  pinMode(board::kPinKey, INPUT_PULLUP);
  pinMode(board::kPinBoot, INPUT_PULLUP);
  pinMode(board::kPinBattery, INPUT);
  analogReadResolution(12);
  analogSetPinAttenuation(board::kPinBattery, ADC_11db);

  utcOffset = storage::LoadUtcOffset();
  struct tm rtc {};
  timeOk = sensors::RtcRead(&rtc);
  if (timeOk) {
    const time_t local = mktime(&rtc);
    struct timeval tv = {local, 0};
    settimeofday(&tv, nullptr);
  }
  batteryMv = static_cast<int>(BatteryMillivolts());
  ReadClimate();

  haveFrame = storage::LoadFrame(base, sizeof(base));
  rowCount = storage::LoadRows(rows, tbf::kMaxRows);
  cursorRow = storage::LoadCursor();
  doneMask = storage::LoadDoneMask();
  if (rowCount > 0 && cursorRow >= rowCount) cursorRow = rowCount - 1;
  etag = storage::LoadETag();
  if (haveFrame) {
    // A cold boot repaints the cache, so a 304 never leaves the board blank.
    Paint();
  } else {
    PaintMessage("TODAY", "WAITING FOR FIRST FRAME", "KEY 3S TO START SETUP");
  }

  if (!storage::Configured()) {
    SerialWindow(10 * 60 * 1000);
    cfg = storage::LoadConfig();
  }
}

void loop() {
  if (portalPending) {
    portalPending = false;
    RunPortal();
    return;
  }

  if (!net::Connected()) {
    const uint32_t now = millis();
    if (lastRetryAt == 0 || now - lastRetryAt >= board::kWifiRetryMs) {
      lastRetryAt = now;
      if (net::Connect(cfg, board::kWifiRetryMs)) {
        wifiFailures = 0;
        lastFrameAt = 0;
      } else {
        if (wifiFailures < 255) wifiFailures++;
        if (wifiFailures >= board::kWifiFailuresBeforePortal) {
          portalPending = true;
          return;
        }
      }
    }
  }

  const uint32_t now = millis();
  if (net::Connected() && (lastFrameAt == 0 || now - lastFrameAt >= board::kFrameIntervalMs)) {
    SendAcks();
    Fetch(false);
    MaybeUpdate(availableFirmware);
  }
  if (lastClimateAt == 0 || now - lastClimateAt >= board::kClimateIntervalMs) {
    ReadClimate();
  }

  // The clock must move even when the network is down: redraw the widgets on
  // a minute boundary. A dark panel only updates RAM.
  struct tm minuteTm {};
  if (getLocalTime(&minuteTm, 0)) {
    const int minute = minuteTm.tm_hour * 60 + minuteTm.tm_min;
    if (minute != lastClockMinute) {
      lastClockMinute = minute;
      if (haveFrame) PushPanel();
    }
  }

  // Night window: DISPOFF keeps RAM. Any key lights the board for 30 s; the
  // normal repaint path leaves the panel dark, it only refreshes RAM.
  if (!nightEnabled) {
    // The server turned night-off off (or never sent it): never stay dark.
    nightDark = false;
    if (screenOff) Paint();
  } else {
    struct tm local {};
    if (getLocalTime(&local, 0)) {
      const int minute = static_cast<int>(local.tm_hour) * 60 + local.tm_min;
      const bool wantNight = night::Inside(minute, nightStart, nightEnd);
      const bool wakeLit = millis() - lastKeyAt <= board::kNightWakeMs;
      if (!wantNight) {
        nightDark = false;
        if (screenOff) Paint();
      } else if (wakeLit) {
        // A key lit the panel: keep RAM updates visible for the 30 s window.
        nightDark = false;
      } else {
        nightDark = true;
        if (!screenOff) {
          display::Off();
          screenOff = true;
        }
      }
    }
  }

  HandleKeys();
  delay(20);
}
