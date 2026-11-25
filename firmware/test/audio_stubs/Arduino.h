#pragma once
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
constexpr int LOW = 0, HIGH = 1, OUTPUT = 1;
inline uint32_t testMillis = 0;
inline int testPa = LOW;
inline uint32_t millis() { return testMillis; }
inline void pinMode(int, int) {}
inline void digitalWrite(int, int value) { testPa = value; }
struct TestSerial {
  void println(const char*) {}
  template <typename... Args> void printf(const char*, Args...) {}
};
inline TestSerial Serial;
