package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// AckResult classifies one device acknowledgement.
type AckResult string

const (
	AckApplied   AckResult = "applied"
	AckDuplicate AckResult = "duplicate"
	AckSkipped   AckResult = "skipped"
)

// ApplyOp records a device operation and its status change in one transaction.
// Recording first and updating second is not enough: a failed update would
// leave the op_id on file, so every retry would report duplicate and the
// button press would be lost forever. An item that no longer exists rolls the
// op back and reports skipped, which is also what the caller wants for a
// retryable failure.
func (s *Store) ApplyOp(ctx context.Context, opID, deviceID, itemID, status string) (AckResult, error) {
	if opID == "" {
		return AckSkipped, errors.New("store: op_id is required")
	}
	now := timeStr(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AckSkipped, fmt.Errorf("store: apply op: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ops
		(op_id, device_id, item_id, status, applied_at) VALUES (?,?,?,?,?)`,
		opID, deviceID, itemID, status, now)
	if err != nil {
		return AckSkipped, fmt.Errorf("store: record op: %w", err)
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return AckSkipped, fmt.Errorf("store: record op: %w", err)
	}
	if inserted == 0 {
		if err := tx.Commit(); err != nil {
			return AckSkipped, fmt.Errorf("store: apply op: %w", err)
		}
		return AckDuplicate, nil
	}

	upd, err := tx.ExecContext(ctx, `UPDATE items SET status=?, updated_at=?
		WHERE id = ? AND deleted_at IS NULL`, status, now, itemID)
	if err != nil {
		return AckSkipped, fmt.Errorf("store: set status: %w", err)
	}
	if n, _ := upd.RowsAffected(); n == 0 {
		return AckSkipped, nil
	}
	if err := tx.Commit(); err != nil {
		return AckSkipped, fmt.Errorf("store: apply op: %w", err)
	}
	return AckApplied, nil
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
