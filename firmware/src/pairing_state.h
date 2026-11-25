#pragma once

#include <stdint.h>

namespace pairingstate {

constexpr uint32_t kWindowMs = 10 * 60 * 1000;
constexpr uint32_t kTicketMs = 30 * 1000;

struct State {
  bool open = false;
  uint32_t openedAt = 0;
  uint32_t blockedAt = 0;
  uint8_t failures = 0;
  bool blocked = false;
  char claimant[33] = {};
  uint32_t claimedAt = 0;
};

enum class Check { Accepted, Wrong, Closed, Blocked };
bool Active(const State& s, uint32_t now);
Check Verify(State& s, const char* expected, const char* input, uint32_t now);
bool EqualSecret(const char* a, const char* b);
bool Claim(State& s, const char* claimant, uint32_t now);
bool Owns(const State& s, const char* claimant, uint32_t now);
bool ValidServer(const char* server, const char* peer);
bool ValidToken(const char* token);

}  // namespace pairingstate
