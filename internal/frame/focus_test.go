package frame

import (
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func TestChronologicalRowsKeepEqualSizeAndCompletion(t *testing.T) {
	in := testInput(t)
	in.Items[0].Done = true
	in.Items[1].Pinned = true
	in.Items[1].Title = "整理项目计划，确认本周交付安排与验收步骤"
	f, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []Item{in.Items[3], in.Items[0], in.Items[1], in.Items[2], in.Items[4]}
	if len(f.Rows) != len(want) {
		t.Fatal("missing rows")
	}
	for i, row := range f.Rows {
		if row.UUID != parseUUID(want[i].ID) || row.Height != RowHeight || row.Y != uint16(72+i*RowHeight) || row.Flags&RowFlagHero != 0 {
			t.Fatalf("row %d is not an ordinary chronological row: %+v", i, row)
		}
		if (row.Flags&RowFlagDone != 0) != want[i].Done {
			t.Fatal("completion status lost")
		}
	}
	if !in.Items[1].Pinned || !in.Items[0].Done {
		t.Fatal("rendering changed source")
	}
}

func TestEmptyUsesLargeClock(t *testing.T) {
	f, err := Build(Input{Now: testNow(t), CursorRow: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Rows) != 0 || f.CursorRow != CursorNone || f.Widgets[0].W != 210 || f.Widgets[0].H != 56 {
		t.Fatal("empty layout changed")
	}
}

func TestFocusRejectsOverlappingOrOversizedRows(t *testing.T) {
	f, err := Build(testInput(t))
	if err != nil {
		t.Fatal(err)
	}
	f.Rows[1].Y = f.Rows[0].Y + 10
	if _, err := f.Encode(); err == nil {
		t.Fatal("overlapping title and list encoded")
	}
	wire := append([]byte(nil), f.Encoded...)
	wire[24+19] = 255
	binary.BigEndian.PutUint32(wire[len(wire)-4:], crc32.ChecksumIEEE(wire[:len(wire)-4]))
	if _, err := Decode(wire); err == nil {
		t.Fatal("oversized focus row decoded")
	}
}
