package serve

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A PERSON ASKS ONE DECIDER (v2 J7 "Priya asks"). santosh raises an Ask on a
// task to abir, who is in the workstream; mia, who is not, is refused by name.
// (The fixture names every person Priya Nair.) The task waits, the thread
// says who asked whom, only abir answers, the
// answer is kept as a decision, and work that was up next is up next again.
func TestAPersonAsksOneDeciderInTheWorkstreamAndTheAnswerIsADecision(t *testing.T) {
	o, path := taskOrg(t, "edit")
	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Fix the two crash reports", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh", Name: "santosh"}})
	ask := func(decider string) map[string]any {
		return map[string]any{"add_ask": map[string]any{"title": "Can the two crash fixes land before the 28th?", "decider": map[string]string{"id": decider, "name": decider}}}
	}

	var refused struct {
		Error string `json:"error"`
	}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, task.Rev, ask("mia"), 422, &refused)
	if !strings.Contains(refused.Error, "mia is not in this workstream") {
		t.Fatalf("an outsider was refused with %q", refused.Error)
	}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, task.Rev, ask("santosh"), 422, nil)

	var asked api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, task.Rev, ask("abir"), 200, &asked)
	if asked.State != factory.TaskNeeds || len(asked.Asks) != 1 {
		t.Fatalf("asked %+v", asked)
	}
	a := asked.Asks[0]
	if a.Decider == nil || a.Decider.ID != "abir" || a.By == nil || a.By.Kind != factory.TaskByPerson || a.By.ID != "santosh" {
		t.Fatalf("the Ask names %+v, raised by %+v", a.Decider, a.By)
	}
	if len(a.Options) != 2 || a.Options[0].Label != "Yes" || a.Options[1].Label != "No" {
		t.Fatalf("a person's Ask offers %+v, want Yes and No", a.Options)
	}
	if !o.threadSays(t, path, task.ID, " asks Priya Nair: Can the two crash fixes land before the 28th?") {
		t.Fatal("the thread does not say who asked whom")
	}

	// Only the decider answers; their answer, in words or Yes/No, decides.
	o.do(t, "santosh", "POST", path+"/tasks/"+task.ID+"/answer", -1, api.TaskAnswer{Ask: a.ID, Option: "yes"}, 403, nil)
	var answered api.WorkstreamTask
	o.do(t, "abir", "POST", path+"/tasks/"+task.ID+"/answer", -1, api.TaskAnswer{Ask: a.ID, Option: "yes", Text: "if the review is done by the 26th"}, 200, &answered)
	if len(answered.Asks) != 0 || answered.State != factory.TaskUpNext {
		t.Fatalf("answered: %d Asks, state %s, want none and up next", len(answered.Asks), answered.State)
	}
	var ws api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	found := false
	for _, d := range ws.Decisions {
		if d.Text == "Can the two crash fixes land before the 28th? Yes · if the review is done by the 26th" && d.By == "abir" {
			found = d.From != nil && d.From.Kind == factory.DecisionFromAsk && d.From.Task == task.ID
		}
	}
	if !found {
		t.Fatalf("the answer is not a decision: %+v", ws.Decisions)
	}

	// Finished work takes no Ask.
	var done api.WorkstreamTask
	cur := o.one(t, path, task.ID)
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, cur.Rev, map[string]any{"state": "done"}, 200, &done)
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, done.Rev, ask("abir"), 422, nil)
}

// LIVE TEST 2026-11-23 #33: a one-click answer to a person's question had no
// Undo. It lets nothing happen by itself, so whoever answered can take it back:
// the question opens again and the decision it made is withdrawn.
func TestTheAnswerToAPersonsQuestionCanBeUndoneByWhoeverGaveIt(t *testing.T) {
	o, path := taskOrg(t, "edit")
	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Pick the launch photo", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh", Name: "santosh"}})
	var asked api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, task.Rev, map[string]any{"add_ask": map[string]any{"title": "The beach photo?", "decider": map[string]string{"id": "abir", "name": "abir"}}}, 200, &asked)
	a := asked.Asks[0]

	var answered api.WorkstreamTask
	o.do(t, "abir", "POST", path+"/tasks/"+task.ID+"/answer", -1, api.TaskAnswer{Ask: a.ID, Option: "yes"}, 200, &answered)
	if len(answered.Undoable) != 1 || answered.Undoable[0].Ask.ID != a.ID {
		t.Fatalf("the answer is not kept to undo: %+v", answered.Undoable)
	}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, answered.Rev, map[string]any{"undo_answer": a.ID}, 403, nil)

	var undone api.WorkstreamTask
	o.do(t, "abir", "PATCH", path+"/tasks/"+task.ID, answered.Rev, map[string]any{"undo_answer": a.ID}, 200, &undone)
	if undone.State != factory.TaskNeeds || len(undone.Asks) != 1 || undone.Asks[0].ID != a.ID || len(undone.Undoable) != 0 {
		t.Fatalf("after the undo: state %s, %d Asks, %d undoable", undone.State, len(undone.Asks), len(undone.Undoable))
	}
	var ws api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	for _, d := range ws.Decisions {
		if strings.HasPrefix(d.Text, "The beach photo?") && d.State != factory.DecisionWithdrawn {
			t.Fatalf("the decision still stands: %+v", d)
		}
	}
}
