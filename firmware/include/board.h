// Board assumptions for the reference build (ESP32-S3-DevKitC-1 N16R8 with a
// UC8176 4.2" panel). Change pins here, not in the driver code.
//
// ESP32-S3 facts that shape this file:
//   * GPIO 22-25 do not exist on the package (SOC_GPIO_VALID_GPIO_MASK clears
//     them), so the classic-ESP32 map (MOSI 23, OK 25) cannot be reused.
//   * Deep-sleep wake pins are RTC GPIO 0-21 only.
//   * ADC1 is GPIO 1-10, so the battery divider has to sit there.
//   * Unlike the classic ESP32, ext1 offers ANY_LOW, so plain
//     "button to GND plus internal pull-up" wakes on a single press.
#pragma once

#include <Arduino.h>
#include <GxEPD2_BW.h>
#include <esp_sleep.h>

namespace board {

// SPI: the ESP32-S3 DevKitC-1 defaults (SCK 12, MOSI 11) with the panel chip
// select and control lines on pins that are free on the header.
constexpr int kPinCs = 10;
constexpr int kPinDC = 17;
constexpr int kPinRst = 16;
constexpr int kPinBusy = 4;
constexpr int kPinSck = 12;
constexpr int kPinMosi = 11;

// Buttons: wired to GND, internal pull-up, all on RTC GPIO 0-21 so ext1 can
// watch them in deep sleep. None of them is a strapping pin.
constexpr int kPinUp = 6;
constexpr int kPinDown = 7;
constexpr int kPinOk = 15;
constexpr int kPinRefresh = 21;

constexpr int kButtonActiveLevel = LOW;
constexpr int kButtonPinMode = INPUT_PULLUP;

// Battery divider: 100k/100k, so ADC millivolts are doubled. ADC1 channel 0.
constexpr int kPinBattery = 1;
constexpr int kBatteryScale = 2;

// Bump when the firmware changes so devices fetch a newly uploaded image.
constexpr char kFirmwareVersion[] = "0.2.0";

constexpr uint16_t kWidth = 400;
constexpr uint16_t kHeight = 300;
constexpr uint16_t kRowHeight = 24;
constexpr size_t kBitmapBytes = (kWidth / 8) * kHeight;

constexpr uint32_t kButtonSleepSeconds = 60;
constexpr uint32_t kFallbackSleepSeconds = 6 * 60 * 60;
constexpr uint32_t kMinSleepSeconds = 60;
constexpr uint16_t kMaxOutbox = 32;

// Cold-boot serial windows: short for a board that is already provisioned,
// effectively open-ended for a blank one (the port stays usable until reset).
constexpr uint32_t kConfigWindowMs = 4000;
constexpr uint32_t kProvisionWindowMs = 10 * 60 * 1000;

using Panel = GxEPD2_BW<GxEPD2_420, GxEPD2_420::HEIGHT>;

inline uint64_t ButtonMask() {
  return (1ULL << kPinUp) | (1ULL << kPinDown) | (1ULL << kPinOk) | (1ULL << kPinRefresh);
}

// Pressing any button pulls its pin low, so ANY_LOW is exactly the condition
// we want. The internal pull-ups only survive deep sleep while the RTC
// peripheral domain stays powered (see DeepSleep in main.cpp).
inline esp_sleep_ext1_wakeup_mode_t WakeMode() { return ESP_EXT1_WAKEUP_ANY_LOW; }

// True when the boot was caused by a button rather than the timer.
inline bool IsButtonWake(esp_sleep_wakeup_cause_t cause) { return cause == ESP_SLEEP_WAKEUP_EXT1; }

}  // namespace board
