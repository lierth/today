// Exercise the real driver with a fake codec and partial I2S writes.
#include <assert.h>
#include <stdio.h>
#include <string.h>
#include <Wire.h>
#include "audio.h"

int main() {
  testFailReg = 0x11;
  assert(!audio::Begin());
  assert(!testI2sStarted && testPa == LOW);
  testFailReg = -1;
  assert(audio::Begin());
  assert(testRegisters[0x00] == 0x80);  // CSM_ON, slave mode
  assert(testRegisters[0x04] == 0x20);  // 16 kHz DAC clock
  assert(testRegisters[0x14] == 0x1A && testRegisters[0x0D] == 0x01);
  assert(testRegisters[0x32] == 0xBF && testPa == LOW);

  int16_t samples[600];
  for (size_t i = 0; i < 600; ++i) samples[i] = static_cast<int16_t>(i);
  assert(audio::Play(samples, 600));
  assert(!audio::Play(samples, 600));
  testWriteLimit = 400;  // Only 100 stereo frames are accepted.
  for (int i = 0; i < 6; ++i) { audio::Loop(); testMillis += 20; }
  assert(testOutput.size() == 2 * sizeof(samples));
  for (size_t i = 0; i < 600; i++) {
    int16_t left, right;
    memcpy(&left, testOutput.data()+4*i, 2);
    memcpy(&right, testOutput.data()+4*i+2, 2);
    assert(left == samples[i] && right == samples[i]);
  }
  audio::Loop();
  assert(audio::Busy() && testPa == HIGH);  // DMA tail still playing.
  testMillis += 100;
  audio::Loop();
  assert(!audio::Busy() && testPa == LOW);

  assert(audio::Play(samples, 600));
  testWriteLimit = 0;
  audio::Loop();
  assert(!audio::Busy() && testPa == LOW);  // Failure must not leave PA on.
  puts("audio driver tests passed");
}
