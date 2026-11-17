package serve

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A LIMIT IS AN ASK, NOT A FAILURE (owner decisions 2026-11-17 §10; DESIGN §12
// L6 acceptance 3). A run past its spending cap, its steps or its time pauses
// with an Ask to the owner instead of overrunning or turning red; Keep going
// starts it again on the same conversation, Stop here stops it. A workstream
// that has spent its budget pauses a run before it spends anything, and a
// model error that may pass is tried once more before the task fails.
func TestARunAtItsLimitAsksTheOwnerInsteadOfOverrunningOrFailing(t *testing.T) {
	o, path := taskOrg(t, "edit")
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}
	wall, retry := taskWallMost, taskRetryAfter
	taskWallMost, taskRetryAfter = 300*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { taskWallMost, taskRetryAfter = wall, retry })

	var mu sync.Mutex
	calls := map[string][]string{}
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		mu.Lock()
		calls[turn.Task.Title] = append(calls[turn.Task.Title], turn.Text)
		n := len(calls[turn.Task.Title])
		mu.Unlock()
		done := func() (AgentTurnEnd, error) {
			_, err := turn.Door.(workDoor).Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done"}`))
			return AgentTurnEnd{}, err
		}
		if n > 1 {
			return done()
		}
		switch turn.Task.Title {
		case "Spender":
			turn.Progress(0.61, 3)
		case "Busy":
			turn.Progress(0.01, taskStepsMost)
		case "Slow":
		case "Flaky":
			return AgentTurnEnd{Transient: true}, errors.New("the model provider answered 503")
		}
		<-ctx.Done()
		return AgentTurnEnd{USD: 0.61}, ctx.Err()
	})
	start := func(title string) api.WorkstreamTask {
		tk := o.task(t, "santosh", path, api.TaskCreate{Title: title, DoneBy: afi})
		o.do(t, "santosh", "PATCH", path+"/tasks/"+tk.ID, tk.Rev, map[string]any{"state": "running"}, 200, nil)
		return tk
	}
	paused := func(tk api.WorkstreamTask, title string) api.WorkstreamTask {
		t.Helper()
		var got api.WorkstreamTask
		waitFor(t, func() bool { got = o.one(t, path, tk.ID); return got.State == factory.TaskNeeds })
		if got.Ask == nil || got.Ask.Title != title || len(got.Ask.Options) != 2 || !o.threadSays(t, path, tk.ID, "Paused: "+title) {
			t.Fatalf("%q paused with %+v", tk.Title, got.Ask)
		}
		return got
	}

	// Past its spending cap ($0.50 with no Start without asking): an Ask.
	spender := paused(start("Spender"), "Afi has spent $0.61 on this · keep going?")
	o.do(t, "santosh", "POST", path+"/tasks/"+spender.ID+"/answer", -1, map[string]any{"ask": spender.Ask.ID, "option": limitKeepGoing}, 200, nil)
	waitFor(t, func() bool { return o.one(t, path, spender.ID).State == factory.TaskDone })
	mu.Lock()
	if got := calls["Spender"]; len(got) != 2 || !strings.Contains(got[1], "said to keep going after you paused") {
		t.Fatalf("Keep going asked %q", got)
	}
	mu.Unlock()

	// Past its steps: an Ask; Stop here stops it.
	busy := paused(start("Busy"), "Afi has taken 60 steps on this · keep going?")
	o.do(t, "santosh", "POST", path+"/tasks/"+busy.ID+"/answer", -1, map[string]any{"ask": busy.Ask.ID, "option": limitStopHere}, 200, nil)
	if got := o.one(t, path, busy.ID); got.State != factory.TaskStopped || got.StoppedBy == nil {
		t.Fatalf("Stop here left %+v", got)
	}

	// Past its time: an Ask. Answered in the owner's own words (Tell it…),
	// Afi carries on with those words, not with the bare question.
	slow := paused(start("Slow"), "Afi has worked 0 minutes on this · keep going?")
	o.do(t, "santosh", "POST", path+"/tasks/"+slow.ID+"/answer", -1, map[string]any{"ask": slow.Ask.ID, "text": "Only the first ten"}, 200, nil)
	waitFor(t, func() bool { return o.one(t, path, slow.ID).State == factory.TaskDone })
	mu.Lock()
	if got := calls["Slow"]; len(got) != 2 || !strings.Contains(got[1], "“Only the first ten”") {
		t.Fatalf("Tell it… asked %q", got)
	}
	mu.Unlock()

	// A model error that may pass: one more try, then done.
	flaky := start("Flaky")
	waitFor(t, func() bool { return o.one(t, path, flaky.ID).State == factory.TaskDone })
	if !o.threadSays(t, path, flaky.ID, "tries again") {
		t.Fatal("the retry is not on the thread")
	}

	// A workstream that has spent its budget: paused before it spends.
	budget := 0.0
	var ws api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	o.do(t, "santosh", "PATCH", path, ws.Rev, api.WorkstreamPatch{BudgetUSD: &budget}, 200, nil)
	poor := paused(start("Poor"), "This workstream has spent its budget · keep going?")
	mu.Lock()
	defer mu.Unlock()
	if len(calls["Poor"]) != 0 {
		t.Fatalf("a run over budget called the model: %v", calls["Poor"])
	}
	_ = poor
}
