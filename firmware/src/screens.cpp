#include "screens.h"

#include <math.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

#include "font.h"

namespace screens {
namespace {

void Pixel(uint8_t* out, int x, int y, bool black) {
  if (x < 0 || y < 0 || x >= static_cast<int>(kWidth) || y >= static_cast<int>(kHeight)) return;
  const size_t index = static_cast<size_t>(y) * kStride + static_cast<size_t>(x) / 8;
  const uint8_t mask = static_cast<uint8_t>(0x80 >> (x % 8));
  if (black) {
    out[index] &= static_cast<uint8_t>(~mask);
  } else {
    out[index] |= mask;
  }
}

void Fill(uint8_t* out, int x, int y, int w, int h, bool black) {
  for (int yy = y; yy < y + h; yy++) {
    for (int xx = x; xx < x + w; xx++) Pixel(out, xx, yy, black);
  }
}

int TextWidth(const char* s, int scale) {
  int n = 0;
  for (; *s != '\0'; s++) n++;
  return n * 6 * scale;
}

}  // namespace

int DrawString(uint8_t* out, int x, int y, const char* s, int scale) {
  if (out == nullptr || s == nullptr) return 0;
  int cursor = x;
  for (; *s != '\0'; s++) {
    const font::Glyph* g = font::Find(*s);
    if (g == nullptr) g = font::Find('?');
    if (g == nullptr) continue;
    for (int col = 0; col < 5; col++) {
      const uint8_t bits = g->col[col];
      // The glcdfont cell is 5x8: bit 7 is the descender of g j p q y.
      // Drawing only seven rows cut the bottom of those letters off
      // ("Todaч", a "p" with no stem).
      for (int row = 0; row < font::kGlyphHeight; row++) {
        if ((bits >> row) & 1) {
          for (int dy = 0; dy < scale; dy++) {
            for (int dx = 0; dx < scale; dx++) {
              Pixel(out, cursor + col * scale + dx, y + row * scale + dy, true);
            }
          }
        }
      }
    }
    cursor += 6 * scale;
  }
  return cursor - x;
}

Text For(Kind kind) {
  switch (kind) {
    case Kind::kBoot:
      return Text{"TODAY", "", "", "", ""};
    case Kind::kWaiting:
      return Text{"TODAY", "1. WAITING FOR THE FIRST FRAME", "2. THE BOARD POLLS EVERY MINUTE",
                  "", "KEY 3S TO SET UP WI-FI"};
    case Kind::kPortal:
      return Text{"WIFI SETUP", "", "", "", ""};
    case Kind::kNoWifi:
      return Text{"NO WI-FI", "1. HOLD KEY FOR 3 SECONDS", "2. JOIN Today-XXXX ON YOUR PHONE",
                  "3. OPEN http://192.168.4.1", "OR USE THE SERIAL WINDOW"};
    case Kind::kUpdating:
      return Text{"UPDATING", "1. KEEP THE POWER ON", "2. THE BOARD REBOOTS BY ITSELF",
                  "", ""};
    case Kind::kBadFrame:
      return Text{"BAD FRAME", "1. THE SERVER SENT A BAD BITMAP", "2. PRESS BOOT TO RETRY",
                  "", "THE CACHE IS KEPT"};
    case Kind::kSetupEnded:
      return Text{"SETUP ENDED", "1. HOLD KEY FOR 3 SECONDS", "2. THE HOTSPOT REOPENS",
                  "", ""};
  }
  return Text{"TODAY", "", "", "", ""};
}

void DrawTemplate(uint8_t* out, const Text& text, bool accent) {
  if (out == nullptr) return;
  memset(out, 0xFF, kBytes);
  // Header band: inverted when the caller wants attention, otherwise a rule
  // under the title (same 44 px as the server frame).
  if (accent) {
    Fill(out, 0, 0, kWidth, kHeaderHeight, true);
  } else {
    Fill(out, 0, kHeaderHeight - 1, kWidth, 1, true);
  }
  if (text.line1 != nullptr && text.line1[0] != '\0') {
    DrawString(out, 16, 12, text.line1, 3);
  }
  const char* steps[3] = {text.line2, text.line3, text.line4};
  int y = kHeaderHeight + 24;
  for (const char* step : steps) {
    if (step != nullptr && step[0] != '\0') {
      DrawString(out, 16, y, step, 2);
      y += 34;
    }
  }
  if (text.hint != nullptr && text.hint[0] != '\0') {
    DrawString(out, 16, kHeight - 24, text.hint, 1);
  }
}

int BannerTextX(int panel, int textWidth) {
  // Centred, but never closer than kBannerPad to the left edge: the widest
  // banner string (33 chars at scale 2 = 396 px on 400) then starts at 2 and
  // still ends inside the panel.
  int x = (panel - textWidth) / 2;
  if (x < kBannerPad) x = kBannerPad;
  if (x + textWidth > panel) x = panel - textWidth;
  if (x < 0) x = 0;
  return x;
}

int ClockTextX(int clockX, int clockW, int scale) {
  const int w = TextWidth("00:00", scale);
  int x = clockX + clockW - w;
  if (x + w > static_cast<int>(kWidth)) x = static_cast<int>(kWidth) - w;
  if (x < 0) x = 0;
  return x;
}

int BannerTextY(int y, int height, int scale) {
  const int top = y + (height - font::kGlyphHeight * scale) / 2;
  return top < y ? y : top;
}

void DrawPortal(uint8_t* out, const char* ssid, const char* password, const char* url) {
  if (out == nullptr) return;
  memset(out, 0xFF, kBytes);
  // Same header band as every other screen: title at scale 3, rule below.
  Fill(out, 0, kHeaderHeight - 1, kWidth, 1, true);
  DrawString(out, 16, 12, "WIFI SETUP", 3);

  DrawString(out, 16, kHeaderHeight + 16, "NETWORK", 1);
  DrawString(out, 16, kHeaderHeight + 34, ssid != nullptr ? ssid : "", 2);

  DrawString(out, 16, kHeaderHeight + 72, "PASSWORD (TYPE WITHOUT SPACES)", 1);
  // The password is the one value copied by hand: the largest scale that
  // still fits 11 characters on a 400 px panel is 5 (11 * 30 = 330 px).
  DrawString(out, 16, kHeaderHeight + 88, password != nullptr ? password : "", 5);

  DrawString(out, 16, kHeight - 72, url != nullptr ? url : "", 2);
  DrawString(out, 16, kHeight - 24, "THE SCREEN CLOSES ON ITS OWN", 1);
}

void DrawBoot(uint8_t* out, const char* version) {
  if (out == nullptr) return;
  memset(out, 0xFF, kBytes);
  // The app icon at half size: a rounded screen (84x64 at 2x) centred, with a
  // filled circle + tick for the first row and two hollow circles below.
  const int w = 168;
  const int h = 128;
  const int x0 = (static_cast<int>(kWidth) - w) / 2;
  const int y0 = 56;
  // Rounded frame: a 2 px box inset by the corner radius.
  const int r = 12;
  for (int x = x0 + r; x < x0 + w - r; x++) {
    Fill(out, x, y0, 1, 2, true);
    Fill(out, x, y0 + h - 2, 1, 2, true);
  }
  for (int y = y0 + r; y < y0 + h - r; y++) {
    Fill(out, x0, y, 2, 1, true);
    Fill(out, x0 + w - 2, y, 2, 1, true);
  }
  // Corners: four small arcs approximated with a 4-step staircase each.
  for (int i = 0; i < 6; i++) {
    const int dx = r - i - 1;
    const int dy = r - static_cast<int>(sqrtf(static_cast<float>(r * r - dx * dx)) + 0.5f);
    Fill(out, x0 + i, y0 + dy, 1, 2, true);
    Fill(out, x0 + w - i - 1, y0 + dy, 1, 2, true);
    Fill(out, x0 + i, y0 + h - dy - 2, 1, 2, true);
    Fill(out, x0 + w - i - 1, y0 + h - dy - 2, 1, 2, true);
  }
  // Three rows inside: done, open, open.
  //
  // The done row is a filled disc with a tick punched out, like the app icon.
  // Drawn as two short strokes with real thickness: the earlier two-pixel
  // diagonal pairs read as "\\" at this size, not as a check.
  const int rowY[3] = {y0 + 28, y0 + 62, y0 + 96};
  for (int i = 0; i < 3; i++) {
    const int cx = x0 + 32;
    const int radius = 10;  // 20 x 20 disc; the tick spans 12 x 12 inside
    if (i == 0) {
      for (int dy = -radius; dy <= radius; dy++) {
        for (int dx = -radius; dx <= radius; dx++) {
          if (dx * dx + dy * dy <= radius * radius) Pixel(out, cx + dx, rowY[i] + dy, true);
        }
      }
      // Tick: (cx-5, rowY+1) -> (cx-1, rowY+5) -> (cx+6, rowY-5), 2 px wide.
      const int x1 = cx - 5, y1 = rowY[i] + 1;
      const int x2 = cx - 1, y2 = rowY[i] + 5;
      const int x3 = cx + 6, y3 = rowY[i] - 5;
      auto stroke = [out](int ax, int ay, int bx, int by) {
        const int steps = abs(bx - ax) > abs(by - ay) ? abs(bx - ax) : abs(by - ay);
        for (int s = 0; s <= steps; s++) {
          const int x = ax + (bx - ax) * s / steps;
          const int y = ay + (by - ay) * s / steps;
          Fill(out, x, y, 2, 2, false);
        }
      };
      stroke(x1, y1, x2, y2);
      stroke(x2, y2, x3, y3);
    } else {
      // Hollow circle: an 18 px ring.
      for (int a = 0; a < 40; a++) {
        const float t = 6.2831853f * static_cast<float>(a) / 40.0f;
        const int px = cx + static_cast<int>(9.0f * cosf(t));
        const int py = rowY[i] + static_cast<int>(9.0f * sinf(t));
        Fill(out, px, py, 2, 2, true);
      }
    }
    Fill(out, x0 + 56, rowY[i] - 5, 88 - i * 18, 10, true);
  }
  if (version != nullptr && version[0] != '\0') {
    char line[24];
    snprintf(line, sizeof(line), "V%s", version);
    const int tw = TextWidth(line, 2);
    DrawString(out, (static_cast<int>(kWidth) - tw) / 2, 218, line, 2);
  }
  const char* boot = "TODAY";
  DrawString(out, (static_cast<int>(kWidth) - TextWidth(boot, 3)) / 2, 250, boot, 3);
}

}  // namespace screens
