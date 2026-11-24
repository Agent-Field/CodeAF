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

// doneRunner serves the organization's Afi with a run that marks its task
// done at once, and records who each run was asked for.
func doneRunner(o *wsOrg, t *testing.T) (asked func(title string) (AgentTurn, bool)) {
	t.Helper()
	var mu sync.Mutex
	seen := map[string]AgentTurn{}
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		mu.Lock()
		seen[turn.Task.Title] = turn
		mu.Unlock()
		_, err := turn.Door.(workDoor).Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+turn.Task.ID+`","state":"done","handoff":"Did the task."}`))
		return AgentTurnEnd{}, err
	})
	return func(title string) (AgentTurn, bool) {
		mu.Lock()
		defer mu.Unlock()
		turn, ok := seen[title]
		return turn, ok
	}
}

// A TASK RELEASED BY FINISHED WORK STARTS BY ITSELF (owner decisions
// 2026-11-17, 1; live use E2 #7: the newsletter and the social thread sat at
// "Ready to start" after the blog post was done, each needing its own Start).
// Start the blog post and the two tasks waiting on it run, with the rights of
// the person who started the work, and their threads say so.
func TestAReleasedAfiTaskStartsByItselfOnTheServer(t *testing.T) {
	o, path := taskOrg(t, "edit")
	asked := doneRunner(o, t)
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}
	post := o.task(t, "santosh", path, api.TaskCreate{Title: "Launch blog post", DoneBy: afi})
	letter := o.task(t, "santosh", path, api.TaskCreate{Title: "Newsletter", DoneBy: afi, WaitingOn: []string{post.ID}})
	thread := o.task(t, "santosh", path, api.TaskCreate{Title: "Social thread", DoneBy: afi, WaitingOn: []string{post.ID}})
	if got := o.one(t, path, letter.ID); got.StartsWhenReady == nil || !*got.StartsWhenReady {
		t.Fatalf("a waiting Afi task reads starts_when_ready %v, want true by default", got.StartsWhenReady)
	}

	o.do(t, "abir", "PATCH", path+"/tasks/"+post.ID, post.Rev, map[string]any{"state": "running"}, 200, nil)
	o.settle()
	for _, w := range []api.WorkstreamTask{letter, thread} {
		if got := o.one(t, path, w.ID); got.State != factory.TaskDone {
			t.Fatalf("%q is %q after the post was done, want it started and done", w.Title, got.State)
		}
		if turn, ok := asked(w.Title); !ok || turn.Asker != "abir" {
			t.Fatalf("%q ran %v as %q, want abir's rights (who started the work)", w.Title, ok, turn.Asker)
		}
		if !o.threadSays(t, path, w.ID, "Started: Launch blog post is done") {
			t.Fatalf("%q's thread does not say it started", w.Title)
		}
	}
}

// ITS OWN WORD WINS, AND ONLY ITS OWNER OR WHOEVER LOOKS AFTER THE WORKSTREAM
// GIVES IT. "Waits for you to start" keeps a released task up next with the
// line it always had; the owner's own default turned off does the same for a
// task with no word; abir, who edits but neither owns the task nor looks after
// the workstream, cannot change it.
func TestATaskThatWaitsForYouStaysReady(t *testing.T) {
	o, path := taskOrg(t, "edit")
	doneRunner(o, t)
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}

	first := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft", DoneBy: afi})
	held := o.task(t, "santosh", path, api.TaskCreate{Title: "Publish", DoneBy: afi, WaitingOn: []string{first.ID}})
	o.do(t, "abir", "PATCH", path+"/tasks/"+held.ID, held.Rev, map[string]any{"starts": "by_hand"}, 403, nil)
	o.do(t, "santosh", "PATCH", path+"/tasks/"+held.ID, held.Rev, map[string]any{"starts": "sometimes"}, 422, nil)
	var set api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+held.ID, held.Rev, map[string]any{"starts": "by_hand"}, 200, &set)
	if set.Starts != factory.TaskStartsByHand || !o.threadSays(t, path, held.ID, "set it to “Waits for you to start”") {
		t.Fatalf("set %+v", set)
	}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+first.ID, first.Rev, map[string]any{"state": "running"}, 200, nil)
	o.settle()
	if got := o.one(t, path, held.ID); got.State != factory.TaskUpNext || *got.StartsWhenReady {
		t.Fatalf("a task that waits for its Start is %q (starts by itself %v)", got.State, *got.StartsWhenReady)
	}
	if !o.threadSays(t, path, held.ID, "Ready to start: Draft is done") {
		t.Fatal("the held task's thread does not say it can start")
	}

	// The owner's own default, off: a task with no word of its own waits too.
	var prefs WorkPrefs
	o.do(t, "santosh", "PUT", "/v1/me/work", -1, map[string]bool{"start_when_ready": false}, 200, &prefs)
	if prefs.StartWhenReady == nil || *prefs.StartWhenReady {
		t.Fatalf("the default reads %+v after turning it off", prefs)
	}
	o.do(t, "abir", "GET", "/v1/me/work", -1, nil, 200, &prefs)
	if !*prefs.StartWhenReady {
		t.Fatal("santosh's setting changed abir's")
	}
	again := o.task(t, "santosh", path, api.TaskCreate{Title: "Outline", DoneBy: afi})
	after := o.task(t, "santosh", path, api.TaskCreate{Title: "Edit", DoneBy: afi, WaitingOn: []string{again.ID}})
	o.do(t, "santosh", "PATCH", path+"/tasks/"+again.ID, again.Rev, map[string]any{"state": "running"}, 200, nil)
	o.settle()
	if got := o.one(t, path, after.ID); got.State != factory.TaskUpNext {
		t.Fatalf("with its owner's default off the released task is %q", got.State)
	}
}

// A PLAN NOBODY HAS STARTED WAITS FOR ITS START, and a person's task never
// starts by itself.
func TestAnUnstartedPlanAndAPersonsTaskDoNotStartByThemselves(t *testing.T) {
	o, path := taskOrg(t, "edit")
	doneRunner(o, t)
	afi := factory.TaskDoneBy{Kind: factory.TaskByAfi}
	brief := o.task(t, "santosh", path, api.TaskCreate{Title: "Brief", DoneBy: afi})
	g := &factory.TaskGroup{Name: "Launch", Kind: "batch"}
	var last api.WorkstreamTask
	for i, title := range []string{"One", "Two", "Three", "Four", "Five"} {
		c := api.TaskCreate{Title: title, DoneBy: afi, Group: g}
		if i == 4 {
			c.WaitingOn = []string{brief.ID}
		}
		last = o.task(t, "santosh", path, c)
		g = last.Group
	}
	mine := o.task(t, "santosh", path, api.TaskCreate{Title: "Review", WaitingOn: []string{brief.ID},
		DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh", Name: "santosh"}})
	o.do(t, "santosh", "PATCH", path+"/tasks/"+brief.ID, brief.Rev, map[string]any{"state": "running"}, 200, nil)
	o.settle()
	if got := o.one(t, path, last.ID); got.State != factory.TaskUpNext || *got.StartsWhenReady {
		t.Fatalf("a task of a plan nobody started is %q", got.State)
	}
	if got := o.one(t, path, mine.ID); got.State != factory.TaskUpNext || got.StartsWhenReady != nil {
		t.Fatalf("a person's task is %+v", got)
	}
	for _, e := range o.tasks(t, "santosh", path) {
		if strings.HasPrefix(e.Title, "One") && e.State != factory.TaskUpNext {
			t.Fatalf("the plan started without its Start: %+v", e)
		}
	}
}
