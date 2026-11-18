// ST7305 4.2" reflective LCD over Arduino SPI.
//
// Initialisation sequence and the landscape framebuffer layout come from the
// vendor demo (waveshareteam/ESP32-S3-RLCD-4.2, Apache-2.0); see NOTICE.
#pragma once

#include <Arduino.h>

#include "board.h"

namespace display {

bool Begin();

// ShowFrame packs one TBF-format bitmap (row-major, MSB first, 1 = black)
// into the panel buffer and pushes it.
void ShowFrame(const uint8_t* tbfBitmap);

// Widgets draw into the same 15,000-byte buffer the packer produced. Call
// Flush() once after the last widget so one SPI burst covers the frame.
uint8_t* Buffer();
void Flush();

// DISPOFF / DISPON keep the controller RAM, so On() only needs a Flush().
void Off();
void On();
bool IsOff();

}  // namespace display
