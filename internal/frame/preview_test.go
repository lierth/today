package frame

import (
	"image"
	"testing"
)

func TestPreviewStatusWidgetsStaySmallAndCentered(t *testing.T) {
	for _, w := range WidgetRects {
		if w.Kind == WidgetClock {
			continue
		}
		img := image.NewGray(image.Rect(0, 0, Width, Height))
		for i := range img.Pix {
			img.Pix[i] = 255
		}
		drawWidgetSamples(img, []Widget{w})
		bounds := image.Rect(int(w.X), int(w.Y), int(w.X+w.W), int(w.Y+w.H))
		top, bottom := Height, -1
		for y := range Height {
			for x := range Width {
				if img.GrayAt(x, y).Y != 0 {
					continue
				}
				if !image.Pt(x, y).In(bounds) {
					t.Fatalf("widget %d draws outside its rectangle at %d,%d", w.Kind, x, y)
				}
				top, bottom = min(top, y), max(bottom, y)
			}
		}
		if bottom < top || bottom-top+1 > 9 {
			t.Fatalf("widget %d has missing or oversized status content: %d..%d", w.Kind, top, bottom)
		}
		if delta := top + bottom - (bounds.Min.Y + bounds.Max.Y - 1); delta < -1 || delta > 1 {
			t.Fatalf("widget %d is not vertically centered: %d..%d", w.Kind, top, bottom)
		}
	}
}
