package frame

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"golang.org/x/image/font"
)

// Typography (logical pixels; docs/PLAN.md §6).
const (
	bodyFontSize   = 18
	headerFontSize = 22
	timeFontSize   = 14
	footerFontSize = 13

	timeColumnWidth = 56
	gutterWidth     = 6
	gutterGap       = 6
	timeGap         = 8

	maxRows = 10
)

// Item is one row of the board.
type Item struct {
	ID       string
	Title    string
	Done     bool
	AllDay   bool
	Pinned   bool
	StartsAt *time.Time
}

// Input is everything a frame depends on.
type Input struct {
	Now         time.Time
	Items       []Item
	CursorRow   int // index into rendered rows; negative means no cursor
	CursorStyle uint8
	ForceFull   bool
}

// Build lays out and renders in deterministically. Items are expected in
// display order; more than ten are folded into a "+N 项" row.
func Build(in Input) (*Frame, error) {
	body, err := faceAt(px(bodyFontSize))
	if err != nil {
		return nil, err
	}
	header, err := faceAt(px(headerFontSize))
	if err != nil {
		return nil, err
	}
	timeFace, err := faceAt(px(timeFontSize))
	if err != nil {
		return nil, err
	}
	footer, err := faceAt(px(footerFontSize))
	if err != nil {
		return nil, err
	}

	c := newCanvas(Width, Height)
	f := NewFrame()
	if in.ForceFull {
		f.Flags |= FlagForceFull
	}

	drawHeader(c, header, timeFace, in.Now)
	c.hlineLogical(0, Width, HeaderHeight, 1, 0)
	c.hlineLogical(0, Width, Height-FooterHeight, 1, 0)
	drawFooter(c, footer, in)

	items := in.Items
	summary := 0
	if len(items) > maxRows {
		summary = len(items) - (maxRows - 1)
		items = items[:maxRows-1]
	}
	hasTime := false
	for _, it := range items {
		if it.StartsAt != nil && !it.AllDay {
			hasTime = true
			break
		}
	}

	rows := make([]Row, 0, maxRows)
	rowCount := len(items)
	if summary > 0 {
		rowCount++
	}
	for i, it := range items {
		y := HeaderHeight + i*RowHeight
		flags := uint8(0)
		if it.Done {
			flags |= RowFlagDone
		}
		rows = append(rows, Row{UUID: parseUUID(it.ID), Y: uint16(y), Flags: flags})
		cursor := in.CursorRow == i
		drawItemRow(c, body, timeFace, it, hasTime, cursor, in.CursorStyle, y)
	}
	if summary > 0 {
		i := rowCount - 1
		y := HeaderHeight + i*RowHeight
		rows = append(rows, Row{Y: uint16(y), Flags: RowFlagSummary})
		cursor := in.CursorRow == i
		drawSummaryRow(c, body, summary, y, cursor, in.CursorStyle)
	}
	if rowCount == 0 {
		drawEmpty(c, body)
	}

	if in.CursorRow < 0 || in.CursorRow >= rowCount {
		f.CursorRow = CursorNone
	} else {
		f.CursorRow = uint8(in.CursorRow)
	}
	f.CursorStyle = in.CursorStyle
	f.Rows = rows
	f.Footer = []Footer{
		{GPIO: 0, Action: ActionUp},
		{GPIO: 1, Action: ActionDown},
		{GPIO: 2, Action: ActionOK},
		{GPIO: 3, Action: ActionRefresh},
	}
	f.Bitmap = c.bitmap(Width, Height)
	encoded, err := f.Encode()
	if err != nil {
		return nil, err
	}
	f.Encoded = encoded
	return f, nil
}

func drawHeader(c *canvas, header, small font.Face, now time.Time) {
	right := fmt.Sprintf("%d月%d日 %s", int(now.Month()), now.Day(), weekdayZH(now.Weekday()))
	right = truncate(small, sanitize(right), px(ContentWidth/2))
	c.textBold(header, "今天", px(ContentX), baseline(header, 0, HeaderHeight), 0)
	w := font.MeasureString(small, right).Ceil()
	c.text(small, right, px(ContentX+ContentWidth)-w, baseline(small, 0, HeaderHeight), 0)
}

func drawFooter(c *canvas, face font.Face, in Input) {
	hint := sanitize("↑↓ 选择   OK 勾选   刷新")
	hint = truncate(face, hint, px(ContentWidth-64))
	base := baseline(face, Height-FooterHeight, FooterHeight)
	c.text(face, hint, px(ContentX), base, 0)

	done := 0
	for _, it := range in.Items {
		if it.Done {
			done++
		}
	}
	progress := fmt.Sprintf("%d/%d", done, len(in.Items))
	w := font.MeasureString(face, progress).Ceil()
	c.text(face, progress, px(ContentX+ContentWidth)-w, base, 0)
}

func drawItemRow(c *canvas, body, timeFace font.Face, it Item, hasTime, cursor bool, style uint8, y int) {
	invert := cursor && style == CursorStyleInvert
	fg := uint8(0)
	if invert {
		c.fillLogical(0, y, Width, y+RowHeight, 0)
		fg = 0xFF
	}
	if cursor {
		c.fillLogical(ContentX, y+3, ContentX+gutterWidth, y+RowHeight-3, fg)
	}

	base := baseline(body, y, RowHeight)
	x := ContentX + gutterWidth + gutterGap
	if it.StartsAt != nil && !it.AllDay {
		ts := it.StartsAt.Format("15:04")
		tw := font.MeasureString(timeFace, ts).Ceil()
		c.text(timeFace, ts, px(x+timeColumnWidth)-tw, baseline(timeFace, y, RowHeight), fg)
		x += timeColumnWidth + timeGap
	}
	title := truncate(body, sanitize(it.Title), px(ContentX+ContentWidth-x))
	if title == "" {
		title = " "
	}
	c.text(body, title, px(x), base, fg)

	if it.Done || (cursor && style == CursorStyleStrike) {
		width := font.MeasureString(body, title).Ceil()
		c.fillRect(px(x), base-px(bodyFontSize/3), px(x)+width, base-px(bodyFontSize/3)+px(1), fg)
	}
}

func drawSummaryRow(c *canvas, face font.Face, n, y int, cursor bool, style uint8) {
	invert := cursor && style == CursorStyleInvert
	fg := uint8(0)
	if invert {
		c.fillLogical(0, y, Width, y+RowHeight, 0)
		fg = 0xFF
	}
	if cursor {
		c.fillLogical(ContentX, y+3, ContentX+gutterWidth, y+RowHeight-3, fg)
	}
	label := fmt.Sprintf("+%d 项", n)
	c.text(face, label, px(ContentX+gutterWidth+gutterGap), baseline(face, y, RowHeight), fg)
}

func drawEmpty(c *canvas, face font.Face) {
	label := "今天没有事项"
	w := font.MeasureString(face, label).Ceil()
	c.text(face, label, px(Width)/2-w/2, baseline(face, 130, RowHeight), 0)
}

func baseline(face font.Face, top, height int) int {
	m := face.Metrics()
	asc, desc := m.Ascent.Ceil(), m.Descent.Ceil()
	return px(top) + (px(height)-(asc+desc))/2 + asc
}

func weekdayZH(d time.Weekday) string {
	return [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[d]
}

func parseUUID(s string) [16]byte {
	var out [16]byte
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 32 {
		return out
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return out
	}
	copy(out[:], b)
	return out
}
