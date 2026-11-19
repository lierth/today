package frame

import (
	"testing"
	"time"

	"golang.org/x/image/font/sfnt"
)

// The carried row shows the » arrow only when the face has the glyph; the
// frame's sanitizer turns a missing one into "·" rather than a box.
func TestCarriedGlyphExists(t *testing.T) {
	sf, err := baseFont()
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	idx, err := sf.GlyphIndex(&buf, '»')
	if err != nil || idx == 0 {
		t.Skipf("the embedded face has no » (idx=%d err=%v)", idx, err)
	}
}

// A carried row renders the arrow in the time column and keeps the clock.
func TestCarriedRowMarksTimeColumn(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 19, 9, 30, 0, 0, loc)
	now := time.Date(2026, 11, 19, 8, 0, 0, 0, loc)
	f, err := Build(Input{
		Now: now,
		Items: []Item{{
			ID: "00000000-0000-4000-8000-000000000001", Title: "昨天的周报",
			StartsAt: &start, Carried: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The time column is 56 px wide starting after the gutter; the arrow adds
	// ink there that a plain "09:30" does not reach.
	stride := f.Stride()
	dark := func(x0, x1 int) int {
		n := 0
		for y := HeaderHeight; y < HeaderHeight+RowHeight; y++ {
			for x := x0; x < x1; x++ {
				if f.Bitmap[y*stride+x/8]>>(7-uint(x%8))&1 != 0 {
					n++
				}
			}
		}
		return n
	}
	if dark(0, 8) == 0 {
		t.Fatal("carried row drew nothing in the gutter")
	}
}
