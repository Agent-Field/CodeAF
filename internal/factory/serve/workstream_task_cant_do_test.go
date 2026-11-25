package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// CAN'T DO IT IS AN ASK, NOT FAILED (owner decisions §10, Q4). The work-loop
// audit's competitor-notes task ended red Failed with "If someone pastes their
// text, I can compare it". Here the doer's failed becomes a question to the
// owner with that way forward as an answer; Do it myself makes it the
// owner's checkbox, Drop it stops it, and none of them is kept as a decision.
func TestAnAgentThatCannotDoATaskAsksTheOwnerInsteadOfFailing(t *testing.T) {
	o := newWSOrg(t)
	ws, _, _, _ := o.orgScene(t)
	ctx := context.Background()
	d := &agentDoor{h: o.h, id: ws.ID, asker: "mia"}
	const brief = `"goal":"Know where our notes stand","deliverable":"An answer on the task","acceptance":["Names three competitors","Quotes their current notes"]`
	newTask := func(title string) string {
		t.Helper()
		line, err := d.Work(ctx, "ws_task_create", json.RawMessage(`{"title":"`+title+`","request":"Compare them.",`+brief+`}`))
		if err != nil {
			t.Fatal(err)
		}
		var out struct{ Task struct{ ID string } }
		_ = json.Unmarshal([]byte(line), &out)
		if _, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+out.Task.ID+`","state":"running"}`)); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+out.Task.ID+`","state":"failed","state_reason":"I can't open live App Store pages. If someone pastes their text, I can compare it."}`)); err != nil {
			t.Fatal(err)
		}
		return out.Task.ID
	}
	answer := func(id, option string) factory.WorkstreamTask {
		t.Helper()
		cur, _ := d.task(ctx, id)
		// Paste what it needs is an answer in the owner's own words, and is
		// never taken without them.
		words := ""
		if option == factory.CantDoPaste {
			words = "Notes app: v4.2 adds widgets."
		}
		ch, err := factory.AnswerTask(cur, "", option, words, factory.TaskActor{Handle: "mia", Edit: true}, factory.TaskEventBy{Kind: factory.TaskByPerson, ID: "mia", Name: "Priya"}, factory.Standin{}, func() string { return "e1" }, o.h.clock())
		if err != nil {
			t.Fatal(err)
		}
		if len(ch.Decisions) != 0 {
			t.Errorf("answering %s kept a decision: %+v", option, ch.Decisions)
		}
		return ch.Task
	}

	id := newTask("Compare competitors' store notes")
	got, _ := d.task(ctx, id)
	if got.State != factory.TaskNeeds || got.Ask == nil || !factory.IsCantDoAsk(*got.Ask) {
		t.Fatalf("a task the agent could not do is %q with ask %+v, want waiting on its owner", got.State, got.Ask)
	}
	if !strings.Contains(got.Ask.Detail, "pastes their text") || got.Ask.Options[0].Label != "Paste what it needs" || !got.Ask.Options[0].Recommended {
		t.Fatalf("the Ask does not carry the way forward: %+v", got.Ask)
	}
	// The v2 boards (2026-11-17): an Ask is a question, and taking work back
	// is "Do it myself", never handing it to some other person.
	if got.Ask.Title != "Can you give Afi what it needs?" || got.Ask.Options[1].Label != "Do it myself" {
		t.Errorf("the Ask reads %q / %q, want the v2 question and Do it myself", got.Ask.Title, got.Ask.Options[1].Label)
	}
	if after := answer(id, factory.CantDoPaste); after.State != factory.TaskRunning || after.DoneBy.Kind != factory.TaskByAfi {
		t.Errorf("Paste what it needs left %q by %q", after.State, after.DoneBy.Kind)
	}
	if after := answer(newTask("Second look"), factory.CantDoPerson); after.State != factory.TaskUpNext || after.DoneBy.Kind != factory.TaskByPerson || after.DoneBy.ID != "mia" {
		t.Errorf("Do it myself left %q done by %+v", after.State, after.DoneBy)
	}
	if after := answer(newTask("Third look"), factory.CantDoDrop); after.State != factory.TaskStopped || after.StoppedBy == nil {
		t.Errorf("Drop it left %q", after.State)
	}

	// An error is still Failed: the run's own ending writes it, not the tool.
	line, err := d.Work(ctx, "ws_task_create", json.RawMessage(`{"title":"Fourth look","request":"Compare them.",`+brief+`}`))
	if err != nil {
		t.Fatal(err)
	}
	var made struct{ Task struct{ ID string } }
	_ = json.Unmarshal([]byte(line), &made)
	errID := made.Task.ID
	if _, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+errID+`","state":"running"}`)); err != nil {
		t.Fatal(err)
	}
	d.wrote = &taskWrites{}
	o.h.finishTaskRun(d, errID, context.DeadlineExceeded)
	if got, _ := d.task(ctx, errID); got.State != factory.TaskFailed || !strings.Contains(got.StateReason, "could not finish") {
		t.Fatalf("a run that broke off is %q (%q), want Failed", got.State, got.StateReason)
	}
}
