// When to raise the provisioning hotspot, and when to lower it again.
//
// Pure arithmetic: no Arduino, no Wi-Fi, no clock. The firmware feeds it
// millis() values and the answers of WiFi.status(), and the host test can
// replay a router outage in microseconds.
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace portalplan {

// Backoff for an unattended portal. The first one opens after the failure
// countdown of the caller (five retries); once it times out with nobody using
// it, a person is probably not at the board, so the next opens are spaced out.
// A router reboot is well inside 10 minutes, which is why the first retry is
// short and later ones are not.
constexpr uint32_t kFirstRetryMs = 10 * 60 * 1000;
constexpr uint32_t kLaterRetryMs = 30 * 60 * 1000;

// A hold on KEY is a person asking right now; no backoff applies.
struct Input {
  uint32_t now;          // millis()
  uint32_t lastPortalEnd;  // millis() when the last portal closed; 0 = never
  uint8_t portalRuns;    // portals opened since boot (after the first, back off)
  bool connected;        // WiFi.status() == WL_CONNECTED
  bool portalOpen;       // a portal is running right now
  bool manualRequest;    // KEY held 3 s, latched by the caller
  bool haveCredentials;  // an SSID is stored; without one there is nothing to retry
  uint8_t failures;      // consecutive failed retry ticks while disconnected
  uint8_t threshold;     // failures before the first unattended portal
};

// ShouldOpen answers "raise the hotspot in this tick". It never opens a second
// portal while one runs, never opens one with a working link, and never opens
// one before the first boot attempt had a chance to connect. A manual request
// (KEY held 3 s) wins over the failure countdown and the backoff.
bool ShouldOpen(const Input& in);

// CloseOnConnect: a portal that is up while the old network comes back yields
// to the network instead of keeping the phone on the setup page.
bool CloseOnConnect(bool portalOpen, bool connected);

// ReopenDelay: milliseconds to wait after a timed-out portal before the next
// unattended one. Exposed for the test and to print the next try.
uint32_t ReopenDelay(uint8_t portalRuns);

// CountdownText formats milliseconds as "M:SS" for the portal screen. Writes
// at most cap bytes; an expired countdown becomes "0:00".
void CountdownText(uint32_t remainingMs, char* out, size_t cap);

// PasswordDisplay groups the 10-digit SoftAP password for the panel:
// "4821993057" -> "48219 93057". The space is display-only; the hotspot
// password has no separator. `out` must hold at least
// portalcfg::kPasswordLength + 2 bytes (10 digits, one space, NUL).
void PasswordDisplay(const char* password, char* out, size_t cap);

}  // namespace portalplan
