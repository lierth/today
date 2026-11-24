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
	dateFontSize   = 20
	headerFontSize = 13 // second header line: lunar + progress
	timeFontSize   = 14
	emptyHintFont  = 14

	timeColumnWidth = 56
	gutterWidth     = 10
	gutterGap       = 8
	timeGap         = 8

	maxRows = 10

	// headerWidth is the room the second header line has before the widgets
	// (which start at x=224). The widgets are the real
	// limit, so the line may use up to their left edge minus a small gap.
	headerWidth = 196
)

// BoardRowLimit is how many of a day's rows can appear on the panel. Past it
// the last visible row becomes "+N 项", so a list of exactly this many still
// fits without a summary row.
const BoardRowLimit = maxRows

// Item is one row of the board.
type Item struct {
	ID       string
	Title    string
	Done     bool
	AllDay   bool
	Pinned   bool
	StartsAt *time.Time
	// Carried marks a row that slipped from an earlier day. The time column
	// then shows "»" ahead of the clock. The embedded face has no U+21BB (↻),
	// and the brief's fallback for that case is "»", which it does have; the
	// glyph lives in the bitmap, so the device needs nothing new.
	Carried bool
}

// Weather is the second header line's tail, already reduced to the words the
// panel can hold. The server fills it; the frame never calls the network.
type Weather struct {
	City  string  // may be empty: the layout then omits the city first
	Text  string  // at most four Han characters (weather.Report.Short)
	TempC float64 // current
	MaxC  float64
	MinC  float64
}

// Input is everything a frame depends on.
type Input struct {
	Now         time.Time
	Items       []Item
	CursorRow   int // index into rendered rows; negative means no cursor
	CursorStyle uint8
	ForceFull   bool
	Weather     Weather // zero value = no weather on the header line
}

// Build lays out and renders in deterministically. Items are expected in
// display order; more than ten are folded into a "+N 项" row.
func Build(in Input) (*Frame, error) {
	// One render at a time: the faces are shared and their Metrics/Glyph
	// calls use an internal buffer that is not safe for concurrent use.
	defer lockRender()()
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
	hint, err := faceAt(px(emptyHintFont))
	if err != nil {
		return nil, err
	}

	c := newCanvas(Width, Height)
	f := NewFrame()
	if in.ForceFull {
		f.Flags |= FlagForceFull
	}

	date, err := faceAt(px(dateFontSize))
	if err != nil {
		return nil, err
	}
	drawHeader(c, date, header, in)
	c.hlineLogical(0, Width, HeaderHeight, 1, 0)

	items := in.Items
	summary := 0
	if len(items) > maxRows {
		summary = len(items) - (maxRows - 1)
		items = items[:maxRows-1]
	}
	hasTime := false
	for _, it := range items {
		if it.Carried || (it.StartsAt != nil && !it.AllDay) {
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
		drawSummaryRow(c, body, summary, y, hasTime, cursor, in.CursorStyle)
	}
	if rowCount == 0 {
		drawEmpty(c, body, hint)
	} else {
		legend := sanitize("KEY 下一项 · 长按勾选 · BOOT 刷新")
		c.text(hint, legend, px(ContentX), baseline(hint, 284, 16), 0)
	}

	if in.CursorRow < 0 || in.CursorRow >= rowCount {
		f.CursorRow = CursorNone
	} else {
		f.CursorRow = uint8(in.CursorRow)
	}
	f.CursorStyle = in.CursorStyle
	f.Rows = rows
	// The v2 board has two keys: KEY (GPIO18) short = next row, KEY long =
	// toggle, BOOT (GPIO0) short = forced refresh. The footer strip is not
	// drawn, but the bindings stay in the frame so the firmware is
	// layout-agnostic.
	f.Footer = []Footer{
		{GPIO: 18, Action: ActionDown},
		{GPIO: 18, Action: ActionLongOK},
		{GPIO: 0, Action: ActionRefresh},
	}
	f.Bitmap = c.bitmap(Width, Height)
	encoded, err := f.Encode()
	if err != nil {
		return nil, err
	}
	f.Encoded = encoded
	return f, nil
}

func drawHeader(c *canvas, date, small font.Face, in Input) {
	// Line 1: 11月18日 周三, 20 px bold.
	// Use the weekday that belongs to "today", not the locale's week start.
	line1 := fmt.Sprintf("%d月%d日 %s", int(in.Now.Month()), in.Now.Day(), weekdayZH(in.Now.Weekday()))
	line1 = truncate(date, sanitize(line1), px(200))
	c.textBold(date, line1, px(ContentX), baseline(date, 0, 24), 0)

	// Line 2: 农历十月廿八 · 小雪 · 2/6, 13 px.
	lunar := LunarFor(in.Now)
	line2 := strings.TrimPrefix(lunar.Label, "农历")
	if lunar.Tag != "" {
		line2 += " · " + lunar.Tag
	} else if lunar.Festival != "" {
		line2 += " · " + lunar.Festival
	}
	line2 = sanitize(line2)
	done := 0
	for _, it := range in.Items {
		if it.Done {
			done++
		}
	}
	// The progress rides at the end of the lunar line: "农历十月十四 · 小雪 · 2/6".
	// An empty board would read "0/0", which says nothing; omit it.
	if len(in.Items) > 0 {
		line2 = fmt.Sprintf("%s · %d/%d", line2, done, len(in.Items))
	}
	// Weather comes last and degrades in two steps: first the city name, then
	// the high/low pair. The condition and the current temperature are the
	// parts a person actually reads, so they go last.
	if w := in.Weather; w.Text != "" {
		weather := fmt.Sprintf("%s %s %s–%s°", w.City, w.Text,
			roundC(w.MinC), roundC(w.MaxC))
		if w.City == "" {
			weather = fmt.Sprintf("%s %s–%s°", w.Text, roundC(w.MinC), roundC(w.MaxC))
		}
		switch {
		case fits(small, line2+" · "+weather, px(headerWidth)):
			line2 += " · " + weather
		default:
			// Drop the city, then the range; keep the condition if it fits.
			noCity := fmt.Sprintf("%s %s–%s°", w.Text, roundC(w.MinC), roundC(w.MaxC))
			cond := fmt.Sprintf("%s %s°", w.Text, roundC(w.TempC))
			if fits(small, line2+" · "+noCity, px(headerWidth)) {
				line2 += " · " + noCity
			} else if fits(small, line2+" · "+cond, px(headerWidth)) {
				line2 += " · " + cond
			}
		}
	}
	line2 = truncate(small, line2, px(headerWidth))
	c.text(small, line2, px(ContentX), baseline(small, 22, 18), 0)
	if len(in.Items) > 0 {
		c.fillLogical(ContentX, 41, ContentX+headerWidth*done/len(in.Items), 43, 0)
	}

	// Widget rectangles are left blank in the real frame; the preview fills
	// them with sample values (drawWidgetSamples).
	for _, w := range WidgetRects {
		c.fillLogical(int(w.X), int(w.Y), int(w.X+w.W), int(w.Y+w.H), 0xFF)
	}
}

// fits reports whether s can be drawn in width logical pixels by face.
func fits(face font.Face, s string, width int) bool {
	return font.MeasureString(face, s).Ceil() <= width
}

// roundC formats a temperature the way the panel shows it.
func roundC(v float64) string {
	n := int(v)
	if v < 0 {
		if v-float64(n) <= -0.5 {
			n--
		}
	} else if v-float64(n) >= 0.5 {
		n++
	}
	return fmt.Sprintf("%d", n)
}

func drawItemRow(c *canvas, body, timeFace font.Face, it Item, hasTime, cursor bool, style uint8, y int) {
	invert := cursor && style == CursorStyleInvert
	fg := uint8(0)
	if invert {
		c.fillLogical(0, y, Width, y+RowHeight, 0)
		fg = 0xFF
	}
	drawCheck(c, ContentX, y+(RowHeight-10)/2, it.Done, fg)

	base := baseline(body, y, RowHeight)
	// Reserve the shared time column only when this frame needs one.
	x := ContentX + gutterWidth + gutterGap
	slot := ""
	if it.StartsAt != nil && !it.AllDay {
		slot = it.StartsAt.Format("15:04")
	} else if it.AllDay && hasTime {
		slot = "全天"
	}
	if it.Carried {
		// A carried all-day row says "↻全天"; a carried timed row keeps its
		// clock but the arrow sits before it, which is the one part a person
		// needs to see at a glance.
		slot = "»" + slot
	}
	if slot != "" {
		tw := font.MeasureString(timeFace, slot).Ceil()
		c.text(timeFace, slot, px(x+timeColumnWidth)-tw, baseline(timeFace, y, RowHeight), fg)
	}
	if hasTime {
		x += timeColumnWidth + timeGap
	}
	title := truncate(body, sanitize(it.Title), px(ContentX+ContentWidth-x))
	if title == "" {
		title = " "
	}
	if it.Pinned && !it.Done {
		c.textBold(body, title, px(x), base, fg)
	} else {
		c.text(body, title, px(x), base, fg)
	}

	if it.Done || (cursor && style == CursorStyleStrike) {
		width := font.MeasureString(body, title).Ceil()
		c.fillRect(px(x), base-px(bodyFontSize/3), px(x)+width, base-px(bodyFontSize/3)+px(1), fg)
	}
}

func drawCheck(c *canvas, x, y int, done bool, fg uint8) {
	if done {
		c.fillLogical(x, y, x+10, y+10, fg)
		for i := range 3 {
			c.fillLogical(x+2+i, y+4+i, x+3+i, y+5+i, ^fg)
		}
		for i := range 5 {
			c.fillLogical(x+4+i, y+6-i, x+5+i, y+7-i, ^fg)
		}
		return
	}
	c.fillLogical(x+2, y, x+8, y+1, fg)
	c.fillLogical(x+2, y+9, x+8, y+10, fg)
	c.fillLogical(x, y+2, x+1, y+8, fg)
	c.fillLogical(x+9, y+2, x+10, y+8, fg)
	for _, corner := range [][2]int{{1, 1}, {8, 1}, {1, 8}, {8, 8}} {
		c.fillLogical(x+corner[0], y+corner[1], x+corner[0]+1, y+corner[1]+1, fg)
	}
}

func drawSummaryRow(c *canvas, face font.Face, n, y int, hasTime, cursor bool, style uint8) {
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
	// The summary stands in for the rows that did not fit, so its text starts
	// where an item title would: one time-column further right than the
	// gutter. That keeps the left edge straight down the whole list.
	x := ContentX + gutterWidth + gutterGap
	if hasTime {
		x += timeColumnWidth + timeGap
	}
	c.text(face, label, px(x),
		baseline(face, y, RowHeight), fg)
}

func drawEmpty(c *canvas, body, hint font.Face) {
	label := "今天没有事项"
	w := font.MeasureString(body, label).Ceil()
	c.text(body, label, px(Width)/2-w/2, baseline(body, 110, RowHeight), 0)

	// The footer strip is gone; the empty board carries the key legend once.
	keys, err := faceAt(px(emptyHintFont))
	if err != nil {
		keys = hint
	}
	line := sanitize("KEY 下一项 · 长按勾选 · BOOT 刷新")
	kw := font.MeasureString(keys, line).Ceil()
	c.text(keys, line, px(Width)/2-kw/2, baseline(keys, 160, 20), 0)
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
