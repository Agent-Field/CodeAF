package factory

import (
	"errors"
	"strings"
	"testing"
)

var byMarco = TaskEventBy{Kind: TaskByPerson, ID: "marco", Name: "Marco"}

// Approval by several people is several Asks: each answered by its own
// decider, and the task waits until every one is answered.
func TestATaskWaitsOnEveryAskAndEachHasOneDecider(t *testing.T) {
	task := asking(afiTask("tk_1", "marco"), taskNow)
	ch, err := ApplyPatch(task, TaskPatch{AddAsk: &TaskAsk{Title: "Sign off the pricing page?", Decider: &PersonRef{ID: "priya", Name: "Priya"},
		Options: []TaskAskOption{{ID: "yes", Label: "Sign off", Decides: "The pricing page is signed off"}}}}, TaskActor{Handle: "marco", Edit: true}, byMarco, nil, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	task = ch.Task
	if len(OpenAsks(task)) != 2 || task.State != TaskNeeds {
		t.Fatalf("want two open Asks and needs, got %d %s", len(OpenAsks(task)), task.State)
	}
	extra := task.Asks[0].ID
	if _, err := AnswerTask(task, extra, "yes", "", TaskActor{Handle: "marco", Edit: true}, byMarco, Standin{}, taskIDs(), taskNow); err == nil {
		t.Fatal("the task's owner answered an Ask whose decider is Priya")
	}
	ch, err = AnswerTask(task, extra, "yes", "", TaskActor{Handle: "priya", Edit: true}, byPriya, Standin{}, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Task.State != TaskNeeds || ch.Task.Ask == nil {
		t.Fatalf("answering one of two Asks let the task run: %s", ch.Task.State)
	}
	if len(ch.Decisions) != 1 || ch.Decisions[0].Text != "The pricing page is signed off" || ch.Decisions[0].By != "priya" || ch.Decisions[0].From.Kind != DecisionFromAsk {
		t.Fatalf("decision = %+v", ch.Decisions)
	}
	if from := ch.Decisions[0].From; from.Question != "Sign off the pricing page?" || from.Answer != "Sign off" {
		t.Fatalf("question and answer = %+v", from)
	}
	ch, err = AnswerTask(ch.Task, "", "publish", "", TaskActor{Handle: "marco", Edit: true}, byMarco, Standin{}, taskIDs(), taskNow)
	if err != nil || ch.Task.State != TaskRunning {
		t.Fatalf("the last answer did not let it run: %v %s", err, ch.Task.State)
	}
	if got := ch.Decisions[0].Text; got != "Publish the notes to the website? Publish notes" {
		t.Errorf("an answer with no decides reads %q", got)
	}
}

// An answer in words alone is recorded as the answer to the Ask, never as the
// Ask's own line: an agent's Ask titled with the trouble it found must not
// become a decision that the trouble is settled.
func TestAnAnswerInWordsIsKeptAsTheAnswerNotTheAsksLine(t *testing.T) {
	task := asking(afiTask("tk_1", "marco"), taskNow)
	ch, err := ApplyPatch(task, TaskPatch{AddAsk: &TaskAsk{Title: "The release notes pasted were placeholder text, not the notes", Decider: &PersonRef{ID: "priya", Name: "Priya"},
		Options: []TaskAskOption{{ID: "o1", Label: "Paste them again", Decides: "The notes are pasted again"}}}}, TaskActor{Handle: "marco", Edit: true}, byMarco, nil, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	ch, err = AnswerTask(ch.Task, ch.Task.Asks[0].ID, "", "Release notes, 1.0: the control plane is GA. Draft from this.", TaskActor{Handle: "priya", Edit: true}, byPriya, Standin{}, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	want := "Asked “The release notes pasted were placeholder text, not the notes”, answered: Release notes, 1.0: the control plane is GA. Draft from this."
	if len(ch.Decisions) != 1 || ch.Decisions[0].Text != want {
		t.Fatalf("decision = %+v", ch.Decisions)
	}
	if from := ch.Decisions[0].From; from.Answer != "Release notes, 1.0: the control plane is GA. Draft from this." {
		t.Fatalf("answer = %q", from.Answer)
	}
}

// A view is one line per person, replaced, never the decider's own.
func TestOthersAttachOneViewEachAndTheDeciderNone(t *testing.T) {
	task := asking(afiTask("tk_1", "marco"), taskNow)
	lena := TaskActor{Handle: "lena", Edit: true}
	got, err := AddView(task, "", "Fine by me", lena, PersonRef{ID: "lena", Name: "Lena"}, taskNow)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = AddView(got, "", "Fine by me, the placeholder image looks right", lena, PersonRef{ID: "lena", Name: "Lena"}, taskNow)
	if v := got.Ask.Views; len(v) != 1 || !strings.Contains(v[0].Text, "placeholder") {
		t.Fatalf("views = %+v", v)
	}
	if _, err := AddView(got, "", "Yes", TaskActor{Handle: "marco", Edit: true}, PersonRef{ID: "marco"}, taskNow); !errors.Is(err, ErrTaskOwnAsk) {
		t.Fatalf("the decider added a view: %v", err)
	}
	ch, _ := AnswerTask(got, "", "publish", "", TaskActor{Handle: "marco", Edit: true}, byMarco, Standin{}, taskIDs(), taskNow)
	if f := ch.Decisions[0].From; len(f.Views) != 1 || f.Views[0] != "Lena" {
		t.Errorf("the decision does not say whose view was attached: %+v", f)
	}
}

// Only the maker withdraws or replaces; the old one stays, greyed.
func TestOnlyTheMakerWithdrawsOrReplacesADecision(t *testing.T) {
	list := []WorkstreamDecision{newDecision("dc_1", "Recipes import with photos only", PersonRef{ID: "marco", Name: "Marco"}, nil, taskNow)}
	if _, err := WithdrawDecision(list, "dc_1", PersonRef{ID: "priya"}, taskNow); !errors.Is(err, ErrDecisionNotMaker) {
		t.Fatalf("someone else withdrew Marco's decision: %v", err)
	}
	next, fresh, err := ReplaceDecision(list, "dc_1", "Fixes under $5.00 start without asking", "dc_2", PersonRef{ID: "marco", Name: "Marco"}, taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 2 || next[0].State != DecisionSuperseded || next[0].ReplacedBy != "dc_2" || fresh.State != DecisionKept {
		t.Fatalf("replace = %+v", next)
	}
	if got := CurrentDecisions(next); len(got) != 1 || got[0].ID != "dc_2" {
		t.Errorf("work is checked against %+v", got)
	}
	if _, err := WithdrawDecision(next, "dc_1", PersonRef{ID: "marco"}, taskNow); err == nil {
		t.Error("a replaced decision was withdrawn again")
	}
}

// A conflict asks whoever made the decision, recommends keeping it, and
// keeping it records nothing new.
func TestAConflictAsksTheDecisionsMaker(t *testing.T) {
	d := newDecision("dc_1", "No price change in 2.4", PersonRef{ID: "priya", Name: "Priya"}, nil, taskNow)
	task := afiTask("tk_1", "marco")
	task.State = TaskRunning
	ch, err := RaiseConflict(task, d, "It announces $5.99", TaskEventBy{Kind: TaskByAfi, Name: "Afi"}, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Task.State != TaskNeeds || len(ch.Task.Asks) != 1 || ch.Task.Asks[0].Decider.ID != "priya" || !ch.Task.Asks[0].Options[0].Recommended {
		t.Fatalf("conflict = %+v", ch.Task)
	}
	again, _ := RaiseConflict(ch.Task, d, "", TaskEventBy{Kind: TaskByAfi, Name: "Afi"}, taskIDs(), taskNow)
	if len(again.Task.Asks) != 1 {
		t.Error("the same decision asked twice")
	}
	keep, err := AnswerTask(ch.Task, ch.Task.Asks[0].ID, ConflictKeep, "", TaskActor{Handle: "priya", Edit: true}, byPriya, Standin{}, taskIDs(), taskNow)
	if err != nil || len(keep.Decisions) != 0 {
		t.Fatalf("keeping the decision recorded %+v (%v)", keep.Decisions, err)
	}
}

// Assumed lines are kept; overturning one on done work sends it back.
func TestSendBackOverturnsAnAssumption(t *testing.T) {
	task := afiTask("tk_1", "priya")
	task.State = TaskRunning
	ch, err := ApplyPatch(task, TaskPatch{Assumed: []string{"Left out two fixes that only affect the team"}}, TaskActor{Handle: "priya", Edit: true}, TaskEventBy{Kind: TaskByAfi, Name: "Afi"}, nil, taskIDs(), taskNow)
	if err != nil || len(ch.Task.Assumed) != 1 {
		t.Fatalf("assumed = %+v (%v)", ch.Task.Assumed, err)
	}
	done := TaskDone
	ch, _ = ApplyPatch(ch.Task, TaskPatch{State: &done}, TaskActor{Handle: "priya", Edit: true}, TaskEventBy{Kind: TaskByAfi, Name: "Afi"}, nil, taskIDs(), taskNow)
	why := "they matter to support"
	ch, err = ApplyPatch(ch.Task, TaskPatch{Overturn: ch.Task.Assumed[0].ID, StateReason: &why}, TaskActor{Handle: "marco", Edit: true}, byMarco, nil, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Task.State != TaskUpNext || ch.Task.Assumed[0].OverturnedBy == nil || !strings.Contains(ch.Task.StateReason, "instead of “Left out two fixes") {
		t.Fatalf("overturn = %s %q", ch.Task.State, ch.Task.StateReason)
	}
}

// Every rule set in People and rules is a decision, made by the steward.
func TestARuleChangeIsADecision(t *testing.T) {
	before := DefaultRules("priya")
	after := before
	after.StartWithoutAskingUSD = 5
	ds := RuleDecisions(before, after, PersonRef{ID: "marco", Name: "Marco"}, func(string) string { return "" }, taskIDs(), taskNow)
	if len(ds) != 1 || ds[0].Text != "Tasks under $5.00 start without asking" || ds[0].By != "marco" || ds[0].From.Kind != DecisionFromRule {
		t.Fatalf("rule decisions = %+v", ds)
	}
}

// ONE DECISION ASKS ONCE, even when the list already holds copies of it from
// before copies were refused (live2 E2 #5: six Asks to Marco, six "Go ahead
// anyway"). And a copy of a kept decision is not kept again.
func TestCopiesOfOneDecisionAskOnceAndAreKeptOnce(t *testing.T) {
	marco := PersonRef{ID: "marco", Name: "Marco"}
	one := newDecision("dc_1", "No Pantry Plus pricing in 2.4", marco, &DecisionFrom{Kind: DecisionFromRequest}, taskNow)
	two := newDecision("dc_2", "No  Pantry Plus pricing in 2.4", marco, &DecisionFrom{Kind: DecisionFromRequest}, taskNow)
	task := afiTask("tk_1", "priya")
	task.State = TaskRunning
	afi := TaskEventBy{Kind: TaskByAfi, Name: "Afi"}
	ch, _ := RaiseConflict(task, one, "", afi, taskIDs(), taskNow)
	again, _ := RaiseConflict(ch.Task, two, "", afi, taskIDs(), taskNow)
	if len(again.Task.Asks) != 1 {
		t.Fatalf("two copies of one decision asked %d times", len(again.Task.Asks))
	}
	if kept := KeepNewDecisions([]WorkstreamDecision{one}, []WorkstreamDecision{two, two}); len(kept) != 0 {
		t.Fatalf("a copy was kept again: %+v", kept)
	}
	other := newDecision("dc_3", "No Pantry Plus pricing in 2.4", PersonRef{ID: "priya", Name: "Priya"}, &DecisionFrom{Kind: DecisionFromRequest}, taskNow)
	rule := newDecision("dc_4", "Everyone owns incoming work", marco, &DecisionFrom{Kind: DecisionFromRule}, taskNow)
	ruleAgain := rule
	ruleAgain.ID = "dc_5"
	if kept := KeepNewDecisions([]WorkstreamDecision{one, rule}, []WorkstreamDecision{other, ruleAgain}); len(kept) != 2 {
		t.Fatalf("another person's decision, or a rule set again, was dropped: %+v", kept)
	}
}

// An Ask that offers an answer in the person's own words keeps the
// reversible it was given: its plain buttons can still be undone, the words
// answer cannot, and the words answer is never taken without the words.
func TestAWordsAnswerIsNotUndoableAndNeverEmpty(t *testing.T) {
	task := afiTask("tk_1", "marco")
	words := TaskAsk{ID: "ask_more", Title: "Which dates?", Reversible: true, Decider: &PersonRef{ID: "priya", Name: "Priya"},
		Options: []TaskAskOption{{ID: "o1", Label: "These dates", Words: true}, {ID: "o2", Label: "Any"}}}
	// Afi raises it: an answer to a person's own question is undoable for
	// another reason (AskFromPerson).
	byAfi := TaskEventBy{Kind: TaskByAfi, Name: "Afi"}
	ch, err := ApplyPatch(task, TaskPatch{AddAsk: &words}, TaskActor{Handle: "marco", Edit: true}, byAfi, nil, taskIDs(), taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.Task.Asks) != 1 || !ch.Task.Asks[0].Reversible {
		t.Fatalf("an Ask with a words answer lost the reversible it was given: %+v", ch.Task.Asks)
	}
	priya := TaskActor{Handle: "priya", Edit: true}
	if _, err := AnswerTask(ch.Task, "ask_more", "o1", "  ", priya, byPriya, Standin{}, taskIDs(), taskNow); err == nil {
		t.Fatal("a words answer was taken without words")
	}
	given, err := AnswerTask(ch.Task, "ask_more", "o1", "June 3 to 5", priya, byPriya, Standin{}, taskIDs(), taskNow)
	if err != nil {
		t.Fatalf("a words answer with its words was refused: %v", err)
	}
	if len(given.Task.Undoable) != 0 {
		t.Errorf("a words answer can be undone: %+v", given.Task.Undoable)
	}
	plain, err := AnswerTask(ch.Task, "ask_more", "o2", "", priya, byPriya, Standin{}, taskIDs(), taskNow)
	if err != nil {
		t.Fatalf("a plain answer was refused: %v", err)
	}
	if len(plain.Task.Undoable) != 1 || plain.Task.Undoable[0].Option != "o2" {
		t.Errorf("a plain button answer on a reversible Ask cannot be undone: %+v", plain.Task.Undoable)
	}
}
