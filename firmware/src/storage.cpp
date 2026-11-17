#include "storage.h"

#include <Preferences.h>
#include <string.h>

namespace storage {
namespace {

Preferences prefs;
constexpr const char* kNamespace = "today";
constexpr const char* kOutbox = "outbox";

String ReadString(const char* key, const char* fallback = "") {
  return prefs.getString(key, fallback);
}

}  // namespace

void Begin() { prefs.begin(kNamespace, false); }

Config LoadConfig() {
  Config cfg;
  cfg.wifiSsid = ReadString("ssid");
  cfg.wifiPass = ReadString("pass");
  cfg.server = ReadString("server");
  cfg.device = ReadString("device", "dev1");
  cfg.token = ReadString("token");
  cfg.mode = ReadString("mode", "battery");
  return cfg;
}

void SaveConfig(const Config& cfg) {
  prefs.putString("ssid", cfg.wifiSsid);
  prefs.putString("pass", cfg.wifiPass);
  prefs.putString("server", cfg.server);
  prefs.putString("device", cfg.device);
  prefs.putString("token", cfg.token);
  prefs.putString("mode", cfg.mode);
}

uint8_t LoadCursor() { return prefs.getUChar("cursor", 0); }
void SaveCursor(uint8_t row) { prefs.putUChar("cursor", row); }

uint32_t LoadSeq() { return prefs.getUInt("seq", 0); }
void SaveSeq(uint32_t seq) { prefs.putUInt("seq", seq); }

String LoadETag() { return ReadString("etag"); }
void SaveETag(const String& etag) { prefs.putString("etag", etag); }

uint32_t LoadDoneMask() { return prefs.getUInt("done", 0); }
void SaveDoneMask(uint32_t mask) { prefs.putUInt("done", mask); }

bool LoadFrame(uint8_t* out, size_t len) {
  const size_t got = prefs.getBytesLength("frame");
  if (got != len) return false;
  return prefs.getBytes("frame", out, len) == len;
}

bool SaveFrame(const uint8_t* data, size_t len) {
  return prefs.putBytes("frame", data, len) == len;
}

uint8_t LoadRows(tbf::Row* out, uint8_t max) {
  uint8_t count = prefs.getUChar("rowcount", 0);
  if (count > max) count = max;
  if (count == 0) return 0;
  if (prefs.getBytesLength("rows") != static_cast<size_t>(count) * 20) return 0;
  return prefs.getBytes("rows", out, static_cast<size_t>(count) * 20) ==
                 static_cast<size_t>(count) * 20
             ? count
             : 0;
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
  const size_t bytes = prefs.getBytes(kOutbox, ops, sizeof(ops));
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
  const size_t bytes = prefs.getBytes(kOutbox, ops, sizeof(ops));
  if (bytes == 0 || index >= bytes / sizeof(OutboxOp)) return false;
  *out = ops[index];
  return true;
}

void ClearOutbox() {
  prefs.remove(kOutbox);
  prefs.putUChar("outcount", 0);
}

void DropOutbox(uint8_t sent) {
  OutboxOp ops[board::kMaxOutbox]{};
  const size_t bytes = prefs.getBytes(kOutbox, ops, sizeof(ops));
  const uint8_t count = static_cast<uint8_t>(bytes / sizeof(OutboxOp));
  if (sent >= count) {
    ClearOutbox();
    return;
  }
  const uint8_t left = count - sent;
  memmove(ops, ops + sent, static_cast<size_t>(left) * sizeof(OutboxOp));
  prefs.putBytes(kOutbox, ops, static_cast<size_t>(left) * sizeof(OutboxOp));
  prefs.putUChar("outcount", left);
}

bool TakeForceFull() {
  const bool value = prefs.getBool("force", true);
  if (value) prefs.putBool("force", false);
  return value;
}

void SetForceFull(bool value) { prefs.putBool("force", value); }

}  // namespace storage
