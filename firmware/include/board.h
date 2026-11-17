// Board assumptions for the reference build. Change pins here, not in the
// driver code. Buttons must stay on RTC GPIOs for ext1 deep-sleep wake.
#pragma once

#include <Arduino.h>
#include <GxEPD2_BW.h>

namespace board {

constexpr int kPinCs = 5;
constexpr int kPinDC = 17;
constexpr int kPinRst = 16;
constexpr int kPinBusy = 4;
constexpr int kPinSck = 18;
constexpr int kPinMosi = 23;

constexpr int kPinUp = 32;
constexpr int kPinDown = 33;
constexpr int kPinOk = 25;
constexpr int kPinRefresh = 26;

// Battery divider: 100k/100k, so ADC millivolts are doubled.
constexpr int kPinBattery = 34;
constexpr int kBatteryScale = 2;

// Bump when the firmware changes so devices fetch a newly uploaded image.
constexpr char kFirmwareVersion[] = "0.1.0";

constexpr uint16_t kWidth = 400;
constexpr uint16_t kHeight = 300;
constexpr uint16_t kRowHeight = 24;
constexpr size_t kBitmapBytes = (kWidth / 8) * kHeight;

constexpr uint32_t kButtonSleepSeconds = 60;
constexpr uint32_t kFallbackSleepSeconds = 6 * 60 * 60;
constexpr uint32_t kMinSleepSeconds = 60;
constexpr uint16_t kMaxOutbox = 32;

using Panel = GxEPD2_BW<GxEPD2_420, GxEPD2_420::HEIGHT>;

inline uint64_t ButtonMask() {
  return (1ULL << kPinUp) | (1ULL << kPinDown) | (1ULL << kPinOk) | (1ULL << kPinRefresh);
}

}  // namespace board
