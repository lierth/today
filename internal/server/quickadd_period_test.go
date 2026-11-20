package server

import (
	"testing"
	"time"
)

// A period word on its own is a time, not part of the title: "晚上 给妈妈打电话"
// used to become an untimed item called "晚上 给妈妈打电话".
func TestQuickAddPeriodOnlySetsADefaultTime(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, loc)
	cases := []struct{ in, title, clock string }{
		{"晚上 给妈妈打电话", "给妈妈打电话", "20:00"},
		{"早上 跑步", "跑步", "08:00"},
		{"下午 交报告", "交报告", "15:00"},
		{"中午 吃饭", "吃饭", "12:00"},
		{"夜里 看球", "看球", "21:00"},
		// A period followed by a digit is the clock matcher's, not this one.
		{"晚上8点 看球", "看球", "20:00"},
		// A title that merely starts with the runes is not a period.
		{"晚上海边散步", "晚上海边散步", ""},
	}
	for _, c := range cases {
		got := ParseQuickAdd(c.in, now, loc)
		if got.Title != c.title || got.Time != c.clock {
			t.Fatalf("%q → title %q time %q, want %q / %q", c.in, got.Title, got.Time, c.title, c.clock)
		}
	}
}

// 下周 alone is next Monday and 周末 alone is this Saturday; both used to stay
// in the title, which is how "下周 交月度复盘" became a literal item name.
func TestQuickAddWeekPhrasesWithoutAWeekday(t *testing.T) {
	loc := time.UTC
	// 2026-11-20 is a Friday.
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, loc)
	cases := []struct{ in, title, date string }{
		{"下周 交月度复盘", "交月度复盘", "2026-11-23"},
		{"周末 去看展", "去看展", "2026-11-21"},
		{"本周末 看电影", "看电影", "2026-11-21"},
		{"下个周末 搬家", "搬家", "2026-11-28"},
		{"下周一 站会", "站会", "2026-11-23"},
	}
	for _, c := range cases {
		got := ParseQuickAdd(c.in, now, loc)
		if got.Title != c.title || got.Date != c.date {
			t.Fatalf("%q → title %q date %q, want %q / %q", c.in, got.Title, got.Date, c.title, c.date)
		}
	}
}
