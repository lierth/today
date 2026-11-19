package frame

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The weather suffix must never push the second header line into the widget
// column, and it must degrade in the documented order: city first, then the
// high/low pair.
func TestHeaderWeatherFitsWidgets(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.Local)
	longCity := "内蒙古自治区呼和浩特市" // a name that is normally shortened
	cases := []struct {
		name string
		w    Weather
	}{
		{"no weather", Weather{}},
		{"typical", Weather{City: "北京", Text: "晴", TempC: 3, MaxC: 12, MinC: -1}},
		{"long city", Weather{City: longCity, Text: "晴", TempC: 3, MaxC: 12, MinC: -1}},
		{"long condition", Weather{City: "北京", Text: "雷阵雪", TempC: 3, MaxC: 12, MinC: -1}},
		{"both long", Weather{City: longCity, Text: "雷阵雪", TempC: -3, MaxC: -1, MinC: -12}},
		{"no city", Weather{Text: "晴", TempC: 3, MaxC: 12, MinC: -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := Build(Input{
				Now:     now,
				Items:   []Item{{ID: "00000000-0000-4000-8000-000000000001", Title: "写周报", StartsAt: &now}},
				Weather: c.w,
			})
			if err != nil {
				t.Fatal(err)
			}
			// The header line lives at y=22..35; the widget column starts at
			// x=232. Any black pixel in that strip right of 232 would collide
			// with the clock/wifi/power/climate blanks.
			stride := f.Stride()
			for y := 18; y < 38; y++ {
				for x := 232; x < Width; x++ {
					bit := f.Bitmap[y*stride+x/8]>>(7-uint(x%8))&1 != 0
					if bit {
						t.Fatalf("black pixel at (%d,%d): header text ran into the widgets", x, y)
					}
				}
			}
		})
	}
}

// A preview of each case, so the reviewer can look at the degradation.
func TestWriteWeatherPreviews(t *testing.T) {
	dir := os.Getenv("TODAY_PREVIEW_DIR")
	if dir == "" {
		dir = "/tmp/todayui"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skip(err)
	}
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.Local)
	cases := map[string]Weather{
		"weather-long-city":   {City: "内蒙古自治区呼和浩特市", Text: "晴", TempC: 3, MaxC: 12, MinC: -1},
		"weather-long-text":   {City: "北京", Text: "雷阵雪", TempC: -3, MaxC: -1, MinC: -12},
		"weather-none":        {},
		"weather-summary-row": {City: "北京", Text: "多云", TempC: 8, MaxC: 15, MinC: 4},
	}
	for name, w := range cases {
		items := []Item{{ID: "00000000-0000-4000-8000-000000000001", Title: "写周报", StartsAt: &now}}
		if name == "weather-summary-row" {
			for i := range 14 {
				items = append(items, Item{
					ID:    "00000000-0000-4000-8000-0000000000" + string(rune('a'+i)),
					Title: "第" + string(rune('一'+i)) + "件事",
				})
			}
		}
		f, err := Build(Input{Now: now, Items: items, Weather: w})
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, name+".png")
		file, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.WritePNG(file, 2); err != nil {
			file.Close()
			t.Fatal(err)
		}
		file.Close()
		t.Logf("wrote %s", out)
	}
}
