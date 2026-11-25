package frame

import (
	"fmt"
	"image"
	"image/png"
	"io"
)

// WritePNG renders the packed 1-bit bitmap as a greyscale PNG, magnifying
// each panel pixel by scale (1 = 400x300). Kept in the frame package so the
// web preview, the CLI and the golden tests all share one renderer.
//
// Preview output also shows sample widget values (clock, wifi, climate,
// power): the encoded frame leaves those rectangles blank, so the picture is
// illustrative only and the ETag never depends on it.
func (f *Frame) WritePNG(w io.Writer, scale int) error {
	if scale < 1 {
		scale = 1
	}
	width, height := int(f.Width), int(f.Height)
	base := image.NewGray(image.Rect(0, 0, width, height))
	for i := range base.Pix {
		base.Pix[i] = 0xFF
	}
	stride := f.Stride()
	for y := range height {
		for x := range width {
			if f.Bitmap[y*stride+x/8]&(1<<(7-uint(x%8))) != 0 {
				base.Pix[y*base.Stride+x] = 0
			}
		}
	}
	drawWidgetSamples(base, f.Widgets)
	img := image.NewGray(image.Rect(0, 0, width*scale, height*scale))
	for y := range height {
		for x := range width {
			v := base.Pix[y*base.Stride+x]
			for dy := range scale {
				row := img.Pix[(y*scale+dy)*img.Stride:]
				for dx := range scale {
					row[x*scale+dx] = v
				}
			}
		}
	}
	return png.Encode(w, img)
}

// drawWidgetSamples paints illustrative values into the preview bitmap.
// The encoded frame leaves these rectangles blank, so the picture is for the
// web/CLI preview only and never changes the ETag.
func drawWidgetSamples(img *image.Gray, widgets []Widget) {
	fill := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			if y < 0 || y >= img.Rect.Dy() {
				continue
			}
			for x := x0; x < x1; x++ {
				if x >= 0 && x < img.Rect.Dx() {
					img.Pix[y*img.Stride+x] = 0
				}
			}
		}
	}
	outline := func(x0, y0, x1, y1 int) {
		fill(x0, y0, x1, y0+1)
		fill(x0, y1-1, x1, y1)
		fill(x0, y0, x0+1, y1)
		fill(x1-1, y0, x1, y1)
	}
	// Use the device's bitmap font so status size and alignment match hardware.
	textAt := func(s string, cx, cy, scale int) {
		for _, ch := range s {
			index := int(ch - '0')
			if ch == ':' {
				index = 10
			} else if ch == '%' {
				index = 11
			}
			for col, bits := range numericGlyphs[index] {
				for row := range 8 {
					if bits&(1<<row) != 0 {
						fill(cx+col*scale, cy+row*scale, cx+(col+1)*scale, cy+(row+1)*scale)
					}
				}
			}
			cx += 6 * scale
		}
	}
	for _, w := range widgets {
		x, y := int(w.X), int(w.Y)
		textY := y + (int(w.H)-8)/2
		switch w.Kind {
		case WidgetClock:
			scale := max(1, min(int(w.W)/30, int(w.H)/8, 7))
			textAt("14:35", x+int(w.W)-30*scale, y+(int(w.H)-8*scale)/2, scale)
		case WidgetWifi:
			// Three ascending bars; the frame declares where, not what.
			baseY := y + (int(w.H)-9)/2 + 9
			for i, h := range []int{3, 6, 9} {
				fill(x+3+i*5, baseY-h, x+3+i*5+3, baseY)
			}
		case WidgetPower:
			bx, by := x+1, textY
			outline(bx, by, bx+14, by+8)
			fill(bx+14, by+2, bx+16, by+5)
			fill(bx+2, by+2, bx+2+(12*76)/100, by+6)
			textAt("76%", x+21, textY, 1)
		case WidgetClimate:
			textAt("23", x+1, textY, 1)
			outline(x+14, textY, x+17, textY+3)
			textAt("45%", x+24, textY, 1)
		}
	}
}

var numericGlyphs = [...][5]byte{
	{0x3E, 0x51, 0x49, 0x45, 0x3E}, {0, 0x42, 0x7F, 0x40, 0},
	{0x72, 0x49, 0x49, 0x49, 0x46}, {0x21, 0x41, 0x49, 0x4D, 0x33},
	{0x18, 0x14, 0x12, 0x7F, 0x10}, {0x27, 0x45, 0x45, 0x45, 0x39},
	{0x3C, 0x4A, 0x49, 0x49, 0x31}, {0x41, 0x21, 0x11, 0x09, 7},
	{0x36, 0x49, 0x49, 0x49, 0x36}, {0x46, 0x49, 0x49, 0x29, 0x1E},
	{0, 0, 0x14, 0, 0},
	{0x23, 0x13, 0x08, 0x64, 0x62}, // %
}

// PBM writes the bitmap as binary PBM (P4), the golden-file format used by
// the render tests.
func (f *Frame) PBM(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "P4\n%d %d\n", f.Width, f.Height); err != nil {
		return err
	}
	if _, err := w.Write(f.Bitmap); err != nil {
		return err
	}
	return nil
}
