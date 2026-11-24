#include <assert.h>
#include <stdlib.h>

#include "clocktime.h"
#include "sound_policy.h"

int main() {
  setenv("TZ", "UTC0", 1);
  tzset();
  const time_t utc = 1795478400;  // 2026-11-24 00:00 UTC
  struct tm local {};
  assert(clocktime::Local(utc, 8 * 3600, &local));
  assert(local.tm_hour == 8 && local.tm_mday == 24);
  assert(clocktime::UtcFromWall(mktime(&local), 8 * 3600) == utc);
  assert(clocktime::Local(utc, -5 * 3600, &local));
  assert(local.tm_hour == 19 && local.tm_mday == 23);
  assert(!clocktime::Local(0, 8 * 3600, &local));

  sound::Alarms alarms;
  alarms.count = 1;
  alarms.at[0] = utc;
  assert(sound::ShouldRing(utc, alarms, true, 60, false, 60) == 0);
  assert(sound::ShouldRing(utc + 8 * 3600, alarms, true, 60, false, 60) == -1);
}
