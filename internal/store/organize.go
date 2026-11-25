package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"strings"
	"time"
)

type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name FROM categories ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) SaveCategory(ctx context.Context, id, name string) (Category, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 30 {
		return Category{}, errors.New("分类名称需要 1–30 个字")
	}
	var exists string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM categories WHERE name=? COLLATE NOCASE AND id<>?`, name, id).Scan(&exists)
	if err == nil {
		return Category{}, errors.New("这个分类已存在")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Category{}, err
	}
	if id == "" {
		id = uuid.NewString()
		_, err = s.db.ExecContext(ctx, `INSERT INTO categories(id,name) VALUES(?,?)`, id, name)
	} else {
		var res sql.Result
		res, err = s.db.ExecContext(ctx, `UPDATE categories SET name=? WHERE id=?`, name, id)
		if err == nil {
			n, _ := res.RowsAffected()
			if n == 0 {
				return Category{}, ErrNotFound
			}
		}
	}
	return Category{ID: id, Name: name}, err
}

func (s *Store) DeleteCategory(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM categories WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) validateCategory(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM categories WHERE id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return errors.New("这个分类不存在，请重新选择")
	}
	return nil
}

const TrashRetention = 3 * 24 * time.Hour

func (s *Store) Trash(ctx context.Context) ([]Item, error) {
	if err := s.PurgeTrash(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemSelect+` FROM items WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) PurgeTrash(ctx context.Context) error {
	return s.purgeDeleted(ctx, "julianday(deleted_at) <= julianday(?)", timeStr(s.Now().Add(-TrashRetention)))
}
func (s *Store) DeletePermanently(ctx context.Context, id string) error {
	return s.purgeDeleted(ctx, "id = ?", id)
}

func (s *Store) purgeDeleted(ctx context.Context, condition string, arg string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	where := `deleted_at IS NOT NULL AND ` + condition
	// Keep only recurrence exceptions, so deleting an occurrence cannot recreate it.
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO series_exclusions(series_id,date_local) SELECT series_id,date_local FROM items WHERE series_id IS NOT NULL AND `+where, arg); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM items WHERE `+where, arg); err != nil {
		return err
	}
	return tx.Commit()
}
