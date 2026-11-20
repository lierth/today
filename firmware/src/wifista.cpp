#include "wifista.h"

#include <stdio.h>
#include <string.h>

namespace wifista {
namespace {

// Copy `src` into `out`, never past cap, always NUL-terminated. Returns the
// length written.
size_t Copy(const char* src, char* out, size_t cap) {
  if (out == nullptr || cap == 0) return 0;
  if (src == nullptr) {
    out[0] = '\0';
    return 0;
  }
  size_t n = 0;
  while (src[n] != '\0' && n + 1 < cap) n++;
  memcpy(out, src, n);
  out[n] = '\0';
  return n;
}

}  // namespace

// The ESP-IDF disconnect reasons that actually happen in a home:
//   15  WIFI_REASON_4WAY_HANDSHAKE_TIMEOUT   wrong password
//   201 WIFI_REASON_NO_AP_FOUND              wrong SSID, or a 5 GHz-only one
//   202 WIFI_REASON_AUTH_FAIL                wrong password (WPA3/WPA2 clash)
//   203 WIFI_REASON_ASSOC_FAIL               weak signal / AP refusing
//   204 WIFI_REASON_HANDSHAKE_TIMEOUT        wrong password (older AP)
//   200 WIFI_REASON_BEACON_TIMEOUT           router went away
size_t ReasonText(uint8_t reason, char* out, size_t cap) {
  const char* text = "WIFI ERROR";
  switch (reason) {
    case 15:
    case 202:
    case 204:
      text = "WRONG PASSWORD?";
      break;
    case 201:
      text = "NETWORK NOT FOUND";
      break;
    case 203:
      text = "SIGNAL TOO WEAK";
      break;
    case 200:
    case 8:  // WIFI_REASON_ASSOC_LEAVE
      text = "ROUTER WENT AWAY";
      break;
    case 205:
      text = "CONNECTION FAILED";
      break;
    default:
      text = "WIFI ERROR";
      break;
  }
  return Copy(text, out, cap);
}

size_t Banner(const char* detail, char* out, size_t cap) {
  if (out == nullptr || cap == 0) return 0;
  if (detail == nullptr || detail[0] == '\0') {
    return Copy("WIFI LOST - HOLD KEY 3S TO SET UP", out, cap);
  }
  const int n = snprintf(out, cap, "WIFI LOST: %s", detail);
  if (n < 0) {
    out[0] = '\0';
    return 0;
  }
  return static_cast<size_t>(n) < cap ? static_cast<size_t>(n) : cap - 1;
}

size_t ReasonAdvice(uint8_t reason, char* out, size_t cap) {
  const char* text = "连不上，检查路由器和密码";
  switch (reason) {
    case 15:
    case 202:
    case 204:
      text = "密码不对，重新输入 2.4 GHz 网络的密码";
      break;
    case 201:
      text = "找不到这个网络：确认名称没打错，并且是 2.4 GHz 的（5 GHz 连不上）";
      break;
    case 203:
      text = "信号太弱，把板子挪近路由器";
      break;
    case 200:
    case 8:
      text = "路由器断开或重启了，稍后自动重连";
      break;
    case 205:
      text = "连接被拒绝，确认路由器没有开启 MAC 过滤";
      break;
    default:
      text = "连不上，检查路由器和密码";
      break;
  }
  return Copy(text, out, cap);
}

}  // namespace wifista
