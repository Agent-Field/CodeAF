package tui3

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── WHAT A PLAN PAGE'S KEYS DO WHEN THERE IS NOTHING LEFT TO STEER ──────────
//
// The key line is the promise: a page names `x` and `p` only while the task
// can still be ended or held ([app.tasksPlanKeyWords]). These hold the keys to
// the same promise, and hold the keys typed before a page opens to the one
// receiver the page claims for them, its note box.

// endPlanPage settles the task on the page that is open, in the store and on
// the surface both, so a re-read on the paint clock reads the same word.
func endPlanPage(a *app, counted *railPlanCounter, status string) {
	id := a.taskSheet.plan.Row.ID
	counted.setStatus(id, status)
	if page, ok := counted.planFake.pages[id]; ok {
		page.Row.Status = status
		counted.planFake.pages[id] = page
	}
	a.taskSheet.plan.Row.Status = status
}

// AN ENDED RUN'S PAGE RAISES NO STOP CARD. `x` asked "Stop this task?" over a
// run that had already finished, and the stop the card then sent changed
// nothing and was reported as though it had. The key line names neither key
// there, so neither key acts: both are letters in the note.
func TestAnEndedRunsPageTakesNeitherTheStopNorTheHold(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	if !a.railTaskPlanOn {
		t.Fatal("the rail row did not open its page")
	}
	endPlanPage(a, counted, "done")
	if foot := a.taskPlanKeys(); strings.Contains(foot, tasksPlanCancelWord) {
		t.Fatalf("the fixture's ended page still offers the stop: %q", foot)
	}
	drive(t, a, key(stopRaiseKey))
	if a.stopping() {
		t.Fatal("x on an ended run's page raised the stop card")
	}
	if len(counted.cancelled) != 0 {
		t.Fatalf("x on an ended run's page asked the store to cancel %v", counted.cancelled)
	}
	if !a.railTaskPlanOn {
		t.Fatal("x on an ended run's page closed the page")
	}
	drive(t, a, key("p"))
	if len(counted.paused)+len(counted.resumed) != 0 {
		t.Fatalf("p on an ended run's page asked the store to hold it: %v %v", counted.paused, counted.resumed)
	}
	if got := a.taskSheet.planNote.String(); got != stopRaiseKey+"p" {
		t.Fatalf("the two letters on an ended page are letters in the note, and the box holds %q", got)
	}
}

// AN ENDED PART'S PAGE ASKS THE STORE NOTHING. `x` sent the store's cancel for
// a part that had finished, and the page drew the store's raw refusal,
// `task "3" is already terminal`, for a key its own foot never offered.
func TestAnEndedPartsPageTakesNeitherTheStopNorTheHold(t *testing.T) {
	a, counted := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	drive(t, a, key("down"))
	drive(t, a, key("enter"))
	if a.taskSheet.plan.Row.ID != "3" {
		t.Fatalf("the part's page did not open: on %q", a.taskSheet.plan.Row.ID)
	}
	endPlanPage(a, counted, "done")
	counted.refuse = errors.New(`task "3" is already terminal`)
	drive(t, a, key(stopRaiseKey))
	drive(t, a, key("p"))
	if len(counted.cancelled)+len(counted.paused)+len(counted.resumed) != 0 {
		t.Fatalf("keys on an ended part's page reached the store: cancel %v pause %v resume %v",
			counted.cancelled, counted.paused, counted.resumed)
	}
	if strings.Contains(a.pageMsg, "terminal") {
		t.Fatalf("an ended part's page drew the store's refusal: %q", a.pageMsg)
	}
	if got := a.taskSheet.planNote.String(); got != stopRaiseKey+"p" {
		t.Fatalf("the box on an ended part's page holds %q, want the two letters", got)
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

// KEYS TYPED WHILE A PAGE IS ON ITS WAY GO TO ITS NOTE BOX AND NOWHERE ELSE.
// The held keys were replayed through the page's whole keyboard, so a note
// that began with the stop key, typed before the page was drawn, cancelled a
// running part with nothing asked. What a person types at a page they cannot
// see yet is a note, and a note typed blind is left in the box, unsent, for
// them to read before they send it.
func TestKeysTypedWhileAPartsPageOpensAreTheNoteAndNothingElse(t *testing.T) {
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

	if !a.railTaskPlanOn || a.taskSheet.plan.Row.ID != "3" {
		t.Fatalf("the answer did not open the part's page: on %v, task %q", a.railTaskPlanOn, a.taskSheet.plan.Row.ID)
	}
	if len(counted.cancelled) != 0 || a.stopping() {
		t.Fatalf("a note typed before the page opened ended the part: cancelled %v, card up %t", counted.cancelled, a.stopping())
	}
	if len(counted.noted) != 0 {
		t.Fatalf("a note typed blind was sent before its page was seen: %v", counted.noted)
	}
	if got := a.taskSheet.planNote.String(); got != typed {
		t.Fatalf("the page's box holds %q, want every key typed while it opened: %q", got, typed)
	}
}

// CTRL+O MEASURES THE BRIEF AT THE WIDTH THE PAGE DRAWS IT. It counted at the
// conversation's body width, which is narrower by the rail, so a brief the
// page drew whole in three rows still toggled a fold nobody could see (#1289).
func TestCtrlOCountsTheBriefAtThePagesOwnWidth(t *testing.T) {
	a, _ := railTaskPageApp(t, true)
	clickRail(t, a, 0)
	if !a.railTaskPlanOn {
		t.Fatal("the rail row did not open its page")
	}
	desc := strings.TrimSpace(strings.Repeat("word ", 110))
	// THE PAGE DRAWS ITS BODY ONE CELL IN FROM EACH EDGE OF THE WHOLE FRAME.
	drawn, narrow := planBriefRows(desc, a.width-2), planBriefRows(desc, a.bodyWidth())
	if len(drawn) > briefFoldLines || len(narrow) <= briefFoldLines {
		t.Fatalf("fixture: the brief wraps to %d rows on the page and %d at the body width; want at most %d and more than %d",
			len(drawn), len(narrow), briefFoldLines, briefFoldLines)
	}
	a.taskSheet.plan.Description = desc
	drive(t, a, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if a.taskSheet.planBriefFull {
		t.Fatal("ctrl+o toggled a fold on a brief the page draws whole")
	}
}

// ── THE NOTE'S RECEIPT ──────────────────────────────────────────────────────

// noteTwoPages is a list with two ordinary tasks, the first one's page open and
// a note typed into its box.
func noteTwoPages(t *testing.T) (*app, *planFake, []session.PlanTaskRow) {
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
	if !a.taskSheet.planOn || a.taskSheet.plan.Row.ID != "t-alpha" {
		t.Fatalf("enter did not open alpha's page: on %v, task %q", a.taskSheet.planOn, a.taskSheet.plan.Row.ID)
	}
	for _, r := range "a note" {
		drive(t, a, key(string(r)))
	}
	return a, fake, rows
}

// ENTER TWICE SENDS A NOTE ONCE. The box was emptied only when the store
// answered, so a second enter pressed before then sent the same words again.
func TestEnterTwiceSendsANoteOnce(t *testing.T) {
	a, fake, _ := noteTwoPages(t)
	_, first := a.Update(key("enter"))
	_, second := a.Update(key("enter"))
	drain(t, a, tea.Batch(first, second))
	if len(fake.noted) != 1 {
		t.Fatalf("two presses of enter wrote %d notes, want one: %v", len(fake.noted), fake.noted)
	}
}

// A NOTE'S RECEIPT LANDS ON ITS OWN PAGE OR NOWHERE. The reply set the open
// page to the page the note was sent from, whichever page a person had moved
// to while the store was answering.
func TestANoteReplyNeverOverwritesAnotherPage(t *testing.T) {
	a, fake, rows := noteTwoPages(t)
	_, sent := a.Update(key("enter"))
	// THE PERSON MOVES ON BEFORE THE STORE ANSWERS.
	a.taskSheet.plan = fake.pages["t-beta"]
	a.taskSheet.planNote.reset()
	a.taskSheet.planNote.insert("for beta")
	drain(t, a, sent)
	if got := a.taskSheet.plan.Row.ID; got != rows[1].ID {
		t.Fatalf("the note's reply put %q's page over the page the person had open", got)
	}
	if got := a.taskSheet.planNote.String(); got != "for beta" {
		t.Fatalf("the note's reply emptied another page's box: it holds %q", got)
	}
	if len(fake.noted) != 1 || fake.noted[0].id != "t-alpha" {
		t.Fatalf("the note went to %v, want alpha once", fake.noted)
	}
}
