// Tone synthesis. Pure C++ (no Arduino, no I2S), so the host test can check
// the envelope and the range of every sample.
//
// Nothing is stored as a file: the four sounds are a few thousand samples of a
// sine at a fixed rate, shaped by a raised-cosine envelope so the first and
// last samples are exactly zero. A discontinuous start is what makes a small
// speaker click.
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace sound {

constexpr uint32_t kSampleRate = 16000;

// What the firmware can ask for.
enum class Cue : uint8_t {
  kNone = 0,
  kCheck = 1,   // tick: one short high note
  kUncheck = 2, // one short low note
  kDone = 3,    // rising arpeggio when the last item is ticked
  kAlarm = 4,   // three chimes, half a second apart
};

// Samples in one second of 16-bit mono PCM.
constexpr size_t kSecondSamples = kSampleRate;

// Fill `out` with the cue and return the number of samples written. The buffer
// must hold kMaxSamples. Volume is 0..100; 0 writes nothing.
size_t Render(Cue cue, uint8_t volume, int16_t* out, size_t cap);

// Samples one cue needs at most (the alarm is the longest: three 300 ms
// chimes plus two 500 ms gaps, rounded up).
constexpr size_t kMaxSamples = 3 * (kSampleRate / 3) + 2 * (kSampleRate / 2);

// Envelope value in 0..32767 for sample `i` of `n`, raised cosine. Exposed so
// the test can assert the endpoints are exactly zero.
int16_t Envelope(size_t i, size_t n);

// Amplitude in 0..32767 for a 0..100 volume; 60 is the default.
int16_t Amplitude(uint8_t volume);

}  // namespace sound
