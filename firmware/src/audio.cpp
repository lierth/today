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

// One buffer in flight. The tones are at most ~1.9 s, which is 60 KB at
// 16 kHz mono; a fixed 8 KB ring would truncate the alarm, so double buffer
// the largest cue instead (allocated once at Begin, never freed).
constexpr size_t kMaxSamples = 3 * (16000 / 3) + 2 * (16000 / 2);
int16_t* s_buf = nullptr;
volatile size_t s_len = 0;
volatile size_t s_pos = 0;
volatile bool s_playing = false;

// ES8311 registers, DAC playback only. Values follow the vendor example's
// es8311 init table (07_Audio_Test); the equalizer banks (0x33..0x74) are left
// at their reset values and skipped, which is why the table stops at 0x32.
struct Reg {
  uint8_t reg;
  uint8_t value;
};
constexpr Reg kInit[] = {
    {0x00, 0x1F},  // reset
    {0x00, 0x00},  // release reset
    {0x01, 0x3F},  // MCLK on, codec in slave mode
    {0x02, 0x00},  // clock pre-divider: MCLK = 256 * fs
    {0x03, 0x10},  // ADC OSR (unused, kept at reset value)
    {0x04, 0x10},  // DAC OSR
    {0x05, 0x00},  // clock dividers, LRCK = fs
    {0x06, 0x03},  // BCLK divider
    {0x07, 0x00},  // LRCK high bits
    {0x08, 0xFF},  // LRCK low bits (256 * fs)
    {0x09, 0x0C},  // serial port in: 16-bit, I2S
    {0x0A, 0x0C},  // serial port out: 16-bit, I2S
    {0x0B, 0x00},  // system: normal
    {0x0C, 0x00},
    {0x0D, 0x01},  // power up the analog block
    {0x0E, 0x02},  // enable analog
    {0x0F, 0x00},  // DAC not muted
    {0x10, 0x1F},  // analog input off (no ADC on this board)
    {0x12, 0x00},  // DAC volume 0 dB
    {0x13, 0x10},  // output gain
    {0x31, 0x00},  // DAC unmute path
    {0x32, 0xBF},  // DAC ramp
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

// Feeds the codec while s_playing. Runs on the Arduino loop task, called from
// audio::Loop(), so no separate task or stack is needed: a 256-sample block is
// 16 ms of audio, far longer than one loop iteration spends elsewhere.
void Pump() {
  if (!s_playing || s_i2s.txChan() == nullptr) return;
  if (s_pos >= s_len) {
    s_playing = false;
    Silence();
    return;
  }
  const size_t want = 256;
  size_t n = s_len - s_pos;
  if (n > want) n = want;
  s_i2s.write(reinterpret_cast<const uint8_t*>(s_buf + s_pos), n * sizeof(int16_t));
  s_pos += n;
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
  for (size_t i = 0; i < sizeof(kInit) / sizeof(kInit[0]); i++) {
    if (!WriteReg(kInit[i].reg, kInit[i].value)) {
      Serial.printf("audio: ES8311 reg 0x%02X failed\n", kInit[i].reg);
      return false;
    }
  }

  s_i2s.setPins(board::kPinAudioBclk, board::kPinAudioWs, board::kPinAudioDout,
                board::kPinAudioDin, board::kPinAudioMclk);
  if (!s_i2s.begin(I2S_MODE_STD, 16000, I2S_DATA_BIT_WIDTH_16BIT, I2S_SLOT_MODE_MONO)) {
    Serial.printf("audio: I2S begin failed (%d)\n", s_i2s.lastError());
    return false;
  }
  s_buf = static_cast<int16_t*>(malloc(kMaxSamples * sizeof(int16_t)));
  if (s_buf == nullptr) {
    Serial.println("audio: no buffer");
    return false;
  }
  s_ready = true;
  Serial.println("audio: ES8311 ready");
  return true;
}

bool Play(const int16_t* samples, size_t count) {
  if (!s_ready || samples == nullptr || count == 0) return false;
  if (s_playing) return false;  // a second cue on top of the first is noise
  if (count == 0) return false;
  if (count > kMaxSamples) count = kMaxSamples;
  memcpy(s_buf, samples, count * sizeof(int16_t));
  s_len = count;
  s_pos = 0;
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
