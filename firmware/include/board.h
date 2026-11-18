// Board assumptions for the reference build. Change pins here, not in the
// driver code. Buttons must stay on RTC GPIOs for ext1 deep-sleep wake.
#pragma once

#include <Arduino.h>
#include <GxEPD2_BW.h>
#include <esp_sleep.h>

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

// The classic ESP32 only exposes two ext1 wake modes: ALL_LOW and ANY_HIGH.
// With the common "button to GND plus internal pull-up" wiring ALL_LOW needs
// every button pressed at the same moment, so one press can never wake the
// board. The reference build therefore wires the buttons active-high (pin to
// 3V3, 100k to GND) and wakes on ANY_HIGH.
//
// Set this to false to keep the old active-low wiring: sleep then falls back to
// ext0 on kWakePin alone, because no ext1 mode can watch a single low level.
constexpr bool kButtonsActiveHigh = true;

// Only used when kButtonsActiveHigh is false. Must be one of the button pins.
constexpr int kWakePin = kPinOk;

constexpr int kButtonActiveLevel = kButtonsActiveHigh ? HIGH : LOW;
// Internal pull-ups are only wanted for the active-low wiring.
constexpr int kButtonPinMode = kButtonsActiveHigh ? INPUT : INPUT_PULLUP;

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

// ESP_EXT1_WAKEUP_ANY_HIGH is the only per-pin mode the classic ESP32 offers
// besides ALL_LOW, so the mask above is armed that way.
inline esp_sleep_ext1_wakeup_mode_t WakeMode() {
  return kButtonsActiveHigh ? ESP_EXT1_WAKEUP_ANY_HIGH : ESP_EXT1_WAKEUP_ALL_LOW;
}

// True when the boot was caused by a button rather than the timer.
inline bool IsButtonWake(esp_sleep_wakeup_cause_t cause) {
  if (kButtonsActiveHigh) return cause == ESP_SLEEP_WAKEUP_EXT1;
  return cause == ESP_SLEEP_WAKEUP_EXT0 || cause == ESP_SLEEP_WAKEUP_EXT1;
}

}  // namespace board
