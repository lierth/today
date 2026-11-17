package frame

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// supersample is the rendering scale: text is drawn at 2x and box-filtered
// down to the 1-bit panel bitmap (docs/PLAN.md §6).
const supersample = 2

const blackLevel = 140

// canvas draws in device pixels (supersampled), then downsamples.
type canvas struct {
	img *image.Gray
}

func newCanvas(w, h int) *canvas {
	img := image.NewGray(image.Rect(0, 0, w*supersample, h*supersample))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	return &canvas{img: img}
}

func px(v int) int { return v * supersample }

// fillRect paints a rectangle given in device pixels.
func (c *canvas) fillRect(x0, y0, x1, y1 int, level uint8) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	x0 = max(x0, 0)
	y0 = max(y0, 0)
	x1 = min(x1, c.img.Rect.Dx())
	y1 = min(y1, c.img.Rect.Dy())
	for y := y0; y < y1; y++ {
		row := c.img.Pix[y*c.img.Stride:]
		for x := x0; x < x1; x++ {
			row[x] = level
		}
	}
}

// fillLogical paints a rectangle given in 1x layout coordinates.
func (c *canvas) fillLogical(x0, y0, x1, y1 int, level uint8) {
	c.fillRect(px(x0), px(y0), px(x1), px(y1), level)
}

// hlineLogical draws a horizontal rule thick1 1x pixels tall.
func (c *canvas) hlineLogical(x0, x1, y, thick1 int, level uint8) {
	c.fillLogical(x0, y, x1, y+thick1, level)
}

// text draws s with its baseline at device-pixel coordinates (x,y).
func (c *canvas) text(face font.Face, s string, x, y int, level uint8) {
	d := font.Drawer{
		Dst:  c.img,
		Src:  image.NewUniform(color.Gray{Y: level}),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

// textBold fakes bold by drawing the string twice with a 1-device-pixel
// offset. The embedded font has a single weight and the plan asks for bold
// only in the header.
func (c *canvas) textBold(face font.Face, s string, x, y int, level uint8) {
	c.text(face, s, x, y, level)
	c.text(face, s, x+supersample, y, level)
}

// bitmap downsamples the 2x canvas into the packed 1-bit panel bitmap,
// row-major, MSB first.
func (c *canvas) bitmap(w, h int) []byte {
	stride := (w + 7) / 8
	out := make([]byte, stride*h)
	for y := range h {
		for x := range w {
			var sum int
			for dy := range supersample {
				row := (y*supersample + dy) * c.img.Stride
				for dx := range supersample {
					sum += int(c.img.Pix[row+x*supersample+dx])
				}
			}
			avg := sum / (supersample * supersample)
			if avg < blackLevel {
				out[y*stride+x/8] |= 1 << (7 - uint(x%8))
			}
		}
	}
	return out
}
