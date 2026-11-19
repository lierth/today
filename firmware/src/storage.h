// NVS-backed device state: credentials, cached frame, cursor and outbox.
#pragma once

#include <Arduino.h>

#include "board.h"
#include "tbf.h"

namespace storage {

struct Config {
  String wifiSsid;
  String wifiPass;
  String server;  // http://192.168.1.10:8787
  String device;  // dev1
  String token;
};

struct OutboxOp {
  uint8_t id[16];
  uint8_t status;  // 0 open, 1 done
  uint32_t seq;
};

void Begin();
Config LoadConfig();
void SaveConfig(const Config& cfg);
bool Configured();

uint8_t LoadCursor();
uint32_t LoadDoneMask();
void SaveDoneMask(uint32_t mask);
void SaveCursor(uint8_t row);

uint32_t LoadSeq();
void SaveSeq(uint32_t seq);

String LoadETag();
void SaveETag(const String& etag);

// The cached frame lives in LittleFS (15 KB is too big for the 20 KB NVS
// partition); the row table stays in NVS.
bool LoadFrame(uint8_t* out, size_t len);
bool SaveFrame(const uint8_t* data, size_t len);
uint8_t LoadRows(tbf::Row* out, uint8_t max);
void SaveRows(const tbf::Row* rows, uint8_t count);

uint32_t LoadOpSeq();
void SaveOpSeq(uint32_t seq);

uint8_t OutboxCount();
bool AppendOutbox(const uint8_t id[16], uint8_t status, uint32_t seq);
bool LoadOutbox(uint8_t index, OutboxOp* out);
void ClearOutbox();

int LoadUtcOffset();
void SaveUtcOffset(int seconds);

// Reminder list and sound policy from the last frame response: kept in NVS so
// a cold boot with no Wi-Fi still rings for today's items.
String LoadAlarms();
void SaveAlarms(const String& alarms);
String LoadSound();
void SaveSound(const String& sound);

bool TakeForceFull();
void SetForceFull(bool value);

}  // namespace storage
