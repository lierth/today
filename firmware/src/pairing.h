#pragma once

#include <Arduino.h>

namespace pairing {

void Begin();
void Open();  // A physical BOOT hold permits replacing an existing pairing.
void Loop(bool configured, bool portalOpen);
bool Active();
bool TakeConfigured();
String ID();
String Code();

}  // namespace pairing
