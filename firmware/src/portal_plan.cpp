#include "portal_plan.h"

#include "portal_config.h"

#include <stdio.h>

namespace portalplan {
namespace {

// The first unattended portal opens once the retry count reached the point
// where the caller would have shown the "WIFI LOST" banner; the caller keeps
// that countdown, so this function only owns the *reopen* cadence.
bool Due(uint32_t now, uint32_t since, uint32_t delayMs) {
  if (since == 0) return true;          // never opened: the caller decides
  return static_cast<int32_t>(now - since) >= static_cast<int32_t>(delayMs);
}

}  // namespace

uint32_t ReopenDelay(uint8_t portalRuns) {
  // Runs 0 and 1: the first automatic portal and the first retry. Only from
  // the second retry on does the cadence stretch to 30 minutes.
  return portalRuns >= 2 ? kLaterRetryMs : kFirstRetryMs;
}

bool ShouldOpen(const Input& in) {
  if (in.portalOpen) return false;
  // A person holding KEY 3 s outranks everything, including a working link:
  // switching to a new router is exactly when the link is still fine.
  if (in.manualRequest) return true;
  if (in.connected) return false;
  if (!in.haveCredentials) return false;
  if (in.failures < in.threshold) return false;
  if (in.portalRuns == 0) return true;  // the first unattended portal
  return Due(in.now, in.lastPortalEnd, ReopenDelay(in.portalRuns));
}

bool CloseOnConnect(bool portalOpen, bool connected) {
  return portalOpen && connected;
}

void CountdownText(uint32_t remainingMs, char* out, size_t cap) {
  if (out == nullptr || cap == 0) return;
  if (remainingMs == 0) {
    snprintf(out, cap, "0:00");
    return;
  }
  // Round up so the last second shows "0:01" rather than "0:00" while the
  // portal is still accepting requests.
  const uint32_t seconds = (remainingMs + 999) / 1000;
  snprintf(out, cap, "%lu:%02lu", static_cast<unsigned long>(seconds / 60),
           static_cast<unsigned long>(seconds % 60));
}

void PasswordDisplay(const char* password, char* out, size_t cap) {
  if (out == nullptr || cap == 0) return;
  out[0] = '\0';
  if (password == nullptr) return;
  size_t n = 0;
  while (password[n] != '\0') n++;
  size_t used = 0;
  for (size_t i = 0; i < n; i++) {
    if (i > 0 && i % portalcfg::kPasswordGroup == 0) {
      if (used + 1 >= cap) break;
      out[used++] = ' ';
    }
    if (used + 1 >= cap) break;
    out[used++] = password[i];
  }
  out[used] = '\0';
}

}  // namespace portalplan
