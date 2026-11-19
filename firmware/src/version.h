// Firmware version comparison. Pure C++ (no Arduino headers) so the host test
// can pin the rules: a board must upgrade only to a strictly newer version,
// because the server keeps advertising the same X-Firmware-Version on every
// frame and an "!=" test would reboot the board forever.
#pragma once

namespace version {

// Newer reports whether `remote` is strictly newer than `local`.
//
// Both sides are "major.minor.patch" with an optional leading v. A missing or
// malformed part counts as 0. A development build ("0.0.0-dev", or anything
// without three numbers) never upgrades itself, and a remote with a suffix
// after the patch ("0.4.0-rc1") is treated as 0.4.0 so a pre-release cannot
// pin a board that already runs the release.
bool Newer(const char* remote, const char* local);

}  // namespace version
