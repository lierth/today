#include "portal.h"

#include <DNSServer.h>
#include <WebServer.h>
#include <string.h>
#include <WiFi.h>

#include "net.h"
#include "netescape.h"
#include "portal_config.h"
#include "portal_page.h"
#include "portal_plan.h"
#include "portalscan.h"
#include "storage.h"
#include "wifista.h"

namespace portal {
namespace {

DNSServer s_dns;
WebServer s_http(80);
bool s_active = false;
bool s_submitted = false;
String s_ssid;
String s_password;
String s_submittedSsid;

// Scan cache. The first kick happens right after the hotspot comes up, and
// /scan?refresh=1 requests another scan; plain /scan reads the state.
constexpr int kMaxNetworks = 24;
// How long to wait before kicking another scan after the driver refused one.
constexpr uint32_t kScanRetryMs = 3000;
struct Network {
  char ssid[33];
  int8_t rssi;
  bool secure;
  uint8_t channel;
};
Network s_networks[kMaxNetworks];
int s_networkCount = 0;
// A scan that never starts (the radio is mid-handshake, or the AP/STA switch
// has not settled) used to leave s_networkCount at zero with nothing to show
// for it, so the page looked like the neighbourhood was empty. portalscan
// keeps the last refusal and a backoff deadline so the page can say so and
// try again, and the host tests can drive it without a radio.
portalscan::State s_scan;
uint32_t s_scannedAt = 0;
uint32_t s_startedAt = 0;
bool s_userOpened = false;
bool s_savedSeen = false;

// Escapes into a String; nil-safe.
String Html(const String& in) {
  char buf[192];
  const size_t need = netescape::Html(in.c_str(), buf, sizeof(buf));
  if (need < sizeof(buf)) return String(buf);
  // Very long SSID (the limit is 32 bytes, escaping can roughly triple it):
  // allocate exactly.
  char* big = new char[need + 1];
  netescape::Html(in.c_str(), big, need + 1);
  String out(big);
  delete[] big;
  return out;
}

String Json(const String& in) {
  char buf[192];
  const size_t need = netescape::Json(in.c_str(), buf, sizeof(buf));
  if (need < sizeof(buf)) return String(buf);
  char* big = new char[need + 1];
  netescape::Json(in.c_str(), big, need + 1);
  String out(big);
  delete[] big;
  return out;
}

// Keeps the strongest advertisement per SSID; hidden networks (empty SSID)
// and duplicates are dropped, then the list is sorted by RSSI descending.
void CacheScanResults(int found) {
  s_networkCount = 0;
  for (int i = 0; i < found && i < 40; i++) {
    const String name = WiFi.SSID(i);
    if (name.isEmpty() || name.length() > 32) continue;
    const int8_t rssi = static_cast<int8_t>(WiFi.RSSI(i));
    const bool secure = WiFi.encryptionType(i) != WIFI_AUTH_OPEN;
    const int chan = WiFi.channel(i);
    int slot = -1;
    for (int j = 0; j < s_networkCount; j++) {
      if (name == s_networks[j].ssid) {
        slot = j;
        break;
      }
    }
    if (slot >= 0) {
      if (rssi > s_networks[slot].rssi) {
        s_networks[slot].rssi = rssi;
        s_networks[slot].secure = secure;
        s_networks[slot].channel = static_cast<uint8_t>(chan);
      }
      continue;
    }
    if (s_networkCount >= kMaxNetworks) continue;
    Network& n = s_networks[s_networkCount++];
    strncpy(n.ssid, name.c_str(), sizeof(n.ssid) - 1);
    n.ssid[sizeof(n.ssid) - 1] = '\0';
    n.rssi = rssi;
    n.secure = secure;
    n.channel = static_cast<uint8_t>(chan);
  }
  // Insertion sort: at most 24 entries, and the list is nearly sorted between
  // rescans.
  for (int i = 1; i < s_networkCount; i++) {
    const Network key = s_networks[i];
    int j = i - 1;
    while (j >= 0 && s_networks[j].rssi < key.rssi) {
      s_networks[j + 1] = s_networks[j];
      j--;
    }
    s_networks[j + 1] = key;
  }
  s_scannedAt = millis();
  Serial.printf("portal: scan found %d access points, %d networks\n", found, s_networkCount);
}

// Scan in the loop while HTTP/DNS keep running. Reading /scan never restarts
// the radio; only an explicit refresh requests another scan.
void KickRescan() {
  if (s_scan.scanning) return;
  if (!portalscan::Ready(s_scan, millis())) return;
  if (WiFi.scanComplete() == WIFI_SCAN_RUNNING) {
    portalscan::NoteKick(s_scan, portalscan::Kick::Busy, 0, millis(), kScanRetryMs);
    return;
  }
  // scanNetworks() kicks the scan and answers WIFI_SCAN_RUNNING; anything else
  // means esp_wifi_scan_start() refused (WIFI_SCAN_FAILED, usually because the
  // station is still associating). Recording it turns a silent empty list into
  // a visible "scan failed, tap rescan" and lets the retry back off.
  const int16_t started = WiFi.scanNetworks(true, false, false, 180);  // async, no hidden networks
  const portalscan::Kick kick =
      started == WIFI_SCAN_RUNNING ? portalscan::Kick::Started : portalscan::Kick::Refused;
  portalscan::NoteKick(s_scan, kick, started, millis(), kScanRetryMs);
  if (kick == portalscan::Kick::Refused) {
    Serial.printf("portal: scan refused (%d), retry in %lu ms\n", s_scan.error,
                  static_cast<unsigned long>(kScanRetryMs));
  }
}

void RequestRescan() {
  if (!portalscan::Request(s_scan, millis())) return;
  net::PauseStation(true);
}

// Polls the asynchronous scan; returns true while one is in flight.
bool PollRescan() {
  if (!s_scan.scanning) return false;
  const int state = WiFi.scanComplete();
  if (portalscan::NotePoll(s_scan, state, millis(), kScanRetryMs)) return true;
  if (state >= 0) {
    CacheScanResults(state);
    WiFi.scanDelete();
    s_savedSeen = false;
    const String saved = SavedSsid();
    for (int i = 0; i < s_networkCount; i++) {
      if (saved.length() > 0 && String(s_networks[i].ssid) == saved) s_savedSeen = true;
    }
  } else {
    Serial.printf("portal: scan failed (%d), retry in %lu ms\n", s_scan.error,
                  static_cast<unsigned long>(kScanRetryMs));
  }
  return false;
}

String ScanJson(bool scanning) {
  String out = "{\"scanning\":";
  out += scanning ? "true" : "false";
  out += ",\"failed\":";
  out += (s_scan.error != 0 && !scanning) ? "true" : "false";
  out += ",\"error\":";
  out += String(s_scan.error);
  out += ",\"complete\":";
  out += s_scan.complete ? "true" : "false";
  out += ",\"scanned_ms\":";
  out += String(millis() - s_scannedAt);
  out += ",\"networks\":[";
  for (int i = 0; i < s_networkCount; i++) {
    if (i > 0) out += ',';
    out += "{\"ssid\":\"";
    out += Json(String(s_networks[i].ssid));
    out += "\",\"rssi\":";
    out += String(s_networks[i].rssi);
    out += ",\"secure\":";
    out += s_networks[i].secure ? "true" : "false";
    if (s_savedSeen && String(s_networks[i].ssid) == SavedSsid()) {
      out += ",\"saved\":true";
    }
    out += '}';
  }
  out += "]}";
  return out;
}

String SavedNote() {
  storage::Config cfg = storage::LoadConfig();
  if (cfg.wifiSsid.isEmpty()) return "";
  String note = "<p class=saved>已保存：<strong>";
  note += Html(cfg.wifiSsid);
  note += "</strong>";
  // The reason is the whole difference between "it is broken" and "the
  // password is wrong": say which one the driver reported.
  char advice[wifista::kAdviceCap];
  const uint8_t reason = net::LastDisconnectReason();
  if (reason != 0) {
    wifista::ReasonAdvice(reason, advice, sizeof(advice));
    note += "<span>";
    note += Html(String(advice));
    note += "</span></p>";
  } else {
    note += "</p>";
  }
  return note;
}

void HandleRoot() {
  // Opening the page is an explicit setup request: do not reconnect the old
  // station or close the hotspot while the person is entering a password.
  s_userOpened = true;
  net::PauseStation(true);
  if (!s_scan.complete && !s_scan.scanning && !s_scan.requested) RequestRescan();
  String html(portalpage::kPage);
  html.replace("{{LOGO}}", portalpage::kLogo);
  html.replace("{{SAVED_NOTE}}", SavedNote());
  s_http.sendHeader("Cache-Control", "no-store");
  s_http.send(200, "text/html; charset=utf-8", html);
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
              "<p>已保存，显示屏正在重启连接 Wi-Fi。连接后，在 Mac App 的「设备」中输入屏幕配对码。</p>");
}

void HandleScan() {
  if (s_http.arg("refresh") == "1") RequestRescan();
  const bool running = s_scan.scanning || s_scan.requested;
  s_http.sendHeader("Cache-Control", "no-store");
  s_http.send(running ? 202 : 200, "application/json; charset=utf-8", ScanJson(running));
}

void HandleNotFound() {
  s_http.sendHeader("Location", "http://192.168.4.1/", true);
  s_http.send(302, "text/plain", "");
}

}  // namespace

void Prepare() {
  if (!s_ssid.isEmpty()) return;  // one identity per boot; a repaint must not reshuffle it
  uint32_t mac = static_cast<uint32_t>(ESP.getEfuseMac() & 0xFFFF);
  char suffix[5];
  snprintf(suffix, sizeof(suffix), "%04X", static_cast<unsigned>(mac));
  s_ssid = String("Today-") + suffix;
  // Ten digits. Rejection sampling instead of `% 10`: 2^32 is not a multiple
  // of 10, so a modulo would favour 0-5 by one part in 4.3e8. The loop throws
  // away the few bottom values that cause the bias.
  constexpr unsigned alphabet = sizeof(portalcfg::kPasswordAlphabet) - 1;
  constexpr uint32_t limit = 0xFFFFFFFFu - (0xFFFFFFFFu % alphabet);
  char pw[portalcfg::kPasswordLength + 1];
  for (unsigned i = 0; i < portalcfg::kPasswordLength; i++) {
    uint32_t r;
    do {
      r = esp_random();
    } while (r >= limit);
    pw[i] = portalcfg::kPasswordAlphabet[r % alphabet];
  }
  pw[portalcfg::kPasswordLength] = '\0';
  s_password = pw;
}

// SavedChannel: the channel the scan saw the saved network on, or 1 (the AP's
// default) when the scan has not found it yet.
uint8_t SavedChannel() {
  const String saved = SavedSsid();
  if (saved.isEmpty()) return 1;
  for (int i = 0; i < s_networkCount; i++) {
    if (String(s_networks[i].ssid) != saved) continue;
    if (s_networks[i].channel >= 1 && s_networks[i].channel <= 13) {
      return static_cast<uint8_t>(s_networks[i].channel);
    }
  }
  return 1;
}

void Start() {
  if (s_active) return;
  Prepare();
  // No scan before the AP comes up. A synchronous scan blocks the CPU for
  // seconds (2-10 s with a busy 2.4 GHz band), and it runs before the caller
  // can draw the screen: holding KEY looked broken because the board stayed on
  // WAITING while the radio searched for a list the portal screen does not
  // even show. The list is filled by an async scan started below; the page
  // polls /scan for it.
  WiFi.mode(WIFI_AP_STA);
  WiFi.setScanTimeout(12000);
  net::PauseStation(true);
  // Pin the hotspot to the channel the saved network already uses when we know
  // it. Left to itself the AP starts on the STA's channel and moves as the STA
  // retries, and every move drops the phone that is filling in the form.
  const int channel = SavedChannel();
  WiFi.softAP(s_ssid.c_str(), s_password.c_str(), channel);
  delay(200);
  s_dns.start(53, "*", WiFi.softAPIP());
  s_http.on("/", HTTP_GET, HandleRoot);
  s_http.on("/scan", HTTP_GET, HandleScan);
  s_http.on("/save", HTTP_POST, HandleSave);
  s_http.onNotFound(HandleNotFound);
  s_http.begin();
  s_active = true;
  s_submitted = false;
  s_scan = portalscan::State{};
  // Kick the async scan now, while the phone is still joining: by the time the
  // page asks for the list it is usually ready, and nobody sees a rescan drop.
  RequestRescan();
  s_startedAt = millis();
  s_savedSeen = false;
  const String saved = SavedSsid();
  for (int i = 0; i < s_networkCount; i++) {
    if (saved.length() > 0 && String(s_networks[i].ssid) == saved) s_savedSeen = true;
  }
  Serial.printf("portal: %s / %s / http://192.168.4.1 (scan async, %d cached)\n",
                s_ssid.c_str(), s_password.c_str(), s_networkCount);
}

void Stop() {
  if (!s_active) return;
  s_http.stop();
  s_dns.stop();
  WiFi.softAPdisconnect(true);
  s_active = false;
  s_scan = portalscan::State{};
  net::PauseStation(false);
  s_userOpened = false;  // the next manual hold sets it again
  // Drop the identity so the next opening gets a fresh password: the ten
  // digits are a one-time key for one five-minute window.
  s_ssid = "";
  s_password = "";
}

void Loop() {
  if (!s_active) return;
  s_dns.processNextRequest();
  PollRescan();
  if (portalscan::Ready(s_scan, millis())) KickRescan();
  if (!s_userOpened && !s_scan.scanning && !s_scan.requested) net::PauseStation(false);
  s_http.handleClient();
}

void StartUserOpened() {
  s_userOpened = true;
  Start();
}

bool UserOpened() { return s_userOpened; }

uint32_t IdleMs() { return s_active ? millis() - s_startedAt : 0; }

bool Active() { return s_active; }
int ScanCount() { return s_networkCount; }
String SavedSsid() { return storage::LoadConfig().wifiSsid; }

void SetSavedSeen(bool seen) { s_savedSeen = seen; }

String Ssid() { return s_ssid; }
String Password() { return s_password; }
String PasswordDisplay() {
  // One formatter for the panel and the host preview (portalplan), so the
  // grouping can never differ between what the board shows and what the test
  // renders.
  char out[portalcfg::kPasswordLength + 2];
  portalplan::PasswordDisplay(s_password.c_str(), out, sizeof(out));
  return String(out);
}

bool Submitted() { return s_submitted; }
String SubmittedSsid() { return s_submittedSsid; }

}  // namespace portal
