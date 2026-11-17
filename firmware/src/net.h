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
};

// FetchFrame writes the bitmap into `buf` and points `out->bitmap` into it.
FetchResult FetchFrame(const storage::Config& cfg, const String& etag,
                       uint8_t* buf, size_t cap, tbf::Frame* out);

int PostAcks(const storage::Config& cfg, const String& json);

}  // namespace net
