#include "portal.h"

#include <DNSServer.h>
#include <WebServer.h>
#include <string.h>
#include <WiFi.h>

#include "netescape.h"
#include "portal_config.h"
#include "portal_plan.h"
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

// Scan cache. Filled once before the AP starts, refreshed on /scan.
constexpr int kMaxNetworks = 24;
struct Network {
  char ssid[33];
  int8_t rssi;
  bool secure;
};
Network s_networks[kMaxNetworks];
int s_networkCount = 0;
bool s_scanning = false;
uint32_t s_scannedAt = 0;
uint32_t s_startedAt = 0;
bool s_userOpened = false;
bool s_scanFresh = false;  // results landed, not served yet
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
      }
      continue;
    }
    if (s_networkCount >= kMaxNetworks) continue;
    Network& n = s_networks[s_networkCount++];
    strncpy(n.ssid, name.c_str(), sizeof(n.ssid) - 1);
    n.ssid[sizeof(n.ssid) - 1] = '\0';
    n.rssi = rssi;
    n.secure = secure;
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
}

// Scanning with the AP up makes the radio hop channels; phones can drop the
// association for a second or two. That is why the boot scan runs in STA mode
// *before* the hotspot is raised.
void ScanBeforeAp() {
  WiFi.mode(WIFI_STA);
  delay(100);
  const int found = WiFi.scanNetworks(false, false);
  CacheScanResults(found);
  WiFi.scanDelete();
}

// A rescan while the hotspot is up is asynchronous: a synchronous scan would
// block the HTTP server (and the keys) for seconds. /scan starts it, the page
// polls until the results land.
void StartRescan() {
  if (s_scanning) return;
  if (WiFi.scanComplete() == WIFI_SCAN_RUNNING) return;
  s_scanning = true;
  WiFi.scanNetworks(true, false);  // async, no hidden networks
}

// Polls the asynchronous scan; returns true while one is in flight.
bool PollRescan() {
  if (!s_scanning) return false;
  const int state = WiFi.scanComplete();
  if (state == WIFI_SCAN_RUNNING) return true;
  s_scanning = false;
  if (state >= 0) {
    CacheScanResults(state);
    WiFi.scanDelete();
    s_savedSeen = false;
    const String saved = SavedSsid();
    for (int i = 0; i < s_networkCount; i++) {
      if (saved.length() > 0 && String(s_networks[i].ssid) == saved) s_savedSeen = true;
    }
    s_scanFresh = true;
  }
  return false;
}

// "3 bars" for the list, RSSI thresholds match the device widget.
String Bars(int8_t rssi) {
  int level = 1;
  if (rssi > -80) level = 2;
  if (rssi > -65) level = 3;
  String out;
  for (int i = 0; i < 3; i++) out += (i < level) ? "●" : "○";
  return out;
}

String ScanListHtml() {
  if (s_networkCount == 0) {
    return "<p class=empty>没有扫描到网络。可手动输入隐藏网络的名称。</p>";
  }
  String out;
  out.reserve(s_networkCount * 120);
  for (int i = 0; i < s_networkCount; i++) {
    const Network& n = s_networks[i];
    const String escaped = Html(String(n.ssid));
    out += "<button type=button class=net data-ssid=\"";
    out += escaped;
    out += "\" data-secure=\"";
    out += n.secure ? "1" : "0";
    out += "\"><span class=name>";
    out += escaped;
    out += "</span><span class=meta>";
    out += Bars(n.rssi);
    if (n.secure) out += " 🔒";
    if (s_savedSeen && String(n.ssid) == SavedSsid()) out += " ★";
    out += "</span></button>";
  }
  return out;
}

String ScanJson(bool scanning) {
  String out = "{\"scanning\":";
  out += scanning ? "true" : "false";
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

const char* kPageStyle =
    "<style>body{font:16px/1.5 -apple-system,system-ui,sans-serif;max-width:26rem;"
    "margin:2rem auto;padding:0 1rem;color:#111;background:#fff}"
    "h1{font-size:20px;margin:0 0 .2rem}p{color:#666;margin:.2rem 0 1rem}"
    "label{display:block;margin:.8rem 0 .2rem;color:#666;font-size:14px}"
    "input{width:100%;padding:.75rem;font:inherit;box-sizing:border-box;"
    "border:1px solid #ccc;border-radius:8px}"
    ".nets{margin:.4rem 0 0;border:1px solid #ddd;border-radius:8px;overflow:hidden}"
    ".net{display:flex;justify-content:space-between;align-items:center;width:100%;"
    "min-height:48px;padding:.5rem .75rem;font:inherit;text-align:left;background:#fff;"
    "color:#111;border:0;border-top:1px solid #eee;cursor:pointer}"
    ".net:first-child{border-top:0}.net:active{background:#f2f2f2}"
    ".net .name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding-right:.5rem}"
    ".net .meta{color:#666;font-size:13px;flex:none}"
    "button.primary{width:100%;min-height:48px;margin-top:1rem;font:inherit;"
    "background:#111;color:#fff;border:0;border-radius:8px}"
    "button.ghost{min-height:44px;padding:.5rem .9rem;font:inherit;background:#fff;"
    "color:#111;border:1px solid #ccc;border-radius:8px}"
    ".row{display:flex;gap:.5rem;align-items:center;margin-top:.6rem}"
    ".empty{color:#666;padding:.8rem .75rem;margin:0}"
    ".note{font-size:13px;color:#8a5a00;background:#fff7e6;border-radius:6px;"
    "padding:.5rem .75rem;margin:.5rem 0}"
    "@media (prefers-color-scheme:dark){body{background:#121211;color:#ecece7}"
    "input,.net{background:#1b1b19;color:#ecece7;border-color:#2b2b28}"
    ".net{border-top-color:#2b2b28}.net:active{background:#242422}"
    "button.ghost{background:#1b1b19;color:#ecece7;border-color:#2b2b28}"
    "button.primary{background:#ecece7;color:#111}}"
    "</style>";

String Page(const String& savedNote, const String& body) {
  String html;
  html.reserve(4096);
  html += "<!doctype html><meta name=viewport content='width=device-width,initial-scale=1'>";
  html += "<meta name=color-scheme content='light dark'><title>Today</title>";
  html += kPageStyle;
  html += "<h1>Today</h1><p>连接 2.4 GHz Wi-Fi，保存后设备重启。</p>";
  html += savedNote;
  html += body;
  html += "<script>";
  html +=
      "var si=document.getElementById('ssid'),pi=document.getElementById('pass'),"
      "pl=document.getElementById('passrow');"
      "function pick(b){si.value=b.dataset.ssid;"
      "var open=b.dataset.secure==='0';"
      "if(pl)pl.style.display=open?'none':'';"
      "if(!open&&pi)pi.focus();else si.blur();}";
  html += "document.querySelectorAll('.net').forEach(function(b){b.onclick=function(){pick(b)}});";
  html +=
      "function render(d){var box=document.getElementById('nets');box.innerHTML='';"
      "d.networks.forEach(function(n){"
      "var b=document.createElement('button');b.type='button';b.className='net';"
      "b.dataset.ssid=n.ssid;b.dataset.secure=n.secure?'1':'0';"
      "var nm=document.createElement('span');nm.className='name';nm.textContent=n.ssid;"
      "var mt=document.createElement('span');mt.className='meta';"
      "var lvl=n.rssi>-80?(n.rssi>-65?3:2):1;"
      "mt.textContent=['●○○','●●○','●●●'][lvl-1]+(n.secure?' 🔒':'')+(n.saved?' ★':'');"
      "b.appendChild(nm);b.appendChild(mt);b.onclick=function(){pick(b)};box.appendChild(b);});}"
      "var rb=document.getElementById('rescan');"
      "function poll(tries){fetch('/scan').then(function(r){return r.json()}).then(function(d){"
      "if(d.scanning&&tries>0){setTimeout(function(){poll(tries-1)},700);return;}"
      "render(d);rb.disabled=false;rb.textContent='重新扫描';"
      "}).catch(function(){rb.disabled=false;rb.textContent='重新扫描';});}"
      "if(rb)rb.onclick=function(){rb.disabled=true;rb.textContent='扫描中…';"
      "document.getElementById('hint').style.display='';poll(20)};";
  html += "</script>";
  return html;
}

String SavedNote() {
  storage::Config cfg = storage::LoadConfig();
  if (cfg.wifiSsid.isEmpty()) return "";
  String note = "<p class=note>当前保存：";
  note += Html(cfg.wifiSsid);
  note += "（连接失败，可重新选择）</p>";
  return note;
}

void HandleRoot() {
  String body;
  body += "<form method=post action=/save>";
  body += "<label for=ssid>网络名称（SSID）</label>";
  body += "<input id=ssid name=ssid required maxlength=32 autocapitalize=off autocorrect=off "
          "autocomplete=off>";
  body += "<div class=nets id=nets>";
  body += ScanListHtml();
  body += "</div>";
  body += "<div class=row><button class=ghost id=rescan type=button>重新扫描</button>";
  body += "<span id=hint style='font-size:13px;color:#8a5a00;display:none'>"
          "扫描时手机可能短暂断开，完成后自动刷新</span></div>";
  body += "<div id=passrow><label for=pass>密码</label>";
  body += "<input id=pass name=pass type=password autocomplete=off></div>";
  body += "<button class=primary type=submit>保存并重启</button></form>";
  s_http.send(200, "text/html; charset=utf-8", Page(SavedNote(), body));
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

void HandleScan() {
  // One call starts the scan, the page polls until "scanning" is false. The
  // reply that carries the fresh list does not start another one.
  bool running = PollRescan();
  const bool fresh = s_scanFresh;
  s_scanFresh = false;
  if (!running && !fresh) {
    StartRescan();
    running = true;
  }
  s_http.send(running ? 202 : 200, "application/json; charset=utf-8", ScanJson(running));
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

  // Scan first, in STA mode: scanning with the AP up makes phones drop for a
  // second or two, and the result would be a list the user sees flicker.
  ScanBeforeAp();

  WiFi.mode(WIFI_AP_STA);
  WiFi.softAP(s_ssid.c_str(), s_password.c_str());
  delay(200);
  s_dns.start(53, "*", WiFi.softAPIP());
  s_http.on("/", HTTP_GET, HandleRoot);
  s_http.on("/scan", HTTP_GET, HandleScan);
  s_http.on("/save", HTTP_POST, HandleSave);
  s_http.onNotFound(HandleNotFound);
  s_http.begin();
  s_active = true;
  s_submitted = false;
  s_startedAt = millis();
  s_savedSeen = false;
  const String saved = SavedSsid();
  for (int i = 0; i < s_networkCount; i++) {
    if (saved.length() > 0 && String(s_networks[i].ssid) == saved) s_savedSeen = true;
  }
  Serial.printf("portal: %s / %s / http://192.168.4.1 (%d networks)\n", s_ssid.c_str(),
                s_password.c_str(), s_networkCount);
}

void Stop() {
  if (!s_active) return;
  s_http.stop();
  s_dns.stop();
  WiFi.softAPdisconnect(true);
  s_active = false;
  s_userOpened = false;  // the next manual hold sets it again
}

void Loop() {
  if (!s_active) return;
  s_dns.processNextRequest();
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
