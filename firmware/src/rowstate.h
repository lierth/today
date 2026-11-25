#pragma once

#include "tbf.h"

namespace rowstate {

inline uint32_t DoneMask(const tbf::Row* rows, uint8_t count) {
  uint32_t mask = 0;
  for (uint8_t i = 0; i < count; i++) {
    if (rows[i].flags & tbf::kRowDone) mask |= (1UL << i);
  }
  return mask;
}

inline uint32_t ApplyOp(uint32_t mask, const tbf::Row* rows, uint8_t count,
                        const uint8_t id[16], bool done) {
  for (uint8_t i = 0; i < count; i++) {
    if (!(rows[i].flags & tbf::kRowSummary) && tbf::SameID(rows[i].id, id)) {
      return done ? (mask | (1UL << i)) : (mask & ~(1UL << i));
    }
  }
  return mask;
}

// Compose in the row-major TBF bitmap (1 = black), before ST7305 packing.
inline void Apply(uint8_t* bitmap, const tbf::Row* rows, uint8_t count,
                  uint32_t done, uint8_t cursor, int contentX, int rowHeight,
                  bool focus = false) {
  auto pixel = [bitmap](int x, int y, bool black) {
    if (x < 0 || x >= 400 || y < 0 || y >= 300) return;
    uint8_t& byte = bitmap[y * 50 + x / 8];
    const uint8_t mask = static_cast<uint8_t>(0x80 >> (x % 8));
    if (black) byte |= mask;
    else byte &= static_cast<uint8_t>(~mask);
  };
  for (uint8_t i = 0; i < count; i++) {
    if (rows[i].flags & tbf::kRowSummary) continue;
    const bool checked = (done & (1UL << i)) != 0;
    if (focus && (rows[i].flags & tbf::kRowHero)) {
      // A pending offline check sits beside the time badge, clear of the title.
      const int x = contentX + 56, y = rows[i].y + 6;
      for (int yy = 0; yy < 10; yy++) for (int xx = 0; xx < 10; xx++) pixel(x+xx,y+yy,checked);
      if (checked) {
        for (int j = 0; j < 3; j++) pixel(x+2+j,y+4+j,false);
        for (int j = 0; j < 5; j++) pixel(x+4+j,y+6-j,false);
      }
      continue;
    }
    const int top = rows[i].y + (rowHeight - 10) / 2;
    for (int y = 0; y < 10; y++) {
      for (int x = 0; x < 10; x++) {
        const bool edge = ((y == 0 || y == 9) && x >= 2 && x < 8) ||
                          ((x == 0 || x == 9) && y >= 2 && y < 8) ||
                          ((x == 1 || x == 8) && (y == 1 || y == 8));
        pixel(contentX + x, top + y, checked || edge);
      }
    }
    if (checked) {
      for (int j = 0; j < 3; j++) pixel(contentX + 2 + j, top + 4 + j, false);
      for (int j = 0; j < 5; j++) pixel(contentX + 4 + j, top + 6 - j, false);
    }
  }
  if (cursor >= count) return;
  if (focus) {
    if (!(rows[cursor].flags & (tbf::kRowHero | tbf::kRowSummary))) {
      for (int y = rows[cursor].y+3; y < rows[cursor].y+rowHeight-3; y++) {
        for (int x = contentX-8; x < contentX-5; x++) pixel(x,y,true);
      }
    }
    return;
  }
  for (int y = rows[cursor].y; y < rows[cursor].y + rowHeight && y < 300; y++) {
    for (int x = 0; x < 50; x++) bitmap[y * 50 + x] ^= 0xFF;
  }
}

inline uint8_t Next(const tbf::Row* rows, uint8_t count, uint8_t cursor) {
  if (count == 0) return 0;
  for (uint8_t step = 1; step <= count; step++) {
    const uint8_t next = static_cast<uint8_t>((cursor + step) % count);
    if (!(rows[next].flags & tbf::kRowSummary)) return next;
  }
  return 0;
}

}  // namespace rowstate
