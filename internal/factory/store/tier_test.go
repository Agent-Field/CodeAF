package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func legacyTierJob(t *testing.T, st *Store) factory.Item {
	t.Helper()
	it, err := st.Create(factory.Item{Origin: factory.OriginForge, Product: "acme", Repo: "api", Author: "bob", Num: 7, Title: "legacy job"})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestTierReadBackoffAndListShareHydration(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	st.clock = func() time.Time { return now }
	it := legacyTierJob(t, st)
	calls := 0
	st.SetTierResolver(func(ctx context.Context, it factory.Item) (factory.Tier, time.Time, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("lookup has no deadline")
		}
		return "", time.Time{}, errors.New("offline")
	})
	for _, delay := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute} {
		before := calls
		items, err := st.List()
		if err != nil || len(items) != 1 {
			t.Fatalf("list=%v err=%v", items, err)
		}
		want := now.Add(delay)
		if calls != before+1 || items[0].Failure == nil || !strings.Contains(items[0].Failure.Detail, want.Format(time.RFC3339)) {
			t.Fatalf("calls=%d failure=%+v; want deadline %s", calls, items[0].Failure, want)
		}
		now = want.Add(-time.Nanosecond)
		if _, err := st.Get(it.ID); err != nil {
			t.Fatal(err)
		}
		if calls != before+1 {
			t.Fatal("retried before deadline")
		}
		now = want
	}
	body, err := os.ReadFile(st.ItemPath(it.ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "offline") || strings.Contains(string(body), "retry after") {
		t.Fatal("retry state was saved")
	}
	reopened, err := Open(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Failure != nil {
		t.Fatalf("retry state survived reopening: %+v", got.Failure)
	}
}

func TestTierReadHonorsLaterRetryDeadline(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	st.clock = func() time.Time { return now }
	it := legacyTierJob(t, st)
	calls := 0
	retry := now.Add(time.Hour)
	st.SetTierResolver(func(context.Context, factory.Item) (factory.Tier, time.Time, error) {
		calls++
		return "", retry, errors.New("rate limited")
	})
	got, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Failure == nil || !strings.Contains(got.Failure.Detail, retry.Format(time.RFC3339)) {
		t.Fatalf("failure=%+v", got.Failure)
	}
	now = retry.Add(-time.Nanosecond)
	if _, err := st.Get(it.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("ignored later retry deadline")
	}
	now = retry
	if _, err := st.Get(it.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("did not retry on read at deadline")
	}
}

func TestTierReadDiscardsChangedAuthorAndKeepsNewRetry(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it := legacyTierJob(t, st)
	entered, release := make(chan struct{}), make(chan struct{})
	aliceCalls := 0
	st.SetTierResolver(func(ctx context.Context, it factory.Item) (factory.Tier, time.Time, error) {
		if it.Author == "bob" {
			close(entered)
			<-release
			return factory.TierCollab, time.Time{}, nil
		}
		aliceCalls++
		return "", time.Time{}, errors.New("offline")
	})
	finished := make(chan error, 1)
	go func() { _, err := st.Get(it.ID); finished <- err }()
	<-entered
	if err := st.Annotate(it.ID, func(cur *factory.Item) error { cur.Author = "alice"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(it.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != "" || got.Author != "alice" || aliceCalls != 1 {
		t.Fatalf("author=%s tier=%q alice lookups=%d; want blank and one failed lookup", got.Author, got.Tier, aliceCalls)
	}
}

func TestSavingReadProjectionDoesNotSaveRetryState(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it := legacyTierJob(t, st)
	st.SetTierResolver(func(context.Context, factory.Item) (factory.Tier, time.Time, error) {
		return "", time.Time{}, errors.New("offline")
	})
	got, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(got); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(st.ItemPath(it.ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "retry after") {
		t.Fatal("saving a read projection persisted retry state")
	}
}
