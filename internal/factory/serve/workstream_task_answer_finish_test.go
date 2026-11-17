package serve

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A RUN AN ANSWER STARTED IS NEVER FAILED FOR ENDING ON A REPLY (mac-studio,
// 2026-11-17, "Release notes for v0.5"). Afi wrote the notes, asked whether to
// keep them a draft, and its run ended; Lena answered, and the run that answer
// started set the task to wait on the two tasks the notes depend on, said so
// on the thread and ended a second later. It had written no file of its own,
// so the server called the task Failed: "Afi stopped before saying the task
// was done". Waiting on unfinished work is a task Up next with its wait
// showing, and a resumed run that only replies has the work its asking run
// wrote behind it, which makes it Done.
func TestARunAnAnswerStartedIsNotFailedForEndingOnAReply(t *testing.T) {
	o, path := taskOrg(t, "edit")
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}

	var mu sync.Mutex
	runs := map[string]int{}
	var blocker string
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		mu.Lock()
		runs[turn.Task.ID]++
		n, wait := runs[turn.Task.ID], blocker
		mu.Unlock()
		d := turn.Door.(workDoor)
		id := turn.Task.ID
		if n == 1 {
			// The asking run: the draft, then the question.
			if _, err := d.Work(ctx, "ws_file_create", json.RawMessage(`{"name":"Notes `+id+`","markdown":"# Notes\n\nA draft."}`)); err != nil {
				return AgentTurnEnd{}, err
			}
			_, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+id+`","ask":{"title":"Keep the draft or hold it?","options":[{"label":"Keep the draft"},{"label":"Hold"}]}}`))
			return AgentTurnEnd{}, err
		}
		if turn.Task.Title == "Waits" && n >= 3 {
			// Started by itself once its wait was done: it finishes.
			_, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+id+`","state":"done","handoff":"Wrote it."}`))
			return AgentTurnEnd{}, err
		}
		if turn.Task.Title == "Waits" {
			if _, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+id+`","waiting_on":["`+wait+`"]}`)); err != nil {
				return AgentTurnEnd{}, err
			}
		}
		_, err := d.Work(ctx, "ws_task_comment", json.RawMessage(`{"task":"`+id+`","text":"Carried on per the answer."}`))
		return AgentTurnEnd{}, err
	})

	run := func(title string) factory.WorkstreamTask {
		t.Helper()
		tk := o.task(t, "santosh", path, api.TaskCreate{Title: title, DoneBy: afi})
		o.do(t, "santosh", "PATCH", path+"/tasks/"+tk.ID, tk.Rev, map[string]any{"state": "running"}, 200, nil)
		o.settle()
		got := o.one(t, path, tk.ID)
		if got.State != factory.TaskNeeds || got.Ask == nil {
			t.Fatalf("Afi's ask left %+v", got)
		}
		o.do(t, "santosh", "POST", path+"/tasks/"+tk.ID+"/answer", -1, api.TaskAnswer{Option: got.Ask.Options[0].ID}, 200, nil)
		o.settle()
		return o.one(t, path, tk.ID)
	}

	// The resumed run waits on work nobody has finished: Up next, waiting.
	review := o.task(t, "santosh", path, api.TaskCreate{Title: "Security review", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh", Name: "santosh"}})
	mu.Lock()
	blocker = review.ID
	mu.Unlock()
	got := run("Waits")
	if got.State != factory.TaskUpNext || !slices.Contains(got.WaitingOn, review.ID) {
		t.Fatalf("a resumed run that left the task waiting on %s made it %q (%q), waiting on %v", review.ID, got.State, got.StateReason, got.WaitingOn)
	}

	// UP NEXT IS NOT PARKED: once the work it waits on is done, the task
	// starts by itself, with Afi, and finishes.
	o.do(t, "santosh", "PATCH", path+"/tasks/"+review.ID, review.Rev, map[string]any{"state": "done"}, 200, nil)
	o.settle()
	mu.Lock()
	waitsRuns := runs[got.ID]
	mu.Unlock()
	if after := o.one(t, path, got.ID); after.State != factory.TaskDone || waitsRuns != 3 {
		t.Fatalf("once its wait was done the task left in Up next is %q after %d run(s)", after.State, waitsRuns)
	}
	if !o.threadSays(t, path, got.ID, "Started: Security review is done") {
		t.Fatal("the thread does not say the waiting task started by itself")
	}

	// The resumed run only replies: the asking run's draft is the result.
	got = run("Replies")
	if got.State != factory.TaskDone || len(got.Results) != 1 || got.Results[0].FileID == "" {
		t.Fatalf("a resumed run that replied after its draft made the task %q (%q) with results %+v", got.State, got.StateReason, got.Results)
	}
}
