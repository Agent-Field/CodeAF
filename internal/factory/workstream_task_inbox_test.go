package factory

import (
	"strings"
	"testing"
)

// The Inbox's verbs on an Ask and on running work (build spec v2, screens 1
// and 2; OWNER-DECISIONS 2026-11-17 §13): a reversible Ask's answer is kept
// so it can be undone from the task, Hand to someone else changes the
// decider, and Take it back makes the owner the doer while what it made stays.

var marcoActs = TaskActor{Handle: "marco", Edit: true}

func TestAReversibleAnswerIsKeptAndUndoneFromItsTask(t *testing.T) {
	task := asking(afiTask("t1", "marco"), taskNow)
	task.Ask.Reversible = true
	task.Ask.Options[0].Decides = "The notes go on the website"
	ch, err := AnswerTask(task, "", "publish", "", marcoActs, byMarco, Standin{}, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.Task.Undoable) != 1 || ch.Task.Undoable[0].Answer != "Publish notes" || len(ch.Decisions) != 1 || ch.Task.Undoable[0].Decision != ch.Decisions[0].ID {
		t.Fatalf("the answer was not kept to undo: %+v %+v", ch.Task.Undoable, ch.Decisions)
	}
	answered := ch.Task
	if _, err := ApplyPatch(answered, TaskPatch{UndoAnswer: "a1"}, TaskActor{Handle: "priya", Edit: true}, byPriya, nil, taskIDs(), taskNow); err == nil {
		t.Fatal("someone who did not answer undid the answer")
	}
	un, err := ApplyPatch(answered, TaskPatch{UndoAnswer: "a1"}, marcoActs, byMarco, nil, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if un.Task.State != TaskNeeds || un.Task.Ask == nil || un.Task.Ask.ID != "a1" || !un.Task.Ask.Reversible || un.Task.Ask.Then != TaskRunning || len(un.Task.Undoable) != 0 {
		t.Fatalf("the Ask did not open again: %s %+v %+v", un.Task.State, un.Task.Ask, un.Task.Undoable)
	}
	if len(un.Withdrawn) != 1 || un.Withdrawn[0] != ch.Decisions[0].ID {
		t.Fatalf("the decision was not withdrawn: %v", un.Withdrawn)
	}
	if len(un.Events) != 1 || !strings.Contains(un.Events[0].Text, "undid the answer “Publish notes”") {
		t.Fatalf("thread = %+v", un.Events)
	}
	if _, err := ApplyPatch(un.Task, TaskPatch{UndoAnswer: "a1"}, marcoActs, byMarco, nil, taskIDs(), taskNow); err == nil {
		t.Fatal("an answer was undone twice")
	}
}

func TestAnAskNotMarkedReversibleCannotBeUndone(t *testing.T) {
	task := asking(afiTask("t1", "marco"), taskNow)
	ch, err := AnswerTask(task, "", "publish", "", marcoActs, byMarco, Standin{}, taskIDs(), taskNow)
	if err != nil || len(ch.Task.Undoable) != 0 {
		t.Fatalf("an irreversible answer was kept to undo: %+v %v", ch.Task.Undoable, err)
	}
	if _, err := ApplyPatch(ch.Task, TaskPatch{UndoAnswer: "a1"}, marcoActs, byMarco, nil, taskIDs(), taskNow); err == nil {
		t.Fatal("an irreversible answer was undone")
	}
}

func TestADoneTaskForgetsItsUndoableAnswers(t *testing.T) {
	task := asking(afiTask("t1", "marco"), taskNow)
	task.Ask.Reversible = true
	ch, _ := AnswerTask(task, "", "publish", "", marcoActs, byMarco, Standin{}, taskIDs(), taskNow)
	done := TaskDone
	fin, err := ApplyPatch(ch.Task, TaskPatch{State: &done}, TaskActor{Handle: "marco", Edit: true}, TaskEventBy{Kind: TaskByAfi, Name: "Afi"}, []WorkstreamTask{ch.Task}, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(fin.Task.Undoable) != 0 {
		t.Fatalf("a done task kept answers to undo: %+v", fin.Task.Undoable)
	}
}

func TestHandToSomeoneElseChangesOnlyTheDecider(t *testing.T) {
	task := asking(afiTask("t1", "marco"), taskNow)
	if _, err := ApplyPatch(task, TaskPatch{AskHand: &TaskAskHand{Ask: "a1", To: PersonRef{ID: "lena", Name: "Lena"}}}, TaskActor{Handle: "priya", Edit: true}, byPriya, nil, taskIDs(), taskNow); err == nil {
		t.Fatal("someone who does not decide the Ask handed it on")
	}
	ch, err := ApplyPatch(task, TaskPatch{AskHand: &TaskAskHand{Ask: "a1", To: PersonRef{ID: "Lena", Name: "Lena"}}}, marcoActs, byMarco, nil, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Task.Ask != nil || len(ch.Task.Asks) != 1 || ch.Task.Asks[0].Decider.ID != "lena" || ch.Task.Owner.ID != "marco" || ch.Task.State != TaskNeeds {
		t.Fatalf("handed = %+v %+v", ch.Task.Ask, ch.Task.Asks)
	}
	if !MayAnswerAsk(ch.Task, "a1", TaskActor{Handle: "lena", Edit: true}, Standin{}, taskNow) || MayAnswerAsk(ch.Task, "a1", marcoActs, Standin{}, taskNow) {
		t.Fatal("the new decider does not answer it alone")
	}
	if _, err := ApplyPatch(ch.Task, TaskPatch{AskHand: &TaskAskHand{Ask: "a1", To: PersonRef{ID: "lena"}}}, TaskActor{Handle: "lena", Edit: true}, byPriya, nil, taskIDs(), taskNow); err == nil {
		t.Fatal("an Ask was handed to the person who already decides it")
	}
}

func TestTakeItBackMakesTheOwnerTheDoerAndKeepsWhatItMade(t *testing.T) {
	task := afiTask("t1", "marco")
	task.State = TaskRunning
	task.Results = []TaskResult{{Kind: "file", FileID: "f1"}}
	if _, err := ApplyPatch(task, TaskPatch{TakeBack: true}, TaskActor{Handle: "priya", Edit: true}, byPriya, nil, taskIDs(), taskNow); err == nil {
		t.Fatal("someone other than the owner took it back")
	}
	ch, err := ApplyPatch(task, TaskPatch{TakeBack: true}, marcoActs, byMarco, []WorkstreamTask{task}, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Task.DoneBy.Kind != TaskByPerson || ch.Task.DoneBy.ID != "marco" || ch.Task.State != TaskUpNext || len(ch.Task.Results) != 1 {
		t.Fatalf("taken back = %+v %s %+v", ch.Task.DoneBy, ch.Task.State, ch.Task.Results)
	}
	if len(ch.Events) != 1 || !strings.Contains(ch.Events[0].Text, "took it back from Afi. What it made stays") {
		t.Fatalf("thread = %+v", ch.Events)
	}
	if _, err := ApplyPatch(ch.Task, TaskPatch{TakeBack: true}, marcoActs, byMarco, nil, taskIDs(), taskNow); err == nil {
		t.Fatal("a task nobody but its owner does was taken back")
	}
}

func TestTakingBackDropsTheAgentsAskAndKeepsAPersons(t *testing.T) {
	task := asking(afiTask("t1", "marco"), taskNow)
	task.Ask.By = &TaskEventBy{Kind: TaskByAfi, Name: "Afi"}
	task.Asks = []TaskAsk{{ID: "a2", Title: "Sign off?", Decider: &PersonRef{ID: "priya"}, By: &byMarco}}
	ch, err := ApplyPatch(task, TaskPatch{TakeBack: true}, marcoActs, byMarco, []WorkstreamTask{task}, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Task.Ask != nil || len(ch.Task.Asks) != 1 || ch.Task.State != TaskNeeds {
		t.Fatalf("asks after take back = %+v %+v %s", ch.Task.Ask, ch.Task.Asks, ch.Task.State)
	}
}
