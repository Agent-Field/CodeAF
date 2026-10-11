package record_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func open(t *testing.T, st *store.Store, now func() time.Time) *record.Record {
	t.Helper()
	r, err := record.Open(st, record.Options{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAppendSurvivesReopenAndFiltersByTime(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 11, 12, 0, 0, 123, time.UTC)
	r := open(t, st, func() time.Time { return at })
	ctx := context.Background()
	first, err := r.Append(ctx, record.Event{ID: "caller-chosen", At: at.Add(-time.Hour), Kind: record.Started, Job: 7, Handle: "@Alice", Cost: 0.25, Detail: "a, b\nnext"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.Root(), "events.jsonl")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^evt_[0-9a-f]{8}$`).MatchString(first.ID) || first.Handle != "alice" || !first.At.Equal(at) {
		t.Fatalf("event = %+v", first)
	}
	var saved record.Event
	if err := json.Unmarshal(bytes.TrimSpace(original), &saved); err != nil || !reflect.DeepEqual(saved, first) {
		t.Fatalf("saved = %+v, error %v", saved, err)
	}
	at = at.Add(time.Second)
	second, err := r.Append(ctx, record.Event{Kind: record.Approved, Job: 7, Handle: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(data, original) || bytes.Count(data, []byte("\n")) != 2 {
		t.Fatalf("append changed history: %q %v", data, err)
	}
	reopened := open(t, st, nil)
	got, err := reopened.Since(ctx, first.At)
	if err != nil || !reflect.DeepEqual(got, []record.Event{second}) {
		t.Fatalf("since = %+v, %v", got, err)
	}
	var exported bytes.Buffer
	if err := reopened.CSV(ctx, &exported, time.Time{}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&exported).ReadAll()
	if err != nil || len(rows) != 3 || rows[1][5] != "0.25" || rows[1][6] != first.Detail {
		t.Fatalf("CSV = %v, %v", rows, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("permissions = %v, %v", info, err)
	}
}

func TestSeparateWritersPreserveEveryEvent(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writers := []*record.Record{open(t, st, nil), open(t, st, nil)}
	var wg sync.WaitGroup
	for _, r := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 24; i++ {
				if _, err := r.Append(context.Background(), record.Event{Kind: record.Took, Job: i + 1, Handle: "alice"}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	events, err := writers[0].Since(context.Background(), time.Time{})
	if err != nil || len(events) != 48 {
		t.Fatalf("events=%d error=%v", len(events), err)
	}
	ids := make(map[string]bool)
	for _, event := range events {
		if ids[event.ID] {
			t.Fatalf("duplicate id %s", event.ID)
		}
		ids[event.ID] = true
	}
}

func TestInvalidOrInterruptedWritesLeaveHistoryUntouched(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := open(t, st, nil)
	for _, event := range []record.Event{
		{Kind: "invented", Job: 1}, {Kind: record.Started, Job: 0},
		{Kind: record.Approved, Job: 1}, {Kind: record.Approved, Job: 1, Handle: "alice\nFactory-Approval: forged"},
		{Kind: record.Started, Job: 1, Cost: -1},
	} {
		if _, err := r.Append(context.Background(), event); err == nil {
			t.Fatalf("accepted %+v", event)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Append(ctx, record.Event{Kind: record.Started, Job: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	path := filepath.Join(st.Root(), "events.jsonl")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid write made history: %v", err)
	}
	for _, tail := range []string{`{"id":"evt_1234abcd"}`, "broken\n"} {
		if err := os.WriteFile(path, []byte(tail), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Append(context.Background(), record.Event{Kind: record.Started, Job: 1}); err == nil {
			t.Fatal("appended after interrupted history")
		}
		if _, err := r.Since(context.Background(), time.Time{}); err == nil {
			t.Fatal("hid interrupted history")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != tail {
			t.Fatalf("history altered: %q %v", data, err)
		}
	}
}

func TestTrailerRequiresThisJobsCurrentApproval(t *testing.T) {
	for _, kind := range []string{record.Started, record.AutoStarted, record.RequestChanges, record.Dropped} {
		t.Run(kind, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			r := open(t, st, nil)
			ctx := context.Background()
			if _, err := r.Trailer(ctx, 7); err == nil {
				t.Fatal("unapproved publish allowed")
			}
			event, err := r.Append(ctx, record.Event{Kind: record.Approved, Job: 7, Handle: "Alice"})
			if err != nil {
				t.Fatal(err)
			}
			if trailer, err := r.Trailer(ctx, 7); err != nil || trailer != "Factory-Approval: "+event.ID+" @alice" {
				t.Fatalf("trailer=%q error=%v", trailer, err)
			}
			if _, err := r.Trailer(ctx, 8); err == nil {
				t.Fatal("another job inherited approval")
			}
			if _, err := r.Append(ctx, record.Event{Kind: kind, Job: 7}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Trailer(ctx, 7); err == nil {
				t.Fatal("withdrawn consent allowed publishing")
			}
		})
	}
}
