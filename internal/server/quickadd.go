package server

import (
	"strings"
	"time"
	"unicode"
)

// QuickAdd is what the parser understood. Empty fields mean "not mentioned";
// the caller keeps its own defaults for those.
type QuickAdd struct {
	Title string
	Date  string // yyyy-mm-dd, local to the person
	Time  string // HH:MM
	RRule string
}

// span is one recognized fragment: a byte range in the input.
type span struct{ start, end int }

func (s span) len() int { return s.end - s.start }

// ParseQuickAdd pulls a date, a clock time and a repeat rule out of one line
// of natural Chinese and leaves the rest as the title. It is a pure function:
// `now` and `loc` decide what "明天" means, so it is testable without a clock
// or a database.
//
// Rules, in the order they apply:
//
//   - A repeat phrase wins over a bare weekday: "每周一三五 跑步" repeats,
//     "周一 跑步" only means next Monday.
//   - Weekdays are Monday-based relative to now's week: "周三" on a Wednesday
//     is today, "周一" on a Wednesday is next Monday, "本周五"/"下周一" pin
//     the week explicitly. A weekday in the past of the current week ("周一"
//     on a Wednesday) is read as next week, never as a past date.
//   - A bare clock time is 24-hour: "3点" is 03:00, "下午3点" is 15:00,
//     "晚上8点" is 20:00. Guessing "the coming 3" would silently move an
//     early appointment, so the parser does not.
//   - "周五前交报告" is a deadline, not a schedule: a weekday directly
//     followed by 前/之前 does not become a repeat rule and the phrase stays
//     in the title. The date is still understood.
//   - Every recognized fragment is removed from the title.
func ParseQuickAdd(text string, now time.Time, loc *time.Location) QuickAdd {
	if loc == nil {
		loc = time.Local
	}
	now = now.In(loc)
	input := strings.TrimSpace(text)
	out := QuickAdd{}
	if input == "" {
		return out
	}

	var spans []span
	// Repeat first: it may consume the weekdays a date phrase would use.
	if rule, sp, ok := matchRepeat(input); ok {
		out.RRule = rule
		spans = append(spans, sp)
	}
	if d, sp, ok := matchDate(input, now); ok {
		if !overlaps(spans, sp) {
			out.Date = d
			spans = append(spans, sp)
		}
	}
	if c, sp, ok := matchClock(input); ok {
		if !overlaps(spans, sp) {
			out.Time = c
			spans = append(spans, sp)
		}
	}

	out.Title = stripSpans(input, spans)
	if out.Title == "" {
		// The whole line was fragments ("明天 10点"): nothing names the item,
		// so the caller keeps the raw text as the title.
		out.Title = input
	}
	return out
}

func overlaps(spans []span, s span) bool {
	for _, other := range spans {
		if s.start < other.end && other.start < s.end {
			return true
		}
	}
	return false
}

// stripSpans removes every span and collapses the whitespace left behind.
func stripSpans(s string, spans []span) string {
	if len(spans) == 0 {
		return strings.TrimSpace(s)
	}
	// Sort by start; the recognizers produce at most three.
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j-1].start > spans[j].start; j-- {
			spans[j-1], spans[j] = spans[j], spans[j-1]
		}
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		if sp.len() <= 0 {
			continue // a kept phrase (a deadline) consumes nothing
		}
		if sp.start > last {
			b.WriteString(s[last:sp.start])
		}
		b.WriteByte(' ')
		last = sp.end
	}
	if last < len(s) {
		b.WriteString(s[last:])
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	return strings.Trim(out, " ,，、·-")
}

// matchRepeat recognizes 每天 / 工作日 / 每周一三五 / 每月15号. It returns the
// RRULE and the fragment.
func matchRepeat(s string) (string, span, bool) {
	if i := strings.Index(s, "每天"); i >= 0 {
		return "FREQ=DAILY", span{i, i + len("每天")}, true
	}
	if i := strings.Index(s, "每日"); i >= 0 {
		return "FREQ=DAILY", span{i, i + len("每日")}, true
	}
	for _, word := range []string{"每个工作日", "工作日"} {
		i := strings.Index(s, word)
		if i < 0 {
			continue
		}
		// "写工作日报" contains "工作日" but is a title; the phrase only
		// counts as its own word, i.e. not glued to a Han character on either
		// side.
		if i > 0 && isHanRune(lastRune(s[:i])) {
			continue
		}
		if end := i + len(word); end < len(s) && isHanRune(firstRune(s[end:])) {
			continue
		}
		return "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", span{i, i + len(word)}, true
	}
	if i := strings.Index(s, "每月"); i >= 0 {
		rest := s[i+len("每月"):]
		if n, used := leadingInt(rest); used > 0 && n >= 1 && n <= 31 {
			end := i + len("每月") + used
			for _, suffix := range []string{"号", "日"} {
				if strings.HasPrefix(s[end:], suffix) {
					end += len(suffix)
					break
				}
			}
			return "FREQ=MONTHLY;BYMONTHDAY=" + itoa(n), span{i, end}, true
		}
		// "每月" with no day: the anchor's day of month, which the RRULE
		// spelling FREQ=MONTHLY already means.
		return "FREQ=MONTHLY", span{i, i + len("每月")}, true
	}
	if i := strings.Index(s, "每周"); i >= 0 {
		tokens, used := takeWeekdays(s[i+len("每周"):])
		if len(tokens) > 0 {
			rule := "FREQ=WEEKLY;BYDAY=" + strings.Join(tokens, ",")
			return rule, span{i, i + len("每周") + used}, true
		}
	}
	// 每两周 / 每2周
	if i := strings.Index(s, "每"); i >= 0 {
		rest := s[i+len("每"):]
		interval, used := 0, 0
		if n, u := leadingInt(rest); u > 0 {
			interval, used = n, u
		} else {
			switch {
			case strings.HasPrefix(rest, "两"):
				interval, used = 2, len("两")
			case strings.HasPrefix(rest, "三"):
				interval, used = 3, len("三")
			}
		}
		if interval >= 2 && strings.HasPrefix(rest[used:], "周") {
			end := i + len("每") + used + len("周")
			days, dUsed := takeWeekdays(s[end:])
			end += dUsed
			rule := "FREQ=WEEKLY;INTERVAL=" + itoa(interval)
			if len(days) > 0 {
				rule += ";BYDAY=" + strings.Join(days, ",")
			}
			return rule, span{i, end}, true
		}
	}
	return "", span{}, false
}

// weekdayIndex maps 一..日/天 to Monday=0 .. Sunday=6.
var weekdayIndex = map[rune]int{
	'一': 0, '二': 1, '三': 2, '四': 3, '五': 4, '六': 5, '日': 6, '天': 6,
}

var weekdayTokens = []string{"MO", "TU", "WE", "TH", "FR", "SA", "SU"}

// takeWeekdays reads a run like 一三五 / 一、三、五 / 一，三，五 and returns
// the RRULE tokens in Monday-first order with the bytes consumed.
func takeWeekdays(s string) ([]string, int) {
	seen := map[int]bool{}
	used := 0
	for _, r := range s {
		if idx, ok := weekdayIndex[r]; ok {
			seen[idx] = true
			used += len(string(r))
			continue
		}
		if r == '、' || r == ',' || r == '，' || r == ' ' {
			used += len(string(r))
			continue
		}
		break
	}
	if used == 0 {
		return nil, 0
	}
	out := make([]string, 0, len(seen))
	for i := range 7 {
		if seen[i] {
			out = append(out, weekdayTokens[i])
		}
	}
	return out, used
}

// matchDate recognizes 今天/明天/后天/大后天, 周X/本周X/下周X, 11月20日 and
// 20号. A weekday already in the past of this week means next week.
func matchDate(s string, now time.Time) (string, span, bool) {
	for _, c := range []struct {
		word string
		add  int
	}{{"大后天", 3}, {"后天", 2}, {"明天", 1}, {"明日", 1}, {"今天", 0}, {"今日", 0}} {
		if i := strings.Index(s, c.word); i >= 0 {
			d := now.AddDate(0, 0, c.add)
			return d.Format("2006-01-02"), span{i, i + len(c.word)}, true
		}
	}
	// 下周X / 本周X / 这周X, then a bare 周X / 星期X.
	for _, prefix := range []struct {
		word   string
		offset int
	}{{"下周", 1}, {"本周", 0}, {"这周", 0}} {
		if i := strings.Index(s, prefix.word); i >= 0 {
			days, used := takeWeekdays(s[i+len(prefix.word):])
			if len(days) > 0 && used > 0 {
				// "下周一" is the next calendar week's Monday. "下周一三" keeps
				// the first listed day as the item's date (one item has one
				// date); the rest of the run is removed with it.
				wd := weekdayIndex[weekdayRune(days[0])]
				d := weekDate(now, wd, prefix.offset)
				return d, span{i, i + len(prefix.word) + used}, true
			}
		}
	}
	for _, word := range []string{"星期", "周"} {
		if i := strings.Index(s, word); i >= 0 {
			if word == "周" && i > 0 && strings.ContainsRune("下本这", rune(s[i-1])) {
				continue // already handled (or a phrase we do not know)
			}
			days, used := takeWeekdays(s[i+len(word):])
			if len(days) == 1 && used > 0 {
				end := i + len(word) + used
				// "周五前交报告" is a deadline: the weekday becomes the date,
				// but the phrase (including 前) stays in the title so the
				// person still reads the sentence they typed.
				if strings.HasPrefix(s[end:], "前") {
					wd := weekdayIndex[weekdayRune(days[0])]
					return weekDate(now, wd, 0), span{end, end}, true
				}
				wd := weekdayIndex[weekdayRune(days[0])]
				return weekDate(now, wd, 0), span{i, end}, true
			}
		}
	}
	// 11月20日 / 11月20号
	if m, d, sp, ok := scanMonthDay(s); ok {
		year := now.Year()
		candidate := time.Date(year, time.Month(m), d, 0, 0, 0, 0, now.Location())
		if candidate.Before(now.AddDate(0, 0, -180)) {
			candidate = candidate.AddDate(1, 0, 0)
		}
		return candidate.Format("2006-01-02"), sp, true
	}
	// 11-20 / 11/20: a month and a day joined by a dash or slash. Both parts
	// must be plausible, so "3-5 份" (a quantity) is not May 3rd.
	if m, d, sp, ok := scanMonthDashDay(s); ok {
		year := now.Year()
		candidate := time.Date(year, time.Month(m), d, 0, 0, 0, 0, now.Location())
		if candidate.Before(now.AddDate(0, 0, -180)) {
			candidate = candidate.AddDate(1, 0, 0)
		}
		return candidate.Format("2006-01-02"), sp, true
	}
	// 5号 / 5日: this month if the day is still ahead, otherwise next month.
	// The old "-28 days" window turned "5号" into a past date when it was
	// entered on the 19th.
	if d, sp, ok := scanDayOfMonth(s); ok {
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		candidate := time.Date(now.Year(), now.Month(), d, 0, 0, 0, 0, now.Location())
		if candidate.Before(midnight) {
			candidate = candidate.AddDate(0, 1, 0)
		}
		return candidate.Format("2006-01-02"), sp, true
	}
	return "", span{}, false
}

// scanMonthDashDay finds "11-20" or "11/20": a month 1..12 then a separator
// then a day 1..31, with both numbers bounded to two digits and a boundary
// before the month (so "20-11-3" is not read as November 3rd).
func scanMonthDashDay(s string) (int, int, span, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != '-' && s[i] != '/' {
			continue
		}
		m, mUsed := trailingInt(s[:i])
		if mUsed == 0 || mUsed > 2 || m < 1 || m > 12 {
			continue
		}
		start := i - mUsed
		if start > 0 && !isWordBoundary(s[:start]) {
			continue
		}
		rest := s[i+1:]
		d, dUsed := leadingInt(rest)
		if dUsed == 0 || dUsed > 2 || d < 1 || d > 31 {
			continue
		}
		end := i + 1 + dUsed
		// A following digit or a unit means this was a quantity or a part of
		// something longer ("11-20个", "3-5 份"), not a date.
		if end < len(s) && (s[end] >= '0' && s[end] <= '9') {
			continue
		}
		if strings.ContainsRune("份个次天页张条件次%％", firstRune(strings.TrimLeft(s[end:], " "))) {
			continue
		}
		return m, d, span{start, end}, true
	}
	return 0, 0, span{}, false
}

func weekdayRune(token string) rune {
	switch token {
	case "MO":
		return '一'
	case "TU":
		return '二'
	case "WE":
		return '三'
	case "TH":
		return '四'
	case "FR":
		return '五'
	case "SA":
		return '六'
	}
	return '日'
}

// weekDate returns the date of weekday `wd` (Monday=0) in the week `offset`
// weeks from now's Monday-based week. A weekday that already passed this week
// moves to next week, so the result is never in the past.
func weekDate(now time.Time, wd, offset int) string {
	anchor := (int(now.Weekday()) + 6) % 7 // Monday=0
	target := 7*offset + wd
	if offset == 0 && wd < anchor {
		target += 7
	}
	d := now.AddDate(0, 0, target-anchor)
	return d.Format("2006-01-02")
}

// scanMonthDay finds 11月20日 / 11月20号.
func scanMonthDay(s string) (int, int, span, bool) {
	i := strings.Index(s, "月")
	if i <= 0 {
		return 0, 0, span{}, false
	}
	m, mUsed := trailingInt(s[:i])
	if mUsed == 0 || m < 1 || m > 12 {
		return 0, 0, span{}, false
	}
	rest := s[i+len("月"):]
	d, dUsed := leadingInt(rest)
	if dUsed == 0 || d < 1 || d > 31 {
		return 0, 0, span{}, false
	}
	end := i + len("月") + dUsed
	for _, suffix := range []string{"号", "日"} {
		if strings.HasPrefix(s[end:], suffix) {
			end += len(suffix)
			break
		}
	}
	return m, d, span{i - mUsed, end}, true
}

// scanDayOfMonth finds a bare 20号 / 20日.
func scanDayOfMonth(s string) (int, span, bool) {
	best := span{}
	d := 0
	for _, suffix := range []string{"号", "日"} {
		i := strings.Index(s, suffix)
		if i <= 0 {
			continue
		}
		n, used := trailingInt(s[:i])
		if used == 0 || n < 1 || n > 31 {
			continue
		}
		sp := span{i - used, i + len(suffix)}
		if best.len() == 0 || sp.start < best.start {
			best, d = sp, n
		}
	}
	if best.len() == 0 {
		return 0, span{}, false
	}
	return d, best, true
}

// matchClock recognizes 10点 / 10点半 / 10:30 / 8点15 / 下午3点 / 晚上8点.
func matchClock(s string) (string, span, bool) {
	// A full-width colon ("10：30") is what a Chinese IME produces by default.
	if i := strings.IndexAny(s, ":："); i > 0 {
		h, hUsed := trailingInt(s[:i])
		sep := s[i:]
		sepLen := 1
		if strings.HasPrefix(sep, "：") {
			sepLen = len("：")
		}
		m, mUsed := leadingInt(s[i+sepLen:])
		if hUsed > 0 && mUsed > 0 && h < 24 && m < 60 {
			start := i - hUsed
			end := i + sepLen + mUsed
			if period, pSpan, ok := takePeriod(s[:start]); ok {
				h = applyPeriod(h, period)
				start = pSpan.start
			}
			return pad2(h) + ":" + pad2(m), span{start, end}, true
		}
	}
	if i := strings.Index(s, "点"); i > 0 {
		h, hUsed := trailingInt(s[:i])
		if hUsed == 0 || h > 24 || h == 0 {
			return "", span{}, false
		}
		start := i - hUsed
		withPeriod := false
		if period, pSpan, ok := takePeriod(s[:start]); ok {
			h = applyPeriod(h, period)
			start = pSpan.start
			withPeriod = true
		}
		// Without a period word the digits must start a word: "一共3点建议"
		// is "three points", not 03:00. A space, punctuation or the start of
		// the line before the digits is what a time looks like.
		if !withPeriod && !isWordBoundary(s[:start]) {
			return "", span{}, false
		}
		end := i + len("点")
		rest := s[end:]
		switch {
		case strings.HasPrefix(rest, "半"):
			return pad2(h%24) + ":30", span{start, end + len("半")}, true
		case strings.HasPrefix(rest, "一刻"):
			return pad2(h%24) + ":15", span{start, end + len("一刻")}, true
		case strings.HasPrefix(rest, "三刻"):
			return pad2(h%24) + ":45", span{start, end + len("三刻")}, true
		case strings.HasPrefix(rest, "整"):
			end += len("整")
		}
		if m, mUsed := leadingInt(rest); mUsed > 0 && m < 60 {
			return pad2(h%24) + ":" + pad2(m), span{start, end + mUsed}, true
		}
		return pad2(h%24) + ":00", span{start, end}, true
	}
	return "", span{}, false
}

// takePeriod looks for an afternoon/evening marker directly before the clock
// digits and reports its span.
func takePeriod(before string) (string, span, bool) {
	trimmed := strings.TrimRight(before, " ")
	for _, p := range []string{"中午", "下午", "傍晚", "晚上", "夜里", "凌晨", "早上", "上午"} {
		if strings.HasSuffix(trimmed, p) {
			start := len(trimmed) - len(p)
			return p, span{start, len(before)}, true
		}
	}
	return "", span{}, false
}

func applyPeriod(h int, period string) int {
	switch period {
	case "中午":
		if h < 11 {
			return h + 12
		}
	case "下午", "傍晚", "晚上", "夜里":
		if h < 12 {
			return h + 12
		}
		if h == 12 {
			// "晚上12点" is midnight: the documented choice is 00:00 (the
			// next day), not 24:00, which the panel cannot show.
			return 0
		}
	case "凌晨":
		return h % 12
	case "早上", "上午":
		if h == 12 {
			return 0
		}
	}
	return h
}

// isWordBoundary reports whether the last rune of `before` separates a time
// from the words around it. Multi-byte punctuation is checked as a suffix so
// the byte index stays usable.
func isWordBoundary(before string) bool {
	if before == "" {
		return true
	}
	for _, p := range []string{" ", "\t", "，", ",", "、", "·", "-", "(", "（", "【", "[", "\"", "“"} {
		if strings.HasSuffix(before, p) {
			return true
		}
	}
	return false
}

func leadingInt(s string) (int, int) {
	n, used := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		n = n*10 + int(s[i]-'0')
		used++
		if used > 4 {
			return 0, 0
		}
	}
	return n, used
}

func trailingInt(s string) (int, int) {
	n, mult := 0, 1
	used := 0
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		n += int(s[i]-'0') * mult
		mult *= 10
		used++
		if used > 4 {
			return 0, 0
		}
	}
	return n, used
}

func pad2(n int) string {
	if n < 0 {
		n = 0
	}
	return string([]byte{byte('0' + (n/10)%10), byte('0' + n%10)})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// isHanRune reports whether r is a Han character; "工作日" glued to Han text on
// either side is part of a word, not the repeat phrase.
func isHanRune(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

func lastRune(s string) rune {
	r := []rune(s)
	if len(r) == 0 {
		return 0
	}
	return r[len(r)-1]
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}
