#include "netescape.h"

#include <stdio.h>

namespace netescape {
namespace {

struct Sink {
  char* out;
  size_t cap;
  size_t written;

  void put(char c) {
    if (out != nullptr && written + 1 < cap) out[written] = c;
    written++;
    if (out != nullptr && cap > 0 && written >= cap) out[cap - 1] = '\0';
  }

  void puts(const char* s) {
    while (*s != '\0') put(*s++);
  }
};

}  // namespace

size_t Html(const char* in, char* out, size_t cap) {
  Sink sink{out, cap, 0};
  if (out != nullptr && cap > 0) out[0] = '\0';
  if (in == nullptr) return 0;
  for (const char* p = in; *p != '\0'; p++) {
    switch (*p) {
      case '&': sink.puts("&amp;"); break;
      case '<': sink.puts("&lt;"); break;
      case '>': sink.puts("&gt;"); break;
      case '"': sink.puts("&quot;"); break;
      case '\'': sink.puts("&#39;"); break;
      default: sink.put(*p); break;
    }
  }
  if (out != nullptr && cap > 0) out[sink.written < cap ? sink.written : cap - 1] = '\0';
  return sink.written;
}

size_t Json(const char* in, char* out, size_t cap) {
  Sink sink{out, cap, 0};
  if (out != nullptr && cap > 0) out[0] = '\0';
  if (in == nullptr) return 0;
  for (const unsigned char* p = reinterpret_cast<const unsigned char*>(in); *p != '\0'; p++) {
    switch (*p) {
      case '"': sink.puts("\\\""); break;
      case '\\': sink.puts("\\\\"); break;
      case '\b': sink.puts("\\b"); break;
      case '\f': sink.puts("\\f"); break;
      case '\n': sink.puts("\\n"); break;
      case '\r': sink.puts("\\r"); break;
      case '\t': sink.puts("\\t"); break;
      default:
        if (*p < 0x20) {
          char buf[8];
          snprintf(buf, sizeof(buf), "\\u%04x", *p);
          sink.puts(buf);
        } else {
          sink.put(static_cast<char>(*p));  // UTF-8 bytes unchanged
        }
        break;
    }
  }
  if (out != nullptr && cap > 0) out[sink.written < cap ? sink.written : cap - 1] = '\0';
  return sink.written;
}

}  // namespace netescape
