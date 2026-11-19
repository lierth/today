// Host-side test for the ST7305 packer and the TBF parser. No Arduino
// dependency: build and run on the Mac with
//
//   clang++ -std=c++17 -Wall -Wextra -I firmware/src \
//     firmware/test/pack_test.cpp firmware/src/st7305.cpp firmware/src/tbf.cpp firmware/src/fontcheck.cpp firmware/src/netescape.cpp firmware/src/portal_plan.cpp firmware/src/sound_synth.cpp firmware/src/sound_policy.cpp firmware/src/screens.cpp firmware/src/version.cpp \
//     -o /tmp/pack_test && /tmp/pack_test
//
// It also parses a fmt-3 frame produced by `todayd preview` (pass its path as
// argv[1]) and checks the widget table matches the server contract.
//
// The self-drawn screens are written as binary PBM (P4) to /tmp/todayui. Turn
// them into PNGs for review with:
//
//   for f in /tmp/todayui/device-*.pbm; do
//     go run ./cmd/todayd pbm2png "$f" "${f%.pbm}.png"
//   done
//
// The test does not encode PNG: an earlier hand-written encoder produced a bad
// chunk CRC and an unterminated zlib stream, and a viewer reading those files
// hung. PBM needs no compression, and image/png does the rest.
#include <stdio.h>
#include <vector>
#include <algorithm>
#include <stdlib.h>
#include <string.h>

#include "font.h"
#include "fontcheck.h"
#include "netescape.h"
#include "portal_plan.h"
#include "screens.h"
#include "sound_policy.h"
#include "version.h"
#include "sound_synth.h"
#include "portal_config.h"
#include "night.h"
#include "st7305.h"
#include "tbf.h"

namespace {

int g_failures = 0;

void Expect(bool ok, const char* what) {
  if (!ok) {
    printf("FAIL %s\n", what);
    g_failures++;
  }
}

void SetBlack(uint8_t* bmp, int x, int y) {
  bmp[static_cast<size_t>(y) * (st7305::kWidth / 8) + (x >> 3)] |=
      static_cast<uint8_t>(0x80 >> (x & 7));
}

bool IsWhite(const uint8_t* packed, int x, int y) {
  const st7305::Cell cell = st7305::CellFor(static_cast<uint16_t>(x), static_cast<uint16_t>(y));
  return (packed[cell.index] & cell.mask) != 0;
}

void TestCorners() {
  uint8_t tbf[st7305::kPackedBytes];
  memset(tbf, 0, sizeof(tbf));
  SetBlack(tbf, 0, 0);
  SetBlack(tbf, st7305::kWidth - 1, 0);
  SetBlack(tbf, 0, st7305::kHeight - 1);
  SetBlack(tbf, st7305::kWidth - 1, st7305::kHeight - 1);
  SetBlack(tbf, 1, 0);
  SetBlack(tbf, 0, 1);

  uint8_t packed[st7305::kPackedBytes];
  Expect(st7305::Pack(tbf, sizeof(tbf), packed, sizeof(packed)), "pack corners");

  Expect(!IsWhite(packed, 0, 0), "pixel (0,0) black");
  Expect(!IsWhite(packed, st7305::kWidth - 1, 0), "pixel (399,0) black");
  Expect(!IsWhite(packed, 0, st7305::kHeight - 1), "pixel (0,299) black");
  Expect(!IsWhite(packed, st7305::kWidth - 1, st7305::kHeight - 1), "pixel (399,299) black");
  Expect(!IsWhite(packed, 1, 0), "pixel (1,0) black");
  Expect(!IsWhite(packed, 0, 1), "pixel (0,1) black");
  Expect(IsWhite(packed, 2, 0), "pixel (2,0) stays white");

  // The formula must be a bijection: every pixel gets a distinct cell+mask,
  // and the 120,000 bits exactly cover the 15,000-byte buffer.
  static bool seen[st7305::kPackedBytes * 8];
  memset(seen, 0, sizeof(seen));
  int duplicates = 0;
  for (int y = 0; y < st7305::kHeight; y++) {
    for (int x = 0; x < st7305::kWidth; x++) {
      const st7305::Cell cell = st7305::CellFor(static_cast<uint16_t>(x), static_cast<uint16_t>(y));
      if (cell.index >= st7305::kPackedBytes) {
        Expect(false, "cell outside buffer");
        continue;
      }
      int bit = 0;
      while (((cell.mask >> bit) & 1) == 0) bit++;  // bit set position 0..7
      const size_t slot = static_cast<size_t>(cell.index) * 8 + static_cast<size_t>(bit);
      if (seen[slot]) duplicates++;
      seen[slot] = true;
    }
  }
  Expect(duplicates == 0, "every pixel has a unique cell+mask");
}

void TestAllBlack() {
  uint8_t tbf[st7305::kPackedBytes];
  memset(tbf, 0xFF, sizeof(tbf));
  uint8_t packed[st7305::kPackedBytes];
  Expect(st7305::Pack(tbf, sizeof(tbf), packed, sizeof(packed)), "pack all black");
  for (size_t i = 0; i < sizeof(packed); i++) {
    if (packed[i] != 0x00) {
      Expect(false, "all-black buffer is not zero");
      return;
    }
  }
}

void TestAllWhite() {
  uint8_t tbf[st7305::kPackedBytes];
  memset(tbf, 0x00, sizeof(tbf));
  uint8_t packed[st7305::kPackedBytes];
  Expect(st7305::Pack(tbf, sizeof(tbf), packed, sizeof(packed)), "pack all white");
  for (size_t i = 0; i < sizeof(packed); i++) {
    if (packed[i] != 0xFF) {
      Expect(false, "all-white buffer is not 0xFF");
      return;
    }
  }
}

void TestNightWindow() {
  // 23:00-07:00: 22:59 outside, 23:00 inside, 03:00 inside, 06:59 inside,
  // 07:00 outside. start == end disables the window.
  Expect(!night::Inside(22 * 60 + 59, 23 * 60, 7 * 60), "22:59 outside");
  Expect(night::Inside(23 * 60, 23 * 60, 7 * 60), "23:00 inside");
  Expect(night::Inside(3 * 60, 23 * 60, 7 * 60), "03:00 inside");
  Expect(night::Inside(6 * 60 + 59, 23 * 60, 7 * 60), "06:59 inside");
  Expect(!night::Inside(7 * 60, 23 * 60, 7 * 60), "07:00 outside");
  Expect(!night::Inside(12 * 60, 23 * 60, 7 * 60), "12:00 outside");
  Expect(!night::Inside(23 * 60, 23 * 60, 23 * 60), "start==end disables");
  // A same-day window (lunch nap) still works.
  Expect(night::Inside(13 * 60, 12 * 60, 14 * 60), "13:00 in 12-14");
  Expect(!night::Inside(14 * 60, 12 * 60, 14 * 60), "14:00 outside 12-14");
}

// Both banners share one strip and one text fitter. The reminder is the
// inverse of the lost-Wi-Fi line; neither may start left of the padding or
// run past the right edge.
void TestBannerGeometry() {
  const int panel = st7305::kWidth;
  // Centred when it fits.
  Expect(screens::BannerTextX(panel, 200) == (panel - 200) / 2, "banner text centred");
  // The widest banner string still lands inside the panel.
  const int tight = screens::BannerTextX(panel, panel - 4);
  Expect(tight == screens::kBannerPad, "wide banner starts at the padding");
  Expect(tight + panel - 4 <= panel, "wide banner stays on the panel");
  // Wider than the panel: pinned to the left edge rather than a negative x.
  Expect(screens::BannerTextX(panel, panel + 40) == 0, "oversized banner starts at 0");
  // Vertically centred in the band. The glyph cell is eight rows now (the
  // descender used to be clipped), so a scale-2 line is 16 px, not 14.
  Expect(screens::BannerTextY(20, 24, 2) == 20 + (24 - 16) / 2, "banner text centred in the band");
  // And the descender still lands inside the band rather than below it.
  Expect(screens::BannerTextY(20, 24, 2) + font::kGlyphHeight * 2 <= 20 + 24,
         "banner line fits its band");
}

// "WIFI LOST - HOLD KEY 3S TO SET UP" replaces the 13 px lunar line under the
// date, so it must fit that strip at scale 2 without touching the widgets.
void TestWifiLostBannerFits() {
  const char* text = "WIFI LOST - HOLD KEY 3S TO SET UP";
  const int width = static_cast<int>(strlen(text)) * 6 * 2;
  printf("wifi lost banner: %d chars at scale 2 = %d px (panel %d)\n",
         static_cast<int>(strlen(text)), width, st7305::kWidth);
  if (width > st7305::kWidth - 4) {
    printf("FAIL banner is %d px wide, panel is %d\n", width, st7305::kWidth);
    g_failures++;
  }
}

// The setup screen draws the 10-digit password grouped as "4821993057" at
// scale 5; 11 chars at 6 px per scale unit plus the 16 px margin must fit.
// The expected string comes from the same formatter, not from counting by
// hand.
void TestPortalLayoutFits() {
  char shown[portalcfg::kPasswordLength + 2];
  portalplan::PasswordDisplay("4821993057", shown, sizeof(shown));
  if (strcmp(shown, "48219 93057") != 0) {
    printf("FAIL password display %s, want %s\n", shown, "48219 93057");
    g_failures++;
  }
  const int chars = static_cast<int>(strlen(shown));
  const int width = chars * 6 * 5;
  printf("portal password: %d chars at scale 5 = %d px (panel %d)\n", chars, width,
         st7305::kWidth);
  if (16 + width > st7305::kWidth) {
    printf("FAIL portal password at scale 5 is %d px wide, panel is %d\n", 16 + width, st7305::kWidth);
    g_failures++;
  }
  // Height, not only width: the password sits at y = header+88 and its
  // descenders reach eight rows down. It must clear the URL line at
  // kHeight-72 and stay inside the panel.
  const int top = screens::kHeaderHeight + 88;
  const int bottom = top + font::kGlyphHeight * 5;
  const int urlTop = screens::kHeight - 72;
  printf("portal password band: y %d..%d, url line at %d\n", top, bottom, urlTop);
  if (bottom >= urlTop || bottom > screens::kHeight) {
    printf("FAIL password band %d..%d collides with the URL line at %d\n", top, bottom, urlTop);
    g_failures++;
  }
}

// The clock text is wider than its declared rect; right-aligning it must keep
// it on the panel ("14:05" is 90 px at scale 3, the panel is 400 wide).
void TestClockFitsPanel() {
  const int x = screens::ClockTextX(320, 64, 3);
  const int w = 5 * 6 * 3;
  printf("clock: x=%d width=%d right=%d (panel %d)\n", x, w, x + w, st7305::kWidth);
  if (x < 0 || x + w > st7305::kWidth) {
    printf("FAIL clock text runs off the panel\n");
    g_failures++;
  }
  // Right-aligned to the rect: the declared 16 px margin on the right stays.
  if (x + w != 320 + 64) {
    printf("FAIL clock not right-aligned to its rect (ends at %d)\n", x + w);
    g_failures++;
  }
  // A rect pushed too far right is clamped to the panel rather than clipped.
  if (screens::ClockTextX(360, 64, 3) + w > st7305::kWidth) {
    printf("FAIL clock clamp does nothing for a further-right rect\n");
    g_failures++;
  }
}

// The portal screen must be complete before the radio is started: StartPortal
// paints first and calls portal::Start() afterwards, so a slow AP bring-up
// never leaves the old screen on the panel. This asserts the layout the paint
// produces is the full one (title rule, password, countdown), i.e. nothing in
// it depends on the radio being up.
void TestPortalPaintIndependentOfRadio() {
  static uint8_t bmp[screens::kBytes];
  memset(bmp, 0xFF, sizeof(bmp));
  char shown[portalcfg::kPasswordLength + 2];
  portalplan::PasswordDisplay("1234567890", shown, sizeof(shown));
  screens::DrawPortal(bmp, "Today-1A2B", shown, "http://192.168.4.1");
  // Header rule present.
  bool rule = false;
  for (size_t x = 0; x < screens::kWidth; x++) {
    if ((bmp[(screens::kHeaderHeight - 1) * screens::kStride + x / 8] & (0x80 >> (x % 8))) == 0) {
      rule = true;
    }
  }
  Expect(rule, "portal draws its header rule without the radio");
  // The SSID line carries ink: a screen painted before Start() is complete.
  int ssidInk = 0;
  for (int y = screens::kHeaderHeight + 34; y < screens::kHeaderHeight + 34 + 16; y++) {
    for (size_t x = 0; x < screens::kWidth; x++) {
      if ((bmp[y * screens::kStride + x / 8] & (0x80 >> (x % 8))) == 0) ssidInk++;
    }
  }
  Expect(ssidInk > 50, "portal shows the SSID before Start()");
}

void TestFontCoverage() {
  if (const char* missing = fontcheck::Missing()) {
    printf("FAIL font: no glyph for \"%s\"\n", missing);
    g_failures++;
  }
}

// Every string literal drawn with PaintMessage/DrawString in main.cpp must be
// covered too. This reads the source so a new literal cannot slip past the
// hand-maintained list in fontcheck.cpp.
void TestSourceStrings(const char* mainPath) {
  FILE* f = fopen(mainPath, "r");
  if (f == nullptr) {
    printf("SKIP source scan %s (not found)\n", mainPath);
    return;
  }
  char line[1024];
  while (fgets(line, sizeof(line), f) != nullptr) {
    const char* call = strstr(line, "PaintMessage(");
    const char* draw = strstr(line, "DrawString(");
    if (call == nullptr && draw == nullptr) continue;
    const char* p = call ? call : draw;
    while (*p != '"' && *p != '\0') p++;
    while (*p == '"') {
      p++;
      char text[256];
      size_t n = 0;
      while (*p != '"' && *p != '\0' && n + 1 < sizeof(text)) {
        if (*p == '\\' && p[1] != '\0') p++;
        text[n++] = *p++;
      }
      text[n] = '\0';
      if (*p == '"') p++;
      // Skip printf-style arguments that are not literals.
      if (n == 0) {
        while (*p != '"' && *p != '\0') p++;
        continue;
      }
      for (size_t i = 0; i < n; i++) {
        if (text[i] == '%') break;  // format placeholder, not drawn
        if (font::Find(text[i]) == nullptr) {
          printf("FAIL font: main.cpp string \"%s\" missing '%c'\n", text, text[i]);
          g_failures++;
        }
      }
      while (*p != '"' && *p != '\0') p++;
    }
  }
  fclose(f);
}

void TestEscaping() {
  char out[512];
  // HTML text/attribute context: & < > " '
  netescape::Html("<script>a&b=\"x\" 'y'</script>", out, sizeof(out));
  Expect(strcmp(out, "&lt;script&gt;a&amp;b=&quot;x&quot; &#39;y&#39;&lt;/script&gt;") == 0,
         "html escaping covers & < > \" '");
  // Backslash is not special in HTML and must survive untouched.
  netescape::Html("C:\\path", out, sizeof(out));
  Expect(strcmp(out, "C:\\path") == 0, "html keeps backslash");
  // UTF-8 (Chinese SSID) passes through byte for byte.
  netescape::Html("我的WiFi", out, sizeof(out));
  Expect(strcmp(out, "我的WiFi") == 0, "html keeps utf-8");
  // 32-byte SSID boundary: escaping expands but never overflows the buffer.
  char ssid32[33];
  memset(ssid32, '<', 32);
  ssid32[32] = '\0';
  const size_t full = netescape::Html(ssid32, out, sizeof(out));
  Expect(full == 32 * 4 && strlen(out) == 32 * 4, "html expands 4x and fits");
  // A too-small buffer must report the full length and stay NUL-terminated.
  char tiny[10];
  const size_t need = netescape::Html(ssid32, tiny, sizeof(tiny));
  Expect(need == 32 * 4, "html reports the full escaped length");
  Expect(strlen(tiny) == sizeof(tiny) - 1, "html truncates at the buffer edge");

  // JSON: quotes, backslash and control characters are escaped.
  netescape::Json("a\"b\\c\td", out, sizeof(out));
  Expect(strcmp(out, "a\\\"b\\\\c\\td") == 0, "json escaping");
  netescape::Json("我的WiFi", out, sizeof(out));
  Expect(strcmp(out, "我的WiFi") == 0, "json keeps utf-8");
}

// A router reboot takes seconds; a changed password takes a human. The
// scheduler must open the first portal only after the failure countdown, then
// space the unattended attempts out, while a KEY hold ignores both.
void TestPortalPlan() {
  portalplan::Input in{};
  in.threshold = 5;
  in.haveCredentials = true;

  // Connected: never open, even with a manual latch (the hold runs later).
  in.connected = true;
  in.failures = 9;
  Expect(!portalplan::ShouldOpen(in), "no portal while connected");

  // Short outage: under the threshold, nothing opens.
  in.connected = false;
  in.failures = 4;
  Expect(!portalplan::ShouldOpen(in), "no portal before the countdown");

  // Reached the countdown: the first unattended portal opens immediately.
  in.failures = 5;
  Expect(portalplan::ShouldOpen(in), "first portal at the countdown");

  // Already open: never a second one.
  in.portalOpen = true;
  Expect(!portalplan::ShouldOpen(in), "no second portal while one runs");

  // Just closed and the link is still down: wait 10 minutes, not 5.
  in.portalOpen = false;
  in.portalRuns = 1;
  in.lastPortalEnd = 1000;
  in.now = 1000 + 9 * 60 * 1000;
  Expect(!portalplan::ShouldOpen(in), "no immediate reopen");
  in.now = 1000 + 10 * 60 * 1000;
  Expect(portalplan::ShouldOpen(in), "reopen after 10 min");

  // A second closed portal stretches the wait to 30 minutes.
  in.portalRuns = 2;
  in.lastPortalEnd = 2000;
  in.now = 2000 + 20 * 60 * 1000;
  Expect(!portalplan::ShouldOpen(in), "no reopen after 20 min");
  in.now = 2000 + 30 * 60 * 1000;
  Expect(portalplan::ShouldOpen(in), "reopen after 30 min");

  // A person holding KEY 3 s opens it at any time, even with a live link.
  in.manualRequest = true;
  in.connected = true;
  in.now = 2500;
  Expect(portalplan::ShouldOpen(in), "manual open wins");
  in.manualRequest = false;

  // No stored SSID: an unattended portal would have nothing to configure.
  in.connected = false;
  in.haveCredentials = false;
  in.portalRuns = 0;
  in.failures = 200;
  Expect(!portalplan::ShouldOpen(in), "no portal without credentials");

  // A portal yields when the link returns, unless the person opened it.
  Expect(portalplan::CloseOnConnect(true, true), "close when link returns");
  Expect(!portalplan::CloseOnConnect(true, false), "stay while still down");
  Expect(!portalplan::CloseOnConnect(false, true), "nothing to close");

  // Countdown formatting.
  char buf[8];
  portalplan::CountdownText(4 * 60 * 1000 + 32 * 1000, buf, sizeof(buf));
  Expect(strcmp(buf, "4:32") == 0, "countdown 4:32");
  portalplan::CountdownText(999, buf, sizeof(buf));
  Expect(strcmp(buf, "0:01") == 0, "countdown rounds up");
  portalplan::CountdownText(0, buf, sizeof(buf));
  Expect(strcmp(buf, "0:00") == 0, "countdown expired");
}

// Sine synthesis: no clipping, zero at both ends, and the cues differ in a way
// a person can hear (length and pitch).
void TestSoundSynth() {
  static int16_t buf[sound::kMaxSamples];
  for (int cue = 1; cue <= 4; cue++) {
    memset(buf, 0, sizeof(buf));
    const size_t n = sound::Render(static_cast<sound::Cue>(cue), 60, buf, sound::kMaxSamples);
    Expect(n > 0, "cue renders samples");
    Expect(n <= sound::kMaxSamples, "cue fits the buffer");
    Expect(buf[0] == 0 && buf[n - 1] == 0, "envelope starts and ends at zero");
    int peak = 0;
    for (size_t i = 0; i < n; i++) {
      if (buf[i] > 32767 || buf[i] < -32768) {
        Expect(false, "sample out of range");
        break;
      }
      const int v = buf[i] < 0 ? -buf[i] : buf[i];
      if (v > peak) peak = v;
    }
    Expect(peak > 1000, "cue is audible");
    Expect(peak <= static_cast<int>(sound::Amplitude(60)), "cue stays under the volume ceiling");
  }
  // Volume 0 is silence, not a quiet tone.
  Expect(sound::Render(sound::Cue::kCheck, 0, buf, sound::kMaxSamples) == 0, "volume 0 is silent");
  // The check tick is shorter than the alarm.
  const size_t tick = sound::Render(sound::Cue::kCheck, 60, buf, sound::kMaxSamples);
  const size_t alarm = sound::Render(sound::Cue::kAlarm, 60, buf, sound::kMaxSamples);
  Expect(tick < alarm, "the alarm is longer than a tick");
  Expect(alarm <= sound::kMaxSamples, "alarm fits");
  Expect(sound::Envelope(0, 100) == 0 && sound::Envelope(99, 100) == 0, "envelope endpoints");
  Expect(sound::Envelope(50, 100) > 30000, "envelope peaks in the middle");
}

void TestSoundPolicy() {
  int64_t out[sound::kMaxAlarms];
  const uint8_t n = sound::ParseAlarms("1789000000,1789003600,1789007200", out, sound::kMaxAlarms);
  Expect(n == 3 && out[0] == 1789000000 && out[2] == 1789007200, "alarm list parses");
  // Bad tokens are skipped, and the list stays ascending.
  Expect(sound::ParseAlarms("abc,,1789000000,1788999999", out, sound::kMaxAlarms) == 1,
         "bad tokens and rewinds are dropped");
  // At most ten, and the earliest win.
  const uint8_t many = sound::ParseAlarms(
      "1,2,3,4,5,6,7,8,9,10,11,12", out, sound::kMaxAlarms);
  Expect(many == 10 && out[9] == 10, "the list is capped");

  sound::Alarms alarms;
  alarms.at[0] = 1000;
  alarms.count = 1;
  // Due, enabled, daytime: rings.
  Expect(sound::ShouldRing(1000, alarms, true, 60, false, 60) == 0, "due alarm rings");
  // Not yet due.
  Expect(sound::ShouldRing(999, alarms, true, 60, false, 60) == -1, "early alarm waits");
  // Night, disabled, or silent volume: never.
  Expect(sound::ShouldRing(1000, alarms, true, 60, true, 60) == -1, "night is silent");
  Expect(sound::ShouldRing(1000, alarms, false, 60, false, 60) == -1, "alarm switch off");
  Expect(sound::ShouldRing(1000, alarms, true, 0, false, 60) == -1, "volume 0");
  // Missed while asleep: no chime for something an hour old.
  Expect(sound::ShouldRing(4600, alarms, true, 60, false, 60) == -1, "stale alarm is dropped");
  // Fired once stays quiet.
  alarms.fired[0] = true;
  Expect(sound::ShouldRing(1000, alarms, true, 60, false, 60) == -1, "no repeat");

  // X-Sound parsing.
  sound::Policy pol;
  sound::ParsePolicy("vol=20;keys=0;alarm=1", &pol);
  Expect(pol.volume == 20 && !pol.keys && pol.alarm, "policy parses");
  pol = sound::Policy{};
  sound::ParsePolicy("vol=250;keys=1", &pol);
  Expect(pol.volume == 60, "out-of-range volume keeps the default");
  sound::ParsePolicy("", &pol);
  Expect(pol.volume == 60 && pol.keys, "empty policy keeps defaults");

  // Key cues.
  Expect(sound::KeyCue(true, 60, true, 1, 5) == static_cast<int>(sound::Cue::kCheck), "tick on check");
  Expect(sound::KeyCue(true, 60, false, 0, 5) == static_cast<int>(sound::Cue::kUncheck), "tock on uncheck");
  Expect(sound::KeyCue(true, 60, true, 5, 5) == static_cast<int>(sound::Cue::kDone), "arpeggio on completion");
  Expect(sound::KeyCue(false, 60, true, 1, 5) == static_cast<int>(sound::Cue::kNone), "keys off is silent");
  Expect(sound::KeyCue(true, 0, true, 1, 5) == static_cast<int>(sound::Cue::kNone), "volume 0 is silent");
}

// Every screen the board draws itself. Rendered to PNG so a reviewer can look
// at them without a board; the layout rules (header rule, step spacing, hint
// line) are asserted on the pixels.
// WritePBM writes a 1-bit landscape buffer as a binary PBM (P4): the format is
// a two-line header and one bit per pixel, so the host test needs no image
// encoder at all. `cmd/todayd pbm2png` turns these into PNGs with Go's
// image/png; the earlier hand-written PNG encoder here produced files with a
// wrong chunk CRC and an unterminated zlib stream, and a viewer that tried to
// decode them hung.
//
// P4 is 1 = black. `bmp` is the same packing the panel uses (1 = white), so
// the bits are inverted on the way out.
static bool WritePBM(const char* path, const uint8_t* bmp, int w, int h) {
  FILE* f = fopen(path, "wb");
  if (f == nullptr) return false;
  fprintf(f, "P4\n%d %d\n", w, h);
  const size_t stride = static_cast<size_t>(w) / 8;
  for (int y = 0; y < h; y++) {
    for (size_t x = 0; x < stride; x++) {
      const uint8_t white = bmp[static_cast<size_t>(y) * stride + x];
      const uint8_t black = static_cast<uint8_t>(~white);
      if (fwrite(&black, 1, 1, f) != 1) {
        fclose(f);
        return false;
      }
    }
  }
  fclose(f);
  return true;
}

void TestDeviceScreens(const char* outDir) {
  static uint8_t bmp[screens::kBytes];
  struct Case {
    const char* name;
    screens::Kind kind;
    bool accent;
  };
  const Case cases[] = {
      {"waiting", screens::Kind::kWaiting, false},
      {"no-wifi", screens::Kind::kNoWifi, false},
      {"updating", screens::Kind::kUpdating, false},
      {"bad-frame", screens::Kind::kBadFrame, true},
      {"setup-ended", screens::Kind::kSetupEnded, false},
  };
  for (const Case& c : cases) {
    screens::DrawTemplate(bmp, screens::For(c.kind), c.accent);
    char path[256];
    snprintf(path, sizeof(path), "%s/device-%s.pbm", outDir, c.name);
    if (!WritePBM(path, bmp, screens::kWidth, screens::kHeight)) {
      printf("SKIP device screen %s (cannot write %s)\n", c.name, path);
      continue;
    }
    // The header rule (or the filled band) must be there, and the bottom hint
    // line must not touch the last row.
    bool headerPixel = false;
    for (size_t x = 0; x < screens::kWidth; x++) {
      const size_t idx = 43 * screens::kStride + x / 8;
      if ((bmp[idx] & (0x80 >> (x % 8))) == 0) headerPixel = true;
    }
    Expect(headerPixel, "template has a header rule");
    bool lastRowClear = true;
    for (size_t x = 0; x < screens::kWidth; x++) {
      if ((bmp[(screens::kHeight - 1) * screens::kStride + x / 8] & (0x80 >> (x % 8))) == 0) {
        lastRowClear = false;
      }
    }
    Expect(lastRowClear, "bottom edge stays clear");

    // Nothing may fall off the panel now that a glyph is eight rows tall:
    // the last step's descenders and the bottom hint must both be inside.
    struct Line { int y; int scale; const char* what; };
    const Line lines[] = {
        {12, 3, "title"},
        {screens::kHeaderHeight + 24, 2, "step 1"},
        {screens::kHeaderHeight + 58, 2, "step 2"},
        {screens::kHeaderHeight + 92, 2, "step 3"},
        {static_cast<int>(screens::kHeight) - 24, 1, "hint"},
    };
    for (const Line& l : lines) {
      if (l.y + font::kGlyphHeight * l.scale > static_cast<int>(screens::kHeight)) {
        printf("FAIL %s (%s) ends at %d, panel is %d\n", l.what, c.name,
               l.y + font::kGlyphHeight * l.scale, static_cast<int>(screens::kHeight));
        g_failures++;
      }
    }
    // The steps must not run into each other either.
    if (screens::kHeaderHeight + 24 + font::kGlyphHeight * 2 >
        screens::kHeaderHeight + 58) {
      printf("FAIL step 1 overlaps step 2\n");
      g_failures++;
    }
  }
  // The setup hotspot shares the same header and shows the password big.
  // The display password goes through the same formatter the device uses, so
  // the preview cannot show a shape the board never draws (the real one is
  // ten digits in two groups of five).
  memset(bmp, 0xFF, sizeof(bmp));
  char shown[portalcfg::kPasswordLength + 2];
  portalplan::PasswordDisplay("4821993057", shown, sizeof(shown));
  screens::DrawPortal(bmp, "Today-1A2B", shown, "http://192.168.4.1");
  {
    char path[256];
    snprintf(path, sizeof(path), "%s/device-portal.pbm", outDir);
    WritePBM(path, bmp, screens::kWidth, screens::kHeight);
    bool headerRule = false;
    for (size_t x = 0; x < screens::kWidth; x++) {
      const size_t idx = 43 * screens::kStride + x / 8;
      if ((bmp[idx] & (0x80 >> (x % 8))) == 0) headerRule = true;
    }
    Expect(headerRule, "portal keeps the shared header rule");
    // The password band is drawn at scale 5, so it has tall black columns.
    int black = 0;
    for (size_t y = 132; y < 180; y++) {
      for (size_t x = 0; x < screens::kWidth; x++) {
        if ((bmp[y * screens::kStride + x / 8] & (0x80 >> (x % 8))) == 0) black++;
      }
    }
    Expect(black > 400, "portal password is drawn large");
  }
  screens::DrawBoot(bmp, "0.3.0");
  char path[256];
  snprintf(path, sizeof(path), "%s/device-boot.pbm", outDir);
  WritePBM(path, bmp, screens::kWidth, screens::kHeight);
  // The boot screen is mostly white with a centred logo block.
  int black = 0;
  for (size_t i = 0; i < screens::kBytes; i++) {
    black += 8 - __builtin_popcount(bmp[i]);
  }
  Expect(black > 2000 && black < 12000, "boot screen has a logo, not a black page");
  // The done disc is at (x0+32, y0+28) with radius 10; the tick is white
  // inside it. Check the tick has both a descending and an ascending arm and
  // that they meet: two disconnected slashes are the bug this asserts away.
  const int w = 168;
  const int x0 = (static_cast<int>(screens::kWidth) - w) / 2;
  const int y0 = 56;
  const int cx = x0 + 32, cy = y0 + 28;
  auto white = [&](int x, int y) {
    return ((bmp[static_cast<size_t>(y) * screens::kStride + static_cast<size_t>(x) / 8] &
             (0x80 >> (x % 8))) != 0);
  };
  int leftArm = 0, rightArm = 0, joint = 0;
  for (int dy = -6; dy <= 7; dy++) {
    for (int dx = -7; dx <= 7; dx++) {
      if (dx * dx + dy * dy > 10 * 10) continue;
      if (!white(cx + dx, cy + dy)) continue;
      if (dx < 0 && dy <= 5) leftArm++;
      if (dx >= 0 && dy < 0) rightArm++;
      if (abs(dx) <= 1 && abs(dy - 5) <= 1) joint++;
    }
  }
  printf("boot tick: left arm %d px, right arm %d px, joint %d\n", leftArm, rightArm, joint);
  Expect(leftArm > 4 && rightArm > 4 && joint > 0, "boot tick is a joined check, not two slashes");
}

// The board upgrades only to a strictly newer version. An "a != b" test
// reboots forever when the server keeps advertising a string that never equals
// the local one (for example a tag WITH "v" and a build without it).
void TestFirmwareVersionCompare() {
  Expect(!version::Newer("0.4.0", "0.4.0"), "same version does not upgrade");
  Expect(version::Newer("0.4.0", "0.3.9"), "patch upgrade");
  Expect(version::Newer("0.4.0", "0.3.0"), "minor upgrade");
  Expect(version::Newer("v0.4.0", "0.3.0"), "v prefix accepted");
  Expect(version::Newer("1.0.0", "v0.9.9"), "major upgrade across the v prefix");
  Expect(!version::Newer("0.3.0", "0.4.0"), "downgrade refused");
  Expect(!version::Newer("0.4.0", "0.4.0-dev"), "dev build never upgrades");
  Expect(!version::Newer("0.4.0-dev", "0.4.0"), "dev release is not newer");
  Expect(!version::Newer("", "0.4.0"), "empty remote never upgrades");
  Expect(!version::Newer("garbage", "0.4.0"), "junk remote never upgrades");
  Expect(!version::Newer("0.4", "0.4.0"), "two-part version is not newer");
  Expect(version::Newer("0.4.0-rc1", "0.3.0"), "pre-release of a newer base");
  Expect(!version::Newer("0.4.0-rc1", "0.4.0"), "pre-release of the same base");
}

void TestFrameFile(const char* path) {
  FILE* f = fopen(path, "rb");
  if (f == nullptr) {
    printf("SKIP frame %s (not found)\n", path);
    return;
  }
  fseek(f, 0, SEEK_END);
  const long n = ftell(f);
  fseek(f, 0, SEEK_SET);
  uint8_t* buf = static_cast<uint8_t*>(malloc(static_cast<size_t>(n)));
  Expect(buf != nullptr, "frame malloc");
  if (buf == nullptr) {
    fclose(f);
    return;
  }
  Expect(fread(buf, 1, static_cast<size_t>(n), f) == static_cast<size_t>(n), "frame read");
  fclose(f);

  tbf::Frame fr{};
  const bool ok = tbf::Parse(buf, static_cast<size_t>(n), &fr);
  if (!ok) {
    printf("FAIL frame parse: %s\n", tbf::ParseError());
    g_failures++;
    free(buf);
    return;
  }
  printf("frame %s: %ux%u header=%u footer=%u rows=%u widgets=%u bitmap=%zu\n", path,
         fr.width, fr.height, fr.headerHeight, fr.footerHeight, fr.rowCount,
         fr.widgetCount, fr.bitmapLen);
  Expect(fr.width == 400 && fr.height == 300, "frame geometry");
  Expect(fr.headerHeight == 44 && fr.footerHeight == 0, "v2 header/footer");
  Expect(fr.widgetCount == 4, "four widgets");
  // Order is a rendering detail; the contract is that each kind is present
  // with a rectangle inside the panel.
  const uint8_t kinds[] = {tbf::kWidgetClock, tbf::kWidgetWifi,
                           tbf::kWidgetClimate, tbf::kWidgetPower};
  for (uint8_t kind : kinds) {
    const tbf::Widget* w = tbf::Find(fr, kind);
    Expect(w != nullptr, "widget kind present");
    if (w != nullptr) {
      Expect(w->w > 0 && w->h > 0, "widget has area");
      Expect(uint32_t(w->x) + w->w <= fr.width && uint32_t(w->y) + w->h <= fr.height,
             "widget inside panel");
    }
  }
  Expect(fr.bitmapLen == st7305::kPackedBytes, "frame bitmap size");
  free(buf);
}

}  // namespace

int main(int argc, char** argv) {
  TestCorners();
  TestAllBlack();
  TestAllWhite();
  TestNightWindow();
  TestEscaping();
  TestPortalPlan();
  TestWifiLostBannerFits();
  TestSoundSynth();
  TestSoundPolicy();
  TestDeviceScreens("/tmp/todayui");
  TestFontCoverage();
  TestPortalLayoutFits();
  TestBannerGeometry();
  TestClockFitsPanel();
  TestPortalPaintIndependentOfRadio();
  TestFirmwareVersionCompare();
  TestSourceStrings("src/main.cpp");
  if (argc > 1) TestFrameFile(argv[1]);
  if (g_failures != 0) {
    printf("%d failure(s)\n", g_failures);
    return 1;
  }
  printf("ok\n");
  return 0;
}
