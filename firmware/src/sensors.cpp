#include "sensors.h"

#include <Wire.h>

#include "board.h"

namespace sensors {
namespace {

uint8_t bcdToDec(uint8_t v) { return static_cast<uint8_t>((v >> 4) * 10 + (v & 0x0F)); }
uint8_t decToBcd(uint8_t v) { return static_cast<uint8_t>(((v / 10) << 4) | (v % 10)); }

bool ReadRegs(uint8_t addr, uint8_t reg, uint8_t* out, size_t len) {
  Wire.beginTransmission(addr);
  Wire.write(reg);
  if (Wire.endTransmission(false) != 0) return false;
  if (Wire.requestFrom(addr, static_cast<uint8_t>(len)) != len) return false;
  for (size_t i = 0; i < len; i++) out[i] = Wire.read();
  return true;
}

bool WriteRegs(uint8_t addr, uint8_t reg, const uint8_t* data, size_t len) {
  Wire.beginTransmission(addr);
  Wire.write(reg);
  for (size_t i = 0; i < len; i++) Wire.write(data[i]);
  return Wire.endTransmission() == 0;
}

uint16_t Crc8(const uint8_t* data, size_t len) {
  // SHTC3 uses CRC-8, polynomial 0x31, init 0xFF.
  uint8_t crc = 0xFF;
  for (size_t i = 0; i < len; i++) {
    crc ^= data[i];
    for (int bit = 0; bit < 8; bit++) {
      crc = (crc & 0x80) ? static_cast<uint8_t>((crc << 1) ^ 0x31) : static_cast<uint8_t>(crc << 1);
    }
  }
  return crc;
}

bool Shtc3Command(uint16_t cmd) {
  const uint8_t bytes[2] = {static_cast<uint8_t>(cmd >> 8), static_cast<uint8_t>(cmd & 0xFF)};
  Wire.beginTransmission(board::kAddrShtc3);
  Wire.write(bytes, 2);
  return Wire.endTransmission() == 0;
}

}  // namespace

void Begin() {
  Wire.begin(board::kPinSda, board::kPinScl);
  Wire.setClock(100000);
}

bool RtcRead(struct tm* out) {
  uint8_t raw[7];
  if (out == nullptr || !ReadRegs(board::kAddrPcf85063, 0x04, raw, sizeof(raw))) return false;
  // Seconds bit 7 is OS (oscillator stopped): the backup cell died and the
  // registers no longer hold a real time. Report failure instead of 2000-00-00.
  if ((raw[0] & 0x80) != 0) return false;
  out->tm_sec = bcdToDec(raw[0] & 0x7F);
  out->tm_min = bcdToDec(raw[1] & 0x7F);
  out->tm_hour = bcdToDec(raw[2] & 0x3F);
  out->tm_mday = bcdToDec(raw[3] & 0x3F);
  out->tm_wday = bcdToDec(raw[4] & 0x07);
  out->tm_mon = bcdToDec(raw[5] & 0x1F) - 1;
  out->tm_year = bcdToDec(raw[6]) + 100;  // tm_year is years since 1900
  out->tm_isdst = 0;
  return true;
}

bool RtcWrite(const struct tm& local) {
  const uint8_t raw[7] = {
      decToBcd(static_cast<uint8_t>(local.tm_sec)),
      decToBcd(static_cast<uint8_t>(local.tm_min)),
      decToBcd(static_cast<uint8_t>(local.tm_hour)),
      decToBcd(static_cast<uint8_t>(local.tm_mday)),
      decToBcd(static_cast<uint8_t>(local.tm_wday)),
      decToBcd(static_cast<uint8_t>(local.tm_mon + 1)),
      decToBcd(static_cast<uint8_t>(local.tm_year - 100)),
  };
  return WriteRegs(board::kAddrPcf85063, 0x04, raw, sizeof(raw));
}

bool ReadClimate(float* temperatureC, float* humidity) {
  if (temperatureC == nullptr || humidity == nullptr) return false;
  if (!Shtc3Command(0x3517)) return false;  // wake
  delay(1);
  if (!Shtc3Command(0x7866)) return false;  // measure T first, no clock stretch
  delay(15);
  uint8_t raw[6];
  if (Wire.requestFrom(board::kAddrShtc3, static_cast<uint8_t>(sizeof(raw))) != sizeof(raw)) {
    return false;
  }
  for (size_t i = 0; i < sizeof(raw); i++) raw[i] = Wire.read();
  if (Crc8(raw, 2) != raw[2] || Crc8(raw + 3, 2) != raw[5]) return false;
  const uint16_t tRaw = static_cast<uint16_t>((raw[0] << 8) | raw[1]);
  const uint16_t hRaw = static_cast<uint16_t>((raw[3] << 8) | raw[4]);
  *temperatureC = -45.0f + 175.0f * static_cast<float>(tRaw) / 65536.0f;
  *humidity = 100.0f * static_cast<float>(hRaw) / 65536.0f;
  // SHTC3 accepts 1.62-3.6 V, so 3.3 V does not bias the reading; a warm
  // board/panel does. The trim constant in board.h is for that self-heating.
  *temperatureC += board::kTemperatureOffsetC;
  if (*humidity > 100.0f) *humidity = 100.0f;
  if (*humidity < 0.0f) *humidity = 0.0f;
  Shtc3Command(0xB098);  // back to sleep: the sensor draws ~0.1 uA there
  return true;
}

}  // namespace sensors
