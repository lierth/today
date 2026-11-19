// Board assumptions for the Waveshare ESP32-S3-RLCD-4.2 (ST7305 reflective
// LCD, always-on build). Change pins here, not in the driver code.
//
// Verified against the vendor schematic and demo package:
//   * display: ST7305 over SPI3, no BUSY line, 400x300 landscape mapping
//   * keys: KEY = GPIO18, BOOT = GPIO0, both active low
//   * I2C: SDA 13, SCL 14 (SHTC3 0x70, PCF85063 0x51, ES8311 0x18, ES7210 0x40)
//   * battery: BAT_ADC on GPIO4 / ADC1_CH3 through a 1:3 divider
//   * GPIO15 is the PCF85063 INT line (open drain, no external pull-up)
#pragma once

#include <Arduino.h>

namespace board {

// Display (SPI3; Arduino SPI, mode 0, 10 MHz).
constexpr int kPinSck = 11;
constexpr int kPinMosi = 12;
constexpr int kPinCs = 40;
constexpr int kPinDc = 5;
constexpr int kPinRst = 41;
constexpr int kPinTe = 6;  // tearing effect, unused

constexpr uint16_t kWidth = 400;
constexpr uint16_t kHeight = 300;
constexpr uint16_t kRowHeight = 24;  // frame rows are 24 px apart
constexpr size_t kBitmapBytes = (kWidth / 2) * (kHeight / 4);  // 15,000

// Keys. BOOT is a strapping pin: only read it at runtime, never use it as a
// wake or power key.
constexpr int kPinKey = 18;
constexpr int kPinBoot = 0;
constexpr int kButtonActiveLevel = LOW;
constexpr uint32_t kLongPressMs = 600;
constexpr uint32_t kDebounceMs = 25;
constexpr uint32_t kProvisionHoldMs = 3000;

// I2C.
constexpr int kPinSda = 13;
constexpr int kPinScl = 14;
constexpr uint8_t kAddrShtc3 = 0x70;
constexpr uint8_t kAddrPcf85063 = 0x51;
constexpr int kPinRtcInt = 15;

// Audio (ES8311 codec + I2S, from the official 07_Audio_Test example,
// board_cfg.h "S3_RLCD_4_2"). The amplifier enable is GPIO46, which is also a
// strapping pin with an external pull-down: it must stay low during reset and
// only go high while a sound plays.
constexpr int kPinAudioMclk = 16;
constexpr int kPinAudioBclk = 9;
constexpr int kPinAudioWs = 45;
constexpr int kPinAudioDout = 8;   // ESP32 -> codec (playback)
constexpr int kPinAudioDin = 10;   // codec -> ESP32 (unused, DAC only)
constexpr int kPinAudioPa = 46;    // amplifier enable, active high
constexpr uint8_t kAddrEs8311 = 0x18;

// Battery: 100k/100k -> 1:3 divider, ADC1 channel 3.
constexpr int kPinBattery = 4;
constexpr int kBatteryScale = 3;
constexpr float kTemperatureOffsetC = 0.0f;  // panel/board self-heating trim

// Cadence.
constexpr size_t kMaxOutbox = 32;
constexpr uint32_t kFrameIntervalMs = 60 * 1000;
constexpr uint32_t kClimateIntervalMs = 60 * 1000;
constexpr uint32_t kWifiRetryMs = 20 * 1000;
constexpr uint8_t kWifiFailuresBeforePortal = 5;
constexpr uint32_t kPortalIdleMs = 5 * 60 * 1000;
constexpr uint32_t kNightWakeMs = 30 * 1000;

// Firmware identity for OTA. The release script injects TODAY_FW_VERSION
// (-DTODAY_FW_VERSION="0.4.0"); a local build without it reports 0.0.0-dev and
// therefore never upgrades itself by accident.
#ifndef TODAY_FW_VERSION
#define TODAY_FW_VERSION "0.0.0-dev"
#endif
constexpr char kFirmwareVersion[] = TODAY_FW_VERSION;

}  // namespace board
