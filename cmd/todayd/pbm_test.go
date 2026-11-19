package main

import (
	"bufio"
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// A P4 file decodes to the same pixels the firmware wrote: 1 = black.
func TestPBM2PNGP4(t *testing.T) {
	// 10x2: first row black, second row white, with padding bits set to 1 so a
	// decoder that ignores them is caught.
	pbm := []byte("P4\n10 2\n")
	pbm = append(pbm, 0xFF, 0xC0) // row 0: 10 black bits
	pbm = append(pbm, 0x00, 0x3F) // row 1: 10 white bits
	dir := t.TempDir()
	in := filepath.Join(dir, "x.pbm")
	out := filepath.Join(dir, "x.png")
	if err := os.WriteFile(in, pbm, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runPBM2PNG([]string{in, out}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 10 || img.Bounds().Dy() != 2 {
		t.Fatalf("size %v", img.Bounds())
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r != 0 || g != 0 || b != 0 {
		t.Fatalf("black pixel decoded as %d,%d,%d", r>>8, g>>8, b>>8)
	}
	r, _, _, _ = img.At(9, 1).RGBA()
	if r>>8 != 255 {
		t.Fatalf("white pixel decoded as %d", r>>8)
	}
}

// Comments and odd whitespace in the header are allowed by the format.
func TestDecodePBMHeaderNoise(t *testing.T) {
	src := "P4\n# written by the firmware test\n  8   1\n" + string([]byte{0xFF})
	img, err := decodePBM(bufio.NewReader(bytes.NewBufferString(src)))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 8 || img.Bounds().Dy() != 1 {
		t.Fatalf("size %v", img.Bounds())
	}
	// Reading one byte too many would leave the row short; this would fail.
	if r, _, _, _ := img.At(0, 0).RGBA(); r>>8 != 0 {
		t.Fatal("first pixel is not black")
	}
}

// Garbage is refused with a message, not a panic.
func TestDecodePBMRejectsJunk(t *testing.T) {
	for _, src := range []string{
		"P6\n1 1\n255\nabc",
		"P4\nabc",
		"P4\n8 1\n", // truncated raster
	} {
		if _, err := decodePBM(bufio.NewReader(bytes.NewBufferString(src))); err == nil {
			t.Errorf("accepted %q", src)
		}
	}
}
