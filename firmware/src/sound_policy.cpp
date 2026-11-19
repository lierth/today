#include "sound_policy.h"

#include <stdlib.h>
#include <string.h>

#include "sound_synth.h"

namespace sound {
namespace {

int64_t ParseInt(const char* s, size_t n) {
  int64_t v = 0;
  for (size_t i = 0; i < n; i++) {
    if (s[i] < '0' || s[i] > '9') return -1;
    v = v * 10 + (s[i] - '0');
  }
  return v;
}

}  // namespace

uint8_t ParseAlarms(const char* text, int64_t* out, size_t cap) {
  if (text == nullptr || out == nullptr || cap == 0) return 0;
  uint8_t count = 0;
  const char* p = text;
  while (*p != '\0' && count < cap) {
    const char* comma = strchr(p, ',');
    const size_t len = comma == nullptr ? strlen(p) : static_cast<size_t>(comma - p);
    if (len > 0) {
      const int64_t v = ParseInt(p, len);
      // Ascending on the wire, but do not trust it: drop anything that would
      // make a later entry rewind time.
      if (v > 0 && (count == 0 || v > out[count - 1])) out[count++] = v;
    }
    if (comma == nullptr) break;
    p = comma + 1;
  }
  return count;
}

void ParsePolicy(const char* text, Policy* policy) {
  if (text == nullptr || policy == nullptr) return;
  const char* p = text;
  while (*p != '\0') {
    const char* end = strchr(p, ';');
    const size_t len = end == nullptr ? strlen(p) : static_cast<size_t>(end - p);
    const char* eq = static_cast<const char*>(memchr(p, '=', len));
    if (eq != nullptr) {
      const size_t klen = static_cast<size_t>(eq - p);
      const char* v = eq + 1;
      const size_t vlen = len - klen - 1;
      const bool truthy = vlen > 0 && (v[0] == '1' || v[0] == 't' || v[0] == 'T');
      if (klen == 3 && strncmp(p, "vol", 3) == 0) {
        int n = 0;
        bool ok = vlen > 0;
        for (size_t i = 0; i < vlen; i++) {
          if (v[i] < '0' || v[i] > '9') {
            ok = false;
            break;
          }
          n = n * 10 + (v[i] - '0');
          if (n > 100) ok = false;
        }
        if (ok) policy->volume = static_cast<uint8_t>(n);
      } else if (klen == 4 && strncmp(p, "keys", 4) == 0) {
        policy->keys = truthy;
      } else if (klen == 5 && strncmp(p, "alarm", 5) == 0) {
        policy->alarm = truthy;
      }
    }
    if (end == nullptr) break;
    p = end + 1;
  }
}

int ShouldRing(int64_t nowUnix, const Alarms& alarms, bool alarmOn, uint8_t volume,
               bool nightNow, int64_t graceSeconds) {
  if (!alarmOn || volume == 0 || nightNow) return -1;
  for (uint8_t i = 0; i < alarms.count; i++) {
    if (alarms.fired[i]) continue;
    const int64_t due = alarms.at[i];
    if (nowUnix < due) continue;            // not yet
    if (nowUnix - due > graceSeconds) continue;  // too old: missed while asleep
    return i;
  }
  return -1;
}

int KeyCue(bool keysOn, uint8_t volume, bool checked, int done, int total) {
  if (!keysOn || volume == 0) return static_cast<int>(Cue::kNone);
  if (checked && total > 0 && done == total) return static_cast<int>(Cue::kDone);
  return static_cast<int>(checked ? Cue::kCheck : Cue::kUncheck);
}

}  // namespace sound
