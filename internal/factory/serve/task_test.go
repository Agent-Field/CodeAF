package serve_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// POST /v1/jobs/{id}/task sends a delegate into the job's checkout on an
// extra step: the step is in the job's steps like any step the run holds,
// with its instructions and who asked, the job's log says who sent it, and
// the record keeps the act with the main agent as its actor.
func TestTaskVerbAddsAStepAndRecordsWhoAsked(t *testing.T) {
	f := newV2Jobs(t)
	stages := []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}, {Name: "proof", Kind: factory.StageChat, On: true}}
	it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateQueued, Assignee: "sam", Cap: 10, Stages: stages, Stream: recordedRun()})
	w := f.post(t, it, "task", `{"words":"Add a note on rounding to the README.","by":"main_agent"}`, 200)
	var sent api.TaskSent
	if err := json.Unmarshal(w.Body.Bytes(), &sent); err != nil || sent.Step != "task" || sent.Runs != api.TaskRunsNow {
		t.Fatalf("answer %+v (%v): %s", sent, err, w.Body)
	}
	j := f.read(t, it)
	var names []string
	var task api.Step
	for _, s := range j.Steps {
		names = append(names, s.Name)
		if s.Name == "task" {
			task = s
		}
	}
	if strings.Join(names, " ") != "plan task write proof" {
		t.Fatalf("steps = %v, want the task before the step the queued job is on", names)
	}
	if !task.InRun || task.InPipeline || task.Ask != "Add a note on rounding to the README." || task.By != api.TaskByMainAgent || task.State != api.StepPending || task.Kind != string(factory.StageChat) {
		t.Fatalf("the task's step on the wire: %+v", task)
	}
	if j.Step == nil || j.Step.Name != "task" || j.Wait == nil || j.Wait.Who != factory.WaitRunner || j.Wait.What != "task" {
		t.Fatalf("the job is on %+v and waits on %+v, want a runner for the task", j.Step, j.Wait)
	}
	saved, _ := f.st.Get(it.ID)
	if n := len(saved.Stream.Log); n == 0 || saved.Stream.Log[n-1].Text != "Task from the main agent: Add a note on rounding to the README." {
		t.Fatalf("the job's log: %+v", saved.Stream.Log)
	}
	events, _ := f.rec.Since(context.Background(), time.Time{})
	if len(events) == 0 {
		t.Fatal("the task left no record")
	}
	last := events[len(events)-1]
	if last.Kind != record.Tasked || last.Verb != "sent a task" || last.Actor.Kind != "agent" || last.Detail != "Add a note on rounding to the README." || last.Job != it.ID {
		t.Fatalf("the record's event: %+v", last)
	}

	// The person's own task says so, and is the person's in the record.
	f.post(t, it, "task", `{"words":"And the changelog."}`, 200)
	saved, _ = f.st.Get(it.ID)
	if n := len(saved.Stream.Log); saved.Stream.Log[n-1].Text != "Task from you: And the changelog." {
		t.Fatalf("the job's log: %+v", saved.Stream.Log[n-1])
	}
	events, _ = f.rec.Since(context.Background(), time.Time{})
	if last = events[len(events)-1]; last.Actor.Kind != "person" || last.Handle != "sam" {
		t.Fatalf("the person's task in the record: %+v", last)
	}
}

// A task the job does not take is refused in a sentence, with nothing
// written and nothing recorded; a body with no instructions or an unknown
// asker is bad input.
func TestTaskVerbRefusesInPlainWords(t *testing.T) {
	f := newV2Jobs(t)
	stages := []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}, {Name: "proof", Kind: factory.StageChat, On: true}}
	cases := []struct {
		name   string
		item   factory.Item
		body   string
		status int
		words  string
	}{
		{"a job nobody started", factory.Item{State: factory.StateNew}, `{"words":"Add the note."}`, 409, "Start the job first."},
		{"a job its person took over", factory.Item{State: factory.StateNeedsYou, Manual: true, QKind: "manual", Stream: recordedRun()}, `{"words":"Add the note."}`, 409, "You have taken this job over."},
		{"the budget is reached", factory.Item{State: factory.StateRunning, Cap: 1, Stream: recordedRun()}, `{"words":"Add the note."}`, 409, "Job budget reached."},
		{"a publish in flight", factory.Item{State: factory.StateQueued, Publishing: true, Stream: recordedRun()}, `{"words":"Add the note."}`, 409, "A push is in flight."},
		{"no instructions", factory.Item{State: factory.StateRunning, Stream: recordedRun()}, `{"words":"  "}`, 422, "Say what the task is."},
		{"an unknown asker", factory.Item{State: factory.StateRunning, Stream: recordedRun()}, `{"words":"Add the note.","by":"someone"}`, 422, "by must be main_agent or you"},
		{"a misspelled field", factory.Item{State: factory.StateRunning, Stream: recordedRun()}, `{"word":"Add the note."}`, 422, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.item.Kind, c.item.Assignee, c.item.Stages = factory.KindIssue, "sam", stages
			if c.item.Cap == 0 {
				c.item.Cap = 10
			}
			it := f.create(t, c.item)
			before, tasked := f.document(t, it.ID), f.receipts(record.Tasked)
			w := f.post(t, it, "task", c.body, c.status)
			if c.words != "" && !strings.Contains(w.Body.String(), c.words) {
				t.Fatalf("refusal %s, want one saying %q", w.Body, c.words)
			}
			if f.document(t, it.ID) != before || f.receipts(record.Tasked) != tasked {
				t.Fatal("the refused task changed the job or the record")
			}
		})
	}
}

// A job on a repository that is switched off takes no task, as it takes no
// other door that would make the factory work on it.
func TestTaskVerbIsRefusedOnARepositoryThatIsOff(t *testing.T) {
	f := newOffFloor(t, "acme/web")
	it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateRunning, Assignee: "sam", Cap: 10, Stream: recordedRun(),
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}, {Name: "proof", Kind: factory.StageChat, On: true}}})
	before := f.document(t, it.ID)
	w := f.post(t, it, "task", `{"words":"Add the note."}`, 409)
	if _, reason := refusal(t, w); reason != "repo_off" {
		t.Fatalf("refusal %s, want repo_off", w.Body)
	}
	if f.document(t, it.ID) != before {
		t.Fatal("the refused task changed the job")
	}
}
