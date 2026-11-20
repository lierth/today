// When the station may be told to start again, and what the last failure was.
//
// Pure arithmetic and string formatting: no Arduino, no Wi-Fi. The firmware
// feeds it WiFi.status(), a millis() stamp and the driver's disconnect reason;
// the host test replays a router outage in microseconds. The bug this exists
// for: EnsureSta() called WiFi.begin() on every 20 s tick even while the driver
// was mid-handshake, the driver answered "sta is connecting, return error"
// (0x3007), and the attempt being torn down every 20 s could never finish.
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace wifista {

// How often the firmware looks at the station at all. The driver retries by
// itself; this is the cadence of our watchdog, not of the radio.
constexpr uint32_t kRetryMs = 30 * 1000;

// How long the status may stay frozen before the firmware kicks the driver
// with a fresh begin(). Two minutes: a live station produces a status change
// or a disconnect event every few seconds, so silence that long means the
// driver stopped retrying, and only a new begin() can help.
constexpr uint32_t kQuietMs = 120 * 1000;

// Status codes as the Arduino core defines them (wl_status_t). Repeated here so
// the host test does not need the ESP32 headers.
constexpr int kIdle = 0;  // WL_IDLE_STATUS: connecting, not a failure
constexpr int kNoSsidAvail = 1;
constexpr int kConnectFailed = 4;
constexpr int kDisconnected = 6;
constexpr int kConnected = 3;

// Station decides when the firmware may call WiFi.begin() again.
//
// The driver answers begin() with 0x3007 (ESP_ERR_WIFI_CONN) when it already
// has an attempt in flight, and that call tears the attempt down. The trouble
// is that WiFi.status() cannot be trusted for this on its own: with
// setAutoReconnect(true) the driver starts its own next attempt immediately
// after a disconnect, and while that attempt is handshaking the status reads
// WL_DISCONNECTED — indistinguishable, at one instant, from a station that
// has given up. Measured on the board: reading 6 and calling begin() 3 s into
// the driver's own retry produced exactly the 0x3007 this exists to prevent.
//
// So: begin only when the status has not changed at all for kQuietMs. A
// handshake in progress flips between states (6 -> 0 -> 6, or 1 on "no AP
// found"); a status frozen for two minutes means nothing is happening, and a
// fresh begin() is then the only thing that can help.
class Station {
 public:
  // Note records what WiFi.status() said at this tick.
  void Note(int status, uint32_t now) {
    if (status != status_) {
      status_ = status;
      since_ = now;
    }
  }

  // ShouldBegin: true on the first call, and otherwise only after kQuietMs
  // in which nothing moved. A live station produces a status
  // change or a disconnect event every few seconds; two silent minutes mean
  // the driver is not retrying at all, and begin() is then the only kick that
  // can help. This is a watchdog, not a retry: the driver retries by itself.
  bool ShouldBegin(uint32_t now, uint32_t quietMs = kQuietMs) const {
    if (began_ == 0) return true;
    if (status_ == kConnected) return false;
    return static_cast<uint32_t>(now - since_) >= quietMs;
  }

  // Touch records that the radio did something even though WiFi.status() did
  // not move — a disconnect event is the driver retrying, and a driver that
  // keeps reporting failures is not a driver that needs a kick. Without this
  // a station stuck on status 6 while events fire every 3 s would still look
  // silent after two minutes, and a begin() would then tear down a live retry.
  void Touch(uint32_t now) { since_ = now; }

  // Began records that begin() was just called.
  void Began(uint32_t now) { began_ = now == 0 ? 1 : now; }

  int status() const { return status_; }

 private:
  int status_ = -1;
  uint32_t since_ = 0;
  uint32_t began_ = 0;
};

// ReasonText: the driver's disconnect reason as one line a person can act on.
// The strings are English and drawn on the panel with the 5x8 font, so they
// stay inside [A-Z0-9 ?()-.]. `out` gets a NUL-terminated string; returns the
// number of bytes written (excluding the NUL).
size_t ReasonText(uint8_t reason, char* out, size_t cap);

// Banner joins the fixed prefix with a short reason: "WIFI LOST: WRONG
// PASSWORD?". The panel draws it at scale 2, which fits 33 characters; every
// detail ReasonText can produce keeps the result inside that. A blank detail
// gives the plain banner.
constexpr size_t kBannerCap = 64;

// ReasonAdvice speaks Chinese, where one character is three bytes; the longest
// sentence needs room for ~30 of them.
constexpr size_t kAdviceCap = 192;
size_t Banner(const char* detail, char* out, size_t cap);

// ReasonAdvice: the same failure as one Chinese sentence for the setup page,
// where there is room to say what to check. Returns the bytes written.
size_t ReasonAdvice(uint8_t reason, char* out, size_t cap);

}  // namespace wifista
