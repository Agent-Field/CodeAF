package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE MARGIN, AS A PERSON MEETS IT (margin.go).
//
// Every test here asserts what is on the column and what a press on it does,
// rather than the shape of the code under it. The fixture is a real
// automations store ([automationLab]) at a frame wide enough for the full
// column, because the column and the automations place are two readings of one
// store and a second fake would be a second answer to what is on the clock.

// marginApp is a surface with automations made in this conversation and a
// frame that lends the full column ([railFloor]).
func marginApp(t *testing.T, items ...automation.Automation) (*app, *automation.Store) {
	t.Helper()
	a, store := automationLab(t, items...)
	a.width, a.height = 140, 25 // the head's air row costs the frame one row
	return a, store
}

// marginRail is the column as a reader sees it, colour stripped.
func marginRail(a *app) string {
	return plain(strings.Join(a.railRows(a.viewHeight()), "\n"))
}

// marginLineAt finds the drawn line a predicate picks out, and the SCREEN ROW it
// landed on — which is what a press needs and only the layout knows.
func marginLine(t *testing.T, a *app, want func(railLine) bool) (railLine, int) {
	t.Helper()
	view, _ := a.railView(a.viewHeight())
	for i, line := range view {
		if want(line) {
			return line, a.topHeight() + i
		}
	}
	t.Fatalf("no such line on the column:\n%s", marginRail(a))
	return railLine{}, 0
}

// pressMargin clicks one line of the column, past the seam.
func pressMargin(t *testing.T, a *app, y int) {
	t.Helper()
	drive(t, a, tea.MouseClickMsg{X: a.railLeft() + 3, Y: y, Button: tea.MouseLeft})
}

// ── 1. two sections, and the labels that make them a map ────────────────────

// THE EMPTY RAIL EARNS ONLY ITS DOORS. They teach how to put something here;
// labels and absence reports would spend pixels saying that nothing exists.
func TestTheEmptyMarginDrawsOnlyItsDoors(t *testing.T) {
	a, _ := marginApp(t)
	rail := marginRail(a)
	for _, want := range []string{marginDoorWord(marginTaskType), marginDoorWord(marginStandType)} {
		if !strings.Contains(rail, want) {
			t.Fatalf("the empty margin is missing %q:\n%s", want, rail)
		}
	}
	for _, row := range strings.Split(rail, "\n") {
		plainRow := strings.TrimSpace(strings.TrimPrefix(row, "│"))
		if plainRow == marginTasksWord || plainRow == marginStandWord || plainRow == "no tasks yet" {
			t.Fatalf("the empty margin announces %q:\n%s", plainRow, rail)
		}
	}
	// THE SECTIONS ARE IN THIS ORDER AND THE DOOR IS AT THE FOOT OF EACH: the
	// work first, because that is what a person came to the column for, then
	// what the conversation put on the clock.
	order := []string{
		marginDoorWord(marginTaskType),
		marginDoorWord(marginStandType),
	}
	at := 0
	for _, want := range order {
		found := strings.Index(rail[at:], want)
		if found < 0 {
			t.Fatalf("%q is out of order on the column:\n%s", want, rail)
		}
		at += found + len(want)
	}
	// AND THE AUTOMATIONS SECTION IS ITS DOOR AND NOTHING ELSE while nothing
	// is on the clock: no count of nothing, no line saying the shelf is empty.
	if strings.Contains(rail, placeWhisper[pageAutomations].whisper) {
		t.Fatalf("the empty automations section reported on its own emptiness:\n%s", rail)
	}
}

// AN AUTOMATION THIS CONVERSATION SET UP IS ONE LINE ON THE COLUMN, under its
// label, and a press on it opens the automations place on it.
func TestAnAutomationMadeHereIsOneLineThatOpensThePlace(t *testing.T) {
	a, _ := marginApp(t, labWork("weekly update", "0 9 * * 1"), labWatch("ci on main"))
	rail := marginRail(a)
	for _, want := range []string{marginStandWord, "weekly update", "ci on main"} {
		if !strings.Contains(rail, want) {
			t.Fatalf("the column is missing %q:\n%s", want, rail)
		}
	}
	line, y := marginLine(t, a, func(l railLine) bool { return l.stand != "" && strings.Contains(plain(l.text), "ci on main") })
	pressMargin(t, a, y)
	if !a.at(pageAutomations) {
		t.Fatal("a press on an automation's line did not open the automations place")
	}
	if item, ok := a.autoPlace.current(a); !ok || item.ID != line.stand {
		t.Fatalf("the place opened on %+v, want the automation pressed (%s)", item, line.stand)
	}
}

// AN AUTOMATION ANOTHER CONVERSATION MADE IS NOT THIS COLUMN'S: the column is
// what this conversation left behind it.
func TestTheMarginListsOnlyWhatThisConversationMade(t *testing.T) {
	elsewhere := labWork("someone else's", "1h")
	elsewhere.Origin.Transcript = "/elsewhere/transcript.jsonl"
	a, _ := marginApp(t, elsewhere)
	if rail := marginRail(a); strings.Contains(rail, "someone else's") {
		t.Fatalf("the column drew an automation another conversation made:\n%s", rail)
	}
}

// ── 2. the scope tail ───────────────────────────────────────────────────────

// ── 3. the row that breathes ────────────────────────────────────────────────

// ── 4. the doors ────────────────────────────────────────────────────────────

// THE `+` ROW TYPES, IT DOES NOT ARM. What lands in the box is the command
// itself, with its trailing space, as ordinary text a person can edit or delete.
func TestPressingADoorTypesItsCommandIntoTheBox(t *testing.T) {
	for _, want := range []string{marginTaskType, marginStandType} {
		a, _ := marginApp(t)
		line, y := marginLine(t, a, func(l railLine) bool { return l.door == want })
		if line.door != want {
			t.Fatalf("the wrong door: %q", line.door)
		}
		pressMargin(t, a, y)
		if got := a.input.String(); got != want {
			t.Fatalf("pressing %q left the box holding %q", want, got)
		}
		// AND THE KEYBOARD IS THE BOX'S: the caret is at the end of the word, so
		// the next letter a person types is the first letter of their sentence.
		if a.input.cursor != len([]rune(want)) {
			t.Fatalf("the caret is at %d, not after %q", a.input.cursor, want)
		}
		if a.railHold {
			t.Fatal("the column kept the keyboard after handing over the typing")
		}
	}
}

// IT GOES AT THE HEAD OF THE LINE AND KEEPS WHAT WAS THERE. A slash is only a
// command as the first thing on a line, and a person half-way through saying
// what the work is has already said the useful half.
func TestADoorKeepsTheSentenceAlreadyInTheBox(t *testing.T) {
	a, _ := marginApp(t)
	a.input.setText("fix the flaky test")
	_, y := marginLine(t, a, func(l railLine) bool { return l.door == marginTaskType })
	pressMargin(t, a, y)
	if got := a.input.String(); got != "/task fix the flaky test" {
		t.Fatalf("the door threw the sentence away: %q", got)
	}
	// Pressing it twice is pressing it once.
	pressMargin(t, a, y)
	if got := a.input.String(); got != "/task fix the flaky test" {
		t.Fatalf("the door said itself twice: %q", got)
	}
}

// ── 5. /standing <words> ────────────────────────────────────────────────────

// ── 6. the tasks door's own command ─────────────────────────────────────────

// A BARE /task IS THE ROSTER. The `+` row types the word into the box, so the
// word arrives in front of somebody who has not said what the work is yet — and
// sent as it stands it answers the only question it can, which is what work
// there is (taskcommand.go).
func TestABareTaskOpensTheTaskPage(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})})
	// The command is RUN rather than typed: the list is open over a draft reading
	// "/task", and enter there is the list's (commands.go's [app.runMenu] puts the
	// word in the box, which is exactly what the margin's own door does).
	if cmd := a.slash("/task"); cmd != nil {
		if msg := cmd(); msg != nil {
			drive(t, a, msg)
		}
	}
	if !a.at(pageTasks) {
		t.Fatalf("a bare /task opened no page:\n%s", plain(frame(a)))
	}
}

// AND THE BRIEF FORMS ARE UNTOUCHED: they still start the work themselves, with
// no page in the way. The two forms are the pair of errands a person has about
// tasks — set one going, and go and look at the ones that already did.
func TestTheTaskBriefFormsStillStartWork(t *testing.T) {
	f := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(f)
	cmd := a.slash("/task solo write the guard")
	if cmd == nil {
		t.Fatal("/task solo <brief> did nothing")
	}
	// The command is run through the helper that looks past the paint clock's
	// tick, in case anything else live on the surface batched one beside it.
	if msg := taskMsg(cmd); msg != nil {
		_, _ = a.Update(msg)
	}
	if a.at(pageTasks) {
		t.Fatal("a brief opened the page instead of starting work")
	}
	if f.singleCalls != 1 || f.brief != "write the guard" {
		t.Fatalf("single=%d brief=%q", f.singleCalls, f.brief)
	}
}

// ── the emphasis law, on the column's two doors ─────────────────────────────

// A DOOR ANSWERS THE POINTER WITH BOTH HALVES OF THE EMPHASIS LAW: its ground
// comes up a step AND its `+` turns accent. It used to answer with the ground
// alone, so the louder of the two cues was the quieter one — the mark said the
// same thing on every frame and only the background moved.
//
// The words beside the mark stay dim throughout. `+ /task` is a sentence for the
// hand that types chords and the `+` is what the hand that points presses, which
// is the same split the standing column's own door already makes.
func TestAMarginDoorLightsItsMarkUnderThePointer(t *testing.T) {
	a, _ := marginApp(t)
	line, y := marginLine(t, a, func(l railLine) bool { return l.door == marginTaskType })

	if strings.Contains(line.text, a.pal.accent(marginDoorMark)) {
		t.Fatalf("a door nobody is pointing at already lights its mark:\n%q", line.text)
	}

	drive(t, a, tea.MouseMotionMsg{X: a.railLeft() + 3, Y: y})
	if !a.hoveringMarginDoor(marginTaskType) {
		t.Fatalf("the pointer did not land on the door at row %d:\n%s", y, marginRail(a))
	}

	lit, _ := marginLine(t, a, func(l railLine) bool { return l.door == marginTaskType })
	if !strings.Contains(lit.text, a.pal.accent(marginDoorMark)) {
		t.Fatalf("the door under the pointer does not light its mark:\n%q", lit.text)
	}
	// The label is not swept up with it: the accent is one cell, not a run.
	if strings.Contains(lit.text, a.pal.accent(strings.TrimSpace(marginTaskType))) {
		t.Fatalf("the accent spread from the mark onto the words:\n%q", lit.text)
	}
	// AND THE ROW'S GROUND CAME UP WITH IT — the other half of the law, which the
	// column applies in its layout pass (task.go's [app.railRows]).
	var grounded bool
	for _, row := range a.railRows(a.viewHeight()) {
		if strings.Contains(plain(row), strings.TrimSpace(marginTaskType)) && strings.Contains(row, hoverBg()) {
			grounded = true
		}
	}
	if !grounded {
		t.Fatalf("the door under the pointer wears no ground:\n%s", strings.Join(a.railRows(a.viewHeight()), "\n"))
	}
}

// Short task windows still draw the standing door when no orders exist.
// The real fourteen-row terminal reached this through a three-row margin.
func TestEmptyStandingMarginFitsShortTaskWindow(t *testing.T) {
	a, _ := marginApp(t)
	for room := 1; room <= marginStandCost+1; room++ {
		rows := a.marginRows(28, room)
		if len(rows) > room {
			t.Fatalf("margin drew %d rows in %d", len(rows), room)
		}
	}
	a.width, a.height = 120, 14
	a.input.value = []rune(strings.Repeat("a long task request ", 20))
	a.touch()
	_ = frame(a)
}
