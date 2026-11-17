// Today e-paper firmware. State machine:
//   deep sleep -> button wake (local cursor/outbox, no Wi-Fi) -> sleep 60 s
//              -> timer wake (Wi-Fi, fetch frame, send acks) -> sleep until
//                 the server's X-Next-Wake-At.
//
// The wire format is defined by internal/frame/frame.go; do not change one
// side without the other.

#include <Arduino.h>
#include <ArduinoJson.h>
#include <SPI.h>
#include <WiFi.h>
#include <esp_sleep.h>

#include "board.h"
#include "net.h"
#include "storage.h"
#include "tbf.h"

namespace {

board::Panel display(GxEPD2_420(board::kPinCs, board::kPinDC, board::kPinRst, board::kPinBusy));

storage::Config cfg;
String availableFirmware;
tbf::Row rows[tbf::kMaxRows];
uint8_t rowCount = 0;
uint8_t cursorRow = 0;
uint32_t doneMask = 0;
uint32_t frameSeq = 0;
bool haveFrame = false;

uint8_t baseBmp[board::kBitmapBytes];
uint8_t workBmp[board::kBitmapBytes];
uint8_t frameBuf[16800];

uint32_t sleepSeconds = board::kFallbackSleepSeconds;

void DeepSleep(uint32_t seconds);

uint32_t BatteryMillivolts() {
  pinMode(board::kPinBattery, INPUT);
  const uint32_t mv = analogReadMilliVolts(board::kPinBattery);
  return mv * board::kBatteryScale;
}

void InitDisplay(bool fullInit) {
  display.init(115200, fullInit, 2, false);
  display.setRotation(0);
  display.setTextColor(GxEPD_BLACK);
}

void DrawMessage(const String& title, const String& line1 = "", const String& line2 = "") {
  display.setFullWindow();
  display.firstPage();
  do {
    display.fillScreen(GxEPD_WHITE);
    display.setTextColor(GxEPD_BLACK);
    display.setTextSize(2);
    display.setCursor(20, 46);
    display.print(title);
    display.setTextSize(1);
    display.setCursor(20, 92);
    display.print(line1);
    display.setCursor(20, 112);
    display.print(line2);
  } while (display.nextPage());
}

void SetPixel(uint8_t* bmp, int x, int y, bool black) {
  if (x < 0 || y < 0 || x >= board::kWidth || y >= board::kHeight) return;
  const size_t index = static_cast<size_t>(y) * (board::kWidth / 8) + (x >> 3);
  const uint8_t mask = 0x80 >> (x & 7);
  if (black) {
    bmp[index] |= mask;
  } else {
    bmp[index] &= static_cast<uint8_t>(~mask);
  }
}

void InvertRow(uint8_t* bmp, int y, int height) {
  for (int row = y; row < y + height; row++) {
    for (int x = 0; x < board::kWidth; x++) {
      const size_t index = static_cast<size_t>(row) * (board::kWidth / 8) + (x >> 3);
      bmp[index] ^= static_cast<uint8_t>(0x80 >> (x & 7));
    }
  }
}

void StrikeRow(uint8_t* bmp, int y, int height) {
  const int lineY = y + height - 7;
  for (int x = 24; x < board::kWidth - 16; x++) SetPixel(bmp, x, lineY, true);
}

void ComposeRow(int index) {
  if (index < 0 || index >= rowCount) return;
  const int y = rows[index].y;
  const size_t offset = static_cast<size_t>(y) * (board::kWidth / 8);
  const size_t bytes = static_cast<size_t>(board::kRowHeight) * (board::kWidth / 8);
  memcpy(workBmp + offset, baseBmp + offset, bytes);
  if (doneMask & (1UL << index)) StrikeRow(workBmp, y, board::kRowHeight);
  if (index == cursorRow) InvertRow(workBmp, y, board::kRowHeight);
}

void DrawFull() {
  if (!haveFrame) {
    DrawMessage("Today", "还没有缓存画面", "连上 Wi-Fi 后自动取帧");
    return;
  }
  memcpy(workBmp, baseBmp, sizeof(workBmp));
  for (int i = 0; i < rowCount; i++) {
    const int y = rows[i].y;
    if (doneMask & (1UL << i)) StrikeRow(workBmp, y, board::kRowHeight);
    if (i == cursorRow) InvertRow(workBmp, y, board::kRowHeight);
  }
  display.setFullWindow();
  display.firstPage();
  do {
    display.fillScreen(GxEPD_WHITE);
    display.drawBitmap(0, 0, workBmp, board::kWidth, board::kHeight, GxEPD_BLACK);
  } while (display.nextPage());
}

void ShowRow(int index) {
  if (index < 0 || index >= rowCount) return;
  const int y = rows[index].y;
  ComposeRow(index);
  const size_t offset = static_cast<size_t>(y) * (board::kWidth / 8);
  // drawBitmap leaves zero bits untouched, so clear the row window first;
  // otherwise moving the cursor would leave the old inversion behind.
  display.fillRect(0, y, board::kWidth, board::kRowHeight, GxEPD_WHITE);
  display.drawBitmap(0, y, workBmp + offset, board::kWidth, board::kRowHeight, GxEPD_BLACK);
  display.displayWindow(0, y, board::kWidth, board::kRowHeight);
}

const char* StatusName(uint8_t status) { return status ? "done" : "open"; }

void ToggleCursor() {
  if (cursorRow >= rowCount) return;
  const uint32_t bit = 1UL << cursorRow;
  const bool wasDone = (doneMask & bit) != 0;
  const uint8_t status = wasDone ? 0 : 1;
  const uint32_t opSeq = storage::LoadOpSeq() + 1;
  if (!storage::AppendOutbox(rows[cursorRow].id, status, opSeq)) {
    DrawMessage("Today", "本地队列已满", "下一次联网会先发送");
    return;
  }
  storage::SaveOpSeq(opSeq);
  doneMask ^= bit;
  storage::SaveDoneMask(doneMask);
  ShowRow(cursorRow);
}

int PressedButton() {
  if (digitalRead(board::kPinOk) == LOW) return board::kPinOk;
  if (digitalRead(board::kPinRefresh) == LOW) return board::kPinRefresh;
  if (digitalRead(board::kPinUp) == LOW) return board::kPinUp;
  if (digitalRead(board::kPinDown) == LOW) return board::kPinDown;
  return -1;
}

void WaitRelease(int pin, uint32_t limitMs) {
  const uint32_t start = millis();
  while (digitalRead(pin) == LOW && millis() - start < limitMs) delay(10);
}

void MoveCursor(int delta) {
  if (rowCount == 0) return;
  const int old = cursorRow;
  int next = static_cast<int>(cursorRow) + delta;
  if (next < 0) next = rowCount - 1;
  if (next >= rowCount) next = 0;
  cursorRow = static_cast<uint8_t>(next);
  storage::SaveCursor(cursorRow);
  ShowRow(old);
  ShowRow(cursorRow);
}

void HandleButtons() {
  pinMode(board::kPinUp, INPUT_PULLUP);
  pinMode(board::kPinDown, INPUT_PULLUP);
  pinMode(board::kPinOk, INPUT_PULLUP);
  pinMode(board::kPinRefresh, INPUT_PULLUP);
  delay(30);

  const int button = PressedButton();
  if (button < 0) return;

  if (button == board::kPinOk) {
    // A long press is a request for a full refresh plus a forced full frame
    // on the next fetch; a short press toggles the cursor row.
    const uint32_t start = millis();
    while (digitalRead(button) == LOW && millis() - start < 700) delay(10);
    if (digitalRead(button) == LOW) {
      WaitRelease(button, 4000);
      storage::SetForceFull(true);
      DrawFull();
      return;
    }
    ToggleCursor();
    return;
  }

  if (button == board::kPinRefresh) {
    WaitRelease(button, 2000);
    storage::SetForceFull(true);
    DrawFull();
    return;
  }

  const int delta = button == board::kPinUp ? -1 : 1;
  WaitRelease(button, 2000);
  MoveCursor(delta);
}

String BuildAcksJson(uint32_t batteryMv) {
  JsonDocument doc;
  doc["frame_seq"] = frameSeq;
  doc["battery_mv"] = batteryMv;
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
    entry["status"] = StatusName(op.status);
  }
  String out;
  serializeJson(doc, out);
  return out;
}

void MaybeUpdate(const String& version) {
  if (version.isEmpty() || version == board::kFirmwareVersion) return;
  Serial.printf("ota: %s -> %s\n", board::kFirmwareVersion, version.c_str());
  DrawMessage("正在升级固件", version, "请保持供电");
  if (net::DownloadFirmware(cfg)) {
    Serial.println("ota: ok, rebooting");
    delay(800);
    ESP.restart();
  }
  Serial.println("ota: failed, keeping current firmware");
}

void SendAcks() {
  const uint8_t count = storage::OutboxCount();
  const String payload = BuildAcksJson(BatteryMillivolts());
  const int code = net::PostAcks(cfg, payload);
  if (code >= 200 && code < 300) {
    storage::ClearOutbox();
    Serial.printf("acks: sent %u\n", count);
  } else {
    Serial.printf("acks: failed %d, kept %u\n", code, count);
  }
}

void FetchFrame() {
  const String etag = storage::LoadETag();
  tbf::Frame frame{};
  const net::FetchResult result =
      net::FetchFrame(cfg, etag, frameBuf, sizeof(frameBuf), &frame);
  if (result.sleepSeconds > 0) {
    sleepSeconds = static_cast<uint32_t>(result.sleepSeconds);
  }
  if (result.status == 304) {
    if (result.seq != 0) {
      frameSeq = result.seq;
      storage::SaveSeq(frameSeq);
    }
    if (result.refresh && haveFrame) DrawFull();
    availableFirmware = result.firmwareVersion;
    return;
  }
  if (!result.changed) {
    Serial.printf("frame: http %d %s\n", result.status, tbf::ParseError());
    if (!haveFrame) DrawMessage("Today", "取帧失败", String("HTTP ") + String(result.status));
    sleepSeconds = 300;
    return;
  }
  if (frame.width != board::kWidth || frame.height != board::kHeight ||
      frame.bitmapLen != sizeof(baseBmp)) {
    Serial.println("frame: panel geometry mismatch");
    DrawMessage("Today", "帧尺寸不匹配", "检查 devices.panel 与固件");
    sleepSeconds = 600;
    return;
  }

  memcpy(baseBmp, frame.bitmap, sizeof(baseBmp));
  haveFrame = true;
  storage::SaveFrame(baseBmp, sizeof(baseBmp));

  rowCount = frame.rowCount > tbf::kMaxRows ? tbf::kMaxRows : frame.rowCount;
  memcpy(rows, frame.rows, sizeof(tbf::Row) * rowCount);
  storage::SaveRows(rows, rowCount);

  frameSeq = result.seq;
  storage::SaveSeq(frameSeq);
  if (!result.etag.isEmpty()) storage::SaveETag(result.etag);

  doneMask = 0;
  for (uint8_t i = 0; i < rowCount; i++) {
    if (frame.rows[i].flags & tbf::kRowDone) doneMask |= 1UL << i;
  }
  storage::SaveDoneMask(doneMask);
  if (rowCount > 0 && cursorRow >= rowCount) cursorRow = rowCount - 1;
  storage::SaveCursor(cursorRow);

  DrawFull();
  Serial.printf("frame: seq %lu rows %u\n", static_cast<unsigned long>(frameSeq), rowCount);
  availableFirmware = result.firmwareVersion;
}

void SerialCommand(String line) {
  line.trim();
  if (line.isEmpty()) return;
  const int space = line.indexOf(' ');
  String command = space < 0 ? line : line.substring(0, space);
  String rest = space < 0 ? "" : line.substring(space + 1);
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
  } else if (command == "mode") {
    if (rest != "battery" && rest != "mains") {
      Serial.println("usage: mode battery|mains");
      return;
    }
    cfg.mode = rest;
    storage::SaveConfig(cfg);
    Serial.println("mode saved; reboot to apply");
  } else if (command == "show") {
    Serial.printf("ssid=%s server=%s device=%s token=%s mode=%s\n", cfg.wifiSsid.c_str(),
                  cfg.server.c_str(), cfg.device.c_str(),
                  cfg.token.isEmpty() ? "(empty)" : "(set)", cfg.mode.c_str());
  } else if (command == "reboot") {
    ESP.restart();
  } else {
    Serial.println("commands: wifi/server/token/device/mode/show/reboot");
  }
}

void SerialConfigWindow(uint32_t windowMs) {
  Serial.println();
  Serial.println("Today firmware");
  Serial.println("wifi <ssid> <password> | server <url> | token <token> | device <id> | mode battery|mains | show | reboot");
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

bool Configured() {
  return !cfg.wifiSsid.isEmpty() && !cfg.server.isEmpty() && !cfg.token.isEmpty();
}

// Mains mode: stay awake, keep Wi-Fi up and pull every 60 s. Buttons are
// handled in place; this is the fallback when deep-sleep current is too high.
void RunMains() {
  pinMode(board::kPinUp, INPUT_PULLUP);
  pinMode(board::kPinDown, INPUT_PULLUP);
  pinMode(board::kPinOk, INPUT_PULLUP);
  pinMode(board::kPinRefresh, INPUT_PULLUP);
  uint32_t last = 0;
  while (true) {
    if (WiFi.status() != WL_CONNECTED) {
      if (!net::Connect(cfg, 20000)) {
        Serial.println("wifi: reconnect failed");
        delay(5000);
        continue;
      }
    }
    if (last == 0 || millis() - last >= 60000) {
      FetchFrame();
      SendAcks();
      MaybeUpdate(availableFirmware);
      last = millis();
    }
    HandleButtons();
    delay(40);
  }
}

void DeepSleep(uint32_t seconds) {
  if (seconds < board::kMinSleepSeconds) seconds = board::kMinSleepSeconds;
  // Never arm ext1 while a button is still held: the level wake would fire
  // again immediately and the device would spin.
  for (int i = 0; i < 200 && PressedButton() >= 0; i++) delay(50);
  Serial.printf("sleep %lu s\n", static_cast<unsigned long>(seconds));
  display.hibernate();
  WiFi.disconnect(true);
  WiFi.mode(WIFI_OFF);
  esp_sleep_enable_timer_wakeup(static_cast<uint64_t>(seconds) * 1000000ULL);
  esp_sleep_enable_ext1_wakeup(board::ButtonMask(), ESP_EXT1_WAKEUP_ALL_LOW);
  Serial.flush();
  esp_deep_sleep_start();
}

}  // namespace

void setup() {
  Serial.begin(115200);
  delay(50);
  storage::Begin();
  cfg = storage::LoadConfig();
  cursorRow = storage::LoadCursor();
  frameSeq = storage::LoadSeq();
  doneMask = storage::LoadDoneMask();
  haveFrame = storage::LoadFrame(baseBmp, sizeof(baseBmp));
  rowCount = storage::LoadRows(rows, tbf::kMaxRows);
  if (!haveFrame) rowCount = 0;
  if (rowCount > 0 && cursorRow >= rowCount) cursorRow = rowCount - 1;

  const esp_sleep_wakeup_cause_t cause = esp_sleep_get_wakeup_cause();
  const bool buttonWake = cause == ESP_SLEEP_WAKEUP_EXT1;
  InitDisplay(!buttonWake);

  if (buttonWake) {
    HandleButtons();
    DeepSleep(board::kButtonSleepSeconds);
  }

  if (cause == ESP_SLEEP_WAKEUP_UNDEFINED) {
    SerialConfigWindow(4000);
    cfg = storage::LoadConfig();
  }

  if (!Configured()) {
    DrawMessage("Today", "通过 USB 串口配置：", "wifi / server / token / device");
    DeepSleep(300);
  }

  if (cfg.mode == "mains") {
    RunMains();
  }

  if (!net::Connect(cfg, 20000)) {
    Serial.println("wifi: connect failed");
    if (!haveFrame) DrawMessage("Today", "Wi-Fi 连接失败", cfg.wifiSsid);
    DeepSleep(300);
  }

  FetchFrame();
  SendAcks();
  MaybeUpdate(availableFirmware);
  DeepSleep(sleepSeconds);
}

void loop() {}
