#include "audio.h"

#include <Arduino.h>
#include <ESP_I2S.h>
#include <Wire.h>
#include <string.h>

#include "board.h"

namespace audio {
namespace {

I2SClass s_i2s;
bool s_ready = false;
bool s_paOn = false;

// One buffer for the longest cue, allocated once.
constexpr size_t kMaxSamples = 3 * (16000 / 3) + 2 * (16000 / 2);
int16_t* s_buf = nullptr;
volatile size_t s_len = 0;
volatile size_t s_pos = 0;
volatile bool s_playing = false;
uint32_t s_drainUntil = 0;
// The pinned Arduino I2S driver queues six 240-frame DMA buffers (90 ms).
constexpr uint32_t kDrainMs = 100;

// Waveshare's ES8311 open/start sequence, DAC only, 16 kHz / 4.096 MHz MCLK.
struct Reg {
  uint8_t reg;
  uint8_t value;
};
constexpr Reg kInit[] = {
    {0x44, 0x08},  // I2C noise immunity; vendor repeats the first write
    {0x44, 0x08},
    {0x01, 0x30},
    {0x02, 0x00},  // clock pre-divider: MCLK = 256 * fs
    {0x03, 0x10},  // ADC OSR (unused, kept at reset value)
    {0x16, 0x24},
    {0x04, 0x20},  // DAC OSR for 16 kHz / 4.096 MHz
    {0x05, 0x00},  // clock dividers, LRCK = fs
    {0x06, 0x03},  // BCLK divider
    {0x07, 0x00},  // LRCK high bits
    {0x08, 0xFF},  // LRCK low bits (256 * fs)
    {0x09, 0x0C},  // serial port in: 16-bit, I2S
    {0x0A, 0x4C},  // unused ADC output disabled
    {0x0B, 0x00},  // system: normal
    {0x0C, 0x00},
    {0x10, 0x1F},
    {0x11, 0x7F},
    {0x00, 0x80},  // CSM_ON: leaving this clear powers down the codec
    {0x01, 0x3F},  // MCLK on, codec in slave mode
    {0x13, 0x10},  // output gain
    {0x1B, 0x0A},
    {0x1C, 0x6A},
    {0x44, 0x58},  // internal DAC reference
    {0x17, 0xBF},
    {0x0E, 0x02},
    {0x12, 0x00},  // power up DAC
    {0x14, 0x1A},
    {0x0D, 0x01},  // power up analog block
    {0x15, 0x40},
    {0x37, 0x08},
    {0x45, 0x00},
    {0x31, 0x00},  // DAC unmute
    {0x32, 0xBF},  // DAC volume 0 dB (software scales each cue)
};

bool WriteReg(uint8_t reg, uint8_t value) {
  Wire.beginTransmission(board::kAddrEs8311);
  Wire.write(reg);
  Wire.write(value);
  return Wire.endTransmission() == 0;
}

bool ReadReg(uint8_t reg, uint8_t* out) {
  Wire.beginTransmission(board::kAddrEs8311);
  Wire.write(reg);
  if (Wire.endTransmission(false) != 0) return false;
  if (Wire.requestFrom(board::kAddrEs8311, static_cast<uint8_t>(1)) != 1) return false;
  *out = Wire.read();
  return true;
}

// A 512-sample block covers the main loop's 20 ms delay.
void Pump() {
  if (!s_playing || s_i2s.txChan() == nullptr) return;
  if (s_pos >= s_len) {
    if (static_cast<int32_t>(millis() - s_drainUntil) >= 0) {
      Serial.printf("audio: played %u samples\n", static_cast<unsigned>(s_len));
      Silence();
    }
    return;
  }
  const size_t want = 512;
  size_t n = s_len - s_pos;
  if (n > want) n = want;
  // The codec expects stereo slots; send the same cue on both channels.
  int16_t stereo[512 * 2];
  for (size_t i = 0; i < n; i++) stereo[2*i] = stereo[2*i+1] = s_buf[s_pos+i];
  const size_t written = s_i2s.write(reinterpret_cast<const uint8_t*>(stereo), n * 2 * sizeof(int16_t));
  if (written == 0 || written % (2 * sizeof(int16_t)) != 0) {
    Serial.printf("audio: I2S write failed (%d)\n", s_i2s.lastError());
    Silence();
    return;
  }
  s_pos += written / (2 * sizeof(int16_t));
  if (s_pos >= s_len) s_drainUntil = millis() + kDrainMs;
}

}  // namespace

bool Begin() {
  // GPIO46 is a strapping pin; drive it low before anything else so a reset
  // during init cannot leave the amplifier on.
  pinMode(board::kPinAudioPa, OUTPUT);
  digitalWrite(board::kPinAudioPa, LOW);

  uint8_t id = 0;
  if (!ReadReg(0xFD, &id)) {
    Serial.println("audio: ES8311 not found");
    return false;
  }
  s_i2s.setPins(board::kPinAudioBclk, board::kPinAudioWs, board::kPinAudioDout,
                -1, board::kPinAudioMclk);
  if (!s_i2s.begin(I2S_MODE_STD, 16000, I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_STEREO)) {
    Serial.printf("audio: I2S begin failed (%d)\n", s_i2s.lastError());
    return false;
  }
  for (size_t i = 0; i < sizeof(kInit) / sizeof(kInit[0]); i++) {
    if (!WriteReg(kInit[i].reg, kInit[i].value)) {
      Serial.printf("audio: ES8311 reg 0x%02X failed\n", kInit[i].reg);
      s_i2s.end();
      return false;
    }
  }

  uint8_t state = 0;
  if (!ReadReg(0x00, &state) || state != 0x80) {
    Serial.println("audio: ES8311 did not start");
    s_i2s.end();
    return false;
  }
  s_buf = static_cast<int16_t*>(malloc(kMaxSamples * sizeof(int16_t)));
  if (s_buf == nullptr) {
    Serial.println("audio: no buffer");
    s_i2s.end();
    return false;
  }
  s_ready = true;
  Serial.println("audio: ES8311 ready");
  return true;
}

bool Play(const int16_t* samples, size_t count) {
  if (!s_ready || samples == nullptr || count == 0) return false;
  if (s_playing) return false;  // a second cue on top of the first is noise
  if (count > kMaxSamples) count = kMaxSamples;
  if (samples != s_buf) memcpy(s_buf, samples, count * sizeof(int16_t));
  s_len = count;
  s_pos = 0;
  s_drainUntil = 0;
  s_playing = true;
  // Amplifier on only for the duration; the strapping pin stays low otherwise.
  digitalWrite(board::kPinAudioPa, HIGH);
  s_paOn = true;
  return true;
}

bool Busy() { return s_playing; }

int16_t* Scratch(size_t* capacity) {
  if (capacity != nullptr) *capacity = s_buf != nullptr ? kMaxSamples : 0;
  return s_buf;
}

void Silence() {
  if (s_paOn) {
    digitalWrite(board::kPinAudioPa, LOW);
    s_paOn = false;
  }
  s_playing = false;
  s_len = 0;
  s_pos = 0;
}

// Loop must be called from the main loop; it feeds I2S and drops the
// amplifier when the buffer is done.
void Loop() { Pump(); }

}  // namespace audio
