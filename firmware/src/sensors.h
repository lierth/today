// I2C sensors and RTC on the shared bus (SDA 13, SCL 14).
#pragma once

#include <Arduino.h>

#include <time.h>

namespace sensors {

void Begin();

// PCF85063 (0x51). Times are local wall-clock values, no timezone maths here.
bool RtcRead(struct tm* out);
bool RtcWrite(const struct tm& local);

// SHTC3 (0x70), clock-stretching disabled. Returns false on a bus error.
bool ReadClimate(float* temperatureC, float* humidity);

}  // namespace sensors
