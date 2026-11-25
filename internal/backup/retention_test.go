package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
	"today/internal/store"
)

func TestBackupRestoresCategoriesAndAssignments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "today.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	category, err := st.SaveCategory(ctx, "", "工作")
	if err != nil {
		t.Fatal(err)
	}
	item, err := st.CreateItem(ctx, store.Item{Title: "阅读", CategoryID: category.ID}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Create(path, filepath.Join(dir, "backups"), "today", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteCategory(ctx, category.ID); err != nil {
		t.Fatal(err)
	}
	if err := Restore(snapshot, path, filepath.Join(dir, "backups"), time.Now()); err != nil {
		t.Fatal(err)
	}
	item, err = st.Item(ctx, item.ID)
	if err != nil || item.CategoryID != category.ID {
		t.Fatal("backup lost category assignment", err)
	}
	categories, err := st.Categories(ctx)
	if err != nil || len(categories) != 1 || categories[0].ID != category.ID {
		t.Fatal("backup lost categories", err)
	}
}

func TestSevenDayExpiryPreservesUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"today-20261118.db", "today-20261119.db.gz", "today-20261125.db", "notes.db"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("snapshot"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneExpired(dir, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "today-20261118.db")); !os.IsNotExist(err) {
		t.Fatal("expired backup retained", err)
	}
	for _, name := range []string{"today-20261119.db.gz", "today-20261125.db", "notes.db"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal("kept file missing", name, err)
		}
	}
}
