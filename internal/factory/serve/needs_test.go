package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestOnlyTheJobsPersonContinuesTheCap(t *testing.T) {
	cfg := recordConfig(t)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Store = st
	job, err := st.Create(factory.Item{
		State: factory.StateNeedsYou, Question: "budget of $5 reached · $5 more, or stop?", QKind: "cap",
		Cap: 5, Starter: "sam", Stream: &factory.Stream{Spent: 5, Cur: 0, Phases: []factory.Phase{{Name: "write", State: factory.PhaseWaiting}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.Seam{
		Load:   func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err },
		GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "mei"}, nil },
		Answer: func(int, bool, string) error { t.Fatal("someone else reached the door"); return nil },
	}
	h := serve.New(seam, cfg)
	w := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/answer", job.ID), `{"choice":"continue"}`)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "only @sam can answer this") {
		t.Fatalf("other person = %d %s", w.Code, w.Body)
	}
	held, err := st.Get(job.ID)
	if err != nil || held.Cap != 5 {
		t.Fatalf("cap moved: %v %+v", err, held.Cap)
	}
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "Sam"}, nil }
	seam.Answer = func(id int, yes bool, words string) error {
		if !yes || words != "continue" {
			t.Fatalf("door got yes=%v words=%q", yes, words)
		}
		return st.Update(id, func(it *factory.Item) error {
			it.Question, it.QKind = "", ""
			it.State = factory.StateRunning
			return nil
		})
	}
	h = serve.New(seam, cfg)
	w = verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/answer", job.ID), `{"words":"continue"}`)
	checkJSON(t, w, 200)
	held, err = st.Get(job.ID)
	if err != nil || held.Cap != 10 || held.Question != "" {
		t.Fatalf("continued = cap %v question %q err %v", held.Cap, held.Question, err)
	}
	events, err := cfg.Record.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 || events[0].Kind != record.Answered || events[0].Detail != "continue" || events[0].Handle != "sam" {
		t.Fatalf("events = %+v %v", events, err)
	}
	w = verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/answer", job.ID), `{"choice":"continue","next_limit":100}`)
	if w.Code != 422 {
		t.Fatalf("the app sent a ceiling and it was accepted: %d %s", w.Code, w.Body)
	}
}

func TestPublishFailedRetriesWithoutASecondApproval(t *testing.T) {
	cfg := recordConfig(t)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Store = st
	job, err := st.Create(factory.Item{State: factory.StateLanded, PublishError: "protected", Starter: "sam", Repo: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	event, err := cfg.Record.Append(context.Background(), record.Event{Kind: record.Approved, Job: job.ID, Handle: "sam"})
	if err != nil {
		t.Fatal(err)
	}
	signed := false
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) {
			items, err := st.List()
			return factory.Snapshot{Items: items}, err
		},
		GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "sam"}, nil },
		SignOff: func(id int, edited bool) (bool, error) {
			signed = true
			err := st.Update(id, func(it *factory.Item) error { return run.PublishItem(it, edited, now) })
			return false, err
		},
	}
	h := serve.New(seam, cfg)
	w := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/approve", job.ID), `{}`)
	checkJSON(t, w, 200)
	var body struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Event != event.ID {
		t.Fatalf("event = %s want %s (%v)", body.Event, event.ID, err)
	}
	if !signed {
		t.Fatal("retry did not publish")
	}
	events, err := cfg.Record.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 {
		t.Fatalf("a second approval was written: %+v %v", events, err)
	}
	held, err := st.Get(job.ID)
	if err != nil || held.PublishError != "" || held.State != factory.StateQueued {
		t.Fatalf("after retry = %s %q %v", held.State, held.PublishError, err)
	}
}
