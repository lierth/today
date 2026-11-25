#include "portalscan.h"

namespace portalscan {

bool Request(State& s, uint32_t now, uint32_t settleMs) {
  if (s.scanning || s.requested) return false;
  s = State{};
  s.requested = true;
  s.retryAt = now + settleMs;
  return true;
}

void NoteKick(State& s, Kick kick, int raw, uint32_t now, uint32_t retryMs) {
  switch (kick) {
    case Kick::Started:
      ++s.attempts;
      s.requested = false;
      s.scanning = true;
      s.error = 0;
      return;
    case Kick::Busy:
      // Somebody else's scan is running; no error, just wait for it.
      s.scanning = true;
      s.requested = false;
      return;
    case Kick::Refused:
      ++s.attempts;
      s.scanning = false;
      s.requested = s.attempts < 3;
      s.error = raw < 0 ? raw : -1;
      s.retryAt = now + retryMs;
      return;
  }
}

bool NotePoll(State& s, int complete, uint32_t now, uint32_t retryMs) {
  if (complete == -1) return true; // WIFI_SCAN_RUNNING
  if (!s.scanning) return false;
  s.scanning = false;
  if (complete >= 0) {
    s.complete = true;
    s.error = 0;
    return false;
  }
  // WIFI_SCAN_FAILED: the driver never reported SCAN_DONE, or the library's
  // configured timeout fired. Record it so the page can say so.
  s.error = complete;
  s.requested = s.attempts < 3;
  s.retryAt = now + retryMs;
  return false;
}

bool Ready(const State& s, uint32_t now) {
  return s.requested && !s.scanning && static_cast<int32_t>(now - s.retryAt) >= 0;
}

} // namespace portalscan
