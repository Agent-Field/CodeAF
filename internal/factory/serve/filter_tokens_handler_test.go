package serve_test

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func queueJobs(t *testing.T, f *v2Jobs, q string) api.Queue {
	t.Helper()
	w := verbRequest(f.h, "GET", "/v1/queue?q="+url.QueryEscape(q), "")
	checkJSON(t, w, 200)
	var got api.Queue
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func setCreated(t *testing.T, f *v2Jobs, it factory.Item, at time.Time) {
	t.Helper()
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Created = at; return nil }); err != nil {
		t.Fatal(err)
	}
}

// The queue filters age: by the handler's injected clock, the same one its rows
// are aged by, never the wall clock.
func TestQueueAgeFilterUsesTheHandlerClock(t *testing.T) {
	f := newV2Jobs(t)
	clock := time.Unix(0, f.clock.Load()).UTC()
	it := f.create(t, factory.Item{Assignee: "sam", State: factory.StateNew})
	setCreated(t, f, it, clock.Add(-72*time.Hour)) // three days old by the handler clock
	if n := len(queueJobs(t, f, "age:>1d").Jobs); n != 1 {
		t.Fatalf("age:>1d under the injected clock = %d jobs, want 1", n)
	}
	if n := len(queueJobs(t, f, "age:<1d").Jobs); n != 0 {
		t.Fatalf("age:<1d under the injected clock = %d jobs, want 0", n)
	}
	f.clock.Add(int64(-71 * time.Hour)) // now the job is one hour old
	if n := len(queueJobs(t, f, "age:<2h").Jobs); n != 1 {
		t.Fatalf("age:<2h after moving the clock = %d jobs, want 1", n)
	}
}

// A quoted value that merely contains the words is:dismissed must not reveal
// the caller's dismissed jobs; only the bare word does.
func TestQueueQuotedDismissedWordDoesNotExposeHiddenJobs(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Assignee: "sam", State: factory.StateNew, Labels: []string{"x is:dismissed"}})
	f.post(t, it, "dismiss", "", 200)
	if n := len(queueJobs(t, f, `label:"x is:dismissed"`).Jobs); n != 0 {
		t.Fatalf("quoted is:dismissed exposed %d hidden jobs", n)
	}
	if n := len(queueJobs(t, f, `label:"x is:dismissed" is:dismissed`).Jobs); n != 1 {
		t.Fatalf("the bare word must still reveal the dismissed job, got %d", n)
	}
}
