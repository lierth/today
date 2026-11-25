#pragma once
#include <algorithm>
#include <vector>
#include "Arduino.h"
constexpr int I2S_MODE_STD = 0, I2S_DATA_BIT_WIDTH_16BIT = 16, I2S_SLOT_MODE_STEREO = 2;
inline bool testI2sStarted = false;
inline size_t testWriteLimit = SIZE_MAX;
inline std::vector<uint8_t> testOutput;
class I2SClass {
 public:
  void setPins(int, int, int, int, int) {}
  bool begin(int, int, int, int) { testI2sStarted = true; return true; }
  bool end() { testI2sStarted = false; return true; }
  void* txChan() { return testI2sStarted ? this : nullptr; }
  int lastError() { return -1; }
  size_t write(const uint8_t* data, size_t count) {
    const size_t n = std::min(count, testWriteLimit);
    testOutput.insert(testOutput.end(), data, data + n);
    return n;
  }
};
