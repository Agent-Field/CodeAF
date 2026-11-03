package serve_test

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func TestBringBackKeepsLifetimeMoney(t *testing.T) {
	for _, stream := range []*factory.Stream{nil, {Spent: .138}} {
		f := newV2Jobs(t)
		it := f.create(t, factory.Item{Title: "Keep money", State: factory.StateDismissed, DoneHow: "dropped", ChatSpent: .03, Stream: stream})
		f.post(t, it, "restore", "", 200)
		saved, err := f.st.Get(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := .03
		if stream != nil {
			want = .168
		}
		if got := factory.JobLifetimeSpent(saved); math.Abs(got-want) > 1e-9 {
			t.Fatalf("bring back lifetime = %g, want %g", got, want)
		}
		if saved.Stream != nil || saved.ChatSpent != 0 {
			t.Fatal("work was not cleared")
		}
	}
}

func TestRunAsNewStartsWithoutLifetimeMoney(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.seam.NewJob = func(repo, title, text string) (int, error) {
		it, err := f.st.Create(factory.Item{Repo: repo, Title: title, Body: text, State: factory.StateNew})
		return it.ID, err
	}
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{Title: "New copy", State: factory.StateShipped, DoneHow: "answered", SpentBefore: .138, Stream: &factory.Stream{Spent: .03, Ended: now}})
	w := f.post(t, it, "run-as-new", "", 201)
	var out struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	saved, err := f.st.Get(out.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := factory.JobLifetimeSpent(saved); got != 0 {
		t.Fatalf("new job lifetime = %g", got)
	}
	if got := f.read(t, it).LifetimeCost; math.Abs(got-.168) > 1e-9 {
		t.Fatalf("source lifetime = %g", got)
	}
}

func TestLateOldRunUsageKeepsLifetimeWithoutChargingNewRun(t *testing.T) {
	f, it, worker, _, body := retainedNoChangeSetup(t)
	path := fmt.Sprintf("/v1/workers/recover/%d", it.ID)
	var out api.WorkerRecovered
	f.request(t, "POST", path, worker.Credential, body, 200, &out)
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.State = factory.StateNew
		return run.QueueItem(it, now.Add(time.Minute))
	}); err != nil {
		t.Fatal(err)
	}
	body.AdditionalCost = .25
	for range 2 {
		f.request(t, "POST", path, worker.Credential, body, 200, &out)
	}
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := factory.JobLifetimeSpent(saved); got != 1 {
		t.Fatalf("late lifetime = %g, want 1", got)
	}
	if got := factory.JobSpent(saved); got != 0 {
		t.Fatalf("old run charged new run %g, want 0", got)
	}
}
