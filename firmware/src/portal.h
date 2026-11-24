// SoftAP provisioning portal: collects Wi-Fi SSID/password only.
#pragma once

#include <Arduino.h>

namespace portal {

// Start opens "Today-XXXX" with a random password and an HTTP + DNS server.
// The screen is expected to display ssid/password/url (see main.cpp).
// Prepare generates the SSID and password without touching the radio. The
// caller paints the screen from them first, then calls Start(): the panel must
// not wait for the AP (or for a stuck HTTP/DNS tick) to show what to type.
void Prepare();

void Start();
void Stop();
void Loop();
bool Active();
// ScanCount: networks in the cached scan (the last completed /scan).
int ScanCount();

// StartUserOpened/UserOpened: a hotspot the person asked for (KEY hold) keeps
// running even when the saved network comes back, so the setup page cannot
// vanish under their thumbs. Only a portal closed by a link coming up while it
// was *not* user-opened clears the flag.
void StartUserOpened();
bool UserOpened();

// IdleMs: how long the hotspot has been up, for the countdown on the panel.
uint32_t IdleMs();

// SavedSsid: credentials already in NVS, shown on the page as "current".
String SavedSsid();
// MarkSaved: the scan came back with the saved network; the page marks it.
void SetSavedSeen(bool seen);

String Ssid();
String Password();
// Same digits with a space every kPasswordGroup characters, for the panel
// only. Never used for the SoftAP key.
String PasswordDisplay();

// True once the form was submitted; SSID/password are already in NVS and the
// caller should reboot.
bool Submitted();
String SubmittedSsid();

}  // namespace portal
