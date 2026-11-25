#include <assert.h>
#include <stdio.h>
#include <vector>

#include "tbf.h"

int main(int argc, char** argv) {
  assert(argc == 2);
  FILE* f = fopen(argv[1], "rb");
  assert(f != nullptr);
  std::vector<uint8_t> encoded;
  int c;
  while ((c = fgetc(f)) != EOF) encoded.push_back(static_cast<uint8_t>(c));
  fclose(f);
  assert(encoded.size() > tbf::kBitmapBytes);
  assert(encoded.size() <= tbf::kMaxFrameBytes);
  tbf::Frame frame {};
  assert(tbf::Parse(encoded.data(), encoded.size(), &frame));
  assert(frame.bitmapLen == tbf::kBitmapBytes);
  assert(frame.widgetCount == 4);
  assert(frame.flags & tbf::kFlagFocus);
  assert(frame.rowCount > 0);
  for (int i = 0; i < frame.rowCount; i++) {
    assert(!(frame.rows[i].flags & tbf::kRowHero));
    assert(frame.rows[i].height == 24);
  }
  encoded[24+19] = 255;
  const uint32_t crc = tbf::CRC32(encoded.data(),encoded.size()-4);
  for (int i=0;i<4;i++) encoded[encoded.size()-4+i] = static_cast<uint8_t>(crc>>(24-8*i));
  assert(!tbf::Parse(encoded.data(),encoded.size(),&frame));
  // Legacy frames leave the per-row byte reserved and use a uniform height.
  encoded[6] &= static_cast<uint8_t>(~tbf::kFlagFocus);
  encoded[13]=0; encoded[14]=44; encoded[15]=0; encoded[16]=0;
  for (int i=0;i<encoded[23];i++) {
    encoded[24+i*20+16]=0;
    encoded[24+i*20+17]=static_cast<uint8_t>(44+i*24);
    encoded[24+i*20+18] &= static_cast<uint8_t>(~tbf::kRowHero);
    encoded[24+i*20+19]=0;
  }
  const uint32_t legacyCRC=tbf::CRC32(encoded.data(),encoded.size()-4);
  for (int i=0;i<4;i++) encoded[encoded.size()-4+i]=static_cast<uint8_t>(legacyCRC>>(24-8*i));
  assert(tbf::Parse(encoded.data(),encoded.size(),&frame));
  assert(!(frame.flags&tbf::kFlagFocus) && frame.rows[0].height==0);
}
