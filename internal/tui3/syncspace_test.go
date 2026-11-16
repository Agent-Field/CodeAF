package tui3

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// The launch reading feeds the first /status, /status answers before it asks
// again, and painting never reaches the relay.
func TestStatusSyncSpaceAnswersAtOnceFromTheLastReading(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	used, limit := int64(14*(1<<30)/5), int64(5<<30)
	calls := 0
	a.syncService = "https://relay.example"
	a.syncStats = func(ctx context.Context) (blobstore.Counts, error) {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > syncSpaceWithin {
			t.Error("stats fetch has no short deadline")
		}
		return blobstore.Counts{StoredBytes: &used, LimitBytes: &limit}, nil
	}
	read := a.refreshSyncSpace()
	if read == nil || calls != 0 {
		t.Fatalf("the launch reading ran on the update loop: command=%v calls=%d", read != nil, calls)
	}
	a.Update(read())

	again := a.slash("/status")
	text := lastNote(t, a)
	if !strings.Contains(strings.Join(strings.Fields(text), " "), "sync space 2.8 GiB of 5 GiB") {
		t.Fatalf("status has no aligned sync space row:\n%s", text)
	}
	if again == nil || calls != 1 {
		t.Fatalf("status did not answer before asking again: command=%v calls=%d", again != nil, calls)
	}
	for _, fact := range a.statusFacts(text) {
		if strings.Contains(fact, "GiB") {
			t.Fatalf("sync space should stay dim, but its value is highlighted: %q", fact)
		}
	}
	for i := 0; i < 3; i++ {
		a.statusText()
		a.statusJSON()
		a.statusRows(120)
		a.View()
	}
	if calls != 1 {
		t.Fatalf("rendering contacted the relay: %d stats calls", calls)
	}
	a.Update(again())
	if calls != 2 {
		t.Fatalf("status refreshed %d times, want once", calls-1)
	}
	a.slash("/status --json")
	var object map[string]string
	if err := json.Unmarshal([]byte(lastNote(t, a)), &object); err != nil {
		t.Fatal(err)
	}
	if object["sync space"] != "2.8 GiB of 5 GiB" {
		t.Fatalf("json status = %v", object)
	}
}

// A relay that never answers must not hold up /status: it used to answer at
// once, and an offline laptop would otherwise wait on every press.
func TestStatusDoesNotWaitOnTheRelay(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.syncService = "https://relay.example"
	a.syncStats = func(ctx context.Context) (blobstore.Counts, error) {
		<-ctx.Done()
		return blobstore.Counts{}, ctx.Err()
	}
	before := len(a.entries)
	a.slash("/status")
	if len(a.entries) == before {
		t.Fatal("status wrote nothing before the relay answered")
	}
	if strings.Contains(lastNote(t, a), "sync space") {
		t.Fatalf("status drew a sync space line with no reading:\n%s", lastNote(t, a))
	}
}

func TestStatusOmitsUnavailableSyncSpace(t *testing.T) {
	used, limit, zero := int64(3<<30), int64(5<<30), int64(0)
	for _, tc := range []struct {
		name      string
		service   string
		counts    blobstore.Counts
		err       error
		wantCalls int
	}{
		{name: "old relay", service: "https://relay.example", wantCalls: 1},
		{name: "unknown amount", service: "https://relay.example", counts: blobstore.Counts{LimitBytes: &limit}, wantCalls: 1},
		{name: "self hosted", service: "https://relay.example", counts: blobstore.Counts{StoredBytes: &used}, wantCalls: 1},
		{name: "zero usage", service: "https://relay.example", counts: blobstore.Counts{StoredBytes: &zero, LimitBytes: &limit}, wantCalls: 1},
		{name: "no relay", counts: blobstore.Counts{StoredBytes: &used, LimitBytes: &limit}},
		{name: "sync off", service: "off", counts: blobstore.Counts{StoredBytes: &used, LimitBytes: &limit}},
		{name: "fetch error", service: "https://relay.example", counts: blobstore.Counts{StoredBytes: &used, LimitBytes: &limit}, err: errors.New("unreachable"), wantCalls: 1},
		{name: "timeout", service: "https://relay.example", err: context.DeadlineExceeded, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.syncService = tc.service
			// An unsuccessful refresh must discard a previous reading.
			a.syncSpace = blobstore.Counts{StoredBytes: &used, LimitBytes: &limit}
			calls := 0
			a.syncStats = func(context.Context) (blobstore.Counts, error) {
				calls++
				return tc.counts, tc.err
			}
			if read := a.refreshSyncSpace(); read != nil {
				a.Update(read())
			}
			a.slash("/status")
			for _, text := range []string{lastNote(t, a), a.statusText(), a.statusJSON()} {
				if strings.Contains(text, "sync space") || strings.Contains(text, "0 B") || strings.Contains(text, "unknown") {
					t.Fatalf("unavailable sync space left a placeholder:\n%s", text)
				}
			}
			a.View()
			if calls != tc.wantCalls {
				t.Fatalf("stats calls = %d, want %d", calls, tc.wantCalls)
			}
		})
	}
}
