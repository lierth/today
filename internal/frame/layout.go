package frame

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/image/font"
)

// Typography (logical pixels).
const (
	bodyFontSize   = 18
	dateFontSize   = 20
	headerFontSize = 13 // second header line: lunar + progress
	timeFontSize   = 14

	timeColumnWidth = 44
	gutterWidth     = 10
	gutterGap       = 11
	timeGap         = 8

	maxRows = 8

	// Keep the date and lunar line clear of the right-aligned clock.
	headerWidth = 252
)

// BoardRowLimit includes the overflow summary when the day has more rows.
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

// Build renders equally sized rows in chronological order.
func Build(in Input) (*Frame, error) {
	defer lockRender()()
	body, err := faceAt(px(bodyFontSize))
	if err != nil {
		return nil, err
	}
	date, err := faceAt(px(dateFontSize))
	if err != nil {
		return nil, err
	}
	small, err := faceAt(px(headerFontSize))
	if err != nil {
		return nil, err
	}
	timeFace, err := faceAt(px(timeFontSize))
	if err != nil {
		return nil, err
	}
	c := newCanvas(Width, Height)
	f := NewFrame()
	if in.ForceFull {
		f.Flags |= FlagForceFull
	}
	f.CursorStyle = CursorStyleFocus
	drawHeader(c, date, small, in)
	c.hlineLogical(16, Width-16, HeaderHeight, 1, 0)
	c.hlineLogical(16, Width-16, Height-FooterHeight, 1, 0)
	items := append([]Item(nil), in.Items...)
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		aTimed, bTimed := a.StartsAt != nil && !a.AllDay, b.StartsAt != nil && !b.AllDay
		if aTimed != bTimed {
			return !aTimed
		}
		return aTimed && a.StartsAt.Before(*b.StartsAt)
	})
	if len(items) == 0 {
		f.Widgets[0] = Widget{Kind: WidgetClock, X: 98, Y: 100, W: 210, H: 56}
		drawEmpty(c, body, "今天没有事项")
	} else {
		visible := min(len(items), maxRows)
		if len(items) > maxRows {
			visible--
		}
		for i := 0; i < visible; i++ {
			item := items[i]
			item.Pinned = false
			y := 72 + i*RowHeight
			var flags uint8
			if item.Done {
				flags = RowFlagDone
			}
			f.Rows = append(f.Rows, Row{UUID: parseUUID(item.ID), Y: uint16(y), Flags: flags, Height: RowHeight})
			drawItemRow(c, body, timeFace, item, true, in.CursorRow == i, CursorStyleFocus, y)
		}
		if hidden := len(items) - visible; hidden > 0 {
			y := 72 + visible*RowHeight
			f.Rows = append(f.Rows, Row{Y: uint16(y), Flags: RowFlagSummary, Height: RowHeight})
			drawSummaryRow(c, body, hidden, y, true, false, CursorStyleFocus)
		}
	}
	if in.CursorRow >= 0 && in.CursorRow < len(f.Rows) && f.Rows[in.CursorRow].Flags&RowFlagSummary == 0 {
		f.CursorRow = uint8(in.CursorRow)
	}
	f.Footer = []Footer{{GPIO: 18, Action: ActionDown}, {GPIO: 18, Action: ActionLongOK}, {GPIO: 0, Action: ActionRefresh}}
	for _, w := range f.Widgets {
		c.fillLogical(int(w.X), int(w.Y), int(w.X+w.W), int(w.Y+w.H), 0xFF)
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
	line1 = truncate(date, sanitize(line1), px(headerWidth))
	c.textBold(date, line1, px(16), baseline(date, 8, 24), 0)

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
	c.text(small, line2, px(16), baseline(small, 34, 18), 0)
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
	if cursor && style == CursorStyleFocus {
		c.fillLogical(ContentX-8, y+3, ContentX-5, y+RowHeight-3, 0)
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
	label := fmt.Sprintf("还有 %d 项", n)
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

func drawEmpty(c *canvas, body font.Face, label string) {
	w := font.MeasureString(body, label).Ceil()
	c.text(body, label, px(Width)/2-w/2, baseline(body, 184, RowHeight), 0)
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
