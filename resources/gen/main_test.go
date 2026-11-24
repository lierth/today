package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestIconShadowIsNeutralNotWhite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "icon.png")
	if err := writeIcon(path, fullSnail(), 128); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(64, 119).RGBA()
	if a == 0 || r != 0 || g != 0 || b != 0 {
		t.Fatalf("shadow must be translucent black, got rgba(%d,%d,%d,%d)", r, g, b, a)
	}
}
