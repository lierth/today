#include "display.h"

#include <SPI.h>
#include <string.h>

#include "st7305.h"

namespace display {
namespace {

SPIClass s_spi(FSPI);
uint8_t s_buffer[st7305::kPackedBytes];
bool s_off = false;

void WriteCommand(uint8_t cmd) {
  digitalWrite(board::kPinDc, LOW);
  digitalWrite(board::kPinCs, LOW);
  s_spi.transfer(cmd);
  digitalWrite(board::kPinCs, HIGH);
}

void WriteData(const uint8_t* data, size_t len) {
  digitalWrite(board::kPinDc, HIGH);
  digitalWrite(board::kPinCs, LOW);
  s_spi.transferBytes(data, nullptr, len);
  digitalWrite(board::kPinCs, HIGH);
}

void WriteData8(uint8_t v) { WriteData(&v, 1); }

void Send(uint8_t cmd, std::initializer_list<uint8_t> values) {
  WriteCommand(cmd);
  if (values.size() > 0) WriteData(values.begin(), values.size());
}

void Reset() {
  digitalWrite(board::kPinRst, HIGH);
  delay(50);
  digitalWrite(board::kPinRst, LOW);
  delay(20);
  digitalWrite(board::kPinRst, HIGH);
  delay(50);
}

}  // namespace

bool Begin() {
  pinMode(board::kPinCs, OUTPUT);
  pinMode(board::kPinDc, OUTPUT);
  pinMode(board::kPinRst, OUTPUT);
  digitalWrite(board::kPinCs, HIGH);
  digitalWrite(board::kPinDc, HIGH);
  digitalWrite(board::kPinRst, HIGH);

  s_spi.begin(board::kPinSck, -1, board::kPinMosi, -1);
  s_spi.beginTransaction(SPISettings(10000000, MSBFIRST, SPI_MODE0));
  s_spi.endTransaction();

  Reset();
  // Init values are the vendor reference (display_bsp.cpp, RLCD_Init).
  Send(0xD6, {0x17, 0x02});  // NVM load
  Send(0xD1, {0x01});        // booster
  Send(0xC0, {0x11, 0x04});  // gate voltage
  Send(0xC1, {0x69, 0x69, 0x69, 0x69});
  Send(0xC2, {0x19, 0x19, 0x19, 0x19});
  Send(0xC4, {0x4B, 0x4B, 0x4B, 0x4B});
  Send(0xC5, {0x19, 0x19, 0x19, 0x19});
  Send(0xD8, {0x80, 0xE9});  // OSC: HPM 25.5/51 Hz
  Send(0xB2, {0x02});        // HPM 25.5 Hz, LPM 1 Hz
  Send(0xB3, {0xE5, 0xF6, 0x05, 0x46, 0x77, 0x77, 0x77, 0x77, 0x76, 0x45});
  Send(0xB4, {0x05, 0x46, 0x77, 0x77, 0x77, 0x77, 0x76, 0x45});
  Send(0x62, {0x32, 0x03, 0x1F});
  Send(0xB7, {0x13});
  Send(0xB0, {0x64});
  Send(0x11, {});  // sleep out
  delay(200);
  Send(0xC9, {0x00});
  Send(0x36, {0x48});  // MADCTL: landscape mapping
  Send(0x3A, {0x11});  // 1 bpp
  Send(0xB9, {0x20});
  Send(0xB8, {0x29});
  Send(0x21, {});  // display inversion: 1 = white
  Send(0x2A, {0x12, 0x2A});  // column window
  Send(0x2B, {0x00, 0xC7});  // row window
  Send(0x35, {0x00});
  Send(0xD0, {0xFF});
  Send(0x38, {});  // HPM
  Send(0x29, {});  // display on

  memset(s_buffer, 0xFF, sizeof(s_buffer));
  Flush();
  s_off = false;
  return true;
}

void Flush() {
  Send(0x2A, {0x12, 0x2A});
  Send(0x2B, {0x00, 0xC7});
  WriteCommand(0x2C);
  WriteData(s_buffer, sizeof(s_buffer));
}

void ShowFrame(const uint8_t* tbfBitmap) {
  st7305::Pack(tbfBitmap, (board::kWidth / 8) * board::kHeight, s_buffer, sizeof(s_buffer));
  // Pack clears every black bit; a DISPOFF/DISPON cycle is not implicit here.
  s_off = false;
}

uint8_t* Buffer() { return s_buffer; }

void Off() {
  Send(0x28, {});  // DISPOFF: RAM keeps its contents
  s_off = true;
}

void On() {
  Send(0x29, {});
  s_off = false;
}

bool IsOff() { return s_off; }

}  // namespace display
