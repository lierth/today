package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DateLayout is how date_local is stored (a calendar date, no zone).
const DateLayout = "2006-01-02"

// Item statuses.
const (
	StatusOpen      = "open"
	StatusDone      = "done"
	StatusCancelled = "cancelled"
)

// Item is one todo/event row.
type Item struct {
	CategoryID  string     `json:"category_id,omitempty"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	AllDay      bool       `json:"all_day"`
	StartsAt    *time.Time `json:"starts_at,omitempty"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	DateLocal   string     `json:"date_local"`
	Status      string     `json:"status"`
	Pinned      bool       `json:"pinned"`
	RRule       string     `json:"rrule,omitempty"`
	SeriesID    string     `json:"series_id,omitempty"`
	SortKey     int64      `json:"sort_key"`
	// CarriedFrom is the local date the item slipped from, when a past open
	// item was moved to today. Empty for ordinary items.
	CarriedFrom string `json:"carried_from,omitempty"`
	// ManualRank is the drag position someone chose for this item on its day;
	// 0 means the row has never been dragged and sorts by its time.
	ManualRank int64     `json:"manual_rank,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Done reports whether the item is checked off.
func (it Item) Done() bool { return it.Status == StatusDone }

const (
	itemSelect = `id, title, description, all_day, starts_at, due_at, date_local,
		status, pinned, rrule, COALESCE(series_id, ''), sort_key, carried_from,
		manual_rank, created_at, updated_at, COALESCE(category_id, ''), deleted_at`
	itemCols = `id, title, description, all_day, starts_at, due_at, date_local,
		status, pinned, rrule, series_id, sort_key, carried_from, manual_rank,
		created_at, updated_at, category_id`
)

func scanItem(row interface{ Scan(...any) error }) (Item, error) {
	var (
		it                   Item
		allDay, pinned       int
		startsAt, dueAt      sql.NullString
		deletedAt            sql.NullString
		rrule                sql.NullString
		createdAt, updatedAt string
	)
	err := row.Scan(&it.ID, &it.Title, &it.Description, &allDay, &startsAt, &dueAt,
		&it.DateLocal, &it.Status, &pinned, &rrule, &it.SeriesID, &it.SortKey,
		&it.CarriedFrom, &it.ManualRank, &createdAt, &updatedAt, &it.CategoryID, &deletedAt)
	if err != nil {
		return Item{}, err
	}
	it.DeletedAt = scanTime(deletedAt)
	it.AllDay = allDay != 0
	it.Pinned = pinned != 0
	it.StartsAt = scanTime(startsAt)
	it.DueAt = scanTime(dueAt)
	it.RRule = rrule.String
	if t := parseTime(createdAt); t != nil {
		it.CreatedAt = *t
	}
	if t := parseTime(updatedAt); t != nil {
		it.UpdatedAt = *t
	}
	return it, nil
}

func validStatus(s string) bool {
	switch s {
	case StatusOpen, StatusDone, StatusCancelled:
		return true
	}
	return false
}

// normalize trims and validates user-supplied fields.
func (it *Item) normalize() error {
	it.Title = strings.TrimSpace(it.Title)
	it.Description = strings.TrimSpace(it.Description)
	if it.Title == "" {
		return errors.New("store: title is required")
	}
	if len([]rune(it.Title)) > 200 {
		return errors.New("store: title is longer than 200 characters")
	}
	if it.Status == "" {
		it.Status = StatusOpen
	}
	if !validStatus(it.Status) {
		return fmt.Errorf("store: unknown status %q", it.Status)
	}
	return nil
}

func localDate(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(DateLayout)
}

func (it *Item) fillDerived(loc *time.Location, now time.Time) {
	if loc == nil {
		loc = time.Local
	}
	if it.StartsAt != nil {
		it.DateLocal = localDate(*it.StartsAt, loc)
	}
	if it.DateLocal == "" {
		it.DateLocal = localDate(now, loc)
	}
	if it.StartsAt != nil {
		it.SortKey = it.StartsAt.Unix()
	} else if day, err := time.ParseInLocation(DateLayout, it.DateLocal, loc); err == nil {
		// All-day items sort at local midnight, ahead of timed items.
		it.SortKey = day.Unix()
	} else {
		it.SortKey = now.Unix()
	}
}

// CreateItem inserts a new item. ID and timestamps are assigned when empty.
func (s *Store) CreateItem(ctx context.Context, in Item, loc *time.Location) (Item, error) {
	if err := ValidateRRule(in.RRule, loc); err != nil {
		return Item{}, err
	}
	if in.RRule != "" && in.StartsAt == nil {
		return Item{}, errors.New("store: recurring item needs a start time")
	}
	now := s.Now()
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	if err := in.normalize(); err != nil {
		return Item{}, err
	}
	if err := s.validateCategory(ctx, in.CategoryID); err != nil {
		return Item{}, err
	}
	in.CreatedAt, in.UpdatedAt = now, now
	// A weekly rule with BYDAY starts on its first listed day, not on whatever
	// weekday the person typed the rule on. Without this, "每周一三五" entered
	// on a Thursday leaves a Thursday master row and skips the first Friday.
	if in.StartsAt != nil && in.RRule != "" {
		if rec, err := parseRRule(in.RRule, loc); err == nil && len(rec.byDay) > 0 {
			aligned := occurrence(in.StartsAt.In(loc), rec, 0)
			in.StartsAt = &aligned
		}
	}
	in.fillDerived(loc, now)

	_, err := s.db.ExecContext(ctx, `INSERT INTO items (`+itemCols+`, deleted_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,NULL)`,
		in.ID, in.Title, in.Description, boolInt(in.AllDay), nullTime(in.StartsAt),
		nullTime(in.DueAt), in.DateLocal, in.Status, boolInt(in.Pinned), in.RRule,
		nullable(in.SeriesID), in.SortKey, in.CarriedFrom, in.ManualRank,
		timeStr(in.CreatedAt), timeStr(in.UpdatedAt), nullable(in.CategoryID))
	if err != nil {
		return Item{}, fmt.Errorf("store: create item: %w", err)
	}
	return in, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Item loads one live item.
func (s *Store) Item(ctx context.Context, id string) (Item, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+itemSelect+` FROM items
		WHERE id = ? AND deleted_at IS NULL`, id)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("store: load item: %w", err)
	}
	return it, nil
}

// ItemsOn lists one local date in display order: pinned first, open before
// done, then by start time.
func (s *Store) ItemsOn(ctx context.Context, dateLocal string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemSelect+` FROM items
		WHERE date_local = ? AND deleted_at IS NULL
		ORDER BY pinned DESC, (status = 'done') ASC, (manual_rank = 0) ASC,
		         manual_rank ASC, sort_key ASC, created_at ASC`,
		dateLocal)
	if err != nil {
		return nil, fmt.Errorf("store: list items: %w", err)
	}
	return collectItems(rows)
}

// AllItems lists recent live items for the management page.
// PastItems returns items before the given local date, newest first.
func (s *Store) PastItems(ctx context.Context, before string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemSelect+` FROM items
		WHERE deleted_at IS NULL AND date_local < ?
		ORDER BY date_local DESC, sort_key DESC, created_at DESC LIMIT ?`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// AllItems lists every live item for the management page: today and later
// first, soonest first, then past days newest first. Ordering by date alone
// put materialized future occurrences above today, which buried the work.
func (s *Store) AllItems(ctx context.Context, today string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemSelect+` FROM items
		WHERE deleted_at IS NULL
		ORDER BY (date_local < ?) ASC,
		         CASE WHEN date_local < ? THEN '' ELSE date_local END ASC,
		         date_local DESC,
		         pinned DESC, (status = 'done') ASC, sort_key ASC
		LIMIT ?`, today, today, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list items: %w", err)
	}
	return collectItems(rows)
}

func collectItems(rows *sql.Rows) ([]Item, error) {
	defer rows.Close()
	items := make([]Item, 0, 16)
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan item: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list items: %w", err)
	}
	return items, nil
}

// Patch is a partial item update. Nil fields are left untouched. An empty
// string on StartsAt/DueAt/RRule clears the value.
type Patch struct {
	CategoryID  *string `json:"category_id"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	AllDay      *bool   `json:"all_day"`
	StartsAt    *string `json:"starts_at"`
	DueAt       *string `json:"due_at"`
	Status      *string `json:"status"`
	Pinned      *bool   `json:"pinned"`
	RRule       *string `json:"rrule"`
	DateLocal   *string `json:"date_local"`
}

// UpdateItem applies a patch and returns the fresh row.
func (s *Store) UpdateItem(ctx context.Context, id string, p Patch, loc *time.Location) (Item, error) {
	it, err := s.Item(ctx, id)
	if err != nil {
		return Item{}, err
	}
	if p.CategoryID != nil {
		it.CategoryID = *p.CategoryID
	}
	if p.Title != nil {
		it.Title = *p.Title
	}
	if p.Description != nil {
		it.Description = *p.Description
	}
	if p.AllDay != nil {
		it.AllDay = *p.AllDay
	}
	if p.StartsAt != nil {
		t, err := parsePatchTime(*p.StartsAt)
		if err != nil {
			return Item{}, err
		}
		it.StartsAt = t
	}
	if p.DueAt != nil {
		t, err := parsePatchTime(*p.DueAt)
		if err != nil {
			return Item{}, err
		}
		it.DueAt = t
	}
	if p.Status != nil {
		it.Status = *p.Status
	}
	if p.Pinned != nil {
		it.Pinned = *p.Pinned
	}
	if p.RRule != nil {
		if err := ValidateRRule(*p.RRule, loc); err != nil {
			return Item{}, err
		}
		it.RRule = strings.TrimSpace(*p.RRule)
		if it.RRule != "" && it.StartsAt == nil {
			return Item{}, errors.New("store: recurring item needs a start time")
		}
	}
	if p.DateLocal != nil && strings.TrimSpace(*p.DateLocal) != "" {
		it.DateLocal = strings.TrimSpace(*p.DateLocal)
	}
	if err := it.normalize(); err != nil {
		return Item{}, err
	}
	if err := s.validateCategory(ctx, it.CategoryID); err != nil {
		return Item{}, err
	}
	it.UpdatedAt = s.Now()
	it.fillDerived(loc, it.UpdatedAt)

	_, err = s.db.ExecContext(ctx, `UPDATE items SET title=?, description=?, all_day=?,
		starts_at=?, due_at=?, date_local=?, status=?, pinned=?, rrule=?, series_id=?,
		sort_key=?, updated_at=?, category_id=? WHERE id = ? AND deleted_at IS NULL`,
		it.Title, it.Description, boolInt(it.AllDay), nullTime(it.StartsAt),
		nullTime(it.DueAt), it.DateLocal, it.Status, boolInt(it.Pinned), it.RRule,
		nullable(it.SeriesID), it.SortKey, timeStr(it.UpdatedAt), nullable(it.CategoryID), it.ID)
	if err != nil {
		return Item{}, fmt.Errorf("store: update item: %w", err)
	}

	// A recurring master owns its future open instances. Keep them in sync so
	// editing a series does not require touching every occurrence by hand.
	if it.SeriesID == "" && (it.RRule != "" || p.RRule != nil) {
		today := it.UpdatedAt.In(loc).Format(DateLayout)
		switch {
		case p.RRule != nil:
			// Hard-delete future open instances so Materialize can create the
			// new rule's dates. User-deleted tombstones stay untouched.
			if _, err := s.db.ExecContext(ctx, `DELETE FROM items
				WHERE series_id=? AND date_local >= ? AND status='open' AND deleted_at IS NULL`,
				it.ID, today); err != nil {
				return Item{}, fmt.Errorf("store: clear series instances: %w", err)
			}
			if it.RRule != "" {
				if _, err := s.Materialize(ctx, loc, it.UpdatedAt, 31); err != nil {
					return Item{}, err
				}
			}
		case p.Title != nil || p.Description != nil || p.StartsAt != nil || p.AllDay != nil || p.CategoryID != nil:
			if err := s.syncSeries(ctx, it, today, loc); err != nil {
				return Item{}, err
			}
		}
		if p.Status != nil && it.Status == StatusCancelled {
			if _, err := s.db.ExecContext(ctx, `UPDATE items SET status=?, updated_at=?
				WHERE series_id=? AND date_local >= ? AND status='open' AND deleted_at IS NULL`,
				StatusCancelled, timeStr(it.UpdatedAt), it.ID, today); err != nil {
				return Item{}, fmt.Errorf("store: cancel series instances: %w", err)
			}
		}
	}
	return it, nil
}

// syncSeries refreshes future open instances from a recurring master while
// keeping each instance's own date.
func (s *Store) syncSeries(ctx context.Context, master Item, today string, loc *time.Location) error {
	if loc == nil {
		loc = time.Local
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, date_local FROM items
		WHERE series_id=? AND date_local >= ? AND status='open' AND deleted_at IS NULL`,
		master.ID, today)
	if err != nil {
		return fmt.Errorf("store: list series instances: %w", err)
	}
	type instance struct{ id, date string }
	var instances []instance
	for rows.Next() {
		var in instance
		if err := rows.Scan(&in.id, &in.date); err != nil {
			rows.Close()
			return fmt.Errorf("store: scan series instance: %w", err)
		}
		instances = append(instances, in)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: list series instances: %w", err)
	}
	for _, in := range instances {
		var starts any
		var sortKey int64
		if !master.AllDay && master.StartsAt != nil {
			clock := master.StartsAt.In(loc).Format("15:04:05")
			start, err := time.ParseInLocation("2006-01-02 15:04:05", in.date+" "+clock, loc)
			if err != nil {
				continue
			}
			starts = timeStr(start)
			sortKey = start.Unix()
		} else {
			parsed, _ := time.ParseInLocation(DateLayout, in.date, loc)
			sortKey = parsed.Unix()
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE items SET title=?, description=?,
			all_day=?, starts_at=?, sort_key=?, updated_at=?, category_id=? WHERE id=?`,
			master.Title, master.Description, boolInt(master.AllDay), starts,
			sortKey, timeStr(master.UpdatedAt), nullable(master.CategoryID), in.id); err != nil {
			return fmt.Errorf("store: sync series instance: %w", err)
		}
	}
	return nil
}

func parsePatchTime(v string) (*time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(timeLayout, v)
	if err != nil {
		return nil, fmt.Errorf("store: bad time %q", v)
	}
	return &t, nil
}

// SetStatus updates only the status (used by device acks).
func (s *Store) SetStatus(ctx context.Context, id, status string) error {
	if !validStatus(status) {
		return fmt.Errorf("store: unknown status %q", status)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE items SET status=?, updated_at=?
		WHERE id = ? AND deleted_at IS NULL`, status, timeStr(s.Now()), id)
	if err != nil {
		return fmt.Errorf("store: set status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteItem soft-deletes an item. Deleting a series master also hides its
// generated instances from today onwards; deleting a generated instance
// affects only that row.
func (s *Store) DeleteItem(ctx context.Context, id string) error {
	now := timeStr(s.Now())
	res, err := s.db.ExecContext(ctx, `UPDATE items SET deleted_at=?, updated_at=?
		WHERE id = ? AND deleted_at IS NULL`, now, now, id)
	if err != nil {
		return fmt.Errorf("store: delete item: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE items SET deleted_at=?, updated_at=?
		WHERE series_id = ? AND deleted_at IS NULL AND date_local >= ?`,
		now, now, id, s.Now().Format(DateLayout)); err != nil {
		return fmt.Errorf("store: delete series instances: %w", err)
	}
	return nil
}

// TodaySummary returns the counts the host shows in the status bar.
func (s *Store) TodaySummary(ctx context.Context, dateLocal string) (total, done int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(status = 'done'), 0)
		FROM items WHERE date_local = ? AND deleted_at IS NULL AND status != 'cancelled'`,
		dateLocal).Scan(&total, &done)
	if err != nil {
		return 0, 0, fmt.Errorf("store: summary: %w", err)
	}
	return total, done, nil
}

// Stats is the small numbers block shown in the web footer.
type Stats struct {
	Total    int    `json:"total"`
	Done     int    `json:"done"`
	Devices  int    `json:"devices"`
	LastSeen string `json:"last_seen,omitempty"`
}

// Stats aggregates today plus device count.
func (s *Store) Stats(ctx context.Context, dateLocal string) (Stats, error) {
	var st Stats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(status = 'done'), 0)
		FROM items WHERE date_local = ? AND deleted_at IS NULL AND status != 'cancelled'`,
		dateLocal).Scan(&st.Total, &st.Done); err != nil {
		return st, fmt.Errorf("store: stats: %w", err)
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE revoked_at IS NULL`).Scan(&st.Devices)
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(last_seen_at), '') FROM devices
		WHERE revoked_at IS NULL`).Scan(&st.LastSeen)
	return st, nil
}

// NextStart returns the earliest open item start after t. The comparison
// uses sort_key (unix seconds) so mixed timezone offsets stay ordered.
func (s *Store) NextStart(ctx context.Context, after time.Time) (*time.Time, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT starts_at FROM items
		WHERE deleted_at IS NULL AND status = 'open' AND starts_at IS NOT NULL AND sort_key > ?
		ORDER BY sort_key ASC LIMIT 1`, after.Unix()).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: next start: %w", err)
	}
	return parseTime(raw), nil
}
