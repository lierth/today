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
}
