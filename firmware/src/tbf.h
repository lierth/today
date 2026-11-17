// TBF1 frame parser. The wire format is shared with internal/frame/frame.go.
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace tbf {

constexpr size_t kMagicLen = 4;
constexpr size_t kMaxRows = 16;
constexpr size_t kMaxFooters = 8;

constexpr uint8_t kFlagForceFull = 1 << 0;

constexpr uint8_t kRowDone = 1 << 0;
constexpr uint8_t kRowSummary = 1 << 1;

constexpr uint8_t kActionUp = 1;
constexpr uint8_t kActionDown = 2;
constexpr uint8_t kActionOK = 3;
constexpr uint8_t kActionRefresh = 4;
constexpr uint8_t kActionLongOK = 5;

struct Row {
  uint8_t id[16];
  uint16_t y;
  uint8_t flags;
};

struct FooterButton {
  uint8_t gpio;
  uint8_t action;
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

}  // namespace tbf
