package frame

import (
	"testing"
	"time"
)

// The "+N 项" row stands in for the rows that did not fit, so its label belongs
// in the title column, not under the round gutter: the list's left edge stays
// straight all the way down.
func TestSummaryRowAlignsWithTitles(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.Local)
	items := make([]Item, 0, 20)
	for i := range 20 {
		at := now.Add(time.Duration(i) * time.Hour)
		items = append(items, Item{
			ID:       "00000000-0000-4000-8000-0000000000" + string(rune('a'+i)),
			Title:    "第" + string(rune('一'+i)) + "件事",
			StartsAt: &at,
		})
	}
	f, err := Build(Input{Now: now, Items: items, CursorRow: -1})
	if err != nil {
		t.Fatal(err)
	}
	var summary *Row
	for i := range f.Rows {
		if f.Rows[i].Flags&RowFlagSummary != 0 {
			summary = &f.Rows[i]
			break
		}
	}
	if summary == nil {
		t.Fatal("no summary row: the list fit on one panel")
	}
	stride := f.Stride()
	leftmost := Width
	for y := int(summary.Y); y < int(summary.Y)+RowHeight; y++ {
		for x := 0; x < Width; x++ {
			if f.Bitmap[y*stride+x/8]>>(7-uint(x%8))&1 != 0 {
				if x < leftmost {
					leftmost = x
				}
			}
		}
	}
	titleColumn := ContentX + gutterWidth + gutterGap + timeColumnWidth + timeGap
	if leftmost < titleColumn-2 {
		t.Fatalf("summary text starts at x=%d, want the title column at x=%d", leftmost, titleColumn)
	}
}
