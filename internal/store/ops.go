package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RecordOp remembers an applied device operation. It reports false when the
// op_id was already applied, which makes acks safely replayable.
func (s *Store) RecordOp(ctx context.Context, opID, deviceID, itemID, status string) (bool, error) {
	if opID == "" {
		return false, errors.New("store: op_id is required")
	}
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO ops
		(op_id, device_id, item_id, status, applied_at) VALUES (?,?,?,?,?)`,
		opID, deviceID, itemID, status, timeStr(time.Now()))
	if err != nil {
		return false, fmt.Errorf("store: record op: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: record op: %w", err)
	}
	return n > 0, nil
}

// PruneOps drops operation records older than the retention window.
func (s *Store) PruneOps(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ops WHERE applied_at < ?`, timeStr(before))
	if err != nil {
		return 0, fmt.Errorf("store: prune ops: %w", err)
	}
	return res.RowsAffected()
}

// RecordAuthFail keeps an audit row for a rejected password attempt.
func (s *Store) RecordAuthFail(ctx context.Context, ip string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO auth_fail (ip, at) VALUES (?, ?)`,
		ip, timeStr(at))
	if err != nil {
		return fmt.Errorf("store: record auth fail: %w", err)
	}
	return nil
}

// CountAuthFails counts failed attempts from ip since a cutoff.
func (s *Store) CountAuthFails(ctx context.Context, ip string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_fail WHERE ip = ? AND at >= ?`,
		ip, timeStr(since)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count auth fails: %w", err)
	}
	return n, nil
}

// PruneAuthFails drops audit rows older than a day.
func (s *Store) PruneAuthFails(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_fail WHERE at < ?`, timeStr(before))
	if err != nil {
		return 0, fmt.Errorf("store: prune auth fails: %w", err)
	}
	return res.RowsAffected()
}
