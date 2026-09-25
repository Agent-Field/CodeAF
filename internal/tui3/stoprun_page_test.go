package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// answerStopCard takes the card's first answer the way a person does: the digit
// moves the choice onto it and `enter` takes it.
func answerStopCard(t *testing.T, a *app) {
	t.Helper()
	if !a.stopping() {
		t.Fatal("no stop card is up to answer")
	}
	drive(t, a, key("1"))
	drive(t, a, key("enter"))
}

// STOP ON THE RUN'S OWN PAGE IS THE STOP CARD, AND THE CARD STOPS THE RUN. The
// page sent the store's cancel for the run's own task, which the store refuses
// for every caller, so the one key the foot offered for ending a run answered
// with a sentence about who owns what (measured on the real binary 2026-09-19).
// Ending a whole run is the act the card exists to confirm, so the page steps
// aside for it the way a background job's page does, and the card's "stop it"
// goes to the run.
func TestStopOnTheRunsOwnPageRaisesTheCardAndTheCardStopsTheRun(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	if !a.railTaskPlanOn {
		t.Fatal("the rail row did not open its page")
	}
	drive(t, a, key(stopRaiseKey))
	if len(counted.cancelled) != 0 {
		t.Fatalf("one keystroke on the page ended the run with nothing asked: %v", counted.cancelled)
	}
	if !a.stopping() {
		t.Fatal("stop on the run's own page raised no card")
	}
	if a.railTaskPlanOn || a.taskSheet.planOn {
		t.Fatal("the page still takes the frame whole, so the card it raised is drawn nowhere")
	}
	if frame := strings.Join(frameOf(t, a), "\n"); !strings.Contains(frame, "Stop this task?") {
		t.Fatalf("the card is up and the frame does not draw it:\n%s", frame)
	}
	answerStopCard(t, a)
	if len(counted.cancelled) != 1 || counted.cancelled[0] != "2" {
		t.Fatalf("the card's stop reached %v, want the run's own task", counted.cancelled)
	}
	if a.stopping() {
		t.Fatal("the card stayed up after its answer")
	}
}

// AND KEEP GOING STOPS NOTHING.
func TestKeepGoingOnTheRunsCardStopsNothing(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	drive(t, a, key(stopRaiseKey))
	drive(t, a, key("esc"))
	if a.stopping() || len(counted.cancelled) != 0 {
		t.Fatalf("esc on the card left it up (%t) or stopped something (%v)", a.stopping(), counted.cancelled)
	}
}

// A PART'S PAGE KEEPS THE STOP IT HAD: the store takes a cancel on a part, so
// nothing is asked twice for it.
func TestStopOnAPartsPageIsStillTheStoresOwnCancel(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	drive(t, a, key("down"))
	drive(t, a, key("enter"))
	if a.taskSheet.plan.Row.ID != "3" {
		t.Fatalf("the part's page did not open: on %q", a.taskSheet.plan.Row.ID)
	}
	counted.setStatus("3", "running")
	a.taskSheet.plan.Row.Status = "running"
	drive(t, a, key(stopRaiseKey))
	if a.stopping() || len(counted.cancelled) != 1 || counted.cancelled[0] != "3" {
		t.Fatalf("stop on a part's page: card up %t, cancelled %v", a.stopping(), counted.cancelled)
	}
}

// A RUN CANNOT BE HELD, SO ITS PAGE NEVER OFFERS TO. The store holds a part and
// everything under it and refuses the run's own task, so `p pause` under a run
// was an offer that could only be refused. The key is a letter there.
func TestPauseIsNeitherOfferedNorTakenOnTheRunsOwnPage(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	if foot := a.taskPlanKeys(); strings.Contains(foot, tasksPlanPauseWord) || strings.Contains(foot, tasksPlanResumeWord) {
		t.Fatalf("the run's own page offers a hold the store refuses: %q", foot)
	}
	if foot := a.taskPlanKeys(); !strings.Contains(foot, tasksPlanCancelWord) {
		t.Fatalf("the run's own page does not offer its stop: %q", foot)
	}
	drive(t, a, key("p"))
	if len(counted.paused)+len(counted.resumed) != 0 {
		t.Fatalf("p on the run's own page asked the store to hold it: %v %v", counted.paused, counted.resumed)
	}
	if got := a.taskSheet.planNote.String(); got != "p" {
		t.Fatalf("p on the run's own page is a letter in the note, and the box holds %q", got)
	}
}

// THE DOOR IS READ BEFORE A PAGE THAT IS ON ITS WAY, AND BEFORE THE PAGE. The
// hold took every key, this one included, so on an engine that never answered
// the only key that did anything was one nothing on screen named. Leaving is
// never modal.
func TestTheDoorIsReadBeforeAPageThatIsOnItsWayAndBeforeThePage(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	held := &heldRailPlan{railPlanCounter: counted, started: make(chan struct{}), release: make(chan struct{})}
	a.agent = held
	cmd := a.openRailPlan("2", nil)
	answer := make(chan tea.Msg, 1)
	go func() { answer <- cmd() }()
	<-held.started
	quit := a.key(key("ctrl+c"))
	if len(a.railPlanPending.keys) != 0 {
		t.Fatalf("the door was held with the page's keys: %v", a.railPlanPending.keys)
	}
	if quit == nil {
		t.Fatal("ctrl+c while a page was on its way did nothing")
	}
	close(held.release)
	<-answer

	b, _ := railTaskPageApp(t, true)
	clickRail(t, b, 0)
	if !b.railTaskPlanOn {
		t.Fatal("the rail row did not open its page")
	}
	if b.key(key("ctrl+c")) == nil {
		t.Fatal("ctrl+c on an open page did nothing")
	}
}

// EVERY ENDING OF THE READ ENDS THE HOLD. The keys are held for exactly as long
// as the page's read is out, and the read has its own bound: over the wire a
// call gives up after its deadline and answers that there is no page. An answer
// that comes back for a front the person has since left opened nothing and used
// to leave the hold standing, taking every key until `esc`.
func TestAnAnswerForAFrontThePersonLeftEndsTheHold(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	held := &heldRailPlan{railPlanCounter: counted, started: make(chan struct{}), release: make(chan struct{})}
	a.agent = held
	cmd := a.openRailPlan("2", nil)
	answer := make(chan tea.Msg, 1)
	go func() { answer <- cmd() }()
	<-held.started
	a.frontGen++
	close(held.release)
	drive(t, a, <-answer)
	if a.railPlanPending.id != "" {
		t.Fatal("the read is over and the hold still takes every key")
	}
	if a.railTaskPlanOn {
		t.Fatal("an answer for a front the person left opened a page over where they are now")
	}
}

// AND A READ THAT GIVES UP ENDS IT TOO, which is what the wire's own deadline
// answers: no page.
func TestAReadThatGivesUpEndsTheHold(t *testing.T) {
	a, counted := railTaskPageApp(t, false)
	a.agent = counted
	opened := false
	drive(t, a, a.openRailPlan("2", func() tea.Cmd { opened = true; return nil })())
	if a.railPlanPending.id != "" || !opened {
		t.Fatalf("a read that found no page left the hold %q, row's own door taken %t", a.railPlanPending.id, opened)
	}
}

// STOP TYPED WHILE THE RUN'S OWN PAGE OPENS IS A LETTER IN ITS NOTE. The keys
// typed in the gap are the page's note and never its verbs (#1244): a person
// typing at a page they cannot see yet is writing to its box, and a stop raised
// by a key typed blind would be a question about a page nobody has read. The
// stop is one key away once the page is drawn.
func TestStopTypedWhileTheRunsOwnPageOpensIsALetterInItsNote(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	held := &heldRailPlan{railPlanCounter: counted, started: make(chan struct{}), release: make(chan struct{})}
	a.agent = held
	cmd := a.openRailPlan("2", nil)
	answer := make(chan tea.Msg, 1)
	go func() { answer <- cmd() }()
	<-held.started
	drive(t, a, key(stopRaiseKey))
	if a.stopping() {
		t.Fatal("the card was raised over a page that is not drawn yet")
	}
	close(held.release)
	drive(t, a, <-answer)
	if a.stopping() || len(counted.cancelled) != 0 {
		t.Fatalf("stop typed before the run's own page opened acted: card up %t, cancelled %v", a.stopping(), counted.cancelled)
	}
	if !a.railTaskPlanOn {
		t.Fatal("the answer did not open the run's own page")
	}
	if got := a.taskSheet.planNote.String(); got != stopRaiseKey {
		t.Fatalf("the page's box holds %q, want the letter typed while it opened", got)
	}
}
