// ES8311 playback: the codec's I2C init and an I2S output path, no more.
//
// Ported from the vendor example (07_Audio_Test, codec_board + esp_codec_dev,
// Apache-2.0) but only the register writes needed for DAC output at 16 kHz
// mono. The ADC, the ES7210 and the whole codec_board component are left out:
// this board only ever plays a few short tones.
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace audio {

// Begin configures the codec and the I2S transmitter. It does not enable the
// amplifier. Returns false when the codec does not answer on I2C; the caller
// then simply stays silent.
bool Begin();

// Play queues one buffer of 16-bit mono samples and returns immediately: a
// main loop feeds I2S DMA in short blocks. The amplifier is raised for the
// duration and dropped again
// afterwards; overlapping calls are dropped, not queued, because a second
// "ding" on top of the first is noise.
bool Play(const int16_t* samples, size_t count);

// Busy is true while a buffer is still playing.
bool Busy();

// Silence turns the amplifier off and stops feeding I2S. Called after each
// sound and when the sound settings change.
void Silence();

// Scratch is a buffer big enough for the longest cue (sound::kMaxSamples).
// Rendering happens in place, so the 64 KB lives on the heap once instead of
// in .bss, where it would cost a sixth of the DRAM.
int16_t* Scratch(size_t* capacity);

// Loop feeds I2S from the queued buffer and drops the amplifier when it is
// done. Call it from the Arduino loop; it never blocks longer than one DMA
// block (512 samples, 32 ms).
void Loop();

}  // namespace audio
