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
func (f *Frame) WritePNG(w io.Writer, scale int) error {
	if scale < 1 {
		scale = 1
	}
	width, height := int(f.Width), int(f.Height)
	img := image.NewGray(image.Rect(0, 0, width*scale, height*scale))
	stride := f.Stride()
	for y := range height {
		for x := range width {
			v := uint8(255)
			if f.Bitmap[y*stride+x/8]&(1<<(7-uint(x%8))) != 0 {
				v = 0
			}
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
