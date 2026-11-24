#pragma once

#include <stdint.h>

// The scan bookkeeping behind the setup page's network list.
//
// It lives apart from portal.cpp so the host tests can drive it: the board's
// radio cannot be exercised in a unit test, but the states that made the list
// look empty with nothing to show for it can.
namespace portalscan {

// The three useful answers to "start a scan": it started, one was already
// running, or the driver refused (usually the station is still associating).
enum class Kick { Started, Busy, Refused };

struct State {
  bool scanning = false;
  int error = 0;        // last refusal, 0 when the last scan was accepted
  uint32_t retryAt = 0; // millis() before which another kick is pointless
};

// NoteKick folds one start attempt into the state. `raw` is the library's
// return value, kept so the page can print the real code.
void NoteKick(State& s, Kick kick, int raw, uint32_t now, uint32_t retryMs);

// NotePoll folds one scanComplete() answer. Returns true while the scan is
// still in flight; a negative answer is recorded as an error with a backoff,
// which is the case that used to vanish silently.
bool NotePoll(State& s, int complete, uint32_t now, uint32_t retryMs);

// Ready reports whether a new kick may be issued now.
bool Ready(const State& s, uint32_t now);

} // namespace portalscan
