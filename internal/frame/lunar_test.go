package frame

import (
	"testing"
	"time"
)

// Expected values come from the Hong Kong Observatory's Gregorian-Lunar
// conversion tables (https://www.hko.gov.hk/en/gts/time/conversion.htm) and
// the Purple Mountain Observatory almanac; each case names the page it was
// checked against.
func TestLunarForFixtures(t *testing.T) {
	cases := []struct {
		name  string
		date  [3]int
		label string
		tag   string
	}{
		// HKO 2025 table: 2025-07-25 is the first day of leap month 6.
		{"leap month", [3]int{2025, 7, 25}, "农历闰六月初一", ""},
		// HKO 2026 table: Spring Festival is 2026-02-17; the day before is 除夕.
		{"spring festival", [3]int{2026, 2, 17}, "农历正月初一", "春节"},
		{"new year's eve", [3]int{2026, 2, 16}, "农历腊月廿九", "除夕"},
		// 2026 小雪 falls on 2026-11-22 (Purple Mountain almanac).
		{"solar term", [3]int{2026, 11, 22}, "农历十月十四", "小雪"},
		// HKO 2024 table: 2024-02-09 is 除夕 (lunar year ends on 三十).
		{"lunar new year 2024 eve", [3]int{2024, 2, 9}, "农历腊月三十", "除夕"},
		// Ordinary day: no tag.
		{"plain day", [3]int{2026, 11, 17}, "农历十月初九", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := LunarFor(date(c.date))
			if got.Label != c.label {
				t.Fatalf("label: got %q want %q", got.Label, c.label)
			}
			if got.Tag != c.tag {
				t.Fatalf("tag: got %q want %q", got.Tag, c.tag)
			}
		})
	}
}

func date(d [3]int) (t time.Time) {
	return time.Date(d[0], time.Month(d[1]), d[2], 9, 0, 0, 0, time.UTC)
}
