// Local widgets drawn on top of the server bitmap: clock, wifi, climate and
// battery. Only ASCII digits and a few hand-drawn icons, no CJK font.
#pragma once

#include <Arduino.h>

#include "tbf.h"

namespace widgets {

constexpr int kWhite = 1;
constexpr int kBlack = 0;

// Widgets draw at 1 px units in panel coordinates (400x300 landscape).
void Begin();
void Clear(const tbf::Widget& rect);

void DrawClock(const tbf::Widget& rect, int hh, int mm);
// "--:--" while the RTC is invalid and no server time has arrived.
void DrawClockUnknown(const tbf::Widget& rect);
void DrawWifi(const tbf::Widget& rect, int rssi, bool connecting, bool portal, bool connected);
void DrawClimate(const tbf::Widget& rect, float temperatureC, float humidity, bool ok);
void DrawPower(const tbf::Widget& rect, int millivolts, bool usbKnown, bool usb);

// DrawBanner paints one full-width strip with vertically centred text. With
// `invert` the strip is black and the text white; otherwise the strip is
// white with black text. The reminder and the lost-Wi-Fi line share it.
void DrawBanner(int y, int height, const char* text, int scale, bool invert);

// Width of `text` at `scale`, without drawing.
int MeasureWidth(const char* text, int scale);

// Battery percentage from a resting Li-ion voltage; -1 when the reading is
// outside the usable window (no battery / not calibrated).
int BatteryPercent(int millivolts);

// Plain ASCII text for boot/provisioning/error screens. Only the glyphs in
// widgets.cpp exist, so callers stick to digits, uppercase and .:%-
int DrawString(int x, int y, const char* text, int scale);

}  // namespace widgets
