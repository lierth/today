#pragma once

#include <stdint.h>
#include <time.h>

namespace clocktime {

// The system and alarm timestamps are UTC; only the display and RTC use wall time.
inline time_t UtcFromWall(time_t wall, int offset) { return wall - offset; }

inline bool Local(time_t utc, int offset, struct tm* out) {
  if (out == nullptr || utc < 1600000000) return false;
  const time_t wall = utc + offset;
  return gmtime_r(&wall, out) != nullptr;
}

}  // namespace clocktime
