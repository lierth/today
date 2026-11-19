// When to ring, and whether a key should make a sound. Pure arithmetic over
// millis() values so the host test can replay a whole evening in microseconds.
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace sound {

// Settings that arrive from the server in X-Sound (and are cached in NVS so a
// cold boot without Wi-Fi still knows them).
struct Policy {
  bool keys = true;   // key feedback
  bool alarm = true;  // item reminders
  uint8_t volume = 60;
};

constexpr size_t kMaxAlarms = 10;

// AlarmList is what the server sent in X-Alarms: Unix seconds, ascending.
struct Alarms {
  int64_t at[kMaxAlarms];
  uint8_t count = 0;
  // fired[i] is set once the alarm has rung, so a stalled loop cannot repeat
  // it. Cleared whenever the list is replaced.
  bool fired[kMaxAlarms] = {};
};

// ParseAlarms reads "1789000000,1789003600" and returns the number accepted
// (at most kMaxAlarms). Bad tokens are skipped; a full list keeps the earliest.
uint8_t ParseAlarms(const char* text, int64_t* out, size_t cap);

// ShouldRing is the whole decision for one tick:
//   * alarms enabled, volume > 0,
//   * the alarm is due (now >= at) but not older than `graceSeconds`, so a
//     board that was asleep or offline does not chime for yesterday,
//   * not already fired,
//   * outside the night window.
// It returns the index to ring, or -1.
int ShouldRing(int64_t nowUnix, const Alarms& alarms, bool alarmOn, uint8_t volume,
               bool nightNow, int64_t graceSeconds);

// ParsePolicy reads "vol=60;keys=1;alarm=1". Missing or junk fields keep the
// current value, so a partial header cannot silently mute the board.
void ParsePolicy(const char* text, Policy* policy);

// KeyCue maps a key action to a cue: tick, tock, or the arpeggio when the
// completion count just reached the total. volume 0 or keys off yields kNone.
// `done` is the completed count after the action, `total` the number of items.
int KeyCue(bool keysOn, uint8_t volume, bool checked, int done, int total);
}  // namespace sound
