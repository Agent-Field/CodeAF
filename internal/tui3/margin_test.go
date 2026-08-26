package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// THE MARGIN, AS A PERSON MEETS IT (margin.go).
//
// Every test here asserts what is on the column and what a press on it does,
// rather than the shape of the code under it. The fixture is the standing
// page's own scripted session ([standPageFake]) at a frame wide enough for the
// full column, because the two surfaces are two readings of one engine seam and
// a second fake would be a second answer to what stands here.

// marginApp is a surface with orders over it and a frame that lends the full
// column ([railFloor]).
func marginApp(t *testing.T, stand ...standing.Item) (*app, *standPageFake) {
	t.Helper()
	a, agent := standPageApp(t, stand, nil)
	a.width, a.height = 140, 24
	return a, agent
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
	// what stands over it.
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
	// AND THE STANDING SECTION IS ITS LABEL AND ITS DOOR AND NOTHING ELSE while
	// nothing stands: no count of nothing, no line saying the shelf is empty.
	if strings.Contains(rail, standHoldsWord) || strings.Contains(rail, standNothingWord) {
		t.Fatalf("the empty standing section reported on its own emptiness:\n%s", rail)
	}
}

// AN ORDER IS ONE LINE: the glyph every aforge surface agrees on, and what the
// order is called.
func TestAStandingOrderIsOneLineOnTheMargin(t *testing.T) {
	a, _ := marginApp(t, standOrder("p1", "keep the tests green", standing.AltitudeProject))
	rail := marginRail(a)
	if !strings.Contains(rail, "keep the tests green") {
		t.Fatalf("the order is not on the column:\n%s", rail)
	}
	// The row is UNDER the standing label and above the door, which is what makes
	// the label a heading rather than a word floating over the work.
	label := strings.Index(rail, marginStandWord)
	row := strings.Index(rail, "keep the tests green")
	door := strings.Index(rail, marginDoorWord(marginStandType))
	if !(label < row && row < door) {
		t.Fatalf("the order is not filed under its label:\n%s", rail)
	}
}

// WHAT HAS AN OCCASION COMING LEADS, AND WHAT MERELY HOLDS SITS UNDER IT. A rule
// is true and does nothing at all; a reminder is going to happen, and when is the
// question a person glancing at a column has.
func TestTheMarginLeadsWithWhatHasAnOccasionComing(t *testing.T) {
	hold := standOrder("h1", "never touch the public API", standing.AltitudeProject)
	hold.When = standing.When{Kind: standing.WhenHold, Words: "always"}
	hold.Rails = standing.Rails{}
	a, _ := marginApp(t, hold, standOrder("e1", "draft the weekly update", standing.AltitudeProject))
	rail := marginRail(a)
	if strings.Index(rail, "draft the weekly update") > strings.Index(rail, "never touch the public API") {
		t.Fatalf("the rule outranked the appointment:\n%s", rail)
	}
}

// ── 2. the scope tail ───────────────────────────────────────────────────────

// A TAIL ONLY WHERE THE REACH IS NOT THE DEFAULT. An order said in a chat governs
// the project, so a tail saying so on every row would be the one column with no
// room to spare printing what is already true of nearly everything on it.
func TestTheScopeTailSaysOnlyWhatIsNotTheDefault(t *testing.T) {
	a, _ := marginApp(t,
		standOrder("m1", "never touch the API", standing.AltitudeMachine),
		standOrder("c1", "keep the tests green", standing.AltitudeConversation),
		standOrder("p1", "draft the update", standing.AltitudeProject),
	)
	rail := marginRail(a)
	if !strings.Contains(rail, standEverywhereWord) {
		t.Fatalf("a machine-wide order does not say %q:\n%s", standEverywhereWord, rail)
	}
	if !strings.Contains(rail, standJustHereTag) {
		t.Fatalf("a conversation order does not say %q:\n%s", standJustHereTag, rail)
	}
	// The project row is the whole test: it wears NOTHING. Its own line is asked
	// for by itself, because the two words above are on the same column.
	line := plain(a.marginStandRow(StandingItemView{
		Item: standOrder("p1", "draft the update", standing.AltitudeProject)}, a.railRoom()))
	if strings.TrimSpace(line) != "◦ draft the update" {
		t.Fatalf("the default reach printed itself: %q", line)
	}
	// AND THE WORDS ARE THE PAGE'S OWN. One vocabulary for the three reaches, or
	// the column and the page are two surfaces calling one thing two things.
	if standScopeTail(standing.AltitudeProject) != "" {
		t.Fatal("the project reach grew a word")
	}
}

// ── 3. the row that breathes ────────────────────────────────────────────────

// AN ORDER IN A PASS'S HANDS BREATHES, exactly as the `keeping an eye on 2` chip
// on the status row does (homestanding.go's [app.keepingWord]): the glyph is the
// spinner while it is being checked or fired, and a still mark every other
// moment.
func TestAnOrderInAPassesHandsBreathesOnTheMargin(t *testing.T) {
	a, _ := marginApp(t, standOrder("p1", "keep the tests green", standing.AltitudeProject))
	still := plain(a.marginStandRow(a.marginStanding()[0], a.railRoom()))
	if !strings.HasPrefix(still, "◦") {
		t.Fatalf("a waiting order is not still: %q", still)
	}
	a.stands.Running = func(string) (standing.RunningMark, bool) {
		return standing.RunningMark{What: standing.RunningChecking, Since: a.now()}, true
	}
	a.standRailAt = time.Time{}
	moving := plain(a.marginStandRow(a.marginStanding()[0], a.railRoom()))
	if strings.HasPrefix(moving, "◦") || !strings.Contains(moving, "keep the tests green") {
		t.Fatalf("a firing order did not take the spinner: %q", moving)
	}
	// The spinner is the one the whole surface turns on, so two moving things on
	// one frame never beat against each other.
	a.paints += spinnerStep
	if next := plain(a.marginStandRow(a.marginStanding()[0], a.railRoom())); next == moving {
		t.Fatalf("the mark did not move with the frame: %q", next)
	}
}

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

// A STANDING ROW OPENS THE PAGE ON ITSELF, so that the keys that act on an order
// act on the one that was pressed (standingpage.go).
func TestPressingAStandingRowOpensThePageOnThatOrder(t *testing.T) {
	a, _ := marginApp(t,
		standOrder("p1", "draft the update", standing.AltitudeProject),
		standOrder("p2", "keep the tests green", standing.AltitudeProject),
	)
	_, y := marginLine(t, a, func(l railLine) bool { return l.stand == "p2" })
	pressMargin(t, a, y)
	if !a.at(pageStanding) {
		t.Fatal("the row opened no page")
	}
	item, ok := a.standPage.current()
	if !ok || item.ID != "p2" {
		t.Fatalf("the page landed on %+v rather than on the row that was pressed", item)
	}
}

// ── 5. /standing <words> ────────────────────────────────────────────────────

// THE WORDS GO THROUGH THE DELIBERATE DOOR and never through the ordinary send.
// A sentence handed over this way is shaped into a card or refused in one line,
// and it is never carried out as one-off work (internal/session's
// standing_mark.go); falling back to the ordinary send here would be the failure
// the marked door exists to end, arriving through a door that promises the
// opposite.
func TestStandingWithWordsGoesThroughTheMarkedDoor(t *testing.T) {
	a, agent := marginApp(t)
	a.stands.Items = func(string) []standing.Item { return nil }
	typeLine(t, a, "/standing always run the tests before you say you are done")
	if len(agent.marked) != 1 || agent.marked[0] != "always run the tests before you say you are done" {
		t.Fatalf("the words did not reach the marked door: %v", agent.marked)
	}
	if len(agent.sent) != 1 || agent.sent[0] != agent.marked[0] {
		t.Fatalf("the journal did not get the person's own words: %v", agent.sent)
	}
	if a.at(pageStanding) {
		t.Fatal("a sentence opened the page as well as standing")
	}
}

// AND THE DOOR'S OWN GESTURE IS THAT ROAD FROM THE FIRST PRESS. The `+` row
// types `/standing ` and stops, which leaves the half that decides what the
// command DOES to the person — so the whole of what the door promises is only
// kept if the sentence typed after it still reaches the marked door. This is the
// press, the typing and the enter, end to end, because the two halves have been
// asserted apart from each other and a person only ever does them together.
func TestTheStandingDoorsSentenceReachesTheMarkedDoor(t *testing.T) {
	a, agent := marginApp(t)
	a.stands.Items = func(string) []standing.Item { return nil }
	_, y := marginLine(t, a, func(l railLine) bool { return l.door == marginStandType })
	pressMargin(t, a, y)
	typeLine(t, a, "keep the tests green")
	if len(agent.marked) != 1 || agent.marked[0] != "keep the tests green" {
		t.Fatalf("the door's sentence did not reach the marked door: %v", agent.marked)
	}
	if a.input.String() != "" {
		t.Fatalf("the box kept the sentence it sent: %q", a.input.String())
	}
}

// AND THE BARE FORM IS UNCHANGED: it is the page, which is what nearly everybody
// types the word for.
func TestBareStandingStillOpensThePage(t *testing.T) {
	a, agent := marginApp(t, standOrder("p1", "keep the tests green", standing.AltitudeProject))
	typeLine(t, a, "/standing")
	if !a.at(pageStanding) {
		t.Fatal("/standing did not open the page")
	}
	if len(agent.marked) != 0 {
		t.Fatalf("the bare command sent something: %v", agent.marked)
	}
}

// A SURFACE WITH NO AMBIENT SIDE SAYS SO AND SENDS NOTHING, in the same words the
// chord answers with (standmark.go).
func TestStandingWithWordsSaysSoWhereNothingCanHoldOne(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 140, 24
	typeLine(t, a, "/standing always run the tests")
	if !strings.Contains(plain(frame(a)), standMarkNowhere) {
		t.Fatalf("the refusal is not on the frame:\n%s", plain(frame(a)))
	}
}

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
	if msg := cmd(); msg != nil {
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
