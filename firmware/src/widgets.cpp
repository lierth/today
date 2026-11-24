#include "widgets.h"

#include <math.h>
#include <stdio.h>
#include <string.h>

#include "board.h"
#include "display.h"
#include "font.h"
#include "screens.h"
#include "st7305.h"

namespace widgets {
namespace {

// The glyph table lives in font.h/font_ascii.inc so the host test can check
// every string the firmware draws; see firmware/test/pack_test.cpp.
using Glyph = font::Glyph;
const Glyph* Find(char c) { return font::Find(c); }

void Pixel(int x, int y, bool black) {
  if (x < 0 || y < 0 || x >= board::kWidth || y >= board::kHeight) return;
  const st7305::Cell cell =
      st7305::CellFor(static_cast<uint16_t>(x), static_cast<uint16_t>(y));
  uint8_t* buf = display::Buffer();
  if (black) {
    buf[cell.index] &= static_cast<uint8_t>(~cell.mask);
  } else {
    buf[cell.index] |= cell.mask;
  }
}

void Box(int x0, int y0, int w, int h, bool fill) {
  for (int y = y0; y < y0 + h; y++) {
    for (int x = x0; x < x0 + w; x++) {
      const bool edge = x == x0 || y == y0 || x == x0 + w - 1 || y == y0 + h - 1;
      if (fill || edge) Pixel(x, y, true);
    }
  }
}

// Draws text with its top-left at (x,y); scale 1 or 2. Returns the width used.
// `inkBlack` picks the ink, so the same glyphs can sit inside an inverted
// band without a second copy of the glyph loop.
int TextInk(int x, int y, const char* s, int scale, bool inkBlack) {
  int cursor = x;
  for (; *s != '\0'; s++) {
    const Glyph* g = Find(*s);
    if (g == nullptr) g = Find('?');
    if (g == nullptr) continue;
    for (int col = 0; col < 5; col++) {
      const uint8_t bits = g->col[col];
      // 5x8 cell: bit 7 carries the descender (see font.h).
      for (int row = 0; row < font::kGlyphHeight; row++) {
        if ((bits >> row) & 1) {
          for (int dy = 0; dy < scale; dy++) {
            for (int dx = 0; dx < scale; dx++) {
              Pixel(cursor + col * scale + dx, y + row * scale + dy, inkBlack);
            }
          }
        }
      }
    }
    cursor += 6 * scale;
  }
  return cursor - x;
}

int Text(int x, int y, const char* s, int scale) { return TextInk(x, y, s, scale, true); }

int TextWidth(const char* s, int scale) {
  int n = 0;
  for (; *s != '\0'; s++) n++;
  return n * 6 * scale;
}

int BatteryPercentRaw(int mv) {
  // Resting-voltage curve for a single Li-ion cell; linear between points.
  static const struct {
    int mv;
    int pct;
  } kCurve[] = {
      {3300, 0}, {3600, 10}, {3700, 30}, {3800, 55},
      {3950, 75}, {4100, 95}, {4200, 100},
  };
  if (mv <= kCurve[0].mv) return 0;
  const int last = static_cast<int>(sizeof(kCurve) / sizeof(kCurve[0])) - 1;
  if (mv >= kCurve[last].mv) return 100;
  for (int i = 1; i <= last; i++) {
    if (mv <= kCurve[i].mv) {
      const int span = kCurve[i].mv - kCurve[i - 1].mv;
      const int dp = kCurve[i].pct - kCurve[i - 1].pct;
      return kCurve[i - 1].pct + (mv - kCurve[i - 1].mv) * dp / span;
    }
  }
  return 100;
}

}  // namespace

void Begin() {}

void Clear(const tbf::Widget& rect) {
  for (int y = rect.y; y < rect.y + rect.h; y++) {
    for (int x = rect.x; x < rect.x + rect.w; x++) Pixel(x, y, false);
  }
}

void DrawClock(const tbf::Widget& rect, int hh, int mm) {
  Clear(rect);
  char line[8];
  snprintf(line, sizeof(line), "%02d:%02d", hh, mm);
  const int scale = 3;
  // The ink may start left of the rect (it is wider); that is fine, the
  // neighbours are to its left with a gap.
  Text(screens::ClockTextX(rect.x, rect.w, scale), rect.y + (rect.h - font::kGlyphHeight * scale) / 2, line, scale);
}

void DrawClockUnknown(const tbf::Widget& rect) {
  Clear(rect);
  const int scale = 3;
  Text(screens::ClockTextX(rect.x, rect.w, scale), rect.y + (rect.h - font::kGlyphHeight * scale) / 2, "--:--", scale);
}

void DrawWifi(const tbf::Widget& rect, int rssi, bool connecting, bool portal,
              bool connected) {
  Clear(rect);
  if (portal) {
    Text(rect.x + 2, rect.y + 3, "AP", 1);
    return;
  }
  if (!connected) {
    Text(rect.x + 2, rect.y + 3, connecting ? "--" : "x", 1);
    return;
  }
  // Three bars: weak < -80, ok < -65, strong otherwise.
  int bars = 1;
  if (rssi > -80) bars = 2;
  if (rssi > -65) bars = 3;
  const int baseY = rect.y + rect.h - 2;
  for (int i = 0; i < 3; i++) {
    const int h = 3 + i * 3;
    const int x = rect.x + 3 + i * 5;
    if (i < bars) {
      Box(x, baseY - h, 3, h, true);
    } else {
      Box(x, baseY - h, 3, h, false);
    }
  }
}

void DrawClimate(const tbf::Widget& rect, float temperatureC, float humidity,
                 bool ok) {
  Clear(rect);
  char line[16];
  if (!ok) {
    Text(rect.x + 1, rect.y + 2, "-- --%", 1);
    return;
  }
  snprintf(line, sizeof(line), "%d", static_cast<int>(lroundf(temperatureC)));
  const int used = Text(rect.x + 1, rect.y + 2, line, 1);
  // Degree mark: a 3x3 ring at the cap height of the digits.
  const int dx = rect.x + 1 + used + 1;
  const int dy = rect.y + 2;
  for (int i = 0; i < 3; i++) {
    Pixel(dx + i, dy, true);
    Pixel(dx + i, dy + 2, true);
    Pixel(dx, dy + i, true);
    Pixel(dx + 2, dy + i, true);
  }
  snprintf(line, sizeof(line), " %d%%", static_cast<int>(lroundf(humidity)));
  Text(dx + 4, rect.y + 2, line, 1);
}

void DrawPower(const tbf::Widget& rect, int millivolts, bool usbKnown, bool usb) {
  Clear(rect);
  if (millivolts < 2500) {
    // No battery (or the divider floats): a plug, never "0%" or "100%".
    const int px0 = rect.x + 2;
    const int py0 = rect.y + 3;
    for (int i = 0; i < 8; i++) Pixel(px0 + i, py0 + 4, true);
    for (int i = 0; i < 3; i++) {
      Pixel(px0 + 2, py0 + 1 + i, true);
      Pixel(px0 + 5, py0 + 1 + i, true);
    }
    Pixel(px0 + 8, py0 + 3, true);
    Pixel(px0 + 8, py0 + 4, true);
    Pixel(px0 + 8, py0 + 5, true);
    Text(rect.x + 14, rect.y + 2, "USB", 1);
    return;
  }
  const int pct = BatteryPercentRaw(millivolts);
  // Battery outline with a 76%-style fill scaled to pct.
  const int bx = rect.x + 1;
  const int by = rect.y + 3;
  Box(bx, by, 14, 8, false);
  Box(bx + 14, by + 2, 2, 3, true);
  if (millivolts > 0) {
    const int fill = (12 * pct) / 100;
    if (fill > 0) Box(bx + 2, by + 2, fill, 4, true);
  }
  if (usbKnown && usb) {
    // Lightning bolt inside the battery.
    Pixel(bx + 7, by + 1, true);
    Pixel(bx + 6, by + 2, true);
    Pixel(bx + 7, by + 2, true);
    Pixel(bx + 7, by + 3, true);
    Pixel(bx + 6, by + 4, true);
  }
  char label[8];
  snprintf(label, sizeof(label), "%d%%", pct);
  Text(rect.x + 18, rect.y + 2, label, 1);
}

int DrawString(int x, int y, const char* text, int scale) {
  return Text(x, y, text, scale);
}

void DrawBanner(int y, int height, const char* text, int scale, bool invert) {
  // A shared monochrome strip for reminders and offline status.
  for (int yy = y; yy < y + height && yy < board::kHeight; yy++) {
    for (int x = 0; x < board::kWidth; x++) Pixel(x, yy, invert);
  }
  const int w = TextWidth(text, scale);
  const int x = screens::BannerTextX(board::kWidth, w);
  const int top = screens::BannerTextY(y, height, scale);
  TextInk(x, top, text, scale, !invert);
}

int MeasureWidth(const char* text, int scale) { return TextWidth(text, scale); }

void FillRect(int x, int y, int w, int h, int color) {
  for (int yy = y; yy < y + h; yy++) {
    for (int xx = x; xx < x + w; xx++) Pixel(xx, yy, color == kBlack);
  }
}

int BatteryPercent(int millivolts) {
  if (millivolts < 2500) return -1;
  return BatteryPercentRaw(millivolts);
}

}  // namespace widgets
