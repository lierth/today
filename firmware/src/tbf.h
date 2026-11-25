// TBF1 frame parser. The wire format is shared with internal/frame/frame.go
// (format 3: widget table after the footer bindings).
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace tbf {

constexpr size_t kMagicLen = 4;
constexpr uint8_t kFormatVersion = 3;
constexpr size_t kMaxRows = 16;
constexpr size_t kMaxFooters = 8;
constexpr size_t kMaxWidgets = 8;
constexpr size_t kBitmapBytes = 400 * 300 / 8;
constexpr size_t kMaxFrameBytes = 24 + kMaxRows * 20 + 1 + kMaxFooters * 2 +
                                 1 + kMaxWidgets * 9 + kBitmapBytes + 4;

constexpr uint8_t kFlagForceFull = 1 << 0;
constexpr uint8_t kFlagFocus = 1 << 1;

constexpr uint8_t kRowDone = 1 << 0;
constexpr uint8_t kRowSummary = 1 << 1;
constexpr uint8_t kRowHero = 1 << 2;

constexpr uint8_t kActionUp = 1;
constexpr uint8_t kActionDown = 2;
constexpr uint8_t kActionOK = 3;
constexpr uint8_t kActionRefresh = 4;
constexpr uint8_t kActionLongOK = 5;

constexpr uint8_t kWidgetClock = 1;
constexpr uint8_t kWidgetWifi = 2;
constexpr uint8_t kWidgetClimate = 3;
constexpr uint8_t kWidgetPower = 4;

struct Row {
  uint8_t id[16];
  uint16_t y;
  uint8_t flags;
  uint8_t height;  // reserved byte in legacy frames
};
static_assert(sizeof(Row) == 20, "NVS row records must remain compatible");

struct FooterButton {
  uint8_t gpio;
  uint8_t action;
};

struct Widget {
  uint8_t kind;
  uint16_t x;
  uint16_t y;
  uint16_t w;
  uint16_t h;
};

struct Frame {
  uint8_t flags;
  uint16_t width;
  uint16_t height;
  uint16_t rowHeight;
  uint16_t headerHeight;
  uint16_t footerHeight;
  uint16_t contentX;
  uint16_t contentWidth;
  uint8_t cursorRow;
  uint8_t cursorStyle;
  uint8_t rowCount;
  Row rows[kMaxRows];
  uint8_t footerCount;
  FooterButton footers[kMaxFooters];
  uint8_t widgetCount;
  Widget widgets[kMaxWidgets];
  const uint8_t* bitmap;
  size_t bitmapLen;
};

// Parses and validates one encoded frame. On success `out.bitmap` points into
// `data`, so the caller must keep the buffer alive until drawing is finished.
bool Parse(const uint8_t* data, size_t len, Frame* out);
bool SameID(const uint8_t a[16], const uint8_t b[16]);
void FormatID(const uint8_t id[16], char out[37]);
uint32_t CRC32(const uint8_t* data, size_t len);
const char* ParseError();

// Finds one widget by kind; returns nullptr when the server did not send it.
const Widget* Find(const Frame& frame, uint8_t kind);

}  // namespace tbf
