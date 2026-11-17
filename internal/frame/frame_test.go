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

func TestSummaryRowCapsAtTen(t *testing.T) {
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
