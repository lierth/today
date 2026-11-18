#include "st7305.h"

#include <string.h>

namespace st7305 {
namespace {

constexpr size_t kRowBytes = kWidth / 8;  // 50

}  // namespace

Cell CellFor(uint16_t x, uint16_t y) {
  const uint16_t invY = static_cast<uint16_t>(kHeight - 1 - y);
  return Cell{
      static_cast<uint16_t>((x >> 1) * (kHeight >> 2) + (invY >> 2)),
      static_cast<uint8_t>(1u << (7 - (((invY & 3) << 1) | (x & 1)))),
  };
}

bool Pack(const uint8_t* tbf, size_t tbfLen, uint8_t* out, size_t outLen) {
  if (tbf == nullptr || out == nullptr) return false;
  if (tbfLen != kRowBytes * kHeight || outLen != kPackedBytes) return false;
  memset(out, 0xFF, outLen);
  for (uint16_t y = 0; y < kHeight; y++) {
    const uint8_t* row = tbf + static_cast<size_t>(y) * kRowBytes;
    for (uint16_t x = 0; x < kWidth; x++) {
      if ((row[x >> 3] & (0x80 >> (x & 7))) == 0) continue;  // white in TBF
      const Cell cell = CellFor(x, y);
      out[cell.index] &= static_cast<uint8_t>(~cell.mask);
    }
  }
  return true;
}

}  // namespace st7305
