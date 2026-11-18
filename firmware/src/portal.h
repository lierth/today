// SoftAP provisioning portal: collects Wi-Fi SSID/password only.
#pragma once

#include <Arduino.h>

namespace portal {

// Start opens "Today-XXXX" with a random password and an HTTP + DNS server.
// The screen is expected to display ssid/password/url (see main.cpp).
void Start();
void Stop();
void Loop();
bool Active();

String Ssid();
String Password();

// True once the form was submitted; SSID/password are already in NVS and the
// caller should reboot.
bool Submitted();
String SubmittedSsid();

}  // namespace portal
