package main

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSpiralBandHasRoundEnds(t *testing.T) {
	p := bandPts([]pt{{0, 0}, {10, 0}}, 1)
	left, right := 0.0, 10.0
	for _, q := range p {
		left, right = math.Min(left, q.x), math.Max(right, q.x)
	}
	if left != -1 || right != 11 {
		t.Fatalf("round caps have bounds %v..%v, want -1..11", left, right)
	}
}

func TestRoundSnailKeepsBothAntennaeAndOpenGroove(t *testing.T) {
	s := smallSnail()
	g := newGrooveField(s)
	for _, tc := range []struct {
		name string
		p    pt
		ink  bool
	}{
		{"left antenna", pt{30.8, 12}, true},
		{"right antenna", pt{38, 12.3}, true},
		{"gap between antennae", pt{34.4, 12.1}, false},
		{"shell rim", pt{6, 20}, true},
		{"open spiral", pt{8.3, 18.7}, false},
		{"foot", pt{17, 34}, true},
	} {
		if got := s.inside(g, tc.p.x, tc.p.y); got != tc.ink {
			t.Errorf("%s: ink = %v, want %v", tc.name, got, tc.ink)
		}
	}
}

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
