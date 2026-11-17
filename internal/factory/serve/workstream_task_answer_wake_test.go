package serve

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// AN ANSWERED ASK PUTS AFI BACK TO WORK (live2 E1 F4). Afi, working a task on
// the server, asks the owner and its run ends; the owner's answer starts Afi
// again in the task's seat with the answer in its words, and the task ends
// done. An answer that lands while the run that asked is still ending starts
// the next run once that one has gone, rather than leaving the task Running
// with nothing behind it.
func TestAnAnsweredAskPutsAfiBackToWork(t *testing.T) {
	o, path := taskOrg(t, "edit")
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}

	var mu sync.Mutex
	var turns []AgentTurn
	hold := make(chan struct{})
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		mu.Lock()
		turns = append(turns, turn)
		n := len(turns)
		mu.Unlock()
		d := turn.Door.(workDoor)
		if n == 1 || n == 3 {
			_, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","ask":{"title":"Which group first?","options":[{"label":"Internal team"},{"label":"Everyone"}]}}`))
			if err != nil {
				return AgentTurnEnd{}, err
			}
			if n == 3 {
				// The run that asked is still ending when the answer lands.
				select {
				case <-hold:
				case <-ctx.Done():
				}
			}
			return AgentTurnEnd{}, nil
		}
		_, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done","handoff":"Did what the answer asked."}`))
		return AgentTurnEnd{}, err
	})

	// A drained worker group is not a receipt for a follow-up run queued by its predecessor.
	waitState := func(id string, state string) {
		t.Helper()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for string(o.one(t, path, id).State) != state {
			select {
			case <-tick.C:
			case <-t.Context().Done():
				t.Fatal(t.Context().Err())
			}
		}
	}
	answer := func(id string) {
		t.Helper()
		got := o.one(t, path, id)
		if got.State != factory.TaskNeeds || got.Ask == nil {
			t.Fatalf("Afi's ask left %+v", got)
		}
		o.do(t, "santosh", "POST", path+"/tasks/"+id+"/answer", -1, api.TaskAnswer{Option: got.Ask.Options[0].ID}, 200, nil)
	}

	notes := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft the notes", DoneBy: afi})
	o.do(t, "santosh", "PATCH", path+"/tasks/"+notes.ID, notes.Rev, map[string]any{"state": "running"}, 200, nil)
	waitState(notes.ID, string(factory.TaskNeeds))
	answer(notes.ID)
	waitState(notes.ID, string(factory.TaskDone))
	if got := o.one(t, path, notes.ID); got.State != factory.TaskDone {
		t.Fatalf("after the answer the task is %q, with Afi never back at it", got.State)
	}
	mu.Lock()
	again := turns[1]
	mu.Unlock()
	if again.Task == nil || again.Task.ID != notes.ID || !strings.Contains(again.Text, "Which group first?") || !strings.Contains(again.Text, "Internal team") {
		t.Fatalf("Afi went on without the answer: %+v", again)
	}

	// The answer arrives while the run that asked is still ending.
	post := o.task(t, "santosh", path, api.TaskCreate{Title: "Write the post", DoneBy: afi})
	o.do(t, "santosh", "PATCH", path+"/tasks/"+post.ID, post.Rev, map[string]any{"state": "running"}, 200, nil)
	waitState(post.ID, string(factory.TaskNeeds))
	answer(post.ID)
	close(hold)
	waitState(post.ID, string(factory.TaskDone))
	o.settle()
}
