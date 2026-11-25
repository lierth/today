#include "net.h"

#include <HTTPClient.h>
#include <Update.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>
#include <time.h>
#include <atomic>
#include <esp_wifi.h>

#include "wifista.h"

extern const uint8_t kCertBundleStart[] asm("_binary_x509_crt_bundle_start");
extern const uint8_t kCertBundleEnd[] asm("_binary_x509_crt_bundle_end");

namespace net {
namespace {

// The driver's last drop, kept for the panel and the setup page. RAM only: a
// reboot has no story to tell, and every reader is in the same power cycle.
uint8_t g_reason = 0;
bool g_listening = false;
std::atomic<bool> g_stationPaused{false};
wifista::Station g_station;

// HTTPClient decodes both chunked and fixed-length responses into this bounded sink.
class FrameSink final : public Stream {
 public:
  FrameSink(uint8_t* data, size_t cap) : data_(data), cap_(cap) {}
  size_t write(uint8_t value) override { return write(&value, 1); }
  size_t write(const uint8_t* data, size_t len) override {
    if (len > cap_ - size_) { setWriteError(); return 0; }
    memcpy(data_ + size_, data, len);
    size_ += len;
    return len;
  }
  int available() override { return 0; }
  int read() override { return -1; }
  int peek() override { return -1; }
  void flush() override {}
  size_t size() const { return size_; }
 private:
  uint8_t* data_;
  size_t cap_;
  size_t size_ = 0;
};

void OnStationEvent(WiFiEvent_t event, WiFiEventInfo_t info) {
  if (event != ARDUINO_EVENT_WIFI_STA_DISCONNECTED) return;
  if (g_stationPaused.load()) return;
  g_reason = info.wifi_sta_disconnected.reason;
  g_station.Touch(millis());
  Serial.printf("wifi: disconnected, reason %u\n", static_cast<unsigned>(g_reason));
}

// Listen once. Registration is idempotent in the core, but a static guard keeps
// the intent obvious and the loop quiet.
void ListenOnce() {
  if (g_listening) return;
  g_listening = true;
  WiFi.onEvent(OnStationEvent, ARDUINO_EVENT_WIFI_STA_DISCONNECTED);
}

String Join(const storage::Config& cfg, const char* path) {
  String base = cfg.server;
  while (base.endsWith("/")) base.remove(base.length() - 1);
  return base + path;
}

long HeaderLong(HTTPClient& http, const char* name) {
  const String value = http.header(name);
  if (value.isEmpty()) return 0;
  return strtol(value.c_str(), nullptr, 10);
}

// "1380-420" -> start/end minutes; false when the header was absent or off.
bool ParseNight(const String& raw, int* start, int* end) {
  if (raw.isEmpty()) return false;
  const int dash = raw.indexOf('-');
  if (dash <= 0) return false;
  *start = raw.substring(0, dash).toInt();
  *end = raw.substring(dash + 1).toInt();
  return *start >= 0 && *start < 24 * 60 && *end >= 0 && *end < 24 * 60;
}

void Prepare(HTTPClient& http) {
  http.setTimeout(15000);
  http.setConnectTimeout(8000);
}

void PrepareTLS(WiFiClientSecure& tls) {
  // The pinned ESP-IDF SDK includes its full public CA bundle.
  tls.setCACertBundle(kCertBundleStart, kCertBundleEnd - kCertBundleStart);
  static bool ntpStarted = false;
  if (!ntpStarted) {
    // Bootstrap certificate dates on a new board without a valid RTC.
    configTime(0, 0, "time.cloudflare.com", "pool.ntp.org");
    ntpStarted = true;
  }
}

}  // namespace

void EnsureSta(const storage::Config& cfg) {
  if (cfg.wifiSsid.isEmpty() || g_stationPaused.load()) return;
  ListenOnce();
  const uint32_t now = millis();
  const int status = WiFi.status();
  g_station.Note(status, now);
  if (!g_station.ShouldBegin(now)) return;
  // A provisioning hotspot must survive the retry: raise the station beside
  // the AP instead of replacing it, or the phone drops off the setup page.
  WiFi.mode(WiFi.getMode() == WIFI_MODE_AP || WiFi.getMode() == WIFI_MODE_APSTA
                ? WIFI_AP_STA
                : WIFI_STA);
  WiFi.setSleep(true);  // modem sleep: the loop is idle between fetches
  // The driver retries on its own between our calls; begin() here only starts
  // a fresh attempt once the previous one has settled (connected, failed, or
  // its own timeout). Calling it mid-handshake answers 0x3007 and tears down
  // the attempt that was about to succeed.
  WiFi.setAutoReconnect(true);
  g_reason = 0;
  g_station.Began(now);
  WiFi.begin(cfg.wifiSsid.c_str(), cfg.wifiPass.c_str());
}

void PauseStation(bool paused) {
  if (g_stationPaused.exchange(paused) == paused) return;
  WiFi.setAutoReconnect(!paused);
  if (paused) {
    // Arduino's disconnect() returns early while association is in progress.
    // The driver call cancels that attempt, which otherwise blocks every scan.
    const esp_err_t err = esp_wifi_disconnect();
    if (err != ESP_OK && err != ESP_ERR_WIFI_NOT_CONNECT) {
      Serial.printf("wifi: setup pause failed (%s)\n", esp_err_to_name(err));
    }
  } else {
    g_station.Touch(millis());
    WiFi.reconnect();
  }
}

uint8_t LastDisconnectReason() { return g_reason; }

void ForgetDisconnectReason() { g_reason = 0; }

bool Connected() { return WiFi.status() == WL_CONNECTED; }
int Rssi() { return WiFi.RSSI(); }

FetchResult FetchFrame(const storage::Config& cfg, const String& etag, bool force,
                       uint8_t (&buf)[tbf::kMaxFrameBytes], tbf::Frame* out) {
  FetchResult res;
  if (cfg.server.isEmpty() || cfg.device.isEmpty() || cfg.token.isEmpty()) return res;
  String path = "/v1/devices/" + cfg.device + "/frame";
  if (force) path += "?force=1";
  const String url = Join(cfg, path.c_str());
  WiFiClient plain;
  WiFiClientSecure tls;
  HTTPClient http;
  Prepare(http);
  const bool secure = url.startsWith("https://");
  if (secure) PrepareTLS(tls);
  const bool began = secure ? http.begin(tls, url) : http.begin(plain, url);
  if (!began) {
    res.status = -2;
    return res;
  }
  http.addHeader("X-Device-Token", cfg.token);
  if (!etag.isEmpty()) http.addHeader("If-None-Match", etag);
  static const char* kHeaders[] = {"X-Frame-Seq",   "X-Server-Now", "X-UTC-Offset",
                                   "X-Night",       "X-Refresh",    "ETag",
                                   "X-Firmware-Version", "X-Alarms", "X-Sound",
                                   "X-Sound-Preview"};
  http.collectHeaders(kHeaders, 10);

  const int code = http.GET();
  res.status = code;
  res.seq = static_cast<uint32_t>(HeaderLong(http, "X-Frame-Seq"));
  res.serverNow = HeaderLong(http, "X-Server-Now");
  res.utcOffset = static_cast<int>(HeaderLong(http, "X-UTC-Offset"));
  res.night = ParseNight(http.header("X-Night"), &res.nightStart, &res.nightEnd);
  res.etag = http.header("ETag");
  res.refresh = http.header("X-Refresh") == "1";
  res.firmwareVersion = http.header("X-Firmware-Version");
  res.alarms = http.header("X-Alarms");
  res.sound = http.header("X-Sound");
  res.soundPreview = http.header("X-Sound-Preview") == "1";

  if (code == HTTP_CODE_NOT_MODIFIED) {
    res.etag = etag;
    http.end();
    return res;
  }
  if (code != HTTP_CODE_OK) {
    http.end();
    return res;
  }

  if (http.getSize() > static_cast<int>(sizeof(buf))) {
    res.status = -3;
    http.end();
    return res;
  }
  FrameSink sink(buf, sizeof(buf));
  const int received = http.writeToStream(&sink);
  http.end();
  if (received <= 0 || sink.size() == 0) {
    res.status = -3;
    return res;
  }
  if (!tbf::Parse(buf, sink.size(), out)) {
    res.status = -4;
    return res;
  }
  res.changed = true;
  res.encodedBytes = sink.size();
  res.forceFull = (out->flags & tbf::kFlagForceFull) != 0;
  return res;
}

int PostAcks(const storage::Config& cfg, const String& json) {
  if (cfg.server.isEmpty() || cfg.device.isEmpty() || cfg.token.isEmpty()) return -1;
  const String url = Join(cfg, ("/v1/devices/" + cfg.device + "/acks").c_str());
  WiFiClient plain;
  WiFiClientSecure tls;
  HTTPClient http;
  Prepare(http);
  const bool secure = url.startsWith("https://");
  if (secure) PrepareTLS(tls);
  if (!(secure ? http.begin(tls, url) : http.begin(plain, url))) return -2;
  http.addHeader("Content-Type", "application/json");
  http.addHeader("X-Device-Token", cfg.token);
  const int code = http.POST(json);
  http.end();
  return code;
}

bool DownloadFirmware(const storage::Config& cfg) {
  if (cfg.server.isEmpty() || cfg.device.isEmpty() || cfg.token.isEmpty()) return false;
  const String url = Join(cfg, ("/v1/devices/" + cfg.device + "/firmware").c_str());
  WiFiClient plain;
  WiFiClientSecure tls;
  HTTPClient http;
  http.setTimeout(30000);
  http.setConnectTimeout(8000);
  const bool secure = url.startsWith("https://");
  if (secure) PrepareTLS(tls);
  const bool began = secure ? http.begin(tls, url) : http.begin(plain, url);
  if (!began) return false;
  http.addHeader("X-Device-Token", cfg.token);
  const int code = http.GET();
  if (code != HTTP_CODE_OK) {
    Serial.printf("ota: http %d\n", code);
    http.end();
    return false;
  }
  const int length = http.getSize();
  if (length <= 0 || !Update.begin(static_cast<size_t>(length))) {
    Serial.printf("ota: begin failed (%d, %s)\n", length, Update.errorString());
    http.end();
    return false;
  }
  WiFiClient* stream = http.getStreamPtr();
  const size_t written = Update.writeStream(*stream);
  const bool ok = written == static_cast<size_t>(length) && Update.end(true) && Update.isFinished();
  if (!ok) {
    Serial.printf("ota: write %u/%d (%s)\n", static_cast<unsigned>(written), length,
                  Update.errorString());
  }
  http.end();
  return ok;
}

}  // namespace net
