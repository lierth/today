#include "storage.h"

#include <LittleFS.h>
#include <Preferences.h>
#include <string.h>

namespace storage {
namespace {

Preferences prefs;
constexpr const char* kNamespace = "today";
constexpr const char* kOutbox = "outbox";
// The 15 KB frame does not fit the 20 KB NVS partition next to credentials
// and the outbox, so it lives in LittleFS. NVS keeps only small values.
constexpr const char* kFramePath = "/frame.bin";

// Preferences::getString/getBytes log an error for every missing key
// ("nvs_get_str len fail: ... NOT_FOUND"), which buries the boot log on a
// fresh board. isKey() probes the type without logging, so every optional key
// is checked first.
String ReadString(const char* key, const char* fallback = "") {
  if (!prefs.isKey(key)) return String(fallback);
  return prefs.getString(key, fallback);
}

size_t ReadBlob(const char* key, void* buf, size_t maxLen) {
  if (!prefs.isKey(key)) return 0;
  return prefs.getBytes(key, buf, maxLen);
}

}  // namespace

void Begin() {
  prefs.begin(kNamespace, false);
  // The partition is named "littlefs" in partitions.csv. LittleFS.begin's
  // default label is "spiffs", so it must be passed explicitly or the mount
  // silently fails and the cached frame is disabled (blank board on boot).
  if (!LittleFS.begin(true, "/littlefs", 10, "littlefs")) {
    Serial.println("storage: LittleFS mount failed; frame cache disabled");
  }
}

Config LoadConfig() {
  Config cfg;
  cfg.wifiSsid = ReadString("ssid");
  cfg.wifiPass = ReadString("pass");
  cfg.server = ReadString("server");
  cfg.device = ReadString("device", "dev1");
  cfg.token = ReadString("token");
  return cfg;
}

void SaveConfig(const Config& cfg) {
  prefs.putString("ssid", cfg.wifiSsid);
  prefs.putString("pass", cfg.wifiPass);
  prefs.putString("server", cfg.server);
  prefs.putString("device", cfg.device);
  prefs.putString("token", cfg.token);
}

bool Configured() {
  const Config cfg = LoadConfig();
  return !cfg.wifiSsid.isEmpty() && !cfg.server.isEmpty() && !cfg.token.isEmpty();
}

uint8_t LoadCursor() { return prefs.getUChar("cursor", 0); }
void SaveCursor(uint8_t row) { prefs.putUChar("cursor", row); }

uint32_t LoadDoneMask() { return prefs.getUInt("done", 0); }
void SaveDoneMask(uint32_t mask) { prefs.putUInt("done", mask); }

uint32_t LoadSeq() { return prefs.getUInt("seq", 0); }
void SaveSeq(uint32_t seq) { prefs.putUInt("seq", seq); }

String LoadETag() { return ReadString("etag"); }
void SaveETag(const String& etag) { prefs.putString("etag", etag); }

bool LoadFrame(uint8_t* out, size_t len) {
  if (out == nullptr || !LittleFS.exists(kFramePath)) return false;
  File f = LittleFS.open(kFramePath, "r");
  if (!f || static_cast<size_t>(f.size()) != len) {
    if (f) f.close();
    return false;
  }
  const size_t got = f.read(out, len);
  f.close();
  return got == len;
}

bool SaveFrame(const uint8_t* data, size_t len) {
  File f = LittleFS.open(kFramePath, "w");
  if (!f) return false;
  const size_t put = f.write(data, len);
  f.close();
  return put == len;
}

uint8_t LoadRows(tbf::Row* out, uint8_t max) {
  uint8_t count = prefs.getUChar("rowcount", 0);
  if (count > max) count = max;
  if (count == 0) return 0;
  const size_t want = static_cast<size_t>(count) * 20;
  return ReadBlob("rows", out, want) == want ? count : 0;
}

void SaveRows(const tbf::Row* rows, uint8_t count) {
  prefs.putUChar("rowcount", count);
  if (count > 0) prefs.putBytes("rows", rows, static_cast<size_t>(count) * 20);
}

uint32_t LoadOpSeq() { return prefs.getUInt("opseq", 0); }
void SaveOpSeq(uint32_t seq) { prefs.putUInt("opseq", seq); }

uint8_t OutboxCount() { return prefs.getUChar("outcount", 0); }

bool AppendOutbox(const uint8_t id[16], uint8_t status, uint32_t seq) {
  OutboxOp ops[board::kMaxOutbox]{};
  const size_t bytes = ReadBlob(kOutbox, ops, sizeof(ops));
  uint8_t count = bytes > 0 ? static_cast<uint8_t>(bytes / sizeof(OutboxOp)) : 0;
  if (count >= board::kMaxOutbox) return false;
  OutboxOp& op = ops[count];
  memcpy(op.id, id, 16);
  op.status = status;
  op.seq = seq;
  count++;
  prefs.putBytes(kOutbox, ops, static_cast<size_t>(count) * sizeof(OutboxOp));
  prefs.putUChar("outcount", count);
  return true;
}

bool LoadOutbox(uint8_t index, OutboxOp* out) {
  OutboxOp ops[board::kMaxOutbox]{};
  const size_t bytes = ReadBlob(kOutbox, ops, sizeof(ops));
  if (bytes == 0 || index >= bytes / sizeof(OutboxOp)) return false;
  *out = ops[index];
  return true;
}

void ClearOutbox() {
  prefs.remove(kOutbox);
  prefs.putUChar("outcount", 0);
}

String LoadAlarms() { return prefs.getString("alarms", ""); }
void SaveAlarms(const String& alarms) { prefs.putString("alarms", alarms); }
String LoadSound() { return prefs.getString("sound", ""); }
void SaveSound(const String& sound) { prefs.putString("sound", sound); }

int LoadUtcOffset() { return prefs.getInt("utcoff", 0); }
void SaveUtcOffset(int seconds) { prefs.putInt("utcoff", seconds); }

bool TakeForceFull() {
  const bool value = prefs.getBool("force", false);
  if (value) prefs.putBool("force", false);
  return value;
}

void SetForceFull(bool value) { prefs.putBool("force", value); }

}  // namespace storage
