// 5x8 font for the device widgets. Pure data, no Arduino dependency, so the
// host test can include it and assert every string the firmware draws has a
// glyph.
//
// Eight rows, not seven: the classic glcdfont cell stores a descender in
// bit 7 (g j p q y and friends), and drawing seven rows truncates it — the
// boot title read "Todaч" and "http" lost the bottom of its p.
//
// Printable ASCII 0x20-0x7E comes from the classic glcdfont table shipped
// with Adafruit GFX (BSD license, see firmware/NOTICE). One byte per column,
// bit 0 = top row.
//
// The degree sign is not in this table: DrawClimate draws it as a 3x3 ring
// (see widgets.cpp) and never passes "°" to Text(), so UTF-8 never reaches
// the font and cannot degrade into "??".
#pragma once

#include <stdint.h>

namespace font {

// kGlyphHeight is the cell's row count; the advance is 6 px per scale unit
// (five columns plus one of spacing).
constexpr int kGlyphHeight = 8;

struct Glyph {
  char c;
  uint8_t col[5];
};

constexpr Glyph kGlyphs[] = {
#include "font_ascii.inc"
};

// Finds a glyph, or nullptr when the character is outside the table.
inline const Glyph* Find(char c) {
  for (const Glyph& g : kGlyphs) {
    if (g.c == c) return &g;
  }
  return nullptr;
}

}  // namespace font
