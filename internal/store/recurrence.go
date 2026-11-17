package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// recurrence is the subset of RRULE the plan supports:
// DAILY | WEEKLY | MONTHLY, INTERVAL, COUNT or UNTIL.
type recurrence struct {
	freq     string
	interval int
	count    int
	until    *time.Time
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
		}
	}
	if rec.freq == "" {
		return rec, fmt.Errorf("store: rrule %q has no FREQ", raw)
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

	rows, err := s.db.QueryContext(ctx, `SELECT id, title, description, all_day, starts_at, rrule
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
	}
	var masters []master
	for rows.Next() {
		var (
			m        master
			allDay   int
			startsAt string
		)
		if err := rows.Scan(&m.id, &m.title, &m.description, &allDay, &startsAt, &m.rrule); err != nil {
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
			if i > 1000 || (rec.count > 0 && i >= rec.count) {
				break
			}
			occ := occurrence(anchor, rec, i)
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
			nowStr := timeStr(time.Now())
			res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO items (`+itemCols+`, deleted_at)
				VALUES (?,?,?,?,?,NULL,?,?,0,'',?,?,?,?,NULL)`,
				uuid.NewString(), m.title, m.description, boolInt(m.allDay), timeStr(occ),
				date, StatusOpen, m.id, occ.Unix(), nowStr, nowStr)
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

func occurrence(anchor time.Time, rec recurrence, i int) time.Time {
	step := i * rec.interval
	switch rec.freq {
	case "DAILY":
		return anchor.AddDate(0, 0, step)
	case "WEEKLY":
		return anchor.AddDate(0, 0, 7*step)
	case "MONTHLY":
		return addMonthsClamped(anchor, step)
	}
	return anchor
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
