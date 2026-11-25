package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// recurrence is the subset of RRULE the plan supports:
// DAILY | WEEKLY | MONTHLY, INTERVAL, COUNT, UNTIL and BYDAY for weekly rules.
//
// BYDAY is a list of weekday tokens (MO,TU,...). For FREQ=WEEKLY the week is
// measured from the anchor's week (Monday-based), and INTERVAL counts weeks
// from that week: "WEEKLY;INTERVAL=2;BYDAY=MO,WE" starting on a Wednesday
// fires Wed of week 0, Mon and Wed of week 2, and so on.
type recurrence struct {
	freq     string
	interval int
	count    int
	until    *time.Time
	byDay    []time.Weekday // empty means "the anchor's weekday"
	// byMonthDay lists days of the month (1..31). A day that does not exist in
	// a given month (the 31st in April) is skipped for that month, which is
	// what RFC 5545's BYMONTHDAY says.
	byMonthDay []int
}

// parseByMonthDay reads "15" or "1,15,-1" (a negative value counts from the
// end of the month).
func parseByMonthDay(val string) ([]int, error) {
	out := make([]int, 0, 4)
	seen := map[int]bool{}
	for _, raw := range strings.Split(val, ",") {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil || n == 0 || n < -31 || n > 31 {
			return nil, fmt.Errorf("store: bad BYMONTHDAY %q", raw)
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("store: BYMONTHDAY is empty")
	}
	sort.Ints(out)
	return out, nil
}

var weekdayTokens = map[string]time.Weekday{
	"MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday, "SU": time.Sunday,
}

func parseByDay(val string) ([]time.Weekday, error) {
	out := make([]time.Weekday, 0, 7)
	seen := map[time.Weekday]bool{}
	for _, raw := range strings.Split(val, ",") {
		tok := strings.ToUpper(strings.TrimSpace(raw))
		if tok == "" {
			continue
		}
		// Numeric prefixes ("1MO") are not supported: they mean "the first
		// Monday", a different rule from "every Monday".
		wd, ok := weekdayTokens[tok]
		if !ok {
			return nil, fmt.Errorf("store: bad BYDAY token %q", raw)
		}
		if seen[wd] {
			continue
		}
		seen[wd] = true
		out = append(out, wd)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("store: BYDAY is empty")
	}
	// Monday-first, so the expansion order (and therefore COUNT) is stable no
	// matter how the rule wrote the days.
	sort.Slice(out, func(a, b int) bool { return (int(out[a])+6)%7 < (int(out[b])+6)%7 })
	return out, nil
}

func parseRRule(raw string, loc *time.Location) (recurrence, error) {
	rec := recurrence{interval: 1}
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return rec, fmt.Errorf("store: bad rrule part %q", part)
		}
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "FREQ":
			switch strings.ToUpper(strings.TrimSpace(val)) {
			case "DAILY", "WEEKLY", "MONTHLY":
				rec.freq = strings.ToUpper(strings.TrimSpace(val))
			default:
				return rec, fmt.Errorf("store: unsupported FREQ %q", val)
			}
		case "INTERVAL":
			n, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil || n < 1 || n > 365 {
				return rec, fmt.Errorf("store: bad INTERVAL %q", val)
			}
			rec.interval = n
		case "COUNT":
			n, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil || n < 1 || n > 10000 {
				return rec, fmt.Errorf("store: bad COUNT %q", val)
			}
			rec.count = n
		case "UNTIL":
			t, err := parseUntil(strings.TrimSpace(val), loc)
			if err != nil {
				return rec, err
			}
			rec.until = &t
		case "BYDAY":
			days, err := parseByDay(val)
			if err != nil {
				return rec, err
			}
			rec.byDay = days
		case "BYMONTHDAY":
			days, err := parseByMonthDay(val)
			if err != nil {
				return rec, err
			}
			rec.byMonthDay = days
		default:
			// Silently ignoring an unknown key turns "每月15号" into "every
			// month on the anchor's day", which is a different rule. Refuse it
			// so the error reaches the person who typed it.
			return rec, fmt.Errorf("store: 不支持 rrule 键 %q", key)
		}
	}
	if rec.freq == "" {
		return rec, fmt.Errorf("store: rrule %q has no FREQ", raw)
	}
	if len(rec.byMonthDay) > 0 && rec.freq != "MONTHLY" {
		return rec, fmt.Errorf("store: BYMONTHDAY is only supported with FREQ=MONTHLY")
	}
	if len(rec.byDay) > 0 && len(rec.byMonthDay) > 0 {
		return rec, fmt.Errorf("store: BYDAY and BYMONTHDAY cannot be combined here")
	}
	if len(rec.byDay) > 0 && rec.freq != "WEEKLY" {
		// DAILY/MONTHLY BYDAY means "only on these weekdays", which the
		// supported subset does not promise; refuse it rather than silently
		// ignoring half the rule.
		return rec, fmt.Errorf("store: BYDAY is only supported with FREQ=WEEKLY")
	}
	return rec, nil
}

func parseUntil(v string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	for _, layout := range []string{"20060102T150405Z", "20060102T150405", "20060102"} {
		t, err := time.ParseInLocation(layout, v, loc)
		if err != nil {
			continue
		}
		if layout == "20060102" {
			return t.Add(24*time.Hour - time.Nanosecond), nil
		}
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("store: bad UNTIL %q", v)
}

// Materialize generates recurring instances inside [today, today+days).
// It is idempotent: an instance is keyed by (series_id, date_local).
// ValidateRRule checks a recurrence expression against the supported subset.
func ValidateRRule(expr string, loc *time.Location) error {
	if strings.TrimSpace(expr) == "" {
		return nil
	}
	_, err := parseRRule(expr, loc)
	return err
}

func (s *Store) Materialize(ctx context.Context, loc *time.Location, now time.Time, days int) (int, error) {
	if loc == nil {
		loc = time.Local
	}
	if days <= 0 {
		days = 31
	}
	local := now.In(loc)
	windowStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	windowEnd := windowStart.AddDate(0, 0, days)

	rows, err := s.db.QueryContext(ctx, `SELECT id, title, description, all_day, starts_at, rrule, COALESCE(category_id, '')
		FROM items WHERE rrule != '' AND starts_at IS NOT NULL AND deleted_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("store: materialize: %w", err)
	}
	type master struct {
		id          string
		title       string
		description string
		allDay      bool
		startsAt    time.Time
		rrule       string
		categoryID  string
	}
	var masters []master
	for rows.Next() {
		var (
			m        master
			allDay   int
			startsAt string
		)
		if err := rows.Scan(&m.id, &m.title, &m.description, &allDay, &startsAt, &m.rrule, &m.categoryID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: materialize scan: %w", err)
		}
		m.allDay = allDay != 0
		t := parseTime(startsAt)
		if t == nil {
			continue
		}
		m.startsAt = *t
		masters = append(masters, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: materialize: %w", err)
	}

	inserted := 0
	for _, m := range masters {
		rec, err := parseRRule(m.rrule, loc)
		if err != nil {
			continue // a bad rule must not break the ticker
		}
		anchor := m.startsAt.In(loc)
		for i := 0; ; i++ {
			if i > 5000 || (rec.count > 0 && i >= rec.count) {
				break
			}
			occ := occurrence(anchor, rec, i)
			if occ.IsZero() {
				break
			}
			if rec.until != nil && occ.After(*rec.until) {
				break
			}
			if occ.After(windowEnd) {
				break
			}
			if i == 0 || occ.Before(windowStart) {
				continue
			}
			date := occ.Format(DateLayout)
			nowStr := timeStr(s.Now())
			res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO items (`+itemCols+`, deleted_at)
				SELECT ?,?,?,?,?,NULL,?,?,0,'',?,?,'',0,?,?,?,NULL WHERE NOT EXISTS (SELECT 1 FROM series_exclusions WHERE series_id=? AND date_local=?)`,
				uuid.NewString(), m.title, m.description, boolInt(m.allDay), timeStr(occ),
				date, StatusOpen, m.id, occ.Unix(), nowStr, nowStr, nullable(m.categoryID), m.id, date)
			if err != nil {
				return inserted, fmt.Errorf("store: materialize insert: %w", err)
			}
			if n, _ := res.RowsAffected(); n > 0 {
				inserted++
			}
		}
	}
	return inserted, nil
}

// occurrence returns the i-th start of the rule, counting from the anchor.
//
// A BYDAY weekly rule never fires before its own start: week 0 contributes the
// listed days that fall on or after the anchor's weekday, later rule weeks
// contribute all of them in Monday-first order, and INTERVAL skips whole
// weeks. Without BYDAY a weekly rule keeps the anchor's weekday.
func occurrence(anchor time.Time, rec recurrence, i int) time.Time {
	switch rec.freq {
	case "DAILY":
		return anchor.AddDate(0, 0, i*rec.interval)
	case "WEEKLY":
		if len(rec.byDay) == 0 {
			return anchor.AddDate(0, 0, 7*i*rec.interval)
		}
		anchorIdx := (int(anchor.Weekday()) + 6) % 7 // Monday=0 .. Sunday=6
		// Days of the anchor's week that are on or after the station.
		first := 0
		for first < len(rec.byDay) && (int(rec.byDay[first])+6)%7 < anchorIdx {
			first++
		}
		inWeek0 := len(rec.byDay) - first
		week := 0
		var day time.Weekday
		if i < inWeek0 {
			day = rec.byDay[first+i]
		} else {
			rest := i - inWeek0
			week = 1 + rest/len(rec.byDay)
			day = rec.byDay[rest%len(rec.byDay)]
		}
		monday := anchor.AddDate(0, 0, -anchorIdx)
		delta := (int(day) + 6) % 7
		return time.Date(monday.Year(), monday.Month(), monday.Day()+7*week*rec.interval+delta,
			anchor.Hour(), anchor.Minute(), anchor.Second(), 0, anchor.Location())
	case "MONTHLY":
		if len(rec.byMonthDay) == 0 {
			return addMonthsClamped(anchor, i*rec.interval)
		}
		return monthlyByDay(anchor, rec, i)
	}
	return anchor
}

// monthlyByDay is the i-th occurrence of a BYMONTHDAY rule. Days that do not
// exist in a month (the 31st of April) simply produce no occurrence, so the
// index walks over the months that do have them; a 100-year bound keeps a
// pathological rule (BYMONTHDAY=31 over a small window) from looping.
func monthlyByDay(anchor time.Time, rec recurrence, i int) time.Time {
	count := 0
	for monthOffset := 0; monthOffset < 1200; monthOffset++ {
		month := addMonthsClamped(anchor, monthOffset*rec.interval)
		last := time.Date(month.Year(), month.Month()+1, 0, 0, 0, 0, 0, month.Location()).Day()
		for _, day := range rec.byMonthDay {
			d := day
			if d < 0 {
				d = last + 1 + d
			}
			if d < 1 || d > last {
				continue
			}
			if count == i {
				return time.Date(month.Year(), month.Month(), d,
					anchor.Hour(), anchor.Minute(), anchor.Second(), 0, month.Location())
			}
			count++
		}
	}
	return time.Time{}
}

// addMonthsClamped keeps the wall-clock day when possible and clamps to the
// last day of shorter months (Jan 31 + 1 month = Feb 28/29).
func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	target := time.Date(y, m+time.Month(months), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := time.Date(target.Year(), target.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
	if d > last {
		d = last
	}
	return time.Date(target.Year(), target.Month(), d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}
