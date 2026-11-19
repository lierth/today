// SoftAP password policy: 10 digits, drawn on the panel in two groups of five
// (e.g. "48219 93057"); the password itself has no space.
//
// Why not 8 digits: an 8-digit numeric WPA2 handshake is exhaustible in about
// two minutes on a GPU, and the hotspot stays up for five. Ten digits raises
// that to hours. Digits are sampled with rejection (see portal.cpp), never
// with a plain modulo, so every digit is equally likely.
//
// Plain header, no Arduino, so the host font test can verify every character
// is drawable on the panel.
#pragma once

namespace portalcfg {

constexpr char kPasswordAlphabet[] = "0123456789";
constexpr unsigned kPasswordLength = 10;
// Display grouping only: groups of five separated by one space, never sent to
// the SoftAP as part of the password.
constexpr unsigned kPasswordGroup = 5;

}  // namespace portalcfg
