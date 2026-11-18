#include "portal.h"

#include <DNSServer.h>
#include <WebServer.h>
#include <WiFi.h>

#include "storage.h"

namespace portal {
namespace {

DNSServer s_dns;
WebServer s_http(80);
bool s_active = false;
bool s_submitted = false;
String s_ssid;
String s_password;
String s_submittedSsid;

void HandleRoot() {
  String page = F(
      "<!doctype html><meta name=viewport content='width=device-width,initial-scale=1'>"
      "<title>Today</title>"
      "<style>body{font:16px/1.5 -apple-system,system-ui,sans-serif;max-width:24rem;"
      "margin:3rem auto;padding:0 1rem}input{width:100%;padding:.6rem;margin:.4rem 0;"
      "font:inherit;box-sizing:border-box}button{width:100%;padding:.7rem;font:inherit;"
      "background:#111;color:#fff;border:0;border-radius:.4rem;margin-top:.6rem}</style>"
      "<h1>Today</h1><p>输入 2.4 GHz Wi-Fi 的 SSID 与密码，保存后设备会重启并连接。</p>"
      "<form method=post action=/save><label>SSID<input name=ssid required "
      "autocapitalize=off autocorrect=off></label>"
      "<label>密码<input name=pass type=password></label>"
      "<button type=submit>保存并重启</button></form>");
  s_http.send(200, "text/html; charset=utf-8", page);
}

void HandleSave() {
  const String ssid = s_http.arg("ssid");
  const String pass = s_http.arg("pass");
  // 802.11 limits: SSID 1..32 bytes; WPA2 passphrase empty (open) or 8..63.
  if (ssid.length() < 1 || ssid.length() > 32) {
    s_http.send(400, "text/plain; charset=utf-8", "SSID 必须是 1-32 字节");
    return;
  }
  if (!pass.isEmpty() && (pass.length() < 8 || pass.length() > 63)) {
    s_http.send(400, "text/plain; charset=utf-8", "密码必须是 8-63 字节，或留空");
    return;
  }
  storage::Config cfg = storage::LoadConfig();
  cfg.wifiSsid = ssid;
  cfg.wifiPass = pass;
  storage::SaveConfig(cfg);
  s_submittedSsid = ssid;
  s_submitted = true;
  s_http.send(200, "text/html; charset=utf-8",
              "<!doctype html><meta name=viewport content='width=device-width'>"
              "<p>已保存，设备正在重启。</p>");
}

void HandleNotFound() {
  s_http.sendHeader("Location", "http://192.168.4.1/", true);
  s_http.send(302, "text/plain", "");
}

}  // namespace

void Start() {
  if (s_active) return;
  uint32_t mac = static_cast<uint32_t>(ESP.getEfuseMac() & 0xFFFF);
  char suffix[5];
  snprintf(suffix, sizeof(suffix), "%04X", static_cast<unsigned>(mac));
  s_ssid = String("Today-") + suffix;
  // 10 chars from an unambiguous alphabet (~59.5 bits): an 8-digit numeric
  // WPA2 handshake is brute-forceable offline in minutes.
  static const char kAlphabet[] = "abcdefghjkmnpqrstuvwxyz23456789";
  const size_t alphabet = sizeof(kAlphabet) - 1;
  char pw[11];
  uint32_t pool = 0;
  int bits = 0;
  for (size_t i = 0; i < 10; i++) {
    if (bits < 5) {
      pool = (pool << 16) ^ esp_random();
      bits += 16;
    }
    pw[i] = kAlphabet[pool % alphabet];
    pool /= alphabet;
    bits -= 5;
  }
  pw[10] = '\0';
  s_password = pw;

  WiFi.mode(WIFI_AP);
  WiFi.softAP(s_ssid.c_str(), s_password.c_str());
  delay(200);
  s_dns.start(53, "*", WiFi.softAPIP());
  s_http.on("/", HTTP_GET, HandleRoot);
  s_http.on("/save", HTTP_POST, HandleSave);
  s_http.onNotFound(HandleNotFound);
  s_http.begin();
  s_active = true;
  s_submitted = false;
  Serial.printf("portal: %s / %s / http://192.168.4.1\n", s_ssid.c_str(), s_password.c_str());
}

void Stop() {
  if (!s_active) return;
  s_http.stop();
  s_dns.stop();
  WiFi.softAPdisconnect(true);
  s_active = false;
}

void Loop() {
  if (!s_active) return;
  s_dns.processNextRequest();
  s_http.handleClient();
}

bool Active() { return s_active; }
String Ssid() { return s_ssid; }
String Password() { return s_password; }
bool Submitted() { return s_submitted; }
String SubmittedSsid() { return s_submittedSsid; }

}  // namespace portal
