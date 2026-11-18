#pragma once

#include <Arduino.h>

#include "storage.h"
#include "tbf.h"

namespace net {

bool Connect(const storage::Config& cfg, uint32_t timeoutMs);
bool Connected();
int Rssi();

struct FetchResult {
  int status = -1;            // HTTP status, -1 on transport error
  bool changed = false;       // body parsed and bitmap replaced
  bool forceFull = false;     // server asked for a full refresh
  bool refresh = false;       // X-Refresh: repaint even on 304
  uint32_t seq = 0;
  int64_t serverNow = 0;      // X-Server-Now, Unix seconds
  int utcOffset = 0;          // X-UTC-Offset, seconds east of UTC
  bool night = false;         // inside X-Night window
  int nightStart = 0;         // minutes of day
  int nightEnd = 0;
  String etag;
  String firmwareVersion;     // X-Firmware-Version, empty when none uploaded
};

// FetchFrame writes the bitmap into `buf` and points `out->bitmap` into it.
// The cursor is drawn on the device; the request never carries it.
FetchResult FetchFrame(const storage::Config& cfg, const String& etag, bool force,
                       uint8_t* buf, size_t cap, tbf::Frame* out);

// PostAcks sends one JSON payload and returns the HTTP status (or <0).
int PostAcks(const storage::Config& cfg, const String& json);

// DownloadFirmware streams the image into the inactive OTA partition.
bool DownloadFirmware(const storage::Config& cfg);

}  // namespace net
