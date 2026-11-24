package serve

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// The four org-server defects the blackmac fill (v2) found, each driven
// through the wire the way the fill drove it.

// A TASK MADE RUNNING IS STARTED. With nothing to run it, the create is
// refused and nothing is kept; with a runner, the doer is woken in the same
// write, so the task never says running with nothing behind it (it used to,
// until it failed as stopped reporting back).
func TestATaskCreatedRunningIsStartedNotLeftRunning(t *testing.T) {
	o, path := taskOrg(t, "edit")
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}

	o.do(t, "abir", "POST", path+"/tasks", -1, api.TaskCreate{Title: "Nothing runs this", DoneBy: afi, State: factory.TaskRunning}, 409, nil)
	if got := o.tasks(t, "santosh", path); len(got) != 0 {
		t.Fatalf("a start nothing could take kept %+v", got)
	}

	release := make(chan struct{})
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return AgentTurnEnd{}, nil
	})
	made := o.task(t, "abir", path, api.TaskCreate{Title: "Write the FAQ", DoneBy: afi, State: factory.TaskRunning})
	if made.State != factory.TaskRunning || !o.h.taskTurnRunning(made.WS, made.ID) {
		t.Fatalf("a task made running has no run: state %q", made.State)
	}
	close(release)
	o.settle()
}

// THE PERSON DOING A TASK WORKS IT. A task santosh made for abir (as Add on
// Afi's cards does) stays santosh's to answer for, and abir ticks its
// checklist and finishes it without its being handed over first.
func TestThePersonDoingATaskTicksAndFinishesIt(t *testing.T) {
	o, path := taskOrg(t, "edit")
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Review the store text", Checklist: []string{"Read it", "Mark it up"},
		DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "abir", Name: "Abir"}})
	if made.Owner.ID != "santosh" {
		t.Fatalf("owner %+v", made.Owner)
	}
	var ticked api.WorkstreamTask
	o.do(t, "abir", "PATCH", path+"/tasks/"+made.ID, made.Rev, map[string]any{"check_done": []string{made.Checklist[0].ID, made.Checklist[1].ID}}, 200, &ticked)
	var done api.WorkstreamTask
	o.do(t, "abir", "PATCH", path+"/tasks/"+made.ID, ticked.Rev, map[string]any{"state": "done", "handoff": "Marked up."}, 200, &done)
	if done.State != factory.TaskDone {
		t.Fatalf("the doer's tick left %q", done.State)
	}

	// Someone neither owning nor doing a person task still cannot tick it.
	other := o.task(t, "abir", path, api.TaskCreate{Title: "Santosh signs off",
		DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh", Name: "Santosh"}, Owner: &factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: "santosh", Name: "Santosh"}})
	o.do(t, "abir", "PATCH", path+"/tasks/"+other.ID, other.Rev, map[string]any{"state": "done"}, 403, nil)
}

// A FIRST ASK GOES TO THE DECIDER IT NAMES. santosh owns the task; an "ask"
// naming abir is abir's to answer, not santosh's, and one naming santosh is
// the task's own Ask.
func TestAFirstAskGoesToTheDeciderItNames(t *testing.T) {
	o, path := taskOrg(t, "edit")
	task := o.task(t, "santosh", path, api.TaskCreate{Title: "Press briefing list", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	var asked api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, task.Rev, map[string]any{"ask": map[string]any{
		"title": "May the briefing mention a pilot?", "decider": map[string]string{"id": "abir", "name": "Abir"}}}, 200, &asked)
	if asked.Ask != nil || len(asked.Asks) != 1 || asked.Asks[0].Decider == nil || asked.Asks[0].Decider.ID != "abir" {
		t.Fatalf("the Ask went to %+v / %+v, want abir", asked.Ask, asked.Asks)
	}
	o.do(t, "santosh", "POST", path+"/tasks/"+task.ID+"/answer", -1, api.TaskAnswer{Ask: asked.Asks[0].ID, Option: "yes"}, 403, nil)
	o.do(t, "abir", "POST", path+"/tasks/"+task.ID+"/answer", -1, api.TaskAnswer{Ask: asked.Asks[0].ID, Option: "yes", Text: "unnamed"}, 200, nil)

	// An outsider named as decider is refused, as add_ask refuses them.
	again := o.one(t, path, task.ID)
	o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, again.Rev, map[string]any{"ask": map[string]any{
		"title": "Ship it?", "decider": map[string]string{"id": "mia"}}}, 422, nil)

	// Naming the owner is the task's own Ask.
	var mine api.WorkstreamTask
	own := o.task(t, "abir", path, api.TaskCreate{Title: "Owner's call", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	o.do(t, "abir", "PATCH", path+"/tasks/"+own.ID, own.Rev, map[string]any{"ask": map[string]any{
		"title": "Which date?", "options": []map[string]string{{"id": "a", "label": "Dec 8"}}, "decider": map[string]string{"id": "abir"}}}, 200, &mine)
	if mine.Ask == nil || len(mine.Asks) != 0 {
		t.Fatalf("an Ask naming the owner is %+v / %+v", mine.Ask, mine.Asks)
	}
}

// A FOLD LEAVES ONE DECISIONS LINE, AND UNDO TAKES IT BACK.
func TestAFoldWritesADecisionAndUndoRemovesIt(t *testing.T) {
	o := newWSOrg(t)
	a := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch swag"})
	b := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch content"})
	var got api.Workstream
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+a.ID, a.Rev, map[string]any{"fold_into": b.ID}, 200, &got)
	if len(got.Decisions) != 1 || !strings.HasSuffix(got.Decisions[0].Text, " folded “Launch swag” into this workstream") ||
		got.Decisions[0].From == nil || got.Decisions[0].From.Kind != factory.DecisionFromFold || got.Decisions[0].By != "santosh" {
		t.Fatalf("the fold's decisions are %+v", got.Decisions)
	}
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+b.ID, got.Rev, map[string]any{"unfold": a.ID}, 200, nil)
	o.do(t, "santosh", "GET", "/v1/workstreams/"+b.ID, -1, nil, 200, &got)
	if len(got.Decisions) != 0 {
		t.Fatalf("Undo left %+v", got.Decisions)
	}
}
