#include "sound_synth.h"

#include <math.h>

namespace sound {
namespace {

constexpr float kPi = 3.14159265358979323846f;

// Writes a sine of `freq` for `ms`, shaped by the raised-cosine envelope, and
// returns the new cursor. Samples are added into `out`, never clipped here —
// the amplitudes are chosen so a single cue cannot overflow.
size_t Note(int16_t* out, size_t at, size_t cap, float freq, int ms, uint8_t volume) {
  if (freq <= 0 || ms <= 0 || volume == 0) return at;
  const size_t n = static_cast<size_t>(kSampleRate) * static_cast<size_t>(ms) / 1000;
  if (at + n > cap) return at;
  const int16_t amp = Amplitude(volume);
  for (size_t i = 0; i < n; i++) {
    const float phase = 2.0f * kPi * freq * static_cast<float>(i) / static_cast<float>(kSampleRate);
    const float v = sinf(phase) * static_cast<float>(amp) *
                    (static_cast<float>(Envelope(i, n)) / 32767.0f);
    out[at + i] = static_cast<int16_t>(v);
  }
  return at + n;
}

size_t Silence(int16_t* out, size_t at, size_t cap, int ms) {
  const size_t n = static_cast<size_t>(kSampleRate) * static_cast<size_t>(ms) / 1000;
  if (at + n > cap) return at;
  for (size_t i = 0; i < n; i++) out[at + i] = 0;
  return at + n;
}

}  // namespace

int16_t Envelope(size_t i, size_t n) {
  if (n == 0 || i >= n) return 0;
  // Raised cosine over the whole note: 0 at both ends, 1 in the middle.
  // The first and last sample are exactly zero, which is what keeps the
  // speaker from clicking.
  const float t = static_cast<float>(i) / static_cast<float>(n - 1);
  const float w = 0.5f - 0.5f * cosf(2.0f * kPi * t);
  return static_cast<int16_t>(w * 32767.0f);
}

int16_t Amplitude(uint8_t volume) {
  if (volume > 100) volume = 100;
  // 60 maps to 60% of full scale; full scale would clip on the small speaker.
  return static_cast<int16_t>(32767L * volume / 100);
}

size_t Render(Cue cue, uint8_t volume, int16_t* out, size_t cap) {
  if (out == nullptr || volume == 0) return 0;
  size_t at = 0;
  switch (cue) {
    case Cue::kCheck:
      at = Note(out, at, cap, 1320.0f, 60, volume);  // E6, short
      break;
    case Cue::kUncheck:
      at = Note(out, at, cap, 660.0f, 90, volume);  // E5, lower
      break;
    case Cue::kDone:
      at = Note(out, at, cap, 880.0f, 90, volume);   // A5
      at = Note(out, at, cap, 1108.0f, 90, volume);  // C#6
      at = Note(out, at, cap, 1320.0f, 140, volume); // E6
      break;
    case Cue::kAlarm:
      // Three chimes, half a second apart, as the brief asks.
      for (int i = 0; i < 3; i++) {
        at = Note(out, at, cap, 1046.0f, 300, volume);  // C6
        if (i < 2) at = Silence(out, at, cap, 500);
      }
      break;
    case Cue::kNone:
    default:
      return 0;
  }
  return at;
}

}  // namespace sound
