// The device's own screens, drawn from pure C++ so the host test can render
// each one to a PNG for review.
//
// One template everywhere: a 44 px header band with a rule under it (the same
// height as the server frame's header), a title at scale 3, numbered steps at
// scale 2, and a key hint at the bottom. Nothing is stacked in the top-left
// corner any more.
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace screens {

// 1-bit, row-major, MSB first, 1 = white, 0 = black: the same packing the
// panel and the server frame use.
constexpr size_t kWidth = 400;
constexpr size_t kHeight = 300;
constexpr size_t kStride = kWidth / 8;
constexpr size_t kBytes = kStride * kHeight;

// The header band is as tall as the server frame's, so a self-drawn screen
// and a server frame line up when the board switches between them.
constexpr int kHeaderHeight = 44;

// Every screen the board can show on its own.
enum class Kind : uint8_t {
  kBoot,          // logo + version
  kWaiting,       // no frame yet
  kPortal,        // Wi-Fi hotspot credentials
  kNoWifi,        // nothing configured / link never came up
  kUpdating,      // OTA in progress
  kBadFrame,      // the frame did not parse
  kSetupEnded,    // the setup hotspot timed out
};

struct Text {
  const char* line1;  // title, scale 3
  const char* line2;  // step 1, scale 2
  const char* line3;  // step 2
  const char* line4;  // step 3
  const char* hint;   // bottom line, scale 1
};

// Content for each template screen. The caller may override the strings for
// a portrait screen (the portal has its own credentials layout).
Text For(Kind kind);

// Draw one template screen into `out` (kBytes). `accent` inverts the header
// band, which the alarm and the lost-Wi-Fi banner use.
void DrawTemplate(uint8_t* out, const Text& text, bool accent);

// The boot screen is its own layout: a centred, downscaled app icon (rounded
// screen with a tick) plus the version, drawn with dots.
void DrawBoot(uint8_t* out, const char* version);

// The setup hotspot is the one screen a person copies values from, so it
// keeps the shared header and puts the password at scale 4.
void DrawPortal(uint8_t* out, const char* ssid, const char* password, const char* url);

// The banner's own left margin; the widest string the firmware draws there is
// 396 px on a 400 px panel, so this is all the slack there is.
constexpr int kBannerPad = 2;

// Banner geometry, shared with widgets::DrawBanner so the host test can
// check it without a panel.
int BannerTextX(int panel, int textWidth);
int BannerTextY(int y, int height, int scale);

// ClockTextX places the five-character clock so it ends at the widget's right
// edge. "14:05" at scale 3 is 90 px wide; older frames reserved only 64, so
// centring it on that rect runs it off the 400 px panel. Right-aligning to
// clockX+clockW leaves the declared margin on the right and never clips.
int ClockTextX(int clockX, int clockW, int scale);

// DrawString writes ASCII text at (x, y) with `scale`. It returns the width
// used. Only the glyphs the firmware font has are supported; unknown bytes
// render as '?'.
int DrawString(uint8_t* out, int x, int y, const char* s, int scale);

}  // namespace screens
