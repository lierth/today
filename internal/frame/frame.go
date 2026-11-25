// Package frame implements the Today device frame: a deterministic 1-bit
// bitmap plus a fixed-size binary header that ESP32 firmware copies straight
// into panel memory.
package frame

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// Contract constants, shared with firmware/src/tbf.h.
const (
	Magic         = "TBF1"
	FormatVersion = 3
	BitDepth      = 1

	Width  = 400
	Height = 300

	RowHeight    = 24
	HeaderHeight = 60
	FooterHeight = 28

	ContentX     = 24
	ContentWidth = 360
)

// Widget kinds. The device fills these rectangles itself; the server leaves
// them blank so minute-level updates never change the ETag.
const (
	WidgetClock   = 1 // HH:MM
	WidgetWifi    = 2
	WidgetClimate = 3 // temperature + humidity
	WidgetPower   = 4 // battery / USB
)

// WidgetRects keeps the clock beside the date, with the live device status
// in the bottom strip. Empty days replace the clock rect with a large one.
var WidgetRects = []Widget{
	{Kind: WidgetClock, X: 294, Y: 10, W: 90, H: 32},
	{Kind: WidgetWifi, X: 312, Y: 276, W: 20, H: 20},
	{Kind: WidgetPower, X: 336, Y: 276, W: 48, H: 20},
	{Kind: WidgetClimate, X: 16, Y: 276, W: 120, H: 20},
}

// Frame flag bits.
const (
	FlagForceFull = 1 << 0
	FlagFocus     = 1 << 1
)

// Row flag bits.
const (
	RowFlagDone    = 1 << 0
	RowFlagSummary = 1 << 1
	RowFlagHero    = 1 << 2
)

// Cursor encodings.
const (
	CursorNone         = 0xFF
	CursorStyleInvert  = 0
	CursorStyleStrike  = 1
	CursorStyleFocus   = 2
	actionUp           = 1
	actionDown         = 2
	actionOK           = 3
	actionRefresh      = 4
	actionLongOK       = 5
	defaultFooterCount = 4
)

// Action codes understood by the firmware: 1 up / 2 down / 3 ok /
// 4 refresh / 5 long_ok.
const (
	ActionUp      = actionUp
	ActionDown    = actionDown
	ActionOK      = actionOK
	ActionRefresh = actionRefresh
	ActionLongOK  = actionLongOK
)

// Widget reserves a rectangle the device draws locally.
type Widget struct {
	Kind uint8
	X, Y uint16
	W, H uint16
}

// Row binds a bitmap row to an item UUID.
type Row struct {
	UUID   [16]byte
	Y      uint16
	Flags  uint8
	Height uint8 // zero in legacy frames; otherwise the row's actual height
}

// Footer maps a panel GPIO button to an action code.
type Footer struct {
	GPIO   uint8
	Action uint8
}

// Frame is one rendered screen.
type Frame struct {
	Flags uint8

	Width, Height   uint16
	RowH            uint16
	HeaderH         uint16
	FooterH         uint16
	ContentX        uint16
	ContentW        uint16
	CursorRow       uint8
	CursorStyle     uint8
	Rows            []Row
	Footer          []Footer
	Widgets         []Widget
	Bitmap          []byte
	Encoded         []byte
	EncodedChecksum uint32
}

// Stride is the bitmap row length in bytes.
func (f *Frame) Stride() int { return int((f.Width + 7) / 8) }

// NewFrame returns a frame carrying the panel geometry defaults.
func NewFrame() *Frame {
	return &Frame{
		Flags:       FlagFocus,
		Width:       Width,
		Height:      Height,
		RowH:        RowHeight,
		HeaderH:     HeaderHeight,
		FooterH:     FooterHeight,
		ContentX:    ContentX,
		ContentW:    ContentWidth,
		CursorRow:   CursorNone,
		CursorStyle: CursorStyleInvert,
		Widgets:     append([]Widget(nil), WidgetRects...),
	}
}

// Encode serializes the frame: header, rows, footer bindings, bitmap, CRC32.
func (f *Frame) Encode() ([]byte, error) {
	if err := f.validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Grow(64 + len(f.Rows)*20 + len(f.Footer)*2 + len(f.Widgets)*9 + int(f.Height)*f.Stride() + 4)
	buf.WriteString(Magic)
	buf.WriteByte(FormatVersion)
	buf.WriteByte(BitDepth)
	buf.WriteByte(f.Flags)
	for _, v := range []uint16{f.Width, f.Height, f.RowH, f.HeaderH, f.FooterH, f.ContentX, f.ContentW} {
		_ = binary.Write(&buf, binary.BigEndian, v)
	}
	buf.WriteByte(f.CursorRow)
	buf.WriteByte(f.CursorStyle)
	buf.WriteByte(byte(len(f.Rows)))
	for _, r := range f.Rows {
		buf.Write(r.UUID[:])
		_ = binary.Write(&buf, binary.BigEndian, r.Y)
		buf.WriteByte(r.Flags)
		buf.WriteByte(r.Height)
	}
	buf.WriteByte(byte(len(f.Footer)))
	for _, b := range f.Footer {
		buf.WriteByte(b.GPIO)
		buf.WriteByte(b.Action)
	}
	buf.WriteByte(byte(len(f.Widgets)))
	for _, w := range f.Widgets {
		buf.WriteByte(w.Kind)
		for _, v := range []uint16{w.X, w.Y, w.W, w.H} {
			_ = binary.Write(&buf, binary.BigEndian, v)
		}
	}
	buf.Write(f.Bitmap)
	sum := crc32.ChecksumIEEE(buf.Bytes())
	f.EncodedChecksum = sum
	_ = binary.Write(&buf, binary.BigEndian, sum)
	return buf.Bytes(), nil
}

// Decode parses and validates an encoded frame.
func Decode(data []byte) (*Frame, error) {
	const fixed = 4 + 3 + 14 + 2 + 1
	if len(data) < fixed+1+4 {
		return nil, fmt.Errorf("frame: short frame (%d bytes)", len(data))
	}
	if string(data[:4]) != Magic {
		return nil, fmt.Errorf("frame: bad magic %q", data[:4])
	}
	if data[4] != FormatVersion {
		return nil, fmt.Errorf("frame: unsupported format %d", data[4])
	}
	if data[5] != BitDepth {
		return nil, fmt.Errorf("frame: unsupported bit depth %d", data[5])
	}
	payload := data[:len(data)-4]
	want := binary.BigEndian.Uint32(data[len(data)-4:])
	if got := crc32.ChecksumIEEE(payload); got != want {
		return nil, fmt.Errorf("frame: crc mismatch (want %08x got %08x)", want, got)
	}
	p := 6
	readU16 := func() uint16 {
		v := binary.BigEndian.Uint16(data[p:])
		p += 2
		return v
	}
	f := &Frame{Flags: data[p]}
	p++
	f.Width, f.Height = readU16(), readU16()
	f.RowH, f.HeaderH, f.FooterH = readU16(), readU16(), readU16()
	f.ContentX, f.ContentW = readU16(), readU16()
	f.CursorRow = data[p]
	f.CursorStyle = data[p+1]
	p += 2
	if f.Width == 0 || f.Height == 0 || f.RowH == 0 {
		return nil, errors.New("frame: zero geometry")
	}
	nRows := int(data[p])
	p++
	need := p + nRows*20
	if len(payload) < need {
		return nil, errors.New("frame: truncated rows")
	}
	f.Rows = make([]Row, nRows)
	for i := range f.Rows {
		copy(f.Rows[i].UUID[:], data[p:p+16])
		f.Rows[i].Y = binary.BigEndian.Uint16(data[p+16:])
		f.Rows[i].Flags = data[p+18]
		f.Rows[i].Height = data[p+19]
		p += 20
	}
	nFooter := int(data[p])
	p++
	need = p + nFooter*2
	if len(payload) < need {
		return nil, errors.New("frame: truncated footer")
	}
	f.Footer = make([]Footer, nFooter)
	for i := range f.Footer {
		f.Footer[i] = Footer{GPIO: data[p], Action: data[p+1]}
		p += 2
	}
	nWidgets := int(data[p])
	p++
	need = p + nWidgets*9
	if len(payload) < need {
		return nil, errors.New("frame: truncated widgets")
	}
	f.Widgets = make([]Widget, nWidgets)
	for i := range f.Widgets {
		f.Widgets[i] = Widget{
			Kind: data[p],
			X:    binary.BigEndian.Uint16(data[p+1:]),
			Y:    binary.BigEndian.Uint16(data[p+3:]),
			W:    binary.BigEndian.Uint16(data[p+5:]),
			H:    binary.BigEndian.Uint16(data[p+7:]),
		}
		p += 9
	}
	bitmapLen := f.Stride() * int(f.Height)
	if len(payload) != p+bitmapLen {
		return nil, fmt.Errorf("frame: bitmap size %d, want %d", len(payload)-p, bitmapLen)
	}
	f.Bitmap = append([]byte(nil), data[p:p+bitmapLen]...)
	f.Encoded = append([]byte(nil), data...)
	f.EncodedChecksum = want
	if err := f.validate(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Frame) validate() error {
	if f.Width == 0 || f.Height == 0 || f.RowH == 0 {
		return errors.New("frame: zero geometry")
	}
	if len(f.Rows) > 255 {
		return errors.New("frame: too many rows")
	}
	if f.Flags&FlagFocus != 0 {
		if int(f.HeaderH)+int(f.FooterH) > int(f.Height) {
			return errors.New("frame: invalid focus content bounds")
		}
		bottom := int(f.Height) - int(f.FooterH)
		previous := int(f.HeaderH)
		for _, row := range f.Rows {
			if int(row.Height) < int(f.RowH) || int(row.Y) < previous || int(row.Y)+int(row.Height) > bottom {
				return errors.New("frame: focus row outside content or overlapping")
			}
			previous = int(row.Y) + int(row.Height)
		}
	}
	if len(f.Footer) > 255 {
		return errors.New("frame: too many footer buttons")
	}
	if len(f.Widgets) > 255 {
		return errors.New("frame: too many widgets")
	}
	for _, w := range f.Widgets {
		if w.Kind == 0 {
			return errors.New("frame: widget kind 0")
		}
		if int(w.X)+int(w.W) > int(f.Width) || int(w.Y)+int(w.H) > int(f.Height) {
			return fmt.Errorf("frame: widget %d outside panel", w.Kind)
		}
	}
	if got, want := len(f.Bitmap), f.Stride()*int(f.Height); got != want {
		return fmt.Errorf("frame: bitmap is %d bytes, want %d", got, want)
	}
	return nil
}
