#include <assert.h>
#include <string.h>

#include "rowstate.h"

int main() {
  uint8_t bitmap[15000] {};
  tbf::Row rows[2] {};
  rows[0].y = 44;
  rows[0].flags = tbf::kRowDone;
  rows[1].y = 68;
  rows[1].flags = tbf::kRowSummary;
  auto black = [&](int x, int y) { return (bitmap[y * 50 + x / 8] & (0x80 >> (x % 8))) != 0; };

  rowstate::Apply(bitmap, rows, 2, 1, 255, 16, 24);
  assert(black(20, 54));  // checked interior
  assert(!black(18, 55)); // white tick
  rowstate::Apply(bitmap, rows, 2, 0, 255, 16, 24);
  assert(!black(20, 54)); // uncheck beats the cached server flag
  assert(black(18, 51));  // the outline remains
  assert(!black(16, 75)); // a summary is never given a checkbox

  memset(bitmap, 0, sizeof(bitmap));
  rowstate::Apply(bitmap, rows, 2, 0, 0, 16, 24);
  assert(black(0, 44) && black(399, 67)); // selection survives frame packing
  assert(!black(0, 68));

  rows[0].id[0] = 2;
  rows[1].id[0] = 1;
  rows[1].flags = 0;
  uint32_t mask = rowstate::DoneMask(rows, 2);
  assert(mask == 1);
  mask = rowstate::ApplyOp(mask, rows, 2, rows[1].id, true);
  mask = rowstate::ApplyOp(mask, rows, 2, rows[0].id, false);
  assert(mask == 2); // pending operations follow IDs after rows are reordered

  memset(bitmap, 0, sizeof(bitmap));
  rows[0].y = 72; rows[0].height = 96; rows[0].flags = tbf::kRowHero;
  rows[1].y = 179; rows[1].height = 24; rows[1].flags = 0;
  bitmap[130 * 50 + 100 / 8] |= 0x80 >> (100 % 8); // large Chinese title ink
  rowstate::Apply(bitmap, rows, 2, 1, 1, 24, 24, true);
  assert(black(100,130)); // overlay does not invert or erase the hero title
  assert(black(84,80));   // an offline completion appears beside the badge
  assert(black(16,183) && !black(0,179)); // narrow selection marker
  rowstate::Apply(bitmap, rows, 2, 0, 0, 24, 24, true);
  assert(!black(84,80) && black(100,130)); // undo preserves the title
  rows[1].flags = tbf::kRowSummary;
  assert(rowstate::Next(rows,2,0)==0); // summary is not an actionable task
}
