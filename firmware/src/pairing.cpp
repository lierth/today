#include "pairing.h"

#include <ArduinoJson.h>
#include <WebServer.h>
#include <WiFi.h>
#include <WiFiUdp.h>

#include "pairing_state.h"
#include "storage.h"

namespace pairing {
namespace {

constexpr uint16_t kPort = 8789;
WebServer s_http(kPort);
WiFiUDP s_udp;
pairingstate::State s_state;
String s_id;
String s_code;
String s_ticket;
uint32_t s_ticketAt = 0;
bool s_manual = false;
bool s_network = false;
bool s_configured = false;
IPAddress s_localIP;

String RandomHex() {
  uint8_t bytes[16];
  esp_fill_random(bytes, sizeof(bytes));
  char out[33];
  for (size_t i = 0; i < sizeof(bytes); ++i) snprintf(out + i * 2, 3, "%02x", bytes[i]);
  return String(out);
}

void StartWindow(bool manual) {
  s_state = pairingstate::State{};
  s_state.open = true;
  s_state.openedAt = millis();
  s_manual = manual;
  s_ticket = "";
  uint32_t random;
  constexpr uint32_t limit = 0xFFFFFFFFu - (0xFFFFFFFFu % 1000000u);
  do { random = esp_random(); } while (random >= limit);
  char code[7];
  snprintf(code, sizeof(code), "%06lu", static_cast<unsigned long>(random % 1000000));
  s_code = code;
}

void Reply(int status, const char* error = nullptr) {
  JsonDocument result;
  result["id"] = s_id;
  if (error != nullptr) result["error"] = error;
  if (status == 200) result["paired"] = true;
  String body;
  serializeJson(result, body);
  s_http.sendHeader("Cache-Control", "no-store");
  s_http.send(status, "application/json", body);
}

bool ReadBody(JsonDocument& body) {
  if (!s_http.header("Origin").isEmpty()) { Reply(403, "origin"); return false; }
  const String raw = s_http.arg("plain");
  if (raw.length() > 768 || deserializeJson(body, raw)) { Reply(400, "request"); return false; }
  if (!Active()) { Reply(409, "closed"); return false; }
  if (body["id"].as<String>() != s_id) { Reply(403, "identity"); return false; }
  return true;
}

void Verify() {
  JsonDocument body;
  if (!ReadBody(body)) return;
  const auto checked = pairingstate::Verify(s_state, s_code.c_str(), body["code"] | "", millis());
  if (checked != pairingstate::Check::Accepted) {
    Reply(checked == pairingstate::Check::Blocked ? 429 : 403, "code");
    return;
  }
  if (!pairingstate::Claim(s_state, body["claim"] | "", millis())) { Reply(423, "busy"); return; }
  if (s_ticket.isEmpty() || s_ticketAt != s_state.claimedAt) {
    s_ticket = RandomHex();
    s_ticketAt = s_state.claimedAt;
  }
  JsonDocument result;
  result["id"] = s_id;
  result["ticket"] = s_ticket;
  String text;
  serializeJson(result, text);
  s_http.sendHeader("Cache-Control", "no-store");
  s_http.send(200, "application/json", text);
}

void Commit() {
  JsonDocument body;
  if (!ReadBody(body)) return;
  if (!pairingstate::Owns(s_state, body["claim"] | "", millis()) || millis() - s_ticketAt >= pairingstate::kTicketMs ||
      !pairingstate::EqualSecret(s_ticket.c_str(), body["ticket"] | "")) {
    Reply(403, "ticket"); return;
  }
  const String server = body["server"] | "";
  const String token = body["token"] | "";
  if (!pairingstate::ValidServer(server.c_str(), s_http.client().remoteIP().toString().c_str()) ||
      !pairingstate::ValidToken(token.c_str())) { Reply(400, "credentials"); return; }
  const storage::Config previous = storage::LoadConfig();
  storage::Config next = previous;
  next.server = server;
  next.device = s_id;
  next.token = token;
  storage::SaveConfig(next);
  const storage::Config saved = storage::LoadConfig();
  if (saved.server != next.server || saved.device != next.device || saved.token != next.token ||
      saved.wifiSsid != previous.wifiSsid || saved.wifiPass != previous.wifiPass) {
    storage::SaveConfig(previous);
    Reply(500, "storage"); return;
  }
  s_configured = true;
  s_state.open = false;
  s_ticket = "";
  s_code = "";
  Reply(200);
  Serial.printf("pairing: saved %s\n", s_id.c_str());
}

void Discovery() {
  const int size = s_udp.parsePacket();
  if (size <= 0) return;
  if (size > 512) { s_udp.clear(); return; }
  char data[513];
  const int got = s_udp.read(data, sizeof(data) - 1);
  if (got <= 0) return;
  data[got] = '\0';
  JsonDocument query;
  if (deserializeJson(query, data) || query["service"] != "today" || query["version"] != 1) return;
  const String nonce = query["nonce"] | "";
  if (nonce.length() != 24) return;
  JsonDocument result;
  result["service"] = "today";
  result["version"] = 1;
  result["nonce"] = nonce;
  result["id"] = s_id;
  result["pairable"] = Active();
  String response;
  serializeJson(result, response);
  s_udp.beginPacket(s_udp.remoteIP(), s_udp.remotePort());
  s_udp.write(reinterpret_cast<const uint8_t*>(response.c_str()), response.length());
  s_udp.endPacket();
}

}  // namespace

void Begin() {
  char id[19];
  snprintf(id, sizeof(id), "today-%012llx", static_cast<unsigned long long>(ESP.getEfuseMac() & 0xFFFFFFFFFFFFULL));
  s_id = id;
  const char* headers[] = {"Origin"};
  s_http.collectHeaders(headers, 1);
  s_http.on("/pair/verify", HTTP_POST, Verify);
  s_http.on("/pair", HTTP_POST, Commit);
  s_http.onNotFound([]() { Reply(404, "not_found"); });
}

void Open() { StartWindow(true); }

void Loop(bool configured, bool portalOpen) {
  if (configured && !s_manual) s_state.open = false;
  if (!configured && !Active()) StartWindow(false);
  if (s_state.open && !Active()) { s_state.open = false; s_code = ""; s_ticket = ""; }
  const bool online = WiFi.status() == WL_CONNECTED && WiFi.localIP() != IPAddress(0, 0, 0, 0) && !portalOpen;
  if (s_network && (!online || s_localIP != WiFi.localIP())) {
    s_udp.stop(); s_http.stop(); s_network = false;
  }
  if (!online) {
    return;
  }
  if (!s_network) {
    s_network = s_udp.begin(WiFi.localIP(), kPort) == 1;
    if (!s_network) return;
    s_localIP = WiFi.localIP();
    s_http.begin();
    Serial.printf("pairing: ready %s\n", s_id.c_str());
  }
  Discovery();
  s_http.handleClient();
}

bool Active() { return pairingstate::Active(s_state, millis()); }
bool TakeConfigured() { const bool result = s_configured; s_configured = false; return result; }
String ID() { return s_id; }
String Code() { return s_code; }

}  // namespace pairing
