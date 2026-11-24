package serve

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// taskOrg is santosh's workstream shared with abir at role.
func taskOrg(t *testing.T, role string) (*wsOrg, string) {
	t.Helper()
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	shares := []factory.Share{{Kind: "person", ID: "abir", Role: role}}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, api.WorkstreamSharesPut{Shares: &shares}, 200, nil)
	return o, "/v1/workstreams/" + ws.ID
}

func (o *wsOrg) task(t *testing.T, who, path string, body api.TaskCreate) api.WorkstreamTask {
	t.Helper()
	var out api.WorkstreamTask
	o.do(t, who, "POST", path+"/tasks", -1, body, 201, &out)
	return out
}

func (o *wsOrg) tasks(t *testing.T, who, path string) []api.WorkstreamTask {
	t.Helper()
	var out api.WorkstreamTasks
	o.do(t, who, "GET", path+"/tasks", -1, nil, 200, &out)
	return out.Tasks
}

func TestViewReadsTasksAndEditWritesThem(t *testing.T) {
	o, path := taskOrg(t, "view")
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft the March release notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	if made.Owner.ID != "santosh" || made.State != factory.TaskUpNext || made.CreatedBy.ID != "santosh" {
		t.Fatalf("made %+v", made)
	}
	if got := o.tasks(t, "abir", path); len(got) != 1 || got[0].ID != made.ID {
		t.Fatalf("abir reads %+v", got)
	}
	o.do(t, "abir", "POST", path+"/tasks", -1, api.TaskCreate{Title: "Mine", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}}, 403, nil)
	o.do(t, "abir", "POST", path+"/tasks/"+made.ID+"/comment", -1, map[string]string{"text": "hello"}, 403, nil)
	o.do(t, "mia", "GET", path+"/tasks", -1, nil, 404, nil)
}

func TestOnlyTheOwnerAnswersAnAskOverTheWireAndEveryoneMeansTheFirstAnswer(t *testing.T) {
	o, path := taskOrg(t, "edit")
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft the March release notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	ask := map[string]any{"ask": factory.TaskAsk{Title: "Publish the notes to the website?", Options: []factory.TaskAskOption{{ID: "publish", Label: "Publish notes", Recommended: true}}}}
	var asked api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, made.Rev, ask, 200, &asked)
	if asked.State != factory.TaskNeeds || asked.Ask == nil {
		t.Fatalf("asked %+v", asked)
	}
	o.do(t, "abir", "POST", path+"/tasks/"+made.ID+"/answer", -1, api.TaskAnswer{Option: "publish"}, 403, nil)
	var answered api.WorkstreamTask
	o.do(t, "santosh", "POST", path+"/tasks/"+made.ID+"/answer", -1, api.TaskAnswer{Option: "publish"}, 200, &answered)
	if answered.Ask != nil || answered.AnsweredBy == nil || answered.AnsweredBy.ID != "santosh" {
		t.Fatalf("answered %+v", answered)
	}

	// Owner Everyone: abir, who edits, answers first; santosh's answer finds it decided.
	every := o.task(t, "santosh", path, api.TaskCreate{Title: "Pick a tagline", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, Owner: &factory.TaskOwner{Kind: factory.TaskOwnerEveryone}})
	o.do(t, "santosh", "PATCH", path+"/tasks/"+every.ID, every.Rev, ask, 200, &asked)
	o.do(t, "abir", "POST", path+"/tasks/"+every.ID+"/answer", -1, api.TaskAnswer{Option: "publish"}, 200, &answered)
	if answered.AnsweredBy.ID != "abir" {
		t.Fatalf("answered by %+v", answered.AnsweredBy)
	}
	o.do(t, "santosh", "POST", path+"/tasks/"+every.ID+"/answer", -1, api.TaskAnswer{Option: "publish"}, 409, nil)
	var events api.WorkstreamTaskEvents
	o.do(t, "santosh", "GET", path+"/tasks/"+every.ID+"/events", -1, nil, 200, &events)
	var answer api.WorkstreamTaskEvent
	for _, e := range events.Events {
		if e.Kind == factory.TaskEventAnswered {
			answer = e
		}
	}
	if answer.Kind != factory.TaskEventAnswered || answer.By.ID != "abir" {
		t.Fatalf("the thread does not record who answered: %+v", events.Events)
	}
}

func TestOnlyTheOwnerTicksAPersonTaskAndTheTickReleasesWhatWaits(t *testing.T) {
	o, path := taskOrg(t, "edit")
	interviews := o.task(t, "santosh", path, api.TaskCreate{Title: "Run five user interviews", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "abir", Name: "abir"},
		Owner: &factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: "abir", Name: "abir"}})
	summary := o.task(t, "santosh", path, api.TaskCreate{Title: "Summarize the interviews", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, WaitingOn: []string{interviews.ID}})
	o.do(t, "santosh", "PATCH", path+"/tasks/"+interviews.ID, interviews.Rev, map[string]any{"state": "done"}, 403, nil)
	var ticked api.WorkstreamTask
	o.do(t, "abir", "PATCH", path+"/tasks/"+interviews.ID, interviews.Rev, map[string]any{"state": "done"}, 200, &ticked)
	if ticked.State != factory.TaskDone {
		t.Fatalf("ticked %+v", ticked)
	}
	var released api.WorkstreamTask
	o.do(t, "santosh", "GET", path+"/tasks/"+summary.ID, -1, nil, 200, &released)
	// Released, not running: nothing has woken Afi (live use, L1 F5, L2 #4).
	if released.State != factory.TaskUpNext || released.Rev != summary.Rev {
		t.Fatalf("the waiting task is %q rev %d, want up next and untouched", released.State, released.Rev)
	}
	var thread api.WorkstreamTaskEvents
	o.do(t, "santosh", "GET", path+"/tasks/"+summary.ID+"/events", -1, nil, 200, &thread)
	if n := len(thread.Events); n == 0 || thread.Events[n-1].Text != "Ready to start: Run five user interviews is done" {
		t.Fatalf("the released task's thread: %+v", thread.Events)
	}
	// A stale rev is refused, as every workstream part is.
	o.do(t, "abir", "PATCH", path+"/tasks/"+interviews.ID, interviews.Rev, map[string]any{"state": "up_next"}, 412, nil)
}

func TestWaitingOnRefusesACircleOverTheWire(t *testing.T) {
	o, path := taskOrg(t, "edit")
	a := o.task(t, "santosh", path, api.TaskCreate{Title: "A", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	b := o.task(t, "santosh", path, api.TaskCreate{Title: "B", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, WaitingOn: []string{a.ID}})
	o.do(t, "santosh", "PATCH", path+"/tasks/"+a.ID, a.Rev, map[string]any{"waiting_on": []string{b.ID}}, 422, nil)
}

func TestACommentNeverReopensADoneTaskAndSendBackDoes(t *testing.T) {
	// Live use, L4 F3 and L2 #8: a comment reopened finished work, and a
	// reviewer had no way to send it back on purpose.
	o, path := taskOrg(t, "edit")
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Write the memo", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "abir", Name: "abir"},
		Owner: &factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: "abir", Name: "abir"}})
	var cur api.WorkstreamTask
	o.do(t, "abir", "PATCH", path+"/tasks/"+made.ID, made.Rev, map[string]any{"state": "done"}, 200, &cur)
	o.do(t, "santosh", "POST", path+"/tasks/"+made.ID+"/comment", -1, map[string]string{"text": "Receipt attached."}, 201, nil)
	var after api.WorkstreamTask
	o.do(t, "abir", "GET", path+"/tasks/"+made.ID, -1, nil, 200, &after)
	if after.State != factory.TaskDone || after.Rev != cur.Rev {
		t.Fatalf("a comment on a done task left it %q rev %d", after.State, after.Rev)
	}
	// santosh, the reviewer, sends it back with a reason; abir sees who and why.
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, after.Rev, map[string]any{"state": "up_next", "state_reason": "section 2 uses last year's numbers"}, 200, &cur)
	if cur.State != factory.TaskUpNext || !strings.HasPrefix(cur.StateReason, "Sent back by ") || !strings.HasSuffix(cur.StateReason, ": section 2 uses last year's numbers") {
		t.Fatalf("sent back %+v", cur)
	}
	o.do(t, "abir", "PATCH", path+"/tasks/"+made.ID, cur.Rev, map[string]any{"owner": factory.TaskOwner{Kind: "person", ID: "santosh", Name: "santosh"}}, 200, &cur)
	if cur.Owner.ID != "santosh" {
		t.Fatalf("owner %+v", cur.Owner)
	}
}

func TestAfisOfferIsTakenWithFiveSecondsToUndo(t *testing.T) {
	o, path := taskOrg(t, "edit")
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	var comment, offer, taken, undone api.WorkstreamTaskEvent
	o.do(t, "abir", "POST", path+"/tasks/"+made.ID+"/comment", -1, map[string]string{"text": "Mention the import fix."}, 201, &comment)
	o.do(t, "santosh", "POST", path+"/tasks/"+made.ID+"/comment", -1, map[string]string{"kind": "offer", "text": "Afi can add this to the draft", "offer_for": comment.ID}, 201, &offer)
	if offer.By.Kind != factory.TaskByAfi || offer.Offer.State != factory.OfferOpen {
		t.Fatalf("offer %+v", offer)
	}
	o.do(t, "abir", "POST", path+"/tasks/"+made.ID+"/comment", -1, map[string]any{"offer": api.TaskOfferSet{Event: offer.ID, State: "taken"}}, 200, &taken)
	if taken.Offer.TakenBy.ID != "abir" || taken.UndoUntil == "" {
		t.Fatalf("taken %+v", taken)
	}
	o.do(t, "santosh", "POST", path+"/tasks/"+made.ID+"/comment", -1, map[string]any{"offer": api.TaskOfferSet{Event: offer.ID, State: "open"}}, 200, &undone)
	if undone.Offer.State != factory.OfferOpen {
		t.Fatalf("undone %+v", undone)
	}
}

func TestMovingATaskTakesItsThreadToTheOtherWorkstream(t *testing.T) {
	o, path := taskOrg(t, "edit")
	other := o.create(t, "santosh", api.WorkstreamNew{Name: "Meal planner"})
	made := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	o.do(t, "santosh", "POST", path+"/tasks/"+made.ID+"/comment", -1, map[string]string{"text": "first"}, 201, nil)
	var moved api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, made.Rev, map[string]any{"move_to": other.ID}, 200, &moved)
	if moved.WS != other.ID {
		t.Fatalf("moved %+v", moved)
	}
	if got := o.tasks(t, "santosh", path); len(got) != 0 {
		t.Fatalf("still here: %+v", got)
	}
	var events api.WorkstreamTaskEvents
	o.do(t, "santosh", "GET", "/v1/workstreams/"+other.ID+"/tasks/"+made.ID+"/events", -1, nil, 200, &events)
	if len(events.Events) != 2 || events.Events[0].Text != "first" || events.Events[1].Kind != factory.TaskEventMoved {
		t.Fatalf("thread %+v", events.Events)
	}
}

// START ALL RUNS A CHAT'S TASKS ON THE SERVER TOO (lane L1, E2 F7), so a
// server that cannot run agents refuses with the reason and keeps nothing:
// every task stays in Up next, never Running with nothing behind it. A
// server that can run them is TestAStartOnTheServerWakesAfiAndOnlyThenSaysRunning's
// Start all.
func TestStartAllOnAChatsTasksWithNoRunnerStartsNothingAndSaysWhy(t *testing.T) {
	o, path := taskOrg(t, "edit")
	g := &factory.TaskGroup{Name: "Release 2.4", Kind: "batch"}
	var first api.WorkstreamTask
	for i := range 5 {
		task := o.task(t, "santosh", path, api.TaskCreate{Title: "Part " + strconv.Itoa(i), DoneBy: factory.TaskDoneBy{Kind: factory.TaskByChat, ChatID: "c1", Name: "Release 2.4"}, Group: g})
		if i == 0 {
			first = task
		}
	}
	o.do(t, "abir", "POST", path+"/tasks/start_all", -1, api.TasksStartAll{Group: first.Group.ID}, 409, nil)
	for _, task := range o.tasks(t, "santosh", path) {
		if task.State != factory.TaskUpNext {
			t.Fatalf("%s is %q after a refused start all", task.Title, task.State)
		}
	}
	o.do(t, "abir", "PATCH", path+"/tasks/"+first.ID, first.Rev, map[string]any{"state": "running"}, 409, nil)
}

func TestAJobInTheWorkstreamIsAFactoryTaskNeverACopy(t *testing.T) {
	o, path := taskOrg(t, "edit")
	it, err := o.st.Create(factory.Item{Repo: "acme/api", Title: "Fix import for recipes without a photo", State: factory.StateRunning, Assignee: "santosh"})
	if err != nil {
		t.Fatal(err)
	}
	o.do(t, "santosh", "PUT", path+"/jobs/"+strconv.Itoa(it.ID), -1, nil, 200, nil)
	got := o.tasks(t, "abir", path)
	if len(got) != 1 || got[0].ID != "job:"+strconv.Itoa(it.ID) || got[0].DoneBy.Kind != factory.TaskByFactory || got[0].State != factory.TaskRunning || got[0].Owner.ID != "santosh" {
		t.Fatalf("tasks %+v", got)
	}
	// Handing it on keeps an overlay; the job itself is not copied or changed.
	var handed api.WorkstreamTask
	o.do(t, "abir", "PATCH", path+"/tasks/"+got[0].ID, got[0].Rev, map[string]any{"owner": factory.TaskOwner{Kind: "person", ID: "abir", Name: "abir"}}, 200, &handed)
	if handed.Owner.ID != "abir" || handed.State != factory.TaskRunning {
		t.Fatalf("handed %+v", handed)
	}
	o.do(t, "abir", "PATCH", path+"/tasks/"+got[0].ID, handed.Rev, map[string]any{"state": "done"}, 409, nil)
	o.do(t, "santosh", "POST", path+"/tasks", -1, api.TaskCreate{Title: "x", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByFactory, Job: it.ID}}, 422, nil)
}

func TestReopeningATaskMakesWhatWaitsOnItWaitAgain(t *testing.T) {
	// Final spec, Tasks › Rules: "Reopening a task makes every task that waits
	// on it wait again." One already running goes back to Up next; each says why.
	o, path := taskOrg(t, "edit")
	o.afiAtWork(t) // Post the notes is running because Afi took it (#2695).
	notes := o.task(t, "santosh", path, api.TaskCreate{Title: "Draft the release notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	post := o.task(t, "santosh", path, api.TaskCreate{Title: "Post the notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}, WaitingOn: []string{notes.ID}})
	var done, running, back, after api.WorkstreamTask
	o.do(t, "santosh", "PATCH", path+"/tasks/"+notes.ID, notes.Rev, map[string]any{"state": "done"}, 200, &done)
	o.do(t, "santosh", "GET", path+"/tasks/"+post.ID, -1, nil, 200, &running)
	o.do(t, "santosh", "PATCH", path+"/tasks/"+post.ID, running.Rev, map[string]any{"state": "running"}, 200, &running)
	o.do(t, "abir", "PATCH", path+"/tasks/"+notes.ID, done.Rev, map[string]any{"state": "up_next", "state_reason": "mention the import fix"}, 200, &back)
	o.do(t, "santosh", "GET", path+"/tasks/"+post.ID, -1, nil, 200, &after)
	if after.State != factory.TaskUpNext || after.Rev != running.Rev+1 {
		t.Fatalf("what waits on the reopened task is %q rev %d, want up next rev %d", after.State, after.Rev, running.Rev+1)
	}
	var thread api.WorkstreamTaskEvents
	o.do(t, "santosh", "GET", path+"/tasks/"+post.ID+"/events", -1, nil, 200, &thread)
	if n := len(thread.Events); n == 0 || thread.Events[n-1].Text != "Waiting again: Draft the release notes was reopened" {
		t.Fatalf("the waiting task's thread: %+v", thread.Events)
	}
}

func TestAStopSaysWhoStoppedIt(t *testing.T) {
	// abir asked for it, so abir owns it and may stop it (only the owner or whoever looks after the workstream stops a task).
	o, path := taskOrg(t, "edit")
	o.afiAtWork(t) // Starting it again wakes Afi (#2695).
	made := o.task(t, "abir", path, api.TaskCreate{Title: "Draft the release notes", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}})
	var stopped, again api.WorkstreamTask
	o.do(t, "abir", "PATCH", path+"/tasks/"+made.ID, made.Rev, map[string]any{"state": "stopped"}, 200, &stopped)
	if stopped.StoppedBy == nil || stopped.StoppedBy.ID != "abir" {
		t.Fatalf("stopped_by %+v, want abir", stopped.StoppedBy)
	}
	var thread api.WorkstreamTaskEvents
	o.do(t, "santosh", "GET", path+"/tasks/"+made.ID+"/events", -1, nil, 200, &thread)
	// The line names the person by the name they joined with, never the handle (E1 F10).
	if n := len(thread.Events); n == 0 || thread.Events[n-1].By.ID != "abir" || thread.Events[n-1].By.Name == "abir" || thread.Events[n-1].Text != "Stopped by "+thread.Events[n-1].By.Name {
		t.Fatalf("the thread: %+v", thread.Events)
	}
	o.do(t, "santosh", "PATCH", path+"/tasks/"+made.ID, stopped.Rev, map[string]any{"state": "running"}, 200, &again)
	if again.StoppedBy != nil {
		t.Fatalf("started again, stopped_by is %+v", again.StoppedBy)
	}
}
