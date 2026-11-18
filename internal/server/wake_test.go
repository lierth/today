package server

import (
	"context"
	"testing"
	"time"

	"today/internal/store"
)

func TestNextWakeSkipsEventsInsideTheLeadWindow(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 9, 58, 0, 0, e.loc)

	add := func(title string, at time.Time) {
		t.Helper()
		start := at
		if _, err := e.st.CreateItem(ctx, store.Item{Title: title, StartsAt: &start}, e.loc); err != nil {
			t.Fatal(err)
		}
	}
	// Three minutes out: the five-minute lead already passed, so waking for it
	// would mean waking every minute until it starts.
	add("马上开始", now.Add(3*time.Minute))

	wake, refresh := e.srv.nextWake(ctx, now)
	if refresh {
		t.Fatalf("refresh = true for an event already inside the lead window")
	}
	// The next wake is the six-hour cap or midnight, whichever comes first —
	// what matters is that it is not the event three minutes out.
	if !wake.After(now.Add(10 * time.Minute)) {
		t.Fatalf("wake = %v, want to skip the event that already started", wake)
	}

	// Ten minutes out: the wake fires five minutes before it and is marked as
	// an event refresh.
	add("十分钟后", now.Add(10*time.Minute))
	wake, refresh = e.srv.nextWake(ctx, now)
	if !refresh || !wake.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("wake = %v refresh = %v, want now+5m with refresh", wake, refresh)
	}
}

func TestNextWakeAlwaysLeadsTheDevice(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	// 23:56:30 is inside the six-hour cap but only 8.5 minutes from midnight,
	// and an event 5 minutes + 30 s out would otherwise land under a second
	// ahead once truncated to Unix seconds.
	now := time.Date(2026, 11, 18, 23, 56, 30, 0, e.loc)
	start := now.Add(5*time.Minute + 30*time.Second)
	if _, err := e.st.CreateItem(ctx, store.Item{Title: "跨夜", StartsAt: &start}, e.loc); err != nil {
		t.Fatal(err)
	}

	wake, _ := e.srv.nextWake(ctx, now)
	if delta := wake.Sub(now); delta < time.Minute {
		t.Fatalf("wake in %v, want at least a minute so the device keeps the deadline", delta)
	}
	if sleep := wake.Unix() - now.Unix(); sleep <= 0 {
		t.Fatalf("sleep seconds = %d, device would fall back to six hours", sleep)
	}
}
