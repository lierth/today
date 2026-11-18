// Host-side test for the ST7305 packer and the TBF parser. No Arduino
// dependency: build and run on the Mac with
//
//   clang++ -std=c++17 -Wall -Wextra -I firmware/src \
//     firmware/test/pack_test.cpp firmware/src/st7305.cpp firmware/src/tbf.cpp \
//     -o /tmp/pack_test && /tmp/pack_test
//
// It also parses a fmt-3 frame produced by `todayd preview` (pass its path as
// argv[1]) and checks the widget table matches the server contract.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "night.h"
#include "st7305.h"
#include "tbf.h"

namespace {

int g_failures = 0;

void Expect(bool ok, const char* what) {
  if (!ok) {
    printf("FAIL %s\n", what);
    g_failures++;
  }
}

void SetBlack(uint8_t* bmp, int x, int y) {
  bmp[static_cast<size_t>(y) * (st7305::kWidth / 8) + (x >> 3)] |=
      static_cast<uint8_t>(0x80 >> (x & 7));
}

bool IsWhite(const uint8_t* packed, int x, int y) {
  const st7305::Cell cell = st7305::CellFor(static_cast<uint16_t>(x), static_cast<uint16_t>(y));
  return (packed[cell.index] & cell.mask) != 0;
}

void TestCorners() {
  uint8_t tbf[st7305::kPackedBytes];
  memset(tbf, 0, sizeof(tbf));
  SetBlack(tbf, 0, 0);
  SetBlack(tbf, st7305::kWidth - 1, 0);
  SetBlack(tbf, 0, st7305::kHeight - 1);
  SetBlack(tbf, st7305::kWidth - 1, st7305::kHeight - 1);
  SetBlack(tbf, 1, 0);
  SetBlack(tbf, 0, 1);

  uint8_t packed[st7305::kPackedBytes];
  Expect(st7305::Pack(tbf, sizeof(tbf), packed, sizeof(packed)), "pack corners");

  Expect(!IsWhite(packed, 0, 0), "pixel (0,0) black");
  Expect(!IsWhite(packed, st7305::kWidth - 1, 0), "pixel (399,0) black");
  Expect(!IsWhite(packed, 0, st7305::kHeight - 1), "pixel (0,299) black");
  Expect(!IsWhite(packed, st7305::kWidth - 1, st7305::kHeight - 1), "pixel (399,299) black");
  Expect(!IsWhite(packed, 1, 0), "pixel (1,0) black");
  Expect(!IsWhite(packed, 0, 1), "pixel (0,1) black");
  Expect(IsWhite(packed, 2, 0), "pixel (2,0) stays white");

  // The formula must be a bijection: every pixel gets a distinct cell+mask,
  // and the 120,000 bits exactly cover the 15,000-byte buffer.
  static bool seen[st7305::kPackedBytes * 8];
  memset(seen, 0, sizeof(seen));
  int duplicates = 0;
  for (int y = 0; y < st7305::kHeight; y++) {
    for (int x = 0; x < st7305::kWidth; x++) {
      const st7305::Cell cell = st7305::CellFor(static_cast<uint16_t>(x), static_cast<uint16_t>(y));
      if (cell.index >= st7305::kPackedBytes) {
        Expect(false, "cell outside buffer");
        continue;
      }
      int bit = 0;
      while (((cell.mask >> bit) & 1) == 0) bit++;  // bit set position 0..7
      const size_t slot = static_cast<size_t>(cell.index) * 8 + static_cast<size_t>(bit);
      if (seen[slot]) duplicates++;
      seen[slot] = true;
    }
  }
  Expect(duplicates == 0, "every pixel has a unique cell+mask");
}

void TestAllBlack() {
  uint8_t tbf[st7305::kPackedBytes];
  memset(tbf, 0xFF, sizeof(tbf));
  uint8_t packed[st7305::kPackedBytes];
  Expect(st7305::Pack(tbf, sizeof(tbf), packed, sizeof(packed)), "pack all black");
  for (size_t i = 0; i < sizeof(packed); i++) {
    if (packed[i] != 0x00) {
      Expect(false, "all-black buffer is not zero");
      return;
    }
  }
}

void TestAllWhite() {
  uint8_t tbf[st7305::kPackedBytes];
  memset(tbf, 0x00, sizeof(tbf));
  uint8_t packed[st7305::kPackedBytes];
  Expect(st7305::Pack(tbf, sizeof(tbf), packed, sizeof(packed)), "pack all white");
  for (size_t i = 0; i < sizeof(packed); i++) {
    if (packed[i] != 0xFF) {
      Expect(false, "all-white buffer is not 0xFF");
      return;
    }
  }
}

void TestNightWindow() {
  // 23:00-07:00: 22:59 outside, 23:00 inside, 03:00 inside, 06:59 inside,
  // 07:00 outside. start == end disables the window.
  Expect(!night::Inside(22 * 60 + 59, 23 * 60, 7 * 60), "22:59 outside");
  Expect(night::Inside(23 * 60, 23 * 60, 7 * 60), "23:00 inside");
  Expect(night::Inside(3 * 60, 23 * 60, 7 * 60), "03:00 inside");
  Expect(night::Inside(6 * 60 + 59, 23 * 60, 7 * 60), "06:59 inside");
  Expect(!night::Inside(7 * 60, 23 * 60, 7 * 60), "07:00 outside");
  Expect(!night::Inside(12 * 60, 23 * 60, 7 * 60), "12:00 outside");
  Expect(!night::Inside(23 * 60, 23 * 60, 23 * 60), "start==end disables");
  // A same-day window (lunch nap) still works.
  Expect(night::Inside(13 * 60, 12 * 60, 14 * 60), "13:00 in 12-14");
  Expect(!night::Inside(14 * 60, 12 * 60, 14 * 60), "14:00 outside 12-14");
}

void TestFrameFile(const char* path) {
  FILE* f = fopen(path, "rb");
  if (f == nullptr) {
    printf("SKIP frame %s (not found)\n", path);
    return;
  }
  fseek(f, 0, SEEK_END);
  const long n = ftell(f);
  fseek(f, 0, SEEK_SET);
  uint8_t* buf = static_cast<uint8_t*>(malloc(static_cast<size_t>(n)));
  Expect(buf != nullptr, "frame malloc");
  if (buf == nullptr) {
    fclose(f);
    return;
  }
  Expect(fread(buf, 1, static_cast<size_t>(n), f) == static_cast<size_t>(n), "frame read");
  fclose(f);

  tbf::Frame fr{};
  const bool ok = tbf::Parse(buf, static_cast<size_t>(n), &fr);
  if (!ok) {
    printf("FAIL frame parse: %s\n", tbf::ParseError());
    g_failures++;
    free(buf);
    return;
  }
  printf("frame %s: %ux%u header=%u footer=%u rows=%u widgets=%u bitmap=%zu\n", path,
         fr.width, fr.height, fr.headerHeight, fr.footerHeight, fr.rowCount,
         fr.widgetCount, fr.bitmapLen);
  Expect(fr.width == 400 && fr.height == 300, "frame geometry");
  Expect(fr.headerHeight == 44 && fr.footerHeight == 0, "v2 header/footer");
  Expect(fr.widgetCount == 4, "four widgets");
  // Order is a rendering detail; the contract is that each kind is present
  // with a rectangle inside the panel.
  const uint8_t kinds[] = {tbf::kWidgetClock, tbf::kWidgetWifi,
                           tbf::kWidgetClimate, tbf::kWidgetPower};
  for (uint8_t kind : kinds) {
    const tbf::Widget* w = tbf::Find(fr, kind);
    Expect(w != nullptr, "widget kind present");
    if (w != nullptr) {
      Expect(w->w > 0 && w->h > 0, "widget has area");
      Expect(uint32_t(w->x) + w->w <= fr.width && uint32_t(w->y) + w->h <= fr.height,
             "widget inside panel");
    }
  }
  Expect(fr.bitmapLen == st7305::kPackedBytes, "frame bitmap size");
  free(buf);
}

}  // namespace

int main(int argc, char** argv) {
  TestCorners();
  TestAllBlack();
  TestAllWhite();
  TestNightWindow();
  if (argc > 1) TestFrameFile(argv[1]);
  if (g_failures != 0) {
    printf("%d failure(s)\n", g_failures);
    return 1;
  }
  printf("ok\n");
  return 0;
}
