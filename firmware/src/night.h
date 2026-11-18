// Night window arithmetic. Pure C++ (no Arduino), shared by the firmware and
// the host test so the cross-midnight edge cases are covered on the Mac.
#pragma once

namespace night {

// Inside reports whether `minute` (0..1439, local) falls in [start, end).
// start == end means "disabled"; start > end wraps over midnight.
inline bool Inside(int minute, int start, int end) {
  if (start == end) return false;
  if (start < end) return minute >= start && minute < end;
  return minute >= start || minute < end;
}

}  // namespace night
