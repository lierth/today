package frame

import (
	"container/list"
	"fmt"
	"time"

	"github.com/6tail/lunar-go/calendar"
)

// LunarInfo is the localised calendar text shown in the header's second line.
type LunarInfo struct {
	// Label is "农历十月廿八" (or "农历闰六月初一").
	Label string
	// Tag is a solar term or traditional festival that falls on this day,
	// e.g. "小雪" or "春节"; empty when there is none.
	Tag string
	// Festival is the solar (Gregorian) festival, e.g. "元旦"; empty usually.
	Festival string
}

// LunarFor returns the lunar date and any solar term/festival for t.
//
// Backed by github.com/6tail/lunar-go (MIT, ~570 KB of source, active).
// Fixtures in lunar_test.go are cross-checked against 中国科学院紫金山天文台
// 编《中国天文年历》 and the Hong Kong Observatory conversion tables.
func LunarFor(t time.Time) LunarInfo {
	solar := calendar.NewSolar(t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second())
	lunar := solar.GetLunar()
	info := LunarInfo{
		Label: fmt.Sprintf("农历%s月%s", lunar.GetMonthInChinese(), lunar.GetDayInChinese()),
		Tag:   lunar.GetJieQi(),
	}
	if info.Tag == "" {
		if v := firstFestival(lunar.GetFestivals()); v != "" {
			info.Tag = v
		}
	}
	info.Festival = firstFestival(solar.GetFestivals())
	return info
}

func firstFestival(l *list.List) string {
	if l == nil {
		return ""
	}
	if e := l.Front(); e != nil {
		if s, ok := e.Value.(string); ok {
			return s
		}
	}
	return ""
}
