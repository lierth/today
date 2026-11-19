package server

import (
	"testing"
	"time"
)

// Wednesday 2026-11-18, 08:00 in Shanghai. Every case states what it expects
// relative to that, so "明天" and "周五" are checkable by hand.
func TestParseQuickAdd(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 18, 8, 0, 0, 0, loc)
	cases := []struct {
		in    string
		title string
		date  string
		clock string
		rule  string
	}{
		// Bare titles: nothing recognized, nothing removed.
		{"买牛奶", "买牛奶", "", "", ""},
		{"写周报", "写周报", "", "", ""},
		{"", "", "", "", ""},
		// Relative days.
		{"今天 写周报", "写周报", "2026-11-18", "", ""},
		{"明天买牛奶", "买牛奶", "2026-11-19", "", ""},
		{"后天 体检", "体检", "2026-11-20", "", ""},
		{"大后天交作业", "交作业", "2026-11-21", "", ""},
		// Clock forms.
		{"10点 站会", "站会", "", "10:00", ""},
		{"10点半 站会", "站会", "", "10:30", ""},
		{"10:30 站会", "站会", "", "10:30", ""},
		{"8点15 出发", "出发", "", "08:15", ""},
		{"下午3点 面试", "面试", "", "15:00", ""},
		{"晚上8点 电影", "电影", "", "20:00", ""},
		{"中午12点 吃饭", "吃饭", "", "12:00", ""},
		{"早上7点 跑步", "跑步", "", "07:00", ""},
		// Weekdays: Wednesday 11-18 is "today".
		{"周三 站会", "站会", "2026-11-18", "", ""},
		{"周五 交报告", "交报告", "2026-11-20", "", ""},
		{"周一 汇报", "汇报", "2026-11-23", "", ""}, // already past this week
		{"下周一 汇报", "汇报", "2026-11-23", "", ""},
		{"本周五 交报告", "交报告", "2026-11-20", "", ""},
		{"星期日 休息", "休息", "2026-11-22", "", ""},
		// Repeat rules.
		{"每天 8点 跑步", "跑步", "", "08:00", "FREQ=DAILY"},
		{"工作日 8点 打卡", "打卡", "", "08:00", "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		{"每周一 站会", "站会", "", "", "FREQ=WEEKLY;BYDAY=MO"},
		{"每周一三五 健身", "健身", "", "", "FREQ=WEEKLY;BYDAY=MO,WE,FR"},
		{"每周一、三、五 健身", "健身", "", "", "FREQ=WEEKLY;BYDAY=MO,WE,FR"},
		{"每月15号 交房租", "交房租", "", "", "FREQ=MONTHLY;BYMONTHDAY=15"},
		{"每两周 复盘", "复盘", "", "", "FREQ=WEEKLY;INTERVAL=2"},
		// Combined.
		{"明天 下午3点 开会", "开会", "2026-11-19", "15:00", ""},
		{"下周三 10点 评审", "评审", "2026-11-25", "10:00", ""},
		{"11月20日 交材料", "交材料", "2026-11-20", "", ""},
		{"11月20号 交材料", "交材料", "2026-11-20", "", ""},
		{"20号 交材料", "交材料", "2026-11-20", "", ""},
		// The reviewer's six: month-day without 月, a day already past this
		// month, a word that contains 工作日, a full-width colon, midnight, and
		// several weekdays after a week prefix.
		{"11-20 报告", "报告", "2026-11-20", "", ""},
		{"11/20 报告", "报告", "2026-11-20", "", ""},
		{"5号 交房租", "交房租", "2026-12-05", "", ""},
		{"写工作日报", "写工作日报", "", "", ""},
		{"10：30 站会", "站会", "", "10:30", ""},
		{"晚上12点 睡觉", "睡觉", "", "00:00", ""},
		{"下周一三 出差", "出差", "2026-11-23", "", ""},
		// Ambiguity that must not fire.
		{"周五前交报告", "周五前交报告", "2026-11-20", "", ""}, // deadline, not a rule
		{"3点 起床", "起床", "", "03:00", ""},           // bare 3点 stays 3:00
		{"一共3点建议", "一共3点建议", "", "", ""},           // 点 inside a word
		{"一点小事", "一点小事", "", "", ""},               // 一点 is not 01:00
		{"跑步 45分钟", "跑步 45分钟", "", "", ""},         // 45分钟 is not a clock
		{"看 2001 太空漫游", "看 2001 太空漫游", "", "", ""},
		{"买 3-5 份水果", "买 3-5 份水果", "", "", ""},
		// A line that is only fragments keeps the raw text as the title.
		{"明天", "明天", "2026-11-19", "", ""},
		{"10点", "10点", "", "10:00", ""},
	}
	for _, c := range cases {
		got := ParseQuickAdd(c.in, now, loc)
		if got.Title != c.title || got.Date != c.date || got.Time != c.clock || got.RRule != c.rule {
			t.Errorf("%q\n  got  title=%q date=%q time=%q rule=%q\n  want title=%q date=%q time=%q rule=%q",
				c.in, got.Title, got.Date, got.Time, got.RRule, c.title, c.date, c.clock, c.rule)
		}
	}
}

// The parser must be a pure function of its inputs: the same line at 23:00
// does not turn "今天" into tomorrow, and the time zone only affects the
// formatting of the dates it derives.
func TestParseQuickAddIsIndependentOfTheHour(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	for _, hour := range []int{0, 8, 12, 23} {
		now := time.Date(2026, 11, 18, hour, 0, 0, 0, loc)
		got := ParseQuickAdd("今天 10点 开会", now, loc)
		if got.Date != "2026-11-18" || got.Time != "10:00" {
			t.Fatalf("%02d:00: %+v", hour, got)
		}
	}
}

// A deadline keeps its weekday in the title but still gets the date: the item
// is due Friday, it does not repeat.
func TestParseQuickAddDeadline(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 11, 18, 8, 0, 0, 0, loc)
	got := ParseQuickAdd("周五前交报告", now, loc)
	if got.RRule != "" {
		t.Fatalf("deadline became a rule: %q", got.RRule)
	}
	if got.Date != "2026-11-20" {
		t.Fatalf("deadline date: %q", got.Date)
	}
	if got.Title != "周五前交报告" {
		t.Fatalf("deadline title changed: %q", got.Title)
	}
}
