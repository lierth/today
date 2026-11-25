// ST7305 1-bit panel packer. Pure C++: no Arduino headers, so the same code
// runs on the device and in the host test (firmware/test/pack_test.cpp).
//
// TBF bitmaps are row-major, MSB first, 50 bytes per 400 px row. ST7305 in
// landscape mode packs 2 columns x 4 rows into one byte:
//
//   inv_y        = height - 1 - y
//   byte_index   = (x >> 1) * (height >> 2) + (inv_y >> 2)
//   bit          = 7 - (((inv_y & 3) << 1) | (x & 1))
//
// A set bit is white: the init sequence enables display inversion (0x21), so
// a cleared bit shows as black ink. See docs/flashing.md for firmware details.
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace st7305 {

constexpr uint16_t kWidth = 400;
constexpr uint16_t kHeight = 300;
constexpr size_t kPackedBytes = (kWidth / 2) * (kHeight / 4);  // 15,000

// Index and bit mask for one landscape pixel. Both outputs are required to
// keep the hot path branch-free.
struct Cell {
  uint16_t index;
  uint8_t mask;
};

Cell CellFor(uint16_t x, uint16_t y);

// Packs a TBF row-major, MSB-first bitmap (1 = black) into the 15,000-byte
// ST7305 buffer (1 = white). Returns false on bad geometry.
bool Pack(const uint8_t* tbf, size_t tbfLen, uint8_t* out, size_t outLen);

}  // namespace st7305
