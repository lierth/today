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
#include <stdio.h>

#include "board.h"
#include "display.h"
#include "fontcheck.h"
#include "net.h"
#include "night.h"
#include "portal.h"
#include "audio.h"
#include "portal_plan.h"
#include "sound_policy.h"
#include "sound_synth.h"
#include "sensors.h"
#include "screens.h"
#include "st7305.h"
#include "storage.h"
#include "wifista.h"
#include "tbf.h"
#include "version.h"

// Timing and input-edge lines are for a person with a serial cable attached,
// not for a board in a kitchen. Off by default; `pio run -e ...-debug` turns
// them on. Everything that answers "why did it open the hotspot / why did it
// reboot / did the device get its frame" stays unconditional.
#ifndef TODAY_DEBUG_LOG
#define TODAY_DEBUG_LOG 0
#endif

#if TODAY_DEBUG_LOG
#define DEBUG_LOG(...) Serial.printf(__VA_ARGS__)
#else
#define DEBUG_LOG(...) do {} while (0)
#endif
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
uint8_t portalRuns = 0;          // unattended hotspots since boot
uint32_t lastPortalEnd = 0;      // millis() of the last close, 0 = never
bool manualPortal = false;       // KEY hold latched, consumed by the loop
bool portalDrawn = false;        // the setup screen is the current panel content
bool lostBannerShown = false;    // the WIFI LOST line is on the panel
#if TODAY_DEBUG_LOG
bool portalPaintLogged = false;   // first successful portal paint was logged
#endif

// kPortalRefreshMs is how often the portal screen is repainted. The countdown
// is the only changing element; a full paint measures 27 ms on the board
// (draw 3 + pack 8 + flush 13), so one second is 2.7% of the CPU and the
// panel's own RAM never flickers.
constexpr uint32_t kPortalRefreshMs = 1000;
uint32_t lastPortalPaint = 0;

// Sound: policy from X-Sound (cached in NVS), alarm times from X-Alarms.
sound::Policy soundPolicy;
sound::Alarms alarms;
uint32_t alarmUntilMs = 0;   // a ringing alarm is showing / sounding
uint32_t alarmFlashAt = 0;
bool alarmBandOn = false;
bool alarmKeyConsumed = false;  // the stop-key must not also toggle a row


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
void PaintWifiLostOverlay();
void PaintPortalScreen();

// ResetReasonName decodes esp_reset_reason() for the boot line.
const char* ResetReasonName(esp_reset_reason_t r) {
  switch (r) {
    case ESP_RST_POWERON: return "power-on";
    case ESP_RST_EXT: return "external";
    case ESP_RST_SW: return "software";
    case ESP_RST_PANIC: return "panic";
    case ESP_RST_INT_WDT: return "int-wdt";
    case ESP_RST_TASK_WDT: return "task-wdt";
    case ESP_RST_WDT: return "wdt";
    case ESP_RST_DEEPSLEEP: return "deep-sleep";
    case ESP_RST_BROWNOUT: return "brownout";
    case ESP_RST_SDIO: return "sdio";
    case ESP_RST_USB: return "usb-uart-reset";
    case ESP_RST_JTAG: return "jtag";
    case ESP_RST_EFUSE: return "efuse";
    case ESP_RST_PWR_GLITCH: return "power-glitch";
    case ESP_RST_CPU_LOCKUP: return "cpu-lockup";
    default: return "unknown";
  }
}
void PlayCue(sound::Cue cue);
void PaintAlarmBanner();

// ReplaceAlarms installs a new list but keeps the "already rang" marks for
// timestamps that survive, so a 200 arriving right after a chime cannot make
// it ring twice.
void ReplaceAlarms(const String& text) {
  int64_t next[sound::kMaxAlarms];
  const uint8_t count = sound::ParseAlarms(text.c_str(), next, sound::kMaxAlarms);
  sound::Alarms merged;
  merged.count = count;
  for (uint8_t i = 0; i < count; i++) {
    merged.at[i] = next[i];
    for (uint8_t j = 0; j < alarms.count; j++) {
      if (alarms.at[j] == next[i]) {
        merged.fired[i] = alarms.fired[j];
        break;
      }
    }
  }
  alarms = merged;
}

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
  if (lostBannerShown) PaintWifiLostOverlay();
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
    // "--" only while the first attempt is still in flight; once a retry has
    // failed the widget shows the disconnected mark, not a forever-spinner.
    widgets::DrawWifi(*w, net::Rssi(), !net::Connected() && wifiFailures == 0,
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
  lostBannerShown = false;
  ComposeLocal();
  PushPanel();
}

// ShowScreensBitmap pushes one screens.cpp bitmap. Those are row-major with
// 1 = white; the TBF the panel packer takes is row-major with 1 = black, so
// the bits are flipped first. `bmp` is scratch (work); Paint() recomposes it.
void ShowScreensBitmap(uint8_t* bmp) {
  for (size_t i = 0; i < sizeof(work); i++) bmp[i] = static_cast<uint8_t>(~bmp[i]);
  display::ShowFrame(bmp);
}

void PaintMessage(const char* line1, const char* line2, const char* line3) {
  // All of the board's own messages share one template: header band, a title
  // at scale 3, steps at scale 2, a hint at the bottom (screens.cpp).
  const screens::Text text{line1, line2, line3, "", ""};
  screens::DrawTemplate(work, text, false);
  ShowScreensBitmap(work);
  display::Flush();
  display::On();
  nightDark = false;
  screenOff = false;
  // The cached frame stays in `base`: a later 304 repaints the board instead
  // of leaving this message on screen forever.
}

// PaintBoot is the first screen after reset: the app icon at half size and
// the firmware version, so someone at the desk can tell what is running.
void PaintBoot() {
  screens::DrawBoot(work, board::kFirmwareVersion);
  ShowScreensBitmap(work);
  display::Flush();
  display::On();
  nightDark = false;
  screenOff = false;
}

// The setup hotspot needs its credentials readable from a metre away: the
// password is the one value a user copies by hand, so it is drawn at the
// largest scale that still fits (11 chars incl. the display space at 5x is
// 330 px on a 400 px panel).
void DrawPortalScreen() {
  screens::DrawPortal(work, portal::Ssid().c_str(), portal::PasswordDisplay().c_str(),
                      "http://192.168.4.1");
  ShowScreensBitmap(work);
  display::Flush();
  display::On();
  nightDark = false;
  screenOff = false;
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
  // The menu and the settings card show these; omit them rather than send a
  // misleading 0 when the sensor read failed.
  if (climateOk) {
    doc["temp_c"] = temperatureC;
    doc["humidity"] = humidity;
  }
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
  // Only a strictly newer version upgrades: the server advertises the same
  // version on every frame, and an equality test would reboot the board on
  // every boot when the two strings disagree.
  if (version.isEmpty() || !version::Newer(version.c_str(), board::kFirmwareVersion)) return;
  Serial.printf("ota: %s -> %s\n", board::kFirmwareVersion, version.c_str());
  PaintMessage("UPDATING", "1. KEEP THE POWER ON", "2. THE BOARD REBOOTS BY ITSELF");
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
  // Reminders and sound settings change independently of the bitmap; they
  // ride in headers on both 200 and 304, so this runs on every response.
  if (res.status == 200 || !res.alarms.isEmpty()) {
    storage::SaveAlarms(res.alarms);
    ReplaceAlarms(res.alarms);
  }
  if (!res.sound.isEmpty()) {
    storage::SaveSound(res.sound);
    sound::ParsePolicy(res.sound.c_str(), &soundPolicy);
  }
  if (res.soundPreview) PlayCue(sound::Cue::kCheck);
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

// PlayCue renders and queues one cue. Volume 0 or a busy output drops it.
void PlayCue(sound::Cue cue) {
  size_t cap = 0;
  int16_t* buf = audio::Scratch(&cap);
  if (buf == nullptr || cap == 0) return;
  const size_t n = sound::Render(cue, soundPolicy.volume, buf, cap);
  if (n > 0) audio::Play(buf, n);
}

// RingAlarm shows the reminder as an inverted header band and plays the chime.
// The band flashes for 30 s or until a key is pressed; the sound itself is
// three chimes and ends on its own.
void RingAlarm() {
  alarmUntilMs = millis() + 30000;
  alarmFlashAt = 0;
  alarmBandOn = true;
  alarmKeyConsumed = false;
  PlayCue(sound::Cue::kAlarm);
  PaintAlarmBanner();
}

// PaintAlarmBanner draws the inverted band under the header line, the same
// style the WIFI LOST banner uses, so a reminder never disturbs the row list.
void PaintAlarmBanner() {
  if (!haveFrame) return;
  display::ShowFrame(work);
  DrawWidgets();
  const int y = 44;  // frame.HeaderHeight: the band sits right under the date
  // The text is ASCII on purpose: the device font has no CJK, so the reminder
  // reads as a time band rather than the item's Chinese title. Same strip as
  // the lost-Wi-Fi banner, inverted.
  widgets::DrawBanner(y, board::kRowHeight, "ALARM - PRESS ANY KEY", 1, true);
  if (lostBannerShown) PaintWifiLostOverlay();
  display::Flush();
  if (!nightDark) display::On();
}

void HandleKeys() {
  // The panel shows the hotspot password: a cursor move or a toggle would
  // repaint over it. The timeout (or a submitted form) ends the portal.
  if (portal::Active()) return;
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
    manualPortal = true;
    return;
  }

  // A ringing alarm: this press only silences it, exactly like the night
  // window's first press only lights the panel.
  if (alarmUntilMs != 0) {
    alarmUntilMs = 0;
    alarmBandOn = false;
    audio::Silence();
    Paint();
    lastKeyAt = millis();
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
    int done = 0;
    for (uint8_t i = 0; i < rowCount; i++) {
      const bool d = (doneMask & RowBit(i)) != 0 || (rows[i].flags & tbf::kRowDone) != 0;
      if (d) done++;
    }
    const int cue = sound::KeyCue(soundPolicy.keys, soundPolicy.volume, !wasDone, done, rowCount);
    if (cue != static_cast<int>(sound::Cue::kNone)) PlayCue(static_cast<sound::Cue>(cue));
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

// SerialWindow returns why it stopped: Timeout means nobody configured the
// board over serial, Portal means the user asked for the setup hotspot, Ready
// means the stored credentials are now complete and boot can continue.
enum class WindowExit { Timeout, Portal, Ready };

WindowExit SerialCommand(const String& line) {
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
      return WindowExit::Timeout;
    }
    cfg.wifiSsid = rest.substring(0, split);
    cfg.wifiPass = rest.substring(split + 1);
    storage::SaveConfig(cfg);
    Serial.println("wifi saved");
    // A board that already had server+token is now fully configured: stop
    // waiting for the rest of the window and go online.
    if (storage::Configured()) {
      Serial.println("configured, leaving setup window");
      return WindowExit::Ready;
    }
  } else if (command == "setup") {
    Serial.println("setup requested");
    return WindowExit::Portal;
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
  return WindowExit::Timeout;
}

WindowExit SerialWindow(uint32_t windowMs) {
  Serial.println();
  Serial.println("Today firmware " + String(board::kFirmwareVersion));
  Serial.println(
      "wifi <ssid> <password> | setup | server <url> | token <token> | device <id> | show | reboot");
  Serial.println("hold KEY 3s to open the setup hotspot");
  String line;
  const uint32_t start = millis();
  bool keyWasDown = false;
  bool bootWasDown = false;
  uint32_t keyDownAt = 0;
  // The station runs here too. A board with Wi-Fi but no server/token spends
  // this window waiting for a person, and an earlier version simply blocked:
  // the radio never got a begin(), so the serial log showed no connection
  // attempt at all and the saved password was never tested.
  uint32_t lastStaAt = 0;
  int lastStaStatus = -1;
  Serial.printf("keys: KEY(GPIO%d)=%d BOOT(GPIO%d)=%d (0 = pressed)\n", board::kPinKey,
                digitalRead(board::kPinKey), board::kPinBoot, digitalRead(board::kPinBoot));
  while (millis() - start < windowMs) {
    if (!cfg.wifiSsid.isEmpty() && (lastStaAt == 0 || millis() - lastStaAt >= board::kWifiRetryMs)) {
      lastStaAt = millis();
      net::EnsureSta(cfg);
    }
    if (!cfg.wifiSsid.isEmpty()) {
      const int status = WiFi.status();
      if (status != lastStaStatus) {
        lastStaStatus = status;
        char want[wifista::kBannerCap];
        const uint8_t reason = net::LastDisconnectReason();
        if (reason != 0) wifista::ReasonText(reason, want, sizeof(want));
        Serial.printf("wifi: status %d%s%s%s\n", status, status == WL_CONNECTED ? " (connected)" : "",
                      reason != 0 ? ", last reason: " : "", reason != 0 ? want : "");
      }
      if (status == WL_CONNECTED && lastStaAt != 0 && WiFi.localIP() != IPAddress(0, 0, 0, 0)) {
        Serial.printf("wifi: got ip %s rssi %d\n", WiFi.localIP().toString().c_str(), WiFi.RSSI());
        lastStaAt = 0;  // announce once
        lastStaStatus = -2;
      }
    }
    // The KEY gesture has to work here too: a board with no credentials has
    // no other way into the SoftAP portal, and this window runs before loop().
    const bool keyDown = digitalRead(board::kPinKey) == board::kButtonActiveLevel;
    const bool bootDown = digitalRead(board::kPinBoot) == board::kButtonActiveLevel;
    // Edge log: the one way to tell "not pressed" from "pressed but not read".
    if (keyDown != keyWasDown) DEBUG_LOG("keys: KEY %s\n", keyDown ? "down" : "up");
    if (bootDown != bootWasDown) DEBUG_LOG("keys: BOOT %s\n", bootDown ? "down" : "up");
    bootWasDown = bootDown;
    if (keyDown && !keyWasDown) keyDownAt = millis();
    if (keyDown && millis() - keyDownAt >= board::kProvisionHoldMs) {
      DEBUG_LOG("[%lu] setup: KEY held, opening hotspot\n", millis());
      return WindowExit::Portal;
    }
    keyWasDown = keyDown;

    while (Serial.available()) {
      const char c = static_cast<char>(Serial.read());
      if (c == '\n') {
        const WindowExit exit = SerialCommand(line);
        line = "";
        if (exit != WindowExit::Timeout) return exit;
      } else if (c != '\r') {
        line += c;
      }
    }
    delay(10);
  }
  return WindowExit::Timeout;
}

// The hotspot runs inside loop(); nothing here waits. A submitted form still
// reboots after a short pause so the browser gets its "saved" reply.
void StartPortal(bool manual) {
#if TODAY_DEBUG_LOG
  const uint32_t t0 = millis();
#endif
  // Show the credentials before the radio exists. The AP takes ~600 ms to come
  // up and the HTTP/DNS tick is not preemptible; painting after Start() meant
  // the panel kept the old screen for as long as any of that took, which is
  // what "hold KEY and nothing happens for a while" looked like.
  portal::Prepare();
#if TODAY_DEBUG_LOG
  const uint32_t t1 = millis();
#endif
  PaintPortalScreen();
#if TODAY_DEBUG_LOG
  const uint32_t t2 = millis();
#endif
  if (manual) {
    portal::StartUserOpened();
  } else {
    portal::Start();
  }
#if TODAY_DEBUG_LOG
  const uint32_t t3 = millis();
#endif
  portalRuns++;
  // The screen is already up; the next tick refreshes the countdown.
  portalDrawn = true;
  lastPortalPaint = millis();
  DEBUG_LOG("[%lu] portal: open (%s) prepare=%lu paint=%lu radio=%lu ms\n", millis(),
            manual ? "manual" : "auto", static_cast<unsigned long>(t1 - t0),
            static_cast<unsigned long>(t2 - t1), static_cast<unsigned long>(t3 - t2));
}

void StopPortal(const char* why) {
  if (portal::Submitted()) {
    Serial.printf("portal: submitted %s\n", portal::SubmittedSsid().c_str());
    delay(400);
    ESP.restart();
  }
  const bool userOpened = portal::UserOpened();
  portal::Stop();
  lastPortalEnd = millis();
  lastPortalPaint = 0;
  Serial.printf("portal: closed (%s)\n", why);
  if (userOpened) {
    // A person opened this and walked away; surface the way back in.
    PaintMessage("SETUP ENDED", "1. HOLD KEY FOR 3 SECONDS", "2. THE HOTSPOT REOPENS");
    return;
  }
  // Paint() draws nothing without a frame, so a board that never fetched one
  // (the common case for the automatic hotspot) would keep the hotspot's
  // countdown frozen on the panel after the door closed. Say where we are
  // instead of leaving a stale screen.
  if (!haveFrame) {
    PaintMessage("WAITING", "1. THE BOARD POLLS EVERY MINUTE", "2. KEY 3S TO SET UP WI-FI");
    return;
  }
  Paint();
}

// WIFI LOST rides over the second header line: rows stay visible and the keys
// keep working, only the lunar line is borrowed. Draws into the panel buffer;
// the caller flushes.
void PaintWifiLostOverlay() {
  // The band replaces the lunar line, not a row: every item stays visible.
  // 33 chars at scale 2 is 396 px, which fits the strip's 8 px padding on
  // both sides only just; DrawBanner does not clip, so wifista::Banner keeps
  // every detail it can produce inside that width. Naming the reason is the
  // difference between "it is broken" and "the password is wrong".
  char detail[wifista::kBannerCap];
  wifista::ReasonText(net::LastDisconnectReason(), detail, sizeof(detail));
  char line[wifista::kBannerCap];
  if (net::LastDisconnectReason() == 0) {
    wifista::Banner("", line, sizeof(line));
  } else {
    wifista::Banner(detail, line, sizeof(line));
  }
  widgets::DrawBanner(20, 24, line, 2, false);
}

void PaintWifiLost() {
  if (!haveFrame) return;
  display::ShowFrame(work);
  DrawWidgets();
  PaintWifiLostOverlay();
  display::Flush();
  if (!nightDark) display::On();
}

// ServicePortal runs one tick of an open hotspot and reports whether it
// handled the tick. Every caller that would otherwise return must try this
// first: the hotspot is a background service, and both the HTTP/DNS handlers
// and the screen live here.
bool ServicePortal() {
  if (!portal::Active()) return false;
  portal::Loop();
  // The countdown has to tick visibly; the measured full paint is 27 ms, so a
  // one-second cadence costs little and the panel never looks frozen. (The
  // reset reason was never a brownout, so there is no power case for slowing
  // it down.)
  if (!portalDrawn || millis() - lastPortalPaint >= kPortalRefreshMs) PaintPortalScreen();
  HandleKeys();  // a no-op while the portal is up
  delay(20);
  return true;
}

// Panel content for a hotspot that is up: the credentials, plus a countdown in
// the bottom right corner so a person who walks away knows how long the door
// stays open.
void PaintPortalScreen() {
#if TODAY_DEBUG_LOG
  const uint32_t t0 = millis();
#endif
  char left[8];
  portalplan::CountdownText(board::kPortalIdleMs - portal::IdleMs(), left, sizeof(left));
  // Draw the countdown into `work` before the bitmap is pushed: the previous
  // version flushed the panel from DrawPortalScreen and then drew one string
  // into the panel buffer, so every second cost two 15 KB SPI bursts and the
  // string sat outside the packed image. One bitmap, one flush.
  screens::DrawPortal(work, portal::Ssid().c_str(), portal::PasswordDisplay().c_str(),
                       "http://192.168.4.1");
  const int w = widgets::MeasureWidth(left, 2);
  screens::DrawString(work, static_cast<int>(screens::kWidth) - 16 - w,
                      static_cast<int>(screens::kHeight) - 30, left, 2);
#if TODAY_DEBUG_LOG
  const uint32_t t1 = millis();
#endif
  ShowScreensBitmap(work);
#if TODAY_DEBUG_LOG
  const uint32_t t2 = millis();
#endif
  display::Flush();
#if TODAY_DEBUG_LOG
  const uint32_t t3 = millis();
  if (!portalPaintLogged) {
    DEBUG_LOG("[%lu] portal: draw=%lu pack=%lu flush=%lu ms\n", millis(),
              static_cast<unsigned long>(t1 - t0), static_cast<unsigned long>(t2 - t1),
              static_cast<unsigned long>(t3 - t2));
  }
#endif
  // DISPON only when the panel is off; repeating it every second restarts the
  // controller's refresh for no reason.
  if (display::IsOff()) display::On();
  portalDrawn = true;
  lastPortalPaint = millis();
  nightDark = false;
  screenOff = false;
#if TODAY_DEBUG_LOG
  if (!portalPaintLogged) {
    portalPaintLogged = true;
    int ink = 0;
    for (size_t i = 0; i < st7305::kPackedBytes; i++) {
      ink += 8 - __builtin_popcount(display::Buffer()[i]);
    }
    DEBUG_LOG("[%lu] portal: painted screen, ink=%d px\n", millis(), ink);
  }
#endif
}

}  // namespace

void setup() {
  Serial.begin(115200);
  delay(50);
  // The ROM line already names the reset; this is the SDK's own view, and it
  // is the only way to tell a brownout from a USB re-enumeration in the field.
  Serial.printf("reset: reason=%d (%s)\n", static_cast<int>(esp_reset_reason()),
                ResetReasonName(esp_reset_reason()));
  storage::Begin();
  cfg = storage::LoadConfig();
  sensors::Begin();
  display::Begin();
  // Audio comes after I2C (the codec shares the bus). A missing codec is not
  // fatal: the board simply stays silent.
  audio::Begin();
  sound::ParsePolicy(storage::LoadSound().c_str(), &soundPolicy);
  ReplaceAlarms(storage::LoadAlarms());
  widgets::Begin();
  // A glyph that is missing from the font renders as "?" on the panel; make
  // that a loud startup error rather than a silent one.
  if (const char* missing = fontcheck::Missing()) {
    Serial.printf("font: no glyph for \"%s\"\n", missing);
  }

  pinMode(board::kPinKey, INPUT_PULLUP);
  pinMode(board::kPinBoot, INPUT_PULLUP);
  pinMode(board::kPinBattery, INPUT);
  // analogSetPinAttenuation only reconfigures a pin that is already attached
  // to the ADC; calling it first logs "Pin is not configured as analog
  // channel" and leaves the default attenuation. One read attaches it.
  analogReadResolution(12);
  (void)analogReadMilliVolts(board::kPinBattery);
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
  // Boot line for the field: with 11 dB attenuation a 1:3 divider on a 4.2 V
  // cell reads ~0.7 V at the pin, i.e. ~1400-2800 raw counts on 12 bit.
  Serial.printf("battery: raw=%u mv=%u (atten 11dB)\n", analogRead(board::kPinBattery),
                analogReadMilliVolts(board::kPinBattery));
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
    // First boot: logo + version, then the hint that a frame is coming.
    PaintBoot();
    delay(1200);
    PaintMessage("WAITING", "1. THE BOARD POLLS EVERY MINUTE", "2. KEY 3S TO SET UP WI-FI");
  }

  if (!storage::Configured() && !cfg.wifiSsid.isEmpty()) {
    // The network is set up; the account is not. The board cannot fetch a
    // frame until the Mac hands it a server address and token, so say that
    // instead of the "polls every minute" line, which would be a lie.
    Serial.printf("setup: wifi saved (%s), no server/token yet - add the device in the Mac app\n",
                  cfg.wifiSsid.c_str());
    const String step = String("2. ADD DEVICE ") + cfg.device;
    PaintMessage("ADD THIS DEVICE", "1. OPEN TODAY ON YOUR MAC", step.c_str());
  }

  if (!storage::Configured()) {
    const WindowExit exit = SerialWindow(10 * 60 * 1000);
    cfg = storage::LoadConfig();
    switch (exit) {
      case WindowExit::Ready:
        break;  // credentials complete: fall through to loop()
      case WindowExit::Portal:
        // Raise the hotspot now; loop() keeps it running without blocking.
        StartPortal(true);
        break;
      case WindowExit::Timeout:
        PaintMessage("NO WI-FI", "1. HOLD KEY FOR 3 SECONDS", "2. OR TYPE setup OVER SERIAL");
        break;
    }
  }
}

void loop() {
  // A hotspot is a background service now: the STA keeps retrying beside it,
  // the keys keep working and the frame path resumes the moment the old
  // network is back.
  if (!portal::Active()) {
    const uint32_t now = millis();
    portalplan::Input in{};
    in.now = now;
    in.lastPortalEnd = lastPortalEnd;
    in.portalRuns = portalRuns;
    in.connected = net::Connected();
    in.portalOpen = false;
    in.manualRequest = manualPortal;
    in.haveCredentials = !cfg.wifiSsid.isEmpty();
    in.failures = wifiFailures;
    in.threshold = board::kWifiFailuresBeforePortal;
    if (portalplan::ShouldOpen(in)) {
      manualPortal = false;
      StartPortal(in.manualRequest);
    }
  } else {
    manualPortal = false;  // already open: the hold already had its effect
    if (portal::Submitted()) StopPortal("submitted");
    if (!portal::Active()) return;
    if (portal::IdleMs() >= board::kPortalIdleMs) {
      StopPortal("idle timeout");
      return;
    }
    // The old network came back while the hotspot was up. A portal the person
    // opened stays until they finish; an automatic one yields immediately.
    if (portalplan::CloseOnConnect(portal::Active(), net::Connected()) &&
        !portal::UserOpened()) {
      StopPortal("link restored");
      wifiFailures = 0;
      lastFrameAt = 0;
      return;
    }
  }

  // A board without Wi-Fi/token cannot fetch anything and must not spin the
  // retry path. HandleKeys() still runs, so KEY 3 s opens the portal.
  // No credentials: the network path below has nothing to do. That is exactly
  // the board that needs the setup portal, so an open hotspot is serviced
  // here instead of falling through to a return that skips it.
  if (cfg.wifiSsid.isEmpty()) {
    if (ServicePortal()) return;
    HandleKeys();
    delay(20);
    return;
  }

  // Non-blocking reconnect: EnsureSta() only kicks the driver, so this branch
  // costs a few microseconds and the keys stay live on every tick.
  if (!net::Connected()) {
    const uint32_t now = millis();
    if (lastRetryAt == 0 || now - lastRetryAt >= board::kWifiRetryMs) {
      lastRetryAt = now;
      net::EnsureSta(cfg);
      if (wifiFailures < 255) wifiFailures++;
    }
    if (wifiFailures == board::kWifiFailuresBeforePortal && !lostBannerShown &&
        !portal::Active()) {
      lostBannerShown = true;
      if (haveFrame) {
        // The banner rides above the row list so the list stays usable; it is
        // painted once, and Paint() clears it when the link returns.
        PaintWifiLost();
      } else {
        // Nothing cached yet: say what failed instead of sitting on WAITING
        // forever. The reason is the only actionable part of the message.
        char detail[wifista::kBannerCap];
        const uint8_t reason = net::LastDisconnectReason();
        if (reason != 0) {
          wifista::ReasonText(reason, detail, sizeof(detail));
        } else {
          detail[0] = '\0';
        }
        char step[wifista::kBannerCap];
        snprintf(step, sizeof(step), "1. %s", detail[0] ? detail : "CHECK THE ROUTER");
        PaintMessage("NO WI-FI", step, "2. HOLD KEY 3 SECONDS TO SET UP");
      }
    }
  } else if (wifiFailures > 0) {
    wifiFailures = 0;
    lastFrameAt = 0;
    net::ForgetDisconnectReason();
    if (lostBannerShown) {
      lostBannerShown = false;
      Paint();
    }
  }

  // Reminders: local clock, cached list, no network needed. The check is
  // cheap and runs every tick so an alarm fires within ~20 ms of its second.
  audio::Loop();
  if (alarmUntilMs != 0 && static_cast<int32_t>(millis() - alarmUntilMs) >= 0) {
    alarmUntilMs = 0;
    alarmBandOn = false;
    Paint();
  } else if (alarmUntilMs != 0 && millis() - alarmFlashAt >= 500) {
    // Blink the band while the reminder is up; the same inverted strip as the
    // Wi-Fi banner, so the two never look like different things.
    alarmFlashAt = millis();
    alarmBandOn = !alarmBandOn;
    if (alarmBandOn) {
      PaintAlarmBanner();
    } else {
      PushPanel();
    }
  }
  {
    const time_t now = time(nullptr);
    if (now > 1600000000) {  // RTC or server time is valid
      struct tm local {};
      const bool haveTime = getLocalTime(&local, 0);
      const bool nightNow = nightEnabled &&
                            night::Inside(local.tm_hour * 60 + local.tm_min, nightStart, nightEnd);
      const int due = sound::ShouldRing(static_cast<int64_t>(now), alarms, soundPolicy.alarm,
                                        soundPolicy.volume, haveTime && nightNow, 60);
      if (due >= 0) {
        alarms.fired[due] = true;
        RingAlarm();
      }
    }
  }

  // The hotspot is serviced on every tick, never in its own blocking loop.
  if (ServicePortal()) return;

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
