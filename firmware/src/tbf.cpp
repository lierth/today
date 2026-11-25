#include "tbf.h"

#include <stdio.h>
#include <string.h>

namespace tbf {
namespace {

const char* g_error = "";

uint16_t ReadBE16(const uint8_t* p) {
  return static_cast<uint16_t>((static_cast<uint16_t>(p[0]) << 8) | p[1]);
}

void SetError(const char* message) {
  g_error = message;
}

}  // namespace

const char* ParseError() { return g_error; }

uint32_t CRC32(const uint8_t* data, size_t len) {
  uint32_t crc = 0xFFFFFFFFu;
  for (size_t i = 0; i < len; i++) {
    crc ^= data[i];
    for (int bit = 0; bit < 8; bit++) {
      const uint32_t mask = static_cast<uint32_t>(-static_cast<int32_t>(crc & 1u));
      crc = (crc >> 1) ^ (0xEDB88320u & mask);
    }
  }
  return ~crc;
}

bool SameID(const uint8_t a[16], const uint8_t b[16]) {
  return memcmp(a, b, 16) == 0;
}

void FormatID(const uint8_t id[16], char out[37]) {
  static const char kHex[] = "0123456789abcdef";
  size_t at = 0;
  for (int i = 0; i < 16; i++) {
    if (i == 4 || i == 6 || i == 8 || i == 10) out[at++] = '-';
    out[at++] = kHex[id[i] >> 4];
    out[at++] = kHex[id[i] & 0x0F];
  }
  out[at] = '\0';
}

bool Parse(const uint8_t* data, size_t len, Frame* out) {
  SetError("");
  if (out == nullptr) {
    SetError("null frame");
    return false;
  }
  if (len < 7 + 14 + 3 + 1 + 1 + 4) {
    SetError("short frame");
    return false;
  }
  if (memcmp(data, "TBF1", 4) != 0) {
    SetError("bad magic");
    return false;
  }
  const uint8_t version = data[4];
  const uint8_t bits = data[5];
  if (version != kFormatVersion || bits != 1) {
    SetError("unsupported format");
    return false;
  }

  Frame f{};
  f.flags = data[6];
  const uint8_t* p = data + 7;
  f.width = ReadBE16(p + 0);
  f.height = ReadBE16(p + 2);
  f.rowHeight = ReadBE16(p + 4);
  f.headerHeight = ReadBE16(p + 6);
  f.footerHeight = ReadBE16(p + 8);
  f.contentX = ReadBE16(p + 10);
  f.contentWidth = ReadBE16(p + 12);
  p += 14;
  f.cursorRow = p[0];
  f.cursorStyle = p[1];
  f.rowCount = p[2];
  p += 3;

  if (f.width != 400 || f.height != 300 || f.rowHeight != 24 ||
      f.contentX + f.contentWidth > f.width) {
    SetError("bad geometry");
    return false;
  }
  if (f.rowCount > kMaxRows) {
    SetError("too many rows");
    return false;
  }
  if ((f.flags & kFlagFocus) && uint32_t(f.headerHeight) + f.footerHeight > f.height) {
    SetError("bad content bounds");
    return false;
  }
  const size_t rowsBytes = static_cast<size_t>(f.rowCount) * 20;
  if (static_cast<size_t>(p - data) + rowsBytes + 1 + 4 > len) {
    SetError("truncated rows");
    return false;
  }
  uint32_t previous = f.headerHeight;
  for (size_t i = 0; i < f.rowCount; i++) {
    Row& row = f.rows[i];
    memcpy(row.id, p, 16);
    row.y = ReadBE16(p + 16);
    row.flags = p[18];
    row.height = p[19];
    const uint32_t height = (f.flags & kFlagFocus) ? row.height : f.rowHeight;
    if (static_cast<uint32_t>(row.y) + height > f.height ||
        ((f.flags & kFlagFocus) && (height < f.rowHeight || row.y < previous ||
          uint32_t(row.y) + height > uint32_t(f.height) - f.footerHeight))) {
      SetError("bad row bounds");
      return false;
    }
    previous = uint32_t(row.y) + height;
    p += 20;
  }
  f.footerCount = *p++;
  if (f.footerCount > kMaxFooters) {
    SetError("too many footers");
    return false;
  }
  if (static_cast<size_t>(p - data) + static_cast<size_t>(f.footerCount) * 2 + 1 + 4 > len) {
    SetError("truncated footers");
    return false;
  }
  for (size_t i = 0; i < f.footerCount; i++) {
    f.footers[i].gpio = p[0];
    f.footers[i].action = p[1];
    p += 2;
  }
  f.widgetCount = *p++;
  if (f.widgetCount > kMaxWidgets) {
    SetError("too many widgets");
    return false;
  }
  if (static_cast<size_t>(p - data) + static_cast<size_t>(f.widgetCount) * 9 + 4 > len) {
    SetError("truncated widgets");
    return false;
  }
  for (size_t i = 0; i < f.widgetCount; i++) {
    Widget& w = f.widgets[i];
    w.kind = p[0];
    w.x = ReadBE16(p + 1);
    w.y = ReadBE16(p + 3);
    w.w = ReadBE16(p + 5);
    w.h = ReadBE16(p + 7);
    if (w.kind == 0 || static_cast<uint32_t>(w.x) + w.w > f.width ||
        static_cast<uint32_t>(w.y) + w.h > f.height) {
      SetError("bad widget");
      return false;
    }
    p += 9;
  }
  f.bitmap = p;
  f.bitmapLen = (static_cast<size_t>(f.width) / 8) * f.height;
  if (static_cast<size_t>(p - data) + f.bitmapLen + 4 != len) {
    SetError("bad bitmap length");
    return false;
  }
  const uint32_t want = (static_cast<uint32_t>(p[f.bitmapLen]) << 24) |
                        (static_cast<uint32_t>(p[f.bitmapLen + 1]) << 16) |
                        (static_cast<uint32_t>(p[f.bitmapLen + 2]) << 8) |
                        static_cast<uint32_t>(p[f.bitmapLen + 3]);
  if (CRC32(data, len - 4) != want) {
    SetError("bad crc");
    return false;
  }
  *out = f;
  return true;
}

const Widget* Find(const Frame& frame, uint8_t kind) {
  for (size_t i = 0; i < frame.widgetCount; i++) {
    if (frame.widgets[i].kind == kind) return &frame.widgets[i];
  }
  return nullptr;
}

}  // namespace tbf
