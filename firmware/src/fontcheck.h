// Character coverage of the device font: every string the firmware draws with
// PaintMessage/DrawString, and every character of the SoftAP password
// alphabet, must have a glyph. Pure C++ so it runs on the Mac.
#pragma once

namespace fontcheck {

// Returns nullptr when every character is covered, or the offending string.
const char* Missing();

}  // namespace fontcheck
