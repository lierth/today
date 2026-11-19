#include "fontcheck.h"

#include "font.h"
#include "portal_config.h"

namespace fontcheck {
namespace {

// Keep in sync with the PaintMessage/DrawString calls in main.cpp. The host
// test also greps main.cpp, so a new literal that is missed here still fails.
const char* const kStrings[] = {
    "TODAY",
    "WAITING",
    "1. THE BOARD POLLS EVERY MINUTE",
    "2. KEY 3S TO SET UP WI-FI",
    "1. HOLD KEY FOR 3 SECONDS",
    "2. THE HOTSPOT REOPENS",
    "2. OR TYPE setup OVER SERIAL",
    "UPDATING",
    "1. KEEP THE POWER ON",
    "2. THE BOARD REBOOTS BY ITSELF",
    "WIFI SETUP",
    "NETWORK",
    "PASSWORD (TYPE WITHOUT SPACES)",
    "THE SCREEN CLOSES ON ITS OWN",
    "http://192.168.4.1",
    "NO WI-FI",
    "WIFI LOST - HOLD KEY 3S TO SET UP",
    "SETUP ENDED",
    "0:00",       // portal countdown, digits and the colon
    "4:32",
    "10:00",
    "30:00",
    "KEY 3S OR 'setup' OVER SERIAL",
    // Literals drawn by widgets.cpp (Text()).
    "AP",
    "--",
    "--:--",
    "-- --%",
    "USB",
    "x",          // wifi disconnected marker
    " %",         // climate "%d%%" suffix
    "PASSWORD",
    "TYPE WITHOUT SPACE",
    "Today-",     // SoftAP SSID prefix
    "wifi ",      // setup command hint in the serial banner
    "server ",
    "device ",
    "token ",
    "show",
};

bool Covered(const char* s) {
  for (; *s != '\0'; s++) {
    if (font::Find(*s) == nullptr) return false;
  }
  return true;
}

}  // namespace

const char* Missing() {
  for (const char* s : kStrings) {
    if (!Covered(s)) return s;
  }
  if (!Covered(portalcfg::kPasswordAlphabet)) return "portal password alphabet";
  return nullptr;
}

}  // namespace fontcheck
