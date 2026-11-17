package frame

import (
	_ "embed"
	"log"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

//go:embed assets/NotoSansSC-Regular.otf
var fontData []byte

var (
	fontOnce sync.Once
	fontSFNT *sfnt.Font
	fontErr  error
	faces    sync.Map // int pixel size -> font.Face
	missing  sync.Map // rune -> struct{}
)

func baseFont() (*sfnt.Font, error) {
	fontOnce.Do(func() {
		fontSFNT, fontErr = sfnt.Parse(fontData)
	})
	return fontSFNT, fontErr
}

// faceAt returns a cached face rendered at sizePx device pixels. Rendering is
// done at 2x and downsampled, so callers pass the doubled size.
func faceAt(sizePx int) (font.Face, error) {
	if f, ok := faces.Load(sizePx); ok {
		return f.(font.Face), nil
	}
	sf, err := baseFont()
	if err != nil {
		return nil, err
	}
	f, err := opentype.NewFace(sf, &opentype.FaceOptions{
		Size:    float64(sizePx),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	faces.Store(sizePx, f)
	return f, nil
}

// sanitize replaces characters the embedded font cannot draw with "·" so a
// title never renders as empty notdef boxes. Each missing rune is logged once.
func sanitize(s string) string {
	sf, err := baseFont()
	if err != nil {
		return s
	}
	var buf sfnt.Buffer
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t':
			b.WriteByte(' ')
			continue
		}
		if r < 0x20 {
			continue
		}
		idx, err := sf.GlyphIndex(&buf, r)
		if err != nil || idx == 0 {
			if _, seen := missing.LoadOrStore(r, struct{}{}); !seen {
				log.Printf("frame: font has no glyph for %q (U+%04X), drawing ·", r, r)
			}
			b.WriteRune('·')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// truncate shortens s so it fits maxPx device pixels, appending an ellipsis.
func truncate(face font.Face, s string, maxPx int) string {
	if maxPx <= 0 {
		return ""
	}
	if font.MeasureString(face, s).Ceil() <= maxPx {
		return s
	}
	ell := "…"
	if font.MeasureString(face, ell).Ceil() > maxPx {
		return ""
	}
	runes := []rune(s)
	for n := len(runes); n > 0; n-- {
		cand := string(runes[:n]) + ell
		if font.MeasureString(face, cand).Ceil() <= maxPx {
			return cand
		}
	}
	return ell
}
