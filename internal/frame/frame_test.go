package frame

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

func testNow(t *testing.T) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, 11, 17, 9, 30, 0, 0, loc)
}

func testInput(t *testing.T) Input {
	now := testNow(t)
	at := func(h, m int) *time.Time {
		t := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
		return &t
	}
	return Input{
		Now: now,
		Items: []Item{
			{ID: "11111111-1111-1111-1111-111111111111", Title: "晨会：同步本周排期", StartsAt: at(9, 30)},
			{ID: "22222222-2222-2222-2222-222222222222", Title: "写完墨水屏帧协议的文档和示例", StartsAt: at(10, 0)},
			{ID: "33333333-3333-3333-3333-333333333333", Title: "给服务端补上 ETag / 304 的逻辑", StartsAt: at(14, 30), Done: true},
			{ID: "44444444-4444-4444-4444-444444444444", Title: "一个特别特别长的标题，用来验证截断和省略号是否正常", AllDay: true},
			{ID: "55555555-5555-5555-5555-555555555555", Title: "晚饭后散步 30 分钟", StartsAt: at(19, 0), Pinned: true},
		},
		CursorRow:   0,
		CursorStyle: CursorStyleInvert,
	}
}

func TestBuildGolden(t *testing.T) {
	f, err := Build(testInput(t))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.PBM(&buf); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "today.pbm")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run go test ./internal/frame -update): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("golden mismatch: got %d bytes, want %d; rerun with -update to inspect", buf.Len(), len(want))
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	a, err := Build(testInput(t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(testInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Encoded, b.Encoded) {
		t.Fatal("two builds of the same input differ")
	}
	if !bytes.Equal(a.Bitmap, b.Bitmap) {
		t.Fatal("bitmaps differ")
	}
}

func TestWidgetInkFitsWithoutOverlappingNeighbours(t *testing.T) {
	for i, a := range WidgetRects {
		if a.X+a.W > Width || a.Y+a.H > Height || (a.Y < Height-FooterHeight && a.Y+a.H > HeaderHeight) {
			t.Fatalf("widget %d leaves its header/footer: %+v", a.Kind, a)
		}
		if a.Kind == WidgetClock && a.W < 5*6*3 {
			t.Fatal("the clock's five glyphs need 90 pixels, including the clear region")
		}
		if a.Kind == WidgetPower && a.W < 20+4*6 {
			t.Fatal("the battery and 100% label do not fit")
		}
		for _, b := range WidgetRects[i+1:] {
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				t.Fatalf("widgets %d and %d overlap", a.Kind, b.Kind)
			}
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	f, err := Build(testInput(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(f.Encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != f.Width || got.Height != f.Height || got.RowH != f.RowH {
		t.Fatalf("geometry: got %dx%d row %d", got.Width, got.Height, got.RowH)
	}
	if len(got.Rows) != len(f.Rows) {
		t.Fatalf("rows: got %d want %d", len(got.Rows), len(f.Rows))
	}
	for i := range f.Rows {
		if got.Rows[i] != f.Rows[i] {
			t.Fatalf("row %d: got %+v want %+v", i, got.Rows[i], f.Rows[i])
		}
	}
	if !bytes.Equal(got.Bitmap, f.Bitmap) {
		t.Fatal("bitmap changed in round trip")
	}
	if got.EncodedChecksum != f.EncodedChecksum {
		t.Fatalf("crc: got %x want %x", got.EncodedChecksum, f.EncodedChecksum)
	}
	if got.CursorRow != f.CursorRow || got.CursorStyle != f.CursorStyle {
		t.Fatalf("cursor: got %d/%d want %d/%d", got.CursorRow, got.CursorStyle, f.CursorRow, f.CursorStyle)
	}
	if f.FooterH != 28 || f.HeaderH != 60 || f.Flags&FlagFocus == 0 {
		t.Fatalf("focus geometry: header %d footer %d", f.HeaderH, f.FooterH)
	}
	if len(f.Widgets) != 4 || len(got.Widgets) != len(f.Widgets) {
		t.Fatalf("widgets: got %d want %d", len(got.Widgets), len(f.Widgets))
	}
	for i := range f.Widgets {
		if got.Widgets[i] != f.Widgets[i] {
			t.Fatalf("widget %d: got %+v want %+v", i, got.Widgets[i], f.Widgets[i])
		}
	}
}

func TestDecodeRejectsCorruption(t *testing.T) {
	f, err := Build(testInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(f.Encoded[:5]); err == nil {
		t.Fatal("truncated frame decoded")
	}
	bad := append([]byte(nil), f.Encoded...)
	bad[0] = 'X'
	if _, err := Decode(bad); err == nil {
		t.Fatal("bad magic decoded")
	}
	flipped := append([]byte(nil), f.Encoded...)
	flipped[len(flipped)/2] ^= 0xFF
	if _, err := Decode(flipped); err == nil {
		t.Fatal("corrupt payload decoded")
	}
}

// The server must leave every widget rectangle blank: the device draws those
// pixels, and a preview-only sample must never change the encoded frame.
func TestWidgetRectsAreBlank(t *testing.T) {
	f, err := Build(testInput(t))
	if err != nil {
		t.Fatal(err)
	}
	stride := f.Stride()
	for _, w := range f.Widgets {
		for y := int(w.Y); y < int(w.Y+w.H); y++ {
			for x := int(w.X); x < int(w.X+w.W); x++ {
				if f.Bitmap[y*stride+x/8]&(1<<(7-uint(x%8))) != 0 {
					t.Fatalf("widget %d pixel (%d,%d) is black in the encoded frame", w.Kind, x, y)
				}
			}
		}
	}
}

// The header height dropped to 44 and the footer is gone in v2; ten rows must
// still fit above the bottom edge.
func TestFocusRowsFitAboveStatus(t *testing.T) {
	f, err := Build(testInput(t))
	if err != nil {
		t.Fatal(err)
	}
	previous := HeaderHeight
	for _, row := range f.Rows {
		if int(row.Y) < previous || int(row.Y)+int(row.Height) > Height-FooterHeight {
			t.Fatalf("row overlaps: %+v", row)
		}
		previous = int(row.Y) + int(row.Height)
	}
}

func TestSummaryRowCapsAtSix(t *testing.T) {
	in := testInput(t)
	in.Items = nil
	now := testNow(t)
	for i := 0; i < 13; i++ {
		in.Items = append(in.Items, Item{
			ID:    "00000000-0000-0000-0000-0000000000" + string(rune('a'+i)),
			Title: "事项",
		})
	}
	in.Now = now
	f, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Rows) != maxRows {
		t.Fatalf("rows: got %d want %d", len(f.Rows), maxRows)
	}
	last := f.Rows[len(f.Rows)-1]
	if last.Flags&RowFlagSummary == 0 {
		t.Fatal("last row is not a summary row")
	}
}

func TestCursorNone(t *testing.T) {
	in := testInput(t)
	in.CursorRow = -1
	f, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if f.CursorRow != CursorNone {
		t.Fatalf("cursor: got %d want %d", f.CursorRow, CursorNone)
	}
}
