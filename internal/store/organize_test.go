package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCategoryAndTrashLifecycle(t *testing.T) {
	st, loc := openTest(t)
	ctx := context.Background()
	now := st.Now()
	category, err := st.SaveCategory(ctx, "", "工作")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveCategory(ctx, "", " 工作 "); err == nil {
		t.Fatal("duplicate category accepted")
	}
	item, err := st.CreateItem(ctx, Item{Title: "计划", CategoryID: category.ID}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.DeleteCategory(ctx, category.ID); err != nil {
		t.Fatal(err)
	}
	item, err = st.Item(ctx, item.ID)
	if err != nil || item.CategoryID != "" {
		t.Fatalf("category deletion lost item: %v %v", item, err)
	}
	if err = st.DeletePermanently(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = st.Item(ctx, item.ID); err != nil {
		t.Fatal("permanent deletion reached a live item")
	}
	if err = st.DeleteItem(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	trash, err := st.Trash(ctx)
	if err != nil || len(trash) != 1 || trash[0].DeletedAt == nil {
		t.Fatalf("trash: %v %v", trash, err)
	}
	if _, err = st.RestoreItem(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if err = st.DeleteItem(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	st.now = func() time.Time { return now.Add(TrashRetention) }
	if _, err = st.RestoreItem(ctx, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired trash was restored", err)
	}
	trash, err = st.Trash(ctx)
	if err != nil || len(trash) != 0 {
		t.Fatal("expired trash retained", err)
	}
	var count int
	if err = st.db.QueryRow(`SELECT COUNT(*) FROM items WHERE id=?`, item.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired row not purged", err)
	}
}

func TestDeletedRecurrenceDoesNotReturnAfterPurge(t *testing.T) {
	st, loc := openTest(t)
	ctx := context.Background()
	now := st.Now()
	start := now.AddDate(0, 0, -1)
	category, err := st.SaveCategory(ctx, "", "习惯")
	if err != nil {
		t.Fatal(err)
	}
	master, err := st.CreateItem(ctx, Item{Title: "阅读", RRule: "FREQ=DAILY", StartsAt: &start, CategoryID: category.ID}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.Materialize(ctx, loc, now, 7); err != nil {
		t.Fatal(err)
	}
	day := now.AddDate(0, 0, 2).Format(DateLayout)
	items, err := st.ItemsOn(ctx, day)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if items[0].CategoryID != category.ID {
		t.Fatal("recurrence lost category")
	}
	id := items[0].ID
	if err = st.DeleteItem(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err = st.DeletePermanently(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = st.Materialize(ctx, loc, now, 7); err != nil {
		t.Fatal(master.ID, err)
	}
	items, err = st.ItemsOn(ctx, day)
	if err != nil || len(items) != 0 {
		t.Fatal("deleted occurrence returned", items, err)
	}
}
