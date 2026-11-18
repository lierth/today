#pragma once

#include <Arduino.h>

#include "storage.h"
#include "tbf.h"

namespace net {

bool Connect(const storage::Config& cfg, uint32_t timeoutMs);

struct FetchResult {
  int status = -1;          // HTTP status, -1 on transport error
  bool changed = false;     // body parsed and bitmap replaced
  bool forceFull = false;   // server asked for a full refresh
  bool refresh = false;     // X-Refresh: repaint even on 304
  uint32_t seq = 0;
  int64_t sleepSeconds = 0;  // server-computed time until next wake
  String etag;
  String firmwareVersion;  // X-Firmware-Version, empty when none uploaded
};

// FetchFrame writes the bitmap into `buf` and points `out->bitmap` into it.
// When force is set the request carries force=1, so the server answers with a
// full repaint instead of a 304 for the cached ETag.
FetchResult FetchFrame(const storage::Config& cfg, const String& etag, bool force,
                       uint8_t* buf, size_t cap, tbf::Frame* out);

int PostAcks(const storage::Config& cfg, const String& json);

// DownloadFirmware streams the image into the inactive OTA partition and
// reboots on success. The caller compares versions first.
bool DownloadFirmware(const storage::Config& cfg);

}  // namespace net
