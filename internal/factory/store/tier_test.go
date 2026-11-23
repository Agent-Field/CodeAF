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

func TestTierReadDoesNotProjectIntoMutations(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it := legacyTierJob(t, st)
	st.SetTierResolver(func(context.Context, factory.Item) (factory.Tier, time.Time, error) {
		return "", time.Time{}, errors.New("offline")
	})
	if got, err := st.Get(it.ID); err != nil || got.Failure == nil {
		t.Fatalf("read failure=%+v err=%v", got.Failure, err)
	}
	for _, mutate := range []func(int, func(*factory.Item) error) error{st.Update, st.Annotate} {
		if err := mutate(it.ID, func(cur *factory.Item) error {
			if cur.Failure != nil {
				t.Fatalf("mutation saw read projection: %+v", cur.Failure)
			}
			cur.Title = "updated"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.GetSaved(it.ID)
	if err != nil || got.Title != "updated" || got.Failure != nil {
		t.Fatalf("saved=%+v err=%v", got, err)
	}
}

func TestSavedTierReadInsideMutationUsesSavedSnapshot(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it := legacyTierJob(t, st)
	other, err := Open(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	inside := true
	for _, reader := range []*Store{st, other} {
		reader.SetTierResolver(func(context.Context, factory.Item) (factory.Tier, time.Time, error) {
			if inside {
				t.Error("nested read attempted author lookup")
			}
			return factory.TierCollab, time.Time{}, nil
		})
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error {
		for _, reader := range []*Store{st, other} {
			got, err := reader.GetSaved(it.ID)
			if err != nil || got.Tier != "" || got.Title != it.Title {
				t.Fatalf("nested snapshot=%+v err=%v", got, err)
			}
		}
		cur.Title = "updated"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	inside = false
	if got, err := st.Get(it.ID); err != nil || got.Title != "updated" || got.Tier != factory.TierCollab {
		t.Fatalf("unlocked read=%+v err=%v", got, err)
	}
}

func TestTierReadSkipsMissingAuthorOrRepository(t *testing.T) {
	for _, field := range []string{"author", "repository"} {
		t.Run(field, func(t *testing.T) {
			st, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			it := factory.Item{Origin: factory.OriginForge, Author: "bob", Repo: "acme/api"}
			if field == "author" {
				it.Author = ""
			} else {
				it.Repo = ""
			}
			it, err = st.Create(it)
			if err != nil {
				t.Fatal(err)
			}
			st.SetTierResolver(func(context.Context, factory.Item) (factory.Tier, time.Time, error) {
				t.Error("lookup without author or repository")
				return "", time.Time{}, errors.New("missing identity")
			})
			got, err := st.Get(it.ID)
			if err != nil || got.Failure != nil {
				t.Fatalf("read=%+v err=%v", got, err)
			}
			items, err := st.List()
			if err != nil || len(items) != 1 || items[0].Failure != nil {
				t.Fatalf("list=%+v err=%v", items, err)
			}
		})
	}
}
