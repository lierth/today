package main

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strconv"
	"strings"
)

// runPBM2PNG converts a binary PBM (P4, or P5 grey) to an 8-bit greyscale PNG.
//
// The firmware host test writes PBM because a 1-bit bitmap needs no compressor;
// this command is the only PNG writer in the project, and it uses image/png
// rather than a hand-rolled encoder. A hand-written encoder in an earlier
// version produced a bad chunk CRC, and reading the result hung the viewer.
func runPBM2PNG(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: todayd pbm2png <in.pbm> <out.png>")
	}
	in, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer in.Close()
	img, err := decodePBM(bufio.NewReader(in))
	if err != nil {
		return err
	}
	out, err := os.Create(args[1])
	if err != nil {
		return err
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		return err
	}
	fmt.Printf("%s: %dx%d\n", args[1], img.Bounds().Dx(), img.Bounds().Dy())
	return nil
}

// decodePBM reads the plain part of the Netpbm family the firmware writes and
// the greyscale variant handy for other previews.
func decodePBM(r *bufio.Reader) (image.Image, error) {
	magic, err := readToken(r)
	if err != nil {
		return nil, err
	}
	switch magic {
	case "P4":
		w, h, err := readDimensions(r)
		if err != nil {
			return nil, err
		}
		// readToken consumes the single whitespace byte that ends the last
		// header token, which is the separator the format promises; reading
		// another byte here would eat the first pixel column.
		img := image.NewGray(image.Rect(0, 0, w, h))
		stride := (w + 7) / 8
		row := make([]byte, stride)
		for y := range h {
			if _, err := io.ReadFull(r, row); err != nil {
				return nil, fmt.Errorf("pbm: row %d: %w", y, err)
			}
			for x := range w {
				bit := row[x/8]&(0x80>>uint(x%8)) != 0
				v := uint8(255)
				if bit { // P4: 1 = black
					v = 0
				}
				img.Pix[y*img.Stride+x] = v
			}
		}
		return img, nil
	case "P5":
		w, h, err := readDimensions(r)
		if err != nil {
			return nil, err
		}
		maxv, err := readToken(r)
		if err != nil {
			return nil, err
		}
		if maxv != "255" {
			return nil, fmt.Errorf("pbm: P5 max value %q, want 255", maxv)
		}
		// The token reader already consumed the separator after the max value.
		img := image.NewGray(image.Rect(0, 0, w, h))
		if _, err := io.ReadFull(r, img.Pix); err != nil {
			return nil, fmt.Errorf("pbm: raster: %w", err)
		}
		return img, nil
	}
	return nil, fmt.Errorf("pbm: unsupported magic %q (P4 or P5)", magic)
}

func readToken(r *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		c, err := r.ReadByte()
		if err != nil {
			if b.Len() > 0 {
				return b.String(), nil
			}
			return "", err
		}
		if c == '#' { // comment to end of line
			for {
				c, err = r.ReadByte()
				if err != nil {
					return b.String(), nil
				}
				if c == '\n' {
					break
				}
			}
			continue
		}
		if c == ' ' || c == '\n' || c == '\t' || c == '\r' {
			if b.Len() > 0 {
				return b.String(), nil
			}
			continue
		}
		b.WriteByte(c)
	}
}

func readDimensions(r *bufio.Reader) (int, int, error) {
	ws, err := readToken(r)
	if err != nil {
		return 0, 0, err
	}
	hs, err := readToken(r)
	if err != nil {
		return 0, 0, err
	}
	w, err := strconv.Atoi(ws)
	if err != nil || w <= 0 || w > 10000 {
		return 0, 0, fmt.Errorf("pbm: bad width %q", ws)
	}
	h, err := strconv.Atoi(hs)
	if err != nil || h <= 0 || h > 10000 {
		return 0, 0, fmt.Errorf("pbm: bad height %q", hs)
	}
	return w, h, nil
}

var _ = color.Gray{}
