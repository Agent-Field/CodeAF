package serve

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// AN ANSWER RECORDS THE DECISION; IT NEVER STARTS A TASK WHOSE WAITS ARE NOT
// DONE (the blackmac demo, 2026-11-17). The launch blog post waited on
// "Confirm the launch date" and carried an Ask; answering it set the post
// running and Afi wrote it before the date was confirmed. The answer is kept
// and decided, the post stays in Up next with its thread saying what it waits
// for, and it starts by itself, with Afi, once that work is done.
func TestAnAnswerOnAWaitingTaskRecordsTheDecisionAndStartsNothing(t *testing.T) {
	o, path := taskOrg(t, "edit")

	var mu sync.Mutex
	var turns []AgentTurn
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		mu.Lock()
		turns = append(turns, turn)
		mu.Unlock()
		_, err := turn.Door.(workDoor).Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done"}`))
		return AgentTurnEnd{}, err
	})
	ran := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(turns)
	}

	confirm := o.task(t, "santosh", path, api.TaskCreate{Title: "Confirm the launch date", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh", Name: "santosh"}})
	post := o.task(t, "santosh", path, api.TaskCreate{Title: "Write the launch blog post", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, WaitingOn: []string{confirm.ID}})
	ask := map[string]any{"title": "Which headline leads?", "options": []map[string]string{{"id": "import", "label": "Recipe import"}, {"id": "share", "label": "Shared lists"}}}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+post.ID, post.Rev, map[string]any{"ask": ask}, 200, nil)

	var answered api.WorkstreamTask
	o.do(t, "santosh", "POST", path+"/tasks/"+post.ID+"/answer", -1, api.TaskAnswer{Option: "share"}, 200, &answered)
	o.settle()
	got := o.one(t, path, post.ID)
	if answered.State != factory.TaskUpNext || got.State != factory.TaskUpNext || got.Ask != nil {
		t.Fatalf("an answer on a task waiting on unfinished work left it %q (answered %q), ask %+v", got.State, answered.State, got.Ask)
	}
	if n := ran(); n != 0 {
		t.Fatalf("the answer started Afi %d time(s) on a task whose wait was not done", n)
	}
	if !o.threadSays(t, path, post.ID, "Up next: waits for Confirm the launch date before it starts") {
		t.Fatal("the thread does not say what the answered task still waits for")
	}
	var ws api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	if !strings.Contains(decisionTexts(ws), "Which headline leads? Shared lists") {
		t.Fatalf("the answer's decision was not kept: %q", decisionTexts(ws))
	}

	// The date is confirmed: the post starts by itself and Afi finishes it.
	o.do(t, "santosh", "PATCH", path+"/tasks/"+confirm.ID, confirm.Rev, map[string]any{"state": "done"}, 200, nil)
	o.settle()
	if got := o.one(t, path, post.ID); got.State != factory.TaskDone || ran() != 1 {
		t.Fatalf("once its wait was done the answered task is %q after %d run(s)", got.State, ran())
	}
	if !o.threadSays(t, path, post.ID, "Started: Confirm the launch date is done") {
		t.Fatal("the thread does not say the task started when its wait was done")
	}
}

// AN ANSWER ON A TASK WHOSE WAITS ARE ALL DONE STILL PUTS AFI BACK TO WORK at
// once: only unfinished work holds it.
func TestAnAnswerOnATaskWhoseWaitsAreDoneGoesOn(t *testing.T) {
	o, path := taskOrg(t, "edit")
	var mu sync.Mutex
	ran := 0
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		mu.Lock()
		ran++
		mu.Unlock()
		_, err := turn.Door.(workDoor).Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done"}`))
		return AgentTurnEnd{}, err
	})
	confirm := o.task(t, "santosh", path, api.TaskCreate{Title: "Confirm the launch date", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh", Name: "santosh"}})
	o.do(t, "santosh", "PATCH", path+"/tasks/"+confirm.ID, confirm.Rev, map[string]any{"state": "done"}, 200, nil)
	post := o.task(t, "santosh", path, api.TaskCreate{Title: "Write the launch blog post", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, WaitingOn: []string{confirm.ID}})
	ask := map[string]any{"title": "Which headline leads?", "options": []map[string]string{{"id": "share", "label": "Shared lists"}}}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+post.ID, post.Rev, map[string]any{"ask": ask}, 200, nil)
	o.do(t, "santosh", "POST", path+"/tasks/"+post.ID+"/answer", -1, api.TaskAnswer{Option: "share"}, 200, nil)
	o.settle()
	mu.Lock()
	n := ran
	mu.Unlock()
	if got := o.one(t, path, post.ID); got.State != factory.TaskDone || n != 1 {
		t.Fatalf("an answer on a task with nothing left to wait for left it %q after %d run(s)", got.State, n)
	}
}

func decisionTexts(ws api.Workstream) string {
	var out []string
	for _, d := range ws.Decisions {
		out = append(out, d.Text)
	}
	return strings.Join(out, " | ")
}
