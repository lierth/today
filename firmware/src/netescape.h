// Escaping for untrusted network names. An SSID is set by anyone nearby, so
// it must be escaped for the HTML page and for the /scan JSON. Pure C, no
// Arduino, so the host test can exercise it.
#pragma once

#include <stddef.h>

namespace netescape {

// Both functions write at most cap bytes including the trailing NUL and return
// the length the full escaped string needs (like snprintf), so truncation is
// detectable.

// For HTML text and quoted attribute values: & < > " '
size_t Html(const char* in, char* out, size_t cap);

// For a JSON string body (without the surrounding quotes): " \ and control
// characters. UTF-8 bytes pass through unchanged.
size_t Json(const char* in, char* out, size_t cap);

}  // namespace netescape
