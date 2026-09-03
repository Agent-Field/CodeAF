package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE REWIND TIMELINE: the page /rewind opens, the turns it can reach that the
// drawn conversation cannot, the search over them, the two-stage enter, and what
// a commit does.

// rewindLongPast is a conversation LONGER THAN THE DRAWN TAIL: fifty display
// entries against a [replayTail] of forty, so its first turns are in the session
// and not on the screen. That gap is the whole reason this page exists.
func rewindLongPast() []session.DisplayEntry {
	out := []session.DisplayEntry{
		{Role: "user", Text: "the very first thing i asked"},
		{Role: "assistant", Text: "the very first answer"},
	}
	for i := 2; i < 25; i++ {
		out = append(out,
			session.DisplayEntry{Role: "user", Text: "later question " + itoa(i)},
			session.DisplayEntry{Role: "assistant", Text: "later answer " + itoa(i)},
		)
	}
	return out
}

// openTimeline opens the page the way /rewind does and fails if it did not go up.
func openTimeline(t *testing.T, a *app) {
	t.Helper()
	runCmd(a.openRewindSheet())
	if !a.rewSheet.open {
		t.Fatalf("the timeline did not open:\n%s", plain(frame(a)))
	}
}

// timelineRowY is the screen row one item of the page landed on, resolved
// exactly as the pointer resolves it.
func timelineRowY(a *app, item int) (int, bool) {
	_, hits, _, _ := a.rewindSheetFrame(a.width, a.height)
	for y, hit := range hits {
		if hit.kind == rewindSheetHitRow && hit.index == item {
			return y, true
		}
	}
	return 0, false
}

// ── 1. the door ─────────────────────────────────────────────────────────────

// /rewind opens the timeline rather than the inline mode, and the page says what
// it is, what a commit costs, and how to leave.
func TestSlashRewindOpensTheTimeline(t *testing.T) {
	a, _ := newRewindApp(t, rewindPast())
	drive(t, a, key("/"))
	typeLine(t, a, "rewind")

	if !a.rewSheet.open {
		t.Fatalf("/rewind did not open the timeline:\n%s", plain(frame(a)))
	}
	if a.rew.on {
		t.Fatal("/rewind opened the inline mode as well as the page")
	}
	got := plain(frame(a))
	for _, want := range []string{
		"⟲ " + rewindSheetTitleWord,
		rewindSheetCloseWord,
		"⟲ drops 1 turn" + rewindSheetDropTail,
		rewindSheetKeys,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the timeline is missing %q:\n%s", want, got)
		}
	}
	// IT OPENS ON THE LAST THING THE PERSON SAID, which is the inline mode's own
	// opening position, and the preview says so in full.
	if !strings.Contains(got, "three") {
		t.Fatalf("the preview does not show the pick:\n%s", got)
	}
}

// The page is built from the SESSION and not from the screen: a conversation
// whose early turns were never drawn still has them on this list, and the cursor
// can walk up to them.
func TestTheTimelineReachesTurnsThePageNeverDrew(t *testing.T) {
	a, _ := newRewindApp(t, rewindLongPast())

	drawn := plain(strings.Join(textsOf(rowsOfBody(a)), "\n"))
	if strings.Contains(drawn, "the very first thing i asked") {
		t.Fatal("the drawn conversation already holds the first turn — this test proves nothing")
	}

	openTimeline(t, a)
	found := false
	for _, row := range a.rewSheet.rows {
		if strings.Contains(row.text, "the very first thing i asked") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the timeline is missing the turns the tail dropped (%d rows)", len(a.rewSheet.rows))
	}

	// And it is reachable: the cursor walks to the oldest point and the frame
	// shows it.
	for i := 0; i < len(a.rewSheet.rows)+2; i++ {
		drive(t, a, key("up"))
	}
	got := plain(frame(a))
	if !strings.Contains(got, "the very first thing i asked") {
		t.Fatalf("↑ never reached the oldest turn:\n%s", got)
	}
	if !strings.Contains(got, "⟲ drops 24 turns") {
		t.Fatalf("the foot does not count the whole conversation:\n%s", got)
	}
}

// A session with nothing to cut says so in the hint slot and opens nothing.
func TestTheTimelineDoesNotOpenOverAnEmptyConversation(t *testing.T) {
	a, _ := newRewindApp(t, nil)
	runCmd(a.openRewindSheet())
	if a.rewSheet.open {
		t.Fatal("the timeline opened over a conversation with nothing in it")
	}
	if got := a.hintWord(); got != rewindEmptyWord {
		t.Fatalf("hint slot = %q, want %q", got, rewindEmptyWord)
	}
}

// The refusals are the inline mode's: a surface where something else owns the
// frame does not gain a fourth page over the top of it.
func TestTheTimelineRefusesWhereTheInlineModeDoes(t *testing.T) {
	for _, tc := range []struct {
		name string
		hold func(*app)
	}{
		{"copy mode", func(a *app) { a.copy.on = true }},
		{"the settings panel", func(a *app) { a.raisePlace(pageSettings) }},
		{"the inline rewind", func(a *app) { runCmd(a.enterRewind()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newRewindApp(t, rewindPast())
			tc.hold(a)
			runCmd(a.openRewindSheet())
			if a.rewSheet.open {
				t.Fatalf("the timeline opened over %s", tc.name)
			}
		})
	}
}

// ── 2. the lift ─────────────────────────────────────────────────────────────

// tab inside the inline mode opens the same conversation as the whole list, with
// the cut already chosen carried across — and the legend says the key is there.
func TestTabLiftsTheInlineModeIntoTheTimeline(t *testing.T) {
	a, _ := newRewindApp(t, rewindPast())
	a.input.setText("half a sentence")
	runCmd(a.enterRewind())

	if got := plain(frame(a)); !strings.Contains(got, rewindSheetLiftWord) {
		t.Fatalf("the mode bar does not name the door onto the timeline:\n%s", got)
	}

	drive(t, a, key("up")) // the cut moves to "two"
	drive(t, a, key("tab"))
	if a.rew.on {
		t.Fatal("the lift left the inline mode up")
	}
	if !a.rewSheet.open {
		t.Fatalf("tab did not lift into the timeline:\n%s", plain(frame(a)))
	}
	point, ok := a.rewindSheetPoint()
	if !ok || point.Said != "two" {
		t.Fatalf("the lift landed on %+v, want the cut the inline mode had", point)
	}
	// The stash travelled with it: esc out of the page puts the sentence back
	// exactly as esc out of the mode would have.
	drive(t, a, key("esc"))
	if got := a.input.String(); got != "half a sentence" {
		t.Fatalf("the draft came back as %q", got)
	}
}

// ── 3. the search ───────────────────────────────────────────────────────────

// Typing narrows the list to the rows that hold the words, says what was typed,
// and leaves enter meaning what it always meant.
func TestTheSearchNarrowsTheTimelineAndEscClearsItFirst(t *testing.T) {
	a, _ := newRewindApp(t, rewindLongPast())
	openTimeline(t, a)
	before := len(a.rewindSheetItems())

	for _, r := range "very first" {
		drive(t, a, key(string(r)))
	}
	rows := a.rewindSheetItems()
	if len(rows) >= before || len(rows) == 0 {
		t.Fatalf("the search kept %d of %d rows", len(rows), before)
	}
	got := plain(frame(a))
	if !strings.Contains(got, rewindSheetSearchWord+"very first") {
		t.Fatalf("the foot does not say what was typed:\n%s", got)
	}
	if !strings.Contains(got, rewindSheetSearchKeys) {
		t.Fatalf("the foot does not say what esc now does:\n%s", got)
	}

	// A filtered enter places the pick on that row's own point.
	drive(t, a, key("enter"))
	point, ok := a.rewindSheetPoint()
	if !ok || point.Said != "the very first thing i asked" {
		t.Fatalf("the filtered enter placed the pick at %+v", point)
	}

	// esc BACKS OUT ONE LAYER AT A TIME: the search first, the page second.
	drive(t, a, key("esc"))
	if !a.rewSheet.open {
		t.Fatal("esc closed the page from inside a search")
	}
	if a.rewindSheetSearching() {
		t.Fatal("esc did not clear the search")
	}
	drive(t, a, key("esc"))
	if a.rewSheet.open {
		t.Fatal("a second esc did not close the page")
	}
}

// A search that matches nothing says so rather than leaving a blank page to be
// read as a page that broke.
func TestASearchThatMatchesNothingSaysSo(t *testing.T) {
	a, _ := newRewindApp(t, rewindLongPast())
	openTimeline(t, a)
	for _, r := range "zzz" {
		drive(t, a, key(string(r)))
	}
	if got := plain(frame(a)); !strings.Contains(got, rewindSheetSearchNone) {
		t.Fatalf("an empty search says nothing:\n%s", got)
	}
}

// ── 4. the pick, the wash, and the two-stage enter ──────────────────────────

// The wash follows the cursor: everything from the pick down is drawn dim,
// because the true statement about all of it is that it goes.
func TestTheWashShowsWhatTheCommitLetsGo(t *testing.T) {
	a, _ := newRewindApp(t, rewindPast())
	openTimeline(t, a)
	drive(t, a, key("up")) // onto the reply above the last turn

	rows := a.rewindSheetItems()
	point, ok := a.rewindSheetPoint()
	if !ok {
		t.Fatal("the page has no pick")
	}
	ink := paintPrefix(a.pal.ink("x"))
	for at, row := range rows {
		if at == a.rewSheet.cursor {
			continue // the cursor's band paints over the wash
		}
		text := a.rewindSheetRowText(row, at, a.width)
		dropped := row.entry >= point.Entry
		if dropped && strings.Contains(text, ink) {
			t.Fatalf("a dropped row kept its paint: %q", text)
		}
		if !dropped && row.kind == rewindRowTurn && !strings.Contains(text, ink) {
			t.Fatalf("the wash reached above the pick: %q", text)
		}
	}
}

// The first enter PLACES the pick and the page says the next one cuts; the
// second enter on the same point cuts, rebuilds the conversation, says so once,
// and puts the message back in the box.
func TestTheFirstEnterPlacesAndTheSecondRewinds(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	openTimeline(t, a)
	drive(t, a, key("up"), key("up")) // onto the turn "two"

	if got := plain(frame(a)); !strings.Contains(got, "⟲ drops 2 turns") {
		t.Fatalf("the foot does not count what the pick takes:\n%s", got)
	}

	drive(t, a, key("enter"))
	if !a.rewSheet.open {
		t.Fatal("the first enter cut the conversation")
	}
	if !a.rewSheet.armed {
		t.Fatal("the first enter did not place the pick")
	}
	if len(agent.cuts) != 0 {
		t.Fatalf("the first enter reached the engine: %v", agent.cuts)
	}
	if got := plain(frame(a)); !strings.Contains(got, rewindSheetPickedKeys) {
		t.Fatalf("the foot does not say what the next enter does:\n%s", got)
	}

	drive(t, a, key("enter"))
	if a.rewSheet.open {
		t.Fatal("the second enter left the page up")
	}
	if len(agent.cuts) != 1 || agent.cuts[0] != 2 {
		t.Fatalf("the engine was cut at %v, want the placed point", agent.cuts)
	}
	body := plain(strings.Join(textsOf(rowsOfBody(a)), "\n"))
	for _, gone := range []string{"two", "second answer", "three", "third answer"} {
		if strings.Contains(body, gone) {
			t.Fatalf("the rewound turn is still drawn (%q):\n%s", gone, body)
		}
	}
	if !strings.Contains(body, "⟲ rewound · 2 turns") {
		t.Fatalf("the conversation does not say what happened:\n%s", body)
	}
	if a.input.String() != "two" {
		t.Fatalf("the draft is %q, want the message the cut took back", a.input.String())
	}
}

// Moving the cursor takes the arming away: a destructive key never waits under a
// row somebody has walked off.
func TestMovingTheCursorTakesThePlacementBack(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	openTimeline(t, a)
	drive(t, a, key("enter"))
	if !a.rewSheet.armed {
		t.Fatal("the first enter did not place the pick")
	}
	drive(t, a, key("up"))
	if a.rewSheet.armed {
		t.Fatal("the placement outlived the row it was made on")
	}
	drive(t, a, key("enter"))
	if len(agent.cuts) != 0 || !a.rewSheet.open {
		t.Fatalf("an enter after a move cut the conversation: %v", agent.cuts)
	}
}

// The engine's refusal is shown in the engine's own words, the page stays up,
// and the same key lands the cut once the engine relents.
func TestARefusalKeepsTheTimelineUpAndSaysWhy(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	agent.err = session.ErrTurnInFlight
	openTimeline(t, a)

	drive(t, a, key("enter"), key("enter"))
	if !a.rewSheet.open {
		t.Fatal("a refused cut closed the page")
	}
	if got := plain(frame(a)); !strings.Contains(got, session.ErrTurnInFlight.Error()) {
		t.Fatalf("the foot does not carry the engine's sentence:\n%s", got)
	}
	agent.err = nil
	drive(t, a, key("enter"))
	if a.rewSheet.open || len(agent.cuts) != 1 {
		t.Fatalf("the retried cut did not land (open=%v, cuts=%v)", a.rewSheet.open, agent.cuts)
	}
}

// ── 5. the pointer ──────────────────────────────────────────────────────────

// A click places the pick, and a click on the point already placed cuts — the
// pointer does everything the keys do and nothing they do not.
func TestAClickPlacesAndAClickOnThePickRewinds(t *testing.T) {
	a, agent := newRewindApp(t, rewindPast())
	openTimeline(t, a)

	// The first turn's row, whichever item it is.
	item := -1
	for i, row := range a.rewSheet.rows {
		if row.pick() && strings.Contains(row.text, "one") {
			item = i
			break
		}
	}
	if item < 0 {
		t.Fatal("the first turn has no pickable row")
	}
	y, ok := timelineRowY(a, item)
	if !ok {
		t.Fatalf("the first turn is not on the frame:\n%s", plain(frame(a)))
	}
	a.rewindSheetPress(y)
	if !a.rewSheet.open || len(agent.cuts) != 0 {
		t.Fatalf("the first click cut the conversation: %v", agent.cuts)
	}
	if point, ok := a.rewindSheetPoint(); !ok || point.Said != "one" {
		t.Fatalf("the click placed the pick at %+v", point)
	}
	// And hovering the row is what says it can be pressed.
	a.rewindSheetHover(y)
	if a.hot.kind != hoverRewindSheet || a.hot.index != item {
		t.Fatalf("hover over a row is %+v, want the row", a.hot)
	}

	a.rewindSheetPress(y)
	if a.rewSheet.open {
		t.Fatal("a second click on the placed pick did not cut")
	}
	if len(agent.cuts) != 1 || agent.cuts[0] != 0 {
		t.Fatalf("the engine was cut at %v, want the placed point", agent.cuts)
	}
}

// ── 6. what a turn cost ─────────────────────────────────────────────────────

// A turn's dim tally counts the calls it made and the files it wrote, and a turn
// that did neither says nothing at all.
func TestATurnSaysWhatItCalledAndWhatItWrote(t *testing.T) {
	past := []session.DisplayEntry{
		{Role: "user", Text: "change the parser"},
		{Role: "tool", Tool: "read", Hint: "read internal/parse/lex.go"},
		{Role: "tool", Tool: "edit", Hint: "edit internal/parse/lex.go"},
		{Role: "tool", Tool: "write", Hint: "write internal/parse/doc.go"},
		{Role: "assistant", Text: "done"},
		{Role: "user", Text: "thanks"},
		{Role: "assistant", Text: "any time"},
	}
	a, _ := newRewindApp(t, past)
	openTimeline(t, a)

	notes := map[string]string{}
	for _, row := range a.rewSheet.rows {
		if row.kind == rewindRowTurn {
			notes[row.text] = row.note
		}
	}
	// THE WORD FOR A TOOL CALL IS SPELLED ONCE, in [toolCallWord], and read
	// from there rather than copied — this line held a THIRD spelling (`3
	// tools` against the card's `3 tool calls`) and a literal here is exactly
	// how a fourth would arrive.
	want := toolCallWord(3) + " · 2 files"
	if got := notes["change the parser"]; got != want {
		t.Fatalf("the turn's tally is %q, want %q", got, want)
	}
	// THE EMPTINESS LAW: a turn that called nothing says nothing, not "0 tools".
	if got := notes["thanks"]; got != "" {
		t.Fatalf("a turn with nothing to report says %q", got)
	}
	// And a call is drawn as the bracketed one-liner, with the tool named once.
	if got := plain(frame(a)); !strings.Contains(got, "[edit: internal/parse/lex.go]") {
		t.Fatalf("the call is not drawn as its own row:\n%s", got)
	}
}

// rowsOfBody is the conversation as the frame lays it out, for the assertions
// above.
func rowsOfBody(a *app) []row {
	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	return body
}
