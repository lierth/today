package server

import (
	"context"
	"testing"
	"time"

	"today/internal/store"
)

// The automatic carry has to run after the 00:05 guard, not before it: the
// earlier order marked the day done at 00:03 and every later tick skipped,
// so it never ran at all.
func TestAutoCarryRunsAfterTheGuard(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	loc := e.loc
	if err := e.st.PutSettings(ctx, map[string]string{"auto_carry": "true"}); err != nil {
		t.Fatal(err)
	}
	// An unfinished item from yesterday.
	day, _, _ := e.srv.Today(ctx)
	y := day.AddDate(0, 0, -1)
	clock := time.Date(y.Year(), y.Month(), y.Day(), 9, 0, 0, 0, loc)
	old, err := e.st.CreateItem(ctx, store.Item{Title: "昨天的", StartsAt: &clock}, loc)
	if err != nil {
		t.Fatal(err)
	}

	// 00:03: the guard refuses, and the day must stay unmarked.
	e.srv.autoCarry(ctx, loc, time.Date(day.Year(), day.Month(), day.Day(), 0, 3, 0, 0, loc))
	if got, _ := e.st.Item(ctx, old.ID); got.DateLocal == day.Format(store.DateLayout) {
		t.Fatal("carry ran inside the 00:00-00:05 guard")
	}
	// 00:08: it runs, once.
	e.srv.autoCarry(ctx, loc, time.Date(day.Year(), day.Month(), day.Day(), 0, 8, 0, 0, loc))
	moved, err := e.st.Item(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if moved.DateLocal != day.Format(store.DateLayout) {
		t.Fatalf("carry did not run at 00:08: item on %s", moved.DateLocal)
	}
	if moved.CarriedFrom == "" {
		t.Fatal("carried_from not recorded by the automatic carry")
	}
	// A second tick on the same day is a no-op, which is what lastCarry is
	// for: a second item added later stays put.
	late := day.AddDate(0, 0, -1)
	clock2 := time.Date(late.Year(), late.Month(), late.Day(), 11, 0, 0, 0, loc)
	second, err := e.st.CreateItem(ctx, store.Item{Title: "后来加的", StartsAt: &clock2}, loc)
	if err != nil {
		t.Fatal(err)
	}
	e.srv.autoCarry(ctx, loc, time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, loc))
	if got, _ := e.st.Item(ctx, second.ID); got.DateLocal == day.Format(store.DateLayout) {
		t.Fatal("the day's carry ran twice")
	}
}
