package frame

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
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
	drawWidgetSamples(base)
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
func drawWidgetSamples(img *image.Gray) {
	// It measures and draws with the shared faces, so it takes the same lock
	// as Build.
	defer lockRender()()
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
	textAt := func(size int, s string, x, y int) {
		face, err := faceAt(size)
		if err != nil {
			return
		}
		d := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(color.Gray{Y: 0}),
			Face: face,
			Dot:  fixed.P(x, y),
		}
		d.DrawString(sanitize(s))
	}
	for _, w := range WidgetRects {
		x, y := int(w.X), int(w.Y)
		switch w.Kind {
		case WidgetClock:
			textAt(22, "14:05", x+8, y+24)
		case WidgetWifi:
			// Three ascending bars; the frame declares where, not what.
			for i, h := range []int{4, 7, 10} {
				fill(x+2+i*5, y+13-h, x+2+i*5+3, y+13)
			}
		case WidgetPower:
			// Battery outline with a 76% fill, narrower than the power rect
			// so "▮ 76%" never runs into the clock to its right.
			bx, by := x, y+3
			outline(bx, by, bx+16, by+9)
			fill(bx+16, by+3, bx+18, by+6)
			fill(bx+2, by+2, bx+2+(12*76)/100, by+7)
			textAt(9, "76%", bx+20, by+9)
		case WidgetClimate:
			textAt(9, "23° 45%", x+1, y+11)
		}
	}
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
