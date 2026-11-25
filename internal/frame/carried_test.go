package frame

import (
	"bytes"
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
	plain, err := Build(Input{Now: now, Items: []Item{{ID: "00000000-0000-4000-8000-000000000001", Title: "昨天的周报", StartsAt: &start}}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(f.Bitmap[72*f.Stride():96*f.Stride()], plain.Bitmap[72*plain.Stride():96*plain.Stride()]) {
		t.Fatal("carried indicator disappeared from the focus time badge")
	}
}
