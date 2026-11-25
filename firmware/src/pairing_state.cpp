#include "pairing_state.h"

#include <stddef.h>
#include <string.h>

namespace pairingstate {

bool Active(const State& s, uint32_t now) {
  return s.open && static_cast<uint32_t>(now - s.openedAt) < kWindowMs;
}

bool EqualSecret(const char* a, const char* b) {
  if (a == nullptr || b == nullptr) return false;
  const size_t length = strlen(a);
  if (length != strlen(b) || length == 0) return false;
  uint8_t difference = 0;
  for (size_t i = 0; i < length; ++i) difference |= static_cast<uint8_t>(a[i] ^ b[i]);
  return difference == 0;
}

Check Verify(State& s, const char* expected, const char* input, uint32_t now) {
  if (!Active(s, now)) return Check::Closed;
  if (s.blocked && static_cast<uint32_t>(now - s.blockedAt) < 30000) return Check::Blocked;
  s.blocked = false;
  if (EqualSecret(expected, input)) { s.failures = 0; return Check::Accepted; }
  if (++s.failures >= 5) {
    s.failures = 0;
    s.blocked = true;
    s.blockedAt = now;
  }
  return Check::Wrong;
}

bool ValidToken(const char* token) {
  if (token == nullptr) return false;
  const size_t n = strlen(token);
  if (n < 24 || n > 96) return false;
  for (size_t i = 0; i < n; ++i) {
    const char c = token[i];
    if (!((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
          (c >= '0' && c <= '9') || c == '-' || c == '_')) return false;
  }
  return true;
}

bool Owns(const State& s, const char* claimant, uint32_t now) {
  return Active(s, now) && static_cast<uint32_t>(now - s.claimedAt) < kTicketMs &&
         EqualSecret(s.claimant, claimant);
}

bool Claim(State& s, const char* claimant, uint32_t now) {
  if (!Active(s, now) || !ValidToken(claimant) || strlen(claimant) != 32) return false;
  if (s.claimant[0] != '\0' && static_cast<uint32_t>(now - s.claimedAt) < kTicketMs) {
    return Owns(s, claimant, now);
  }
  memcpy(s.claimant, claimant, 33);
  s.claimedAt = now;
  return true;
}

bool ValidServer(const char* server, const char* peer) {
  if (server == nullptr || peer == nullptr || strncmp(server, "http://", 7) != 0) return false;
  const char* host = server + 7;
  const char* colon = strchr(host, ':');
  if (colon == nullptr || colon == host || colon - host > 70) return false;
  const size_t n = static_cast<size_t>(colon - host);
  const bool samePeer = strlen(peer) == n && strncmp(peer, host, n) == 0;
  if (!samePeer) {
    if (n <= 6 || strncmp(host + n - 6, ".local", 6) != 0) return false;
    for (size_t i = 0; i < n - 6; ++i) {
      const char c = host[i];
      if (!((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-')) return false;
    }
  }
  unsigned port = 0;
  const char* digit = colon + 1;
  if (*digit == '\0' || strlen(digit) > 5) return false;
  for (; *digit != '\0'; ++digit) {
    if (*digit < '0' || *digit > '9') return false;
    port = port * 10 + static_cast<unsigned>(*digit - '0');
  }
  return port > 0 && port <= 65535;
}

}  // namespace pairingstate
