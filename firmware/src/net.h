#pragma once

#include <Arduino.h>

#include "storage.h"
#include "tbf.h"

namespace net {

// EnsureSta starts (or restarts) the station with the stored credentials and
// returns immediately: the link comes up in the background, so a dead router
// never blocks the keys. Safe to call repeatedly, but it will not call begin()
// while the driver is mid-handshake: doing that returns 0x3007 and, at one
// call every 20 s, tore down the attempt that was about to succeed. When it
// does nothing, the driver's own retry is in charge. Keeps AP_STA when a
// provisioning hotspot is up.
void EnsureSta(const storage::Config& cfg);

// Pause station retries for scanning and form entry without stopping the AP.
void PauseStation(bool paused);

// LastDisconnectReason is the ESP-IDF reason of the most recent drop (0 when
// the link is up or nothing has failed yet). It lives in RAM: a reboot has no
// story to tell, and the panel reads it within the same power cycle.
uint8_t LastDisconnectReason();

// ForgetDisconnectReason clears it once the link is back, so a banner never
// shows a stale password complaint long after a successful join.
void ForgetDisconnectReason();

bool Connected();
int Rssi();

struct FetchResult {
  int status = -1;            // HTTP status, -1 on transport error
  bool changed = false;       // body parsed and bitmap replaced
  size_t encodedBytes = 0;    // complete validated TBF, including metadata and CRC
  bool forceFull = false;     // server asked for a full refresh
  bool refresh = false;       // X-Refresh: repaint even on 304
  uint32_t seq = 0;
  int64_t serverNow = 0;      // X-Server-Now, Unix seconds
  int utcOffset = 0;          // X-UTC-Offset, seconds east of UTC
  bool night = false;         // inside X-Night window
  int nightStart = 0;         // minutes of day
  int nightEnd = 0;
  String etag;
  String firmwareVersion;     // X-Firmware-Version, empty when none uploaded
  String alarms;              // X-Alarms: Unix seconds, comma separated
  String sound;               // X-Sound: vol=60;keys=1;alarm=1
  bool soundPreview = false;  // X-Sound-Preview: play one test tone
};

// FetchFrame writes the bitmap into `buf` and points `out->bitmap` into it.
// The cursor is drawn on the device; the request never carries it.
FetchResult FetchFrame(const storage::Config& cfg, const String& etag, bool force,
                       uint8_t (&buf)[tbf::kMaxFrameBytes], tbf::Frame* out);

// PostAcks sends one JSON payload and returns the HTTP status (or <0).
int PostAcks(const storage::Config& cfg, const String& json);

// DownloadFirmware streams the image into the inactive OTA partition.
bool DownloadFirmware(const storage::Config& cfg);

}  // namespace net
