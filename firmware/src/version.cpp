#include "version.h"

namespace version {
namespace {

struct Parts {
  int n[3];
  bool ok;
};

// Parse "v1.2.3", "1.2.3-rc1", "0.0.0-dev". The first three dot-separated
// numbers count; a suffix on the third part is ignored.
Parts Parse(const char* raw) {
  Parts out{{0, 0, 0}, false};
  if (raw == nullptr) return out;
  const char* p = raw;
  if (*p == 'v' || *p == 'V') p++;
  int part = 0;
  bool digits = false;
  for (; *p != '\0' && part < 3; p++) {
    if (*p >= '0' && *p <= '9') {
      out.n[part] = out.n[part] * 10 + (*p - '0');
      digits = true;
      if (out.n[part] > 100000) return Parts{{0, 0, 0}, false};
      continue;
    }
    if (*p == '.') {
      if (!digits) return Parts{{0, 0, 0}, false};
      part++;
      digits = false;
      continue;
    }
    break;  // a suffix (-rc1, -dev, +build) ends the numeric part
  }
  out.ok = part == 2 && digits;
  return out;
}

}  // namespace

bool Newer(const char* remote, const char* local) {
  const Parts r = Parse(remote);
  const Parts l = Parse(local);
  if (!r.ok || !l.ok) return false;
  for (int i = 0; i < 3; i++) {
    if (r.n[i] != l.n[i]) return r.n[i] > l.n[i];
  }
  return false;
}

}  // namespace version
