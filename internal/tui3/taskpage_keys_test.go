package tui3

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── WHAT A RUN'S TASK'S ROOM DOES WITH ITS KEYS WHEN THERE IS NOTHING LEFT TO
// STEER ─────────────────────────────────────────────────────────────────────
//
// The room offers `x` only while the task can still be ended (planroom.go's
// [app.planRoomStopTarget]). These hold the keys to that promise, and hold the
// keys typed before a room opens to the one receiver the room claims for them,
// its box.

// endPlanTask settles one store task in the store, so the room's read wears it.
func endPlanTask(counted *railPlanCounter, id, status string) {
	counted.setStatus(id, status)
	if page, ok := counted.planFake.pages[id]; ok {
		page.Row.Status = status
		counted.planFake.pages[id] = page
	}
}

// AN ENDED RUN'S ROOM RAISES NO STOP CARD. `x` asked "Stop this task?" over a
// run that had already finished, and the stop the card then sent changed
// nothing and was reported as though it had. Both keys are letters in the box.
func TestAnEndedRunsRoomTakesNeitherTheStopNorTheHold(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	endPlanTask(counted, "2", "done")
	openPlanRoomNow(t, a, "2")
	if a.roomPlan() == nil {
		t.Fatal("the run's room did not open")
	}
	drive(t, a, key(stopRaiseKey))
	if a.stopping() || len(counted.cancelled) != 0 {
		t.Fatalf("x on an ended run's room raised a card (%t) or cancelled %v", a.stopping(), counted.cancelled)
	}
	drive(t, a, key("p"))
	if len(counted.paused)+len(counted.resumed) != 0 {
		t.Fatalf("p on an ended run's room asked the store to hold it: %v %v", counted.paused, counted.resumed)
	}
	if got := string(a.input.value); got != stopRaiseKey+"p" {
		t.Fatalf("the two letters on an ended room are letters in the box, and the box holds %q", got)
	}
}

// AN ENDED PART'S ROOM ASKS THE STORE NOTHING, and draws no refusal for a key
// it never offered.
func TestAnEndedPartsRoomTakesNeitherTheStopNorTheHold(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	openPlanRoomNow(t, a, "3")
	if plan := a.roomPlan(); plan == nil || plan.id != "3" {
		t.Fatal("the part's room did not open")
	}
	counted.refuse = errors.New(`task "3" is already terminal`)
	drive(t, a, key(stopRaiseKey))
	drive(t, a, key("p"))
	if len(counted.cancelled)+len(counted.paused)+len(counted.resumed) != 0 {
		t.Fatalf("keys on an ended part's room reached the store: cancel %v pause %v resume %v",
			counted.cancelled, counted.paused, counted.resumed)
	}
	if strings.Contains(planRoomText(t, a), "terminal") {
		t.Fatalf("an ended part's room drew the store's refusal:\n%s", planRoomText(t, a))
	}
	if got := string(a.input.value); got != stopRaiseKey+"p" {
		t.Fatalf("the box on an ended part's room holds %q, want the two letters", got)
	}
}

// AND AN ENDED ROW IN THE LIST TAKES NEITHER KEY, which is the list's half of
// the same law: its foot names neither ([app.tasksPlanKeyWords]).
func TestAnEndedPlanRowInTheListTakesNeitherKey(t *testing.T) {
	rows := []session.PlanTaskRow{{ID: "t-alpha", Parent: "t-run", Title: "Alpha", Status: "done"}}
	a, fake := planAppWith(t, rows, nil)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	if item, ok := a.taskSheetCurrent(); !ok || item.plan == nil {
		t.Fatalf("the cursor is not on a plan row: %+v", item.entry)
	}
	drive(t, a, key("x"))
	drive(t, a, key("p"))
	if len(fake.cancelled)+len(fake.paused)+len(fake.resumed) != 0 {
		t.Fatalf("keys on an ended row reached the store: cancel %v pause %v resume %v",
			fake.cancelled, fake.paused, fake.resumed)
	}
}

// KEYS TYPED WHILE A ROOM IS ON ITS WAY GO TO ITS BOX AND NOWHERE ELSE. The
// held keys were replayed through the page's whole keyboard, so a note that
// began with the stop key, typed before the page was drawn, cancelled a running
// part with nothing asked. What a person types at a room they cannot see yet is
// a note, and a note typed blind is left in the box, unsent, for them to read
// before they send it.
func TestKeysTypedWhileAPartsRoomOpensAreTheBoxAndNothingElse(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	counted.setStatus("3", "running")
	part := counted.planFake.pages["3"]
	part.Row.Status = "running"
	counted.planFake.pages["3"] = part

	held := &heldRailPlan{railPlanCounter: counted, started: make(chan struct{}), release: make(chan struct{})}
	a.agent = held
	cmd := a.openRailPlan("3", nil)
	answer := make(chan tea.Msg, 1)
	go func() { answer <- cmd() }()
	<-held.started
	const typed = "x-axis labels are wrong"
	for _, r := range typed {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("enter"))
	close(held.release)
	drive(t, a, <-answer)

	if plan := a.roomPlan(); plan == nil || plan.id != "3" {
		t.Fatalf("the answer did not open the part's room: room %v", a.roomOpen())
	}
	if len(counted.cancelled) != 0 || a.stopping() {
		t.Fatalf("a note typed before the room opened ended the part: cancelled %v, card up %t", counted.cancelled, a.stopping())
	}
	if len(counted.noted) != 0 {
		t.Fatalf("a note typed blind was sent before its room was seen: %v", counted.noted)
	}
	if got := string(a.input.value); got != typed {
		t.Fatalf("the room's box holds %q, want every key typed while it opened: %q", got, typed)
	}
}

// ── THE NOTE'S RECEIPT ──────────────────────────────────────────────────────

// noteTwoRooms is a list with two ordinary tasks, the first one's room open
// from the tasks place and a note typed into its box.
func noteTwoRooms(t *testing.T) (*app, *planFake, []session.PlanTaskRow) {
	t.Helper()
	rows := []session.PlanTaskRow{
		{ID: "t-alpha", Parent: "t-run", Title: "Alpha", Status: "claimed"},
		{ID: "t-beta", Parent: "t-run", Title: "Beta", Status: "claimed"},
	}
	pages := map[string]session.PlanTaskPage{
		"t-alpha": {Row: rows[0], Description: "alpha's work order"},
		"t-beta":  {Row: rows[1], Description: "beta's work order"},
	}
	a, fake := planAppWith(t, rows, pages)
	if !openTaskPlaceWithRows(a) {
		t.Fatal("the place refused to open over a plan")
	}
	drive(t, a, key("enter"))
	if plan := a.roomPlan(); plan == nil || plan.id != "t-alpha" || a.at(pageTasks) {
		t.Fatalf("enter did not open alpha's room over the conversation: room %v, tasks %v", a.roomOpen(), a.at(pageTasks))
	}
	for _, r := range "a note" {
		drive(t, a, key(string(r)))
	}
	return a, fake, rows
}

// ENTER TWICE SENDS A NOTE ONCE. The box is emptied the instant the words
// leave, so a second enter has nothing to send.
func TestEnterTwiceSendsANoteOnce(t *testing.T) {
	a, fake, _ := noteTwoRooms(t)
	_, first := a.Update(key("enter"))
	_, second := a.Update(key("enter"))
	drain(t, a, tea.Batch(first, second))
	if len(fake.noted) != 1 {
		t.Fatalf("two presses of enter wrote %d notes, want one: %v", len(fake.noted), fake.noted)
	}
}

// A NOTE'S RECEIPT LANDS ON ITS OWN ROOM OR NOWHERE. A person can move to
// another task's room while the store is answering, and the answer must not
// touch the room they moved to.
func TestANoteReplyNeverOverwritesAnotherRoom(t *testing.T) {
	a, fake, rows := noteTwoRooms(t)
	_, sent := a.Update(key("enter"))
	// THE PERSON MOVES ON BEFORE THE STORE ANSWERS.
	openPlanRoomNow(t, a, "t-beta")
	for _, r := range "for beta" {
		drive(t, a, key(string(r)))
	}
	drain(t, a, sent)
	if plan := a.roomPlan(); plan == nil || plan.id != rows[1].ID {
		t.Fatal("the note's reply moved the person off beta's room")
	}
	if got := string(a.input.value); got != "for beta" {
		t.Fatalf("the note's reply emptied another room's box: it holds %q", got)
	}
	if len(fake.noted) != 1 || fake.noted[0].id != "t-alpha" {
		t.Fatalf("the note went to %v, want alpha once", fake.noted)
	}
}
