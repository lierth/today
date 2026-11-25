#pragma once
#include <array>
#include <vector>
#include "ESP_I2S.h"
inline int testFailReg = -1;
inline std::array<uint8_t, 256> testRegisters{};
struct TestWire {
  std::vector<uint8_t> bytes;
  uint8_t reg = 0;
  void beginTransmission(uint8_t) { bytes.clear(); }
  void write(uint8_t b) { bytes.push_back(b); }
  int endTransmission(bool = true) {
    reg = bytes[0];
    if (reg == testFailReg) return 1;
    if (bytes.size() == 2) {
      // Codec writes must happen with the master clock already running.
      if (!testI2sStarted) return 1;
      testRegisters[reg] = bytes[1];
    }
    return 0;
  }
  int requestFrom(uint8_t, uint8_t) { return 1; }
  uint8_t read() { return testRegisters[reg]; }
};
inline TestWire Wire;
