package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// carryWindowDays is how far back the "unfinished from earlier" block looks.
// Two weeks is enough to catch a missed day without turning the page into an
// archive; older leftovers stay on /items and in the search.
const carryWindowDays = 14

// Unfinished returns past open items that should be offered for carrying:
// status=open, inside the last carryWindowDays local days (strictly before
// `today`), and not an already-past instance of a repeating series. A series
// that was missed should not pile up one row per skipped day; the master row
// is the thing to edit, so its past occurrences are not offered here.
func (s *Store) Unfinished(ctx context.Context, today string, loc *time.Location) ([]Item, error) {
	day, err := time.ParseInLocation(DateLayout, today, loc)
	if err != nil {
		return nil, fmt.Errorf("store: bad date %q", today)
	}
	from := day.AddDate(0, 0, -carryWindowDays).Format(DateLayout)
	// Neither a generated instance (series_id set) nor a series master
	// (rrule set, series_id NULL) is offered: moving a master's start would
	// shift the whole series, and moving an instance collides with the
	// (series_id, date_local) unique index.
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemSelect+` FROM items
		WHERE deleted_at IS NULL AND status = 'open'
		  AND date_local < ? AND date_local >= ?
		  AND (series_id IS NULL OR series_id = '')
		  AND COALESCE(rrule, '') = ''
		ORDER BY date_local ASC, sort_key ASC, created_at ASC`,
		today, from)
	if err != nil {
		return nil, fmt.Errorf("store: list unfinished: %w", err)
	}
	return collectItems(rows)
}

// CarryResult says how many rows moved and why the others did not.
type CarryResult struct {
	Moved   int
	Skipped map[string]string // id → reason
}

// CarryItems moves the given items to `today`, keeping their clock time and
// remembering the day they slipped from.
//
// Rows that must not move are skipped with a reason rather than failing the
// batch: a stale page or a device ack can race the request, and one bad id
// must not roll back the rest. A series master and a series instance are
// refused for different reasons (see the comments inline).
func (s *Store) CarryItems(ctx context.Context, ids []string, today string, loc *time.Location) (CarryResult, error) {
	res := CarryResult{Skipped: map[string]string{}}
	if len(ids) == 0 {
		return res, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("store: carry tx: %w", err)
	}
	defer tx.Rollback()
	now := s.Now()
	for _, id := range ids {
		row := tx.QueryRowContext(ctx, `SELECT `+itemSelect+` FROM items
			WHERE id = ? AND deleted_at IS NULL`, id)
		it, err := scanItem(row)
		if errors.Is(err, sql.ErrNoRows) {
			res.Skipped[id] = "不存在或已删除"
			continue
		}
		if err != nil {
			return res, fmt.Errorf("store: carry load %s: %w", id, err)
		}
		switch {
		case it.SeriesID != "":
			// A generated instance: moving it would either duplicate the
			// (series_id, date_local) pair or silently detach it from its
			// series. Edit the series instead.
			res.Skipped[id] = "重复事项的实例不顺延，请改系列本身"
			continue
		case it.RRule != "":
			// A series master: its start defines every occurrence, so moving
			// it would shift the whole series.
			res.Skipped[id] = "重复事项的主行不顺延，请改开始日期"
			continue
		case it.Status != StatusOpen || it.DateLocal >= today:
			res.Skipped[id] = "不是今天之前未完成的事项"
			continue
		}
		var starts any
		var sortKey int64
		if it.StartsAt != nil {
			starts = it.StartsAt
			// Same clock, new day.
			y, m, d := mustDate(today, loc)
			next := time.Date(y, m, d, it.StartsAt.Hour(), it.StartsAt.Minute(),
				it.StartsAt.Second(), 0, loc)
			starts = timeStr(next)
			sortKey = next.Unix()
		} else if day, err := time.ParseInLocation(DateLayout, today, loc); err == nil {
			sortKey = day.Unix()
		}
		from := it.DateLocal
		if it.CarriedFrom != "" {
			// Keep the original slip date: "from 11月12日" stays true even
			// after a second carry.
			from = it.CarriedFrom
		}
		if _, err := tx.ExecContext(ctx, `UPDATE items SET date_local=?, starts_at=?,
			sort_key=?, carried_from=?, updated_at=? WHERE id=?`,
			today, starts, sortKey, from, timeStr(now), id); err != nil {
			return res, fmt.Errorf("store: carry %s: %w", id, err)
		}
		res.Moved++
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("store: carry commit: %w", err)
	}
	return res, nil
}

// AutoCarry is the 00:05 ticker's version of the manual move: every unfinished
// item in the window moves to today. It returns how many moved.
func (s *Store) AutoCarry(ctx context.Context, today string, loc *time.Location) (int, error) {
	items, err := s.Unfinished(ctx, today, loc)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	res, err := s.CarryItems(ctx, ids, today, loc)
	if err != nil {
		return res.Moved, err
	}
	return res.Moved, nil
}

func mustDate(date string, loc *time.Location) (int, time.Month, int) {
	t, err := time.ParseInLocation(DateLayout, date, loc)
	if err != nil {
		// A malformed date has no "today" to fall back on; the callers only
		// pass dates that came out of the store, so zero is a bug worth seeing
		// rather than a silent substitution of the current date.
		return 1970, time.January, 1
	}
	return t.Date()
}

// DoneItems lists items completed in the last window, newest first, for the
// folded "已完成" group. `from` is a local date.
func (s *Store) DoneItems(ctx context.Context, from string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemSelect+` FROM items
		WHERE deleted_at IS NULL AND status = 'done' AND date_local >= ?
		ORDER BY date_local DESC, sort_key DESC, updated_at DESC LIMIT ?`, from, limit)
	if err != nil {
		return nil, fmt.Errorf("store: done items: %w", err)
	}
	return collectItems(rows)
}

// Reorder writes one ordering for the given ids in a single transaction.
// Only rows that exist and are live are touched; a stale id is ignored so a
// second browser cannot fail the whole drag.
func (s *Store) Reorder(ctx context.Context, ids []string, loc *time.Location) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: reorder tx: %w", err)
	}
	defer tx.Rollback()
	now := timeStr(s.Now())
	today := s.Now().In(loc).Format(DateLayout)
	for i, id := range ids {
		// The rank is 1-based so 0 keeps meaning "never dragged, sort by the
		// clock". sort_key stays the start time: the frame, the menu and the
		// wake calculation all read it.
		//
		// Only today's live rows are ranked. A drag that names an id from
		// another day (a second tab left open past midnight, a carried row)
		// must not reorder a page the person is not looking at.
		if _, err := tx.ExecContext(ctx, `UPDATE items SET manual_rank=?, updated_at=?
			WHERE id=? AND deleted_at IS NULL AND date_local=?`,
			int64(i+1), now, id, today); err != nil {
			return fmt.Errorf("store: reorder %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: reorder commit: %w", err)
	}
	return nil
}

// RestoreItem undoes a soft delete.
func (s *Store) RestoreItem(ctx context.Context, id string) (Item, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE items SET deleted_at=NULL, updated_at=?
		WHERE id=? AND deleted_at IS NOT NULL AND julianday(deleted_at) > julianday(?)`, timeStr(s.Now()), id, timeStr(s.Now().Add(-TrashRetention)))
	if err != nil {
		return Item{}, fmt.Errorf("store: restore item: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Item{}, ErrNotFound
	}
	return s.Item(ctx, id)
}

// Search matches the title and the note with LIKE. The corpus is one person's
// todo list, so a scan is fine; if it ever passes a few thousand rows this is
// the place to add an FTS5 table (SQLite ships with it).
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Item, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	// Escape LIKE's own wildcards: a title with "%" must not match everything.
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	pattern := "%" + esc + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemSelect+` FROM items
		WHERE deleted_at IS NULL
		  AND (title LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\')
		ORDER BY date_local DESC, sort_key ASC, created_at DESC
		LIMIT ?`, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}
	return collectItems(rows)
}

// WeekStats counts items finished in [from, to) by their local date. The
// history page shows this week next to last week.
func (s *Store) WeekStats(ctx context.Context, from, to string) (done, total int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(status='done'),0), COUNT(*)
		FROM items WHERE deleted_at IS NULL AND status != 'cancelled'
		  AND date_local >= ? AND date_local < ?`, from, to).Scan(&done, &total)
	if err != nil {
		return 0, 0, fmt.Errorf("store: week stats: %w", err)
	}
	return done, total, nil
}

// Upcoming returns the future items the "coming up" view groups: live, not
// cancelled, on or after `today`. Repeating series are collapsed to the
// soonest instance per series so one weekly stand-up does not fill the list.
func (s *Store) Upcoming(ctx context.Context, today string, days int, limit int) ([]Item, error) {
	if days <= 0 {
		days = 31
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	day, err := time.Parse(DateLayout, today)
	if err != nil {
		return nil, fmt.Errorf("store: bad date %q", today)
	}
	to := day.AddDate(0, 0, days).Format(DateLayout)
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemSelect+` FROM items
		WHERE deleted_at IS NULL AND status != 'cancelled'
		  AND date_local >= ? AND date_local < ?
		ORDER BY date_local ASC, sort_key ASC, created_at ASC
		LIMIT ?`, today, to, limit)
	if err != nil {
		return nil, fmt.Errorf("store: upcoming: %w", err)
	}
	items, err := collectItems(rows)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]Item, 0, len(items))
	for _, it := range items {
		series := it.SeriesID
		if series == "" && it.RRule != "" {
			series = it.ID
		}
		if series != "" && it.Status != StatusDone {
			if seen[series] {
				continue
			}
			seen[series] = true
		}
		out = append(out, it)
	}
	return out, nil
}

// UncarryItems moves rows back to the day they were carried from. The undo of
// a carry is exact because carried_from is kept on the row; a row that was
// never carried is left alone.
func (s *Store) UncarryItems(ctx context.Context, ids []string, loc *time.Location) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: uncarry tx: %w", err)
	}
	defer tx.Rollback()
	now := s.Now()
	moved := 0
	for _, id := range ids {
		row := tx.QueryRowContext(ctx, `SELECT `+itemSelect+` FROM items
			WHERE id = ? AND deleted_at IS NULL`, id)
		it, err := scanItem(row)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return moved, fmt.Errorf("store: uncarry load %s: %w", id, err)
		}
		if it.CarriedFrom == "" {
			continue
		}
		var starts any
		var sortKey int64
		if it.StartsAt != nil {
			y, m, d := mustDate(it.CarriedFrom, loc)
			back := time.Date(y, m, d, it.StartsAt.Hour(), it.StartsAt.Minute(),
				it.StartsAt.Second(), 0, loc)
			starts = timeStr(back)
			sortKey = back.Unix()
		} else if day, err := time.ParseInLocation(DateLayout, it.CarriedFrom, loc); err == nil {
			sortKey = day.Unix()
		}
		if _, err := tx.ExecContext(ctx, `UPDATE items SET date_local=?, starts_at=?,
			sort_key=?, carried_from='', updated_at=? WHERE id=?`,
			it.CarriedFrom, starts, sortKey, timeStr(now), id); err != nil {
			return moved, fmt.Errorf("store: uncarry %s: %w", id, err)
		}
		moved++
	}
	if err := tx.Commit(); err != nil {
		return moved, fmt.Errorf("store: uncarry commit: %w", err)
	}
	return moved, nil
}
