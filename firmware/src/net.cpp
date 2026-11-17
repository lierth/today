#include "net.h"

#include <HTTPClient.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>

namespace net {
namespace {

String Join(const storage::Config& cfg, const char* path) {
  String base = cfg.server;
  while (base.endsWith("/")) base.remove(base.length() - 1);
  return base + path;
}

uint32_t HeaderULong(HTTPClient& http, const char* name) {
  const String value = http.header(name);
  if (value.isEmpty()) return 0;
  return static_cast<uint32_t>(strtoul(value.c_str(), nullptr, 10));
}

}  // namespace

bool Connect(const storage::Config& cfg, uint32_t timeoutMs) {
  if (cfg.wifiSsid.isEmpty()) return false;
  WiFi.mode(WIFI_STA);
  WiFi.setSleep(false);
  WiFi.begin(cfg.wifiSsid.c_str(), cfg.wifiPass.c_str());
  const uint32_t start = millis();
  while (WiFi.status() != WL_CONNECTED) {
    if (millis() - start > timeoutMs) return false;
    delay(200);
  }
  return true;
}

FetchResult FetchFrame(const storage::Config& cfg, const String& etag,
                       uint8_t* buf, size_t cap, tbf::Frame* out) {
  FetchResult res;
  if (cfg.server.isEmpty() || cfg.device.isEmpty() || cfg.token.isEmpty()) {
    return res;
  }
  const String url = Join(cfg, ("/v1/devices/" + cfg.device + "/frame").c_str());
  WiFiClient plain;
  WiFiClientSecure tls;
  HTTPClient http;
  http.setTimeout(15000);
  http.setConnectTimeout(8000);
  const bool secure = url.startsWith("https://");
  if (secure) tls.setInsecure();  // Funnel uses a public cert; see README.
  const bool began = secure ? http.begin(tls, url) : http.begin(plain, url);
  if (!began) {
    res.status = -2;
    return res;
  }
  http.addHeader("X-Device-Token", cfg.token);
  if (!etag.isEmpty()) http.addHeader("If-None-Match", etag);
  static const char* kHeaders[] = {"X-Frame-Seq", "X-Server-Now", "X-Next-Wake-At", "X-Refresh", "ETag"};
  http.collectHeaders(kHeaders, 5);

  const int code = http.GET();
  res.status = code;
  res.seq = HeaderULong(http, "X-Frame-Seq");
  const uint32_t serverNow = HeaderULong(http, "X-Server-Now");
  const uint32_t nextWake = HeaderULong(http, "X-Next-Wake-At");
  if (serverNow > 0 && nextWake > serverNow) {
    res.sleepSeconds = static_cast<int64_t>(nextWake - serverNow);
  }
  res.etag = http.header("ETag");
  res.refresh = http.header("X-Refresh") == "1";

  if (code == HTTP_CODE_NOT_MODIFIED) {
    res.etag = etag;
    http.end();
    return res;
  }
  if (code != HTTP_CODE_OK) {
    http.end();
    return res;
  }

  const String body = http.getString();
  http.end();
  if (body.length() == 0 || body.length() > cap) {
    res.status = -3;
    return res;
  }
  memcpy(buf, body.c_str(), body.length());
  if (!tbf::Parse(buf, body.length(), out)) {
    res.status = -4;
    return res;
  }
  res.changed = true;
  res.forceFull = (out->flags & tbf::kFlagForceFull) != 0;
  return res;
}

int PostAcks(const storage::Config& cfg, const String& json) {
  if (cfg.server.isEmpty() || cfg.device.isEmpty() || cfg.token.isEmpty()) return -1;
  const String url = Join(cfg, ("/v1/devices/" + cfg.device + "/acks").c_str());
  WiFiClient plain;
  WiFiClientSecure tls;
  HTTPClient http;
  http.setTimeout(15000);
  const bool secure = url.startsWith("https://");
  if (secure) tls.setInsecure();
  if (!(secure ? http.begin(tls, url) : http.begin(plain, url))) return -2;
  http.addHeader("Content-Type", "application/json");
  http.addHeader("X-Device-Token", cfg.token);
  const int code = http.POST(json);
  http.end();
  return code;
}

}  // namespace net
