package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// THE STANDING ORDERS PAGE, AS A PERSON MEETS IT.
//
// Every test below asserts the fact the page exists for — what is on the
// screen, which shelf a row is filed under, and what reaches the engine when a
// key is pressed — rather than the shape of the code under it
// (standingpage.go).

// standingPlaceFake is a session that can be asked what stands over it. It embeds
// the scripted agent every other test here runs against, because the standing
// seam is a widening of that session and not a different one, and it MOVES ITS
// OWN ROWS the way the engine's resolver would: an order excepted from here
// stops standing here and starts being a "not here" line, and one stood down
// stops existing at all. A fake that answered the same three rows after every
// write would let a page that never re-read the engine pass.
type standingPlaceFake struct {
	*fakeAgent
	stand    []standing.Item
	excepted []standing.Item
	// what the page asked for, in the order it asked.
	paused, downed, excepts []string
	// status is what a pause answers with, and refusal is what every verb
	// answers with when it is set — the stub's own state (internal/session's
	// standing_orders.go), which is what this page is built against.
	status  standing.Status
	refusal error
}

func (f *standingPlaceFake) StandingHere() ([]standing.Item, []standing.Item) {
	return f.stand, f.excepted
}

func (f *standingPlaceFake) StandingExcept(id string) error {
	if f.refusal != nil {
		return f.refusal
	}
	f.excepts = append(f.excepts, id)
	if item, ok := f.take(id); ok {
		f.excepted = append(f.excepted, item)
	}
	return nil
}

func (f *standingPlaceFake) StandingStandDown(id string) error {
	if f.refusal != nil {
		return f.refusal
	}
	f.downed = append(f.downed, id)
	f.take(id)
	return nil
}

func (f *standingPlaceFake) StandingPause(id string) (standing.Status, error) {
	if f.refusal != nil {
		return "", f.refusal
	}
	f.paused = append(f.paused, id)
	for i, item := range f.stand {
		if item.ID != id {
			continue
		}
		f.stand[i].Status = f.status
	}
	return f.status, nil
}

// take removes one order from the shelves and answers it.
func (f *standingPlaceFake) take(id string) (standing.Item, bool) {
	for i, item := range f.stand {
		if item.ID != id {
			continue
		}
		f.stand = append(f.stand[:i:i], f.stand[i+1:]...)
		return item, true
	}
	return standing.Item{}, false
}

// standOrder is one plausible standing order at one reach.
func standOrder(id, title string, level standing.Altitude) standing.Item {
	return standing.Item{
		ID:        id,
		Words:     title + ", every time, without me asking",
		Brief:     standing.Brief{Title: title},
		Workspace: "/tmp/lab",
		Altitude:  level,
		When:      standing.When{Kind: standing.WhenEvery, Words: "Mondays at 9am", Every: "168h"},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "said"},
		Rails:     standing.Rails{PerRunUSD: 0.05, MaxPerDay: 1},
		Status:    standing.StatusActive,
	}
}

// standingPlaceApp is a surface with an ambient side under it, on a pinned clock —
// a status clause counted in minutes cannot be tested by waiting.
func standingPlaceApp(t *testing.T, stand, excepted []standing.Item) (*app, *standingPlaceFake) {
	t.Helper()
	agent := &standingPlaceFake{
		fakeAgent: &fakeAgent{model: "m"},
		stand:     stand, excepted: excepted,
		status: standing.StatusPaused,
	}
	a := newTestApp(agent)
	a.width, a.height = 100, 24
	now := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	return a, agent
}

// standingPlaceScreen is the open page as a reader sees it, blank lines dropped —
// the block is padded to the height the frame reserved, and the padding is not
// something a person reads.
// standingPlaceScreen is what a person sees on the standing PLACE. It reads the
// frame rather than the overlay rows: the list was an overlay under the draft
// until the router promoted it, and it takes the whole terminal now
// (placebodies.go).
func standingPlaceScreen(a *app) string {
	out := make([]string, 0, standRowsMax)
	width, height := a.size()
	lines, _, _, _ := a.standingPlaceFrame(width, height)
	for _, line := range lines {
		line = plain(line)
		if strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimRight(line, " "))
		}
	}
	return strings.Join(out, "\n")
}

// ── 1. the command ──────────────────────────────────────────────────────────

// /standing IS ON THE LIST AND IN /help, which is one table read twice
// (commands.go), and the word people bring with them reaches the same row.
func TestStandingIsOnTheCommandListAndInHelp(t *testing.T) {
	found := false
	for _, c := range commands {
		found = found || c.name == "standing"
	}
	if !found {
		t.Fatal("/standing is not on the command list")
	}
	help := helpText("")
	for _, want := range []string{"/standing", "also /orders", "in /standing"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help is missing %q:\n%s", want, help)
		}
	}
	if got := canonicalCommand("orders"); got != "standing" {
		t.Fatalf("/orders ran as /%s", got)
	}
	if err := checkCommands(commands); err != nil {
		t.Fatalf("the table stopped being a table: %v", err)
	}
	// POSITION IN THE TABLE IS A CLAIM ABOUT FREQUENCY, and [menuRows] is what
	// makes it cost something: a row added above /compact would push a command
	// people reach for daily into a scroll.
	for index, c := range commands {
		if c.name == "compact" {
			if index >= menuRows {
				t.Fatalf("/compact is row %d of the first %d — it is now behind a scroll", index+1, menuRows)
			}
			break
		}
	}
}

// ── 2. what the page draws ──────────────────────────────────────────────────

// THREE SHELVES, IN THE ORDER A PERSON READS OUTWARD FROM WHERE THEY STAND, and
// every row is a mark, what the order is called, and where it stands.
func TestTheStandingPageDrawsThreeShelvesOutward(t *testing.T) {
	a, _ := standingPlaceApp(t, []standing.Item{
		standOrder("m1", "never touch the public API", standing.AltitudeMachine),
		standOrder("c1", "keep the tests green", standing.AltitudeConversation),
		standOrder("p1", "draft the weekly update", standing.AltitudeProject),
	}, nil)
	typeLine(t, a, "/standing")
	if !a.at(pageStanding) {
		t.Fatal("/standing opened nothing")
	}
	screen := standingPlaceScreen(a)
	for _, want := range []string{
		standHeading,
		standInHereWord,
		standWaitGlyph + " keep the tests green",
		standProjectWord,
		standWaitGlyph + " draft the weekly update",
		standEverywhereWord,
		standWaitGlyph + " never touch the public API",
		// The clause is home's own, drawn from the item's own cadence
		// ([standRollup]) and not a second derivation.
		"Mondays at 9am",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the page does not say %q:\n%s", want, screen)
		}
	}
	here, project, everywhere := strings.Index(screen, standInHereWord),
		strings.Index(screen, standProjectWord), strings.Index(screen, standEverywhereWord)
	if !(here < project && project < everywhere) {
		t.Fatalf("the shelves are out of order:\n%s", screen)
	}
	// AND NOTHING ON IT SPEAKS THE MACHINERY'S OWN WORDS.
	for _, banned := range []string{"altitude", "scope", "machine", "conversation altitude"} {
		if strings.Contains(strings.ToLower(screen), banned) {
			t.Fatalf("the page says the machinery's word %q:\n%s", banned, screen)
		}
	}
}

// AN EMPTY SHELF DRAWS NOTHING AT ALL — not the heading, not a line saying it is
// empty. It is the emptiness law at its most literal, and it is what keeps this
// page one line long on the ordinary conversation.
func TestAnEmptyShelfDrawsNothing(t *testing.T) {
	a, _ := standingPlaceApp(t, []standing.Item{
		standOrder("p1", "draft the weekly update", standing.AltitudeProject),
	}, nil)
	typeLine(t, a, "/standing")
	screen := standingPlaceScreen(a)
	if !strings.Contains(screen, standProjectWord) {
		t.Fatalf("the one shelf with something on it is missing:\n%s", screen)
	}
	for _, gone := range []string{standInHereWord, standEverywhereWord} {
		if strings.Contains(screen, gone) {
			t.Fatalf("an empty shelf drew its heading %q:\n%s", gone, screen)
		}
	}
}

// A PLACE AN ORDER DOES NOT REACH IS ONE DIM LINE UNDER THE SHELF IT WOULD HAVE
// BEEN ON, and it is not a row: no mark about what it is doing, no clause about
// where it stands, and no cursor.
func TestAnExceptedOrderIsOneLineUnderItsShelf(t *testing.T) {
	a, _ := standingPlaceApp(t,
		[]standing.Item{standOrder("p1", "draft the weekly update", standing.AltitudeProject)},
		[]standing.Item{standOrder("m1", "never touch the public API", standing.AltitudeMachine)})
	typeLine(t, a, "/standing")
	screen := standingPlaceScreen(a)
	want := standNotHereGlyph + " " + standNotHereWord + ": never touch the public API"
	if !strings.Contains(screen, want) {
		t.Fatalf("the page does not say %q:\n%s", want, screen)
	}
	// The excepted order brought its own shelf with it — the heading is what
	// says which place it is not reaching.
	if !strings.Contains(screen, standEverywhereWord) {
		t.Fatalf("the excepted order has no shelf over it:\n%s", screen)
	}
	// AND THE CURSOR CANNOT REST ON IT. There is one order on this page and the
	// cursor is on it, whatever else is drawn.
	item, ok := a.orders.current()
	if !ok || item.ID != "p1" {
		t.Fatalf("the cursor is on %v (%v), not on the one order there is", item.ID, ok)
	}
	a.orders.move(1)
	if item, _ := a.orders.current(); item.ID != "p1" {
		t.Fatalf("the cursor walked onto something that is not an order: %v", item.ID)
	}
}

// NOTHING STANDS, SO THE PAGE OPENS AND SAYS WHAT STANDING ORDERS ARE.
//
// THIS TEST USED TO PIN THE OPPOSITE — one sentence in the conversation and no
// overlay to dismiss before it could be told it was useless. The sentence stayed
// ([standNothingWord]) and moved into the place's own body, because a machine
// nothing stands on is every machine for its first week, and refusing there made
// `alt+3` a key that did nothing at all.
func TestStandingOnNothingOpensThePageAndTeachesIt(t *testing.T) {
	a, _ := standingPlaceApp(t, nil, nil)
	typeLine(t, a, "/standing")
	if !a.at(pageStanding) {
		t.Fatal("a conversation nothing stands over opened no page")
	}
	if text := standingPlaceScreen(a); !strings.Contains(text, standNothingWord) {
		t.Fatalf("the empty place does not say what it is for:\n%s", text)
	}
}

// AND ASKING FOUR TIMES IS FOUR OPENINGS OF ONE PAGE, with nothing stacked in
// the transcript behind it. The repetition this used to guard against was a
// repetition of the REFUSAL; there is no refusal, so there is nothing to repeat.
func TestStandingOnNothingWritesNothingIntoTheTranscript(t *testing.T) {
	a, _ := standingPlaceApp(t, nil, nil)
	for range 4 {
		typeLine(t, a, "/standing")
	}
	if n := notesSaying(a, standNothingWord); n != 0 {
		t.Fatalf("the place's own body was written into the transcript %d times:\n%s",
			n, strings.Join(plainRows(a), "\n"))
	}
	if !a.at(pageStanding) {
		t.Fatal("asking four times left no page open")
	}
}

// A SURFACE WHOSE SESSION HAS NO AMBIENT SIDE OPENS THE SAME PAGE. The seam is
// absent rather than broken — which is what the whole codebase does with a
// capability that cannot work — and what a place does with an absent seam is
// spend the frame saying what would be here if it were not.
func TestASessionWithNoAmbientSideStillOpensThePlace(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 100, 24
	typeLine(t, a, "/standing")
	if !a.at(pageStanding) {
		t.Fatal("a session with no ambient side opened no page")
	}
	if text := standingPlaceScreen(a); !strings.Contains(text, standNothingWord) {
		t.Fatalf("the empty place does not say what it is for:\n%s", text)
	}
}

// ── 3. the keys ─────────────────────────────────────────────────────────────

// THE THREE WRITES REACH THE ENGINE AND THE PAGE IS READ AGAIN FROM IT. What a
// person sees after a keystroke is what the store says, never what the surface
// wished ([app.homeItemWrite] states the law).
func TestTheStandingPageKeysReachTheEngine(t *testing.T) {
	a, agent := standingPlaceApp(t, []standing.Item{
		standOrder("c1", "keep the tests green", standing.AltitudeConversation),
		standOrder("p1", "draft the weekly update", standing.AltitudeProject),
	}, nil)
	typeLine(t, a, "/standing")

	// THE VERBS ARE ON THE ROW'S STRIP NOW. `p` was a bare letter while this was
	// a modal overlay with no box under it; it is a place with a composer, so `→`
	// draws the verbs and only then is a letter a verb (verbstrip.go).
	drive(t, a, key("right"))
	drive(t, a, key("p"))
	if len(agent.paused) != 1 || agent.paused[0] != "c1" {
		t.Fatalf("p paused %v", agent.paused)
	}
	if text := strings.Join(plainRows(a), "\n"); !strings.Contains(text, homeItemPaused+" · keep the tests green") {
		t.Fatalf("no receipt for the pause:\n%s", text)
	}
	// The row is redrawn from what the engine now says, so a paused order wears
	// the paused mark and the paused clause.
	screen := standingPlaceScreen(a)
	if !strings.Contains(screen, standOffGlyph+" keep the tests green") {
		t.Fatalf("the paused order kept its old mark:\n%s", screen)
	}

	drive(t, a, key("right"))
	drive(t, a, key("n"))
	if len(agent.excepts) != 1 || agent.excepts[0] != "c1" {
		t.Fatalf("n excepted %v", agent.excepts)
	}
	screen = standingPlaceScreen(a)
	if !strings.Contains(screen, standNotHereWord+": keep the tests green") {
		t.Fatalf("the excepted order is still a row:\n%s", screen)
	}

	// The cursor held its PLACE, so the next verb lands on whatever moved up
	// into it rather than on nothing.
	drive(t, a, key("right"))
	drive(t, a, key("s"))
	if len(agent.downed) != 1 || agent.downed[0] != "p1" {
		t.Fatalf("s stopped %v", agent.downed)
	}
	if text := strings.Join(plainRows(a), "\n"); !strings.Contains(text, homeItemStopped+" · draft the weekly update") {
		t.Fatalf("no receipt for the stop:\n%s", text)
	}
	// AND AN EMPTIED PAGE STAYS OPEN, with its heading and nothing under it.
	if !a.at(pageStanding) {
		t.Fatal("the page closed itself out from under the person")
	}
}

// A PAUSED ORDER STARTED AGAIN SAYS SO IN THE WORD THE TRANSCRIPT ALREADY USES
// FOR THAT EVENT — one event read by a person in two places may not be two
// different words.
func TestStartingAPausedOrderAgainSaysGoingAgain(t *testing.T) {
	a, agent := standingPlaceApp(t, []standing.Item{
		standOrder("p1", "draft the weekly update", standing.AltitudeProject),
	}, nil)
	agent.status = standing.StatusActive
	typeLine(t, a, "/standing")
	drive(t, a, key("right"), key("p"))
	if text := strings.Join(plainRows(a), "\n"); !strings.Contains(text, standResumedWord+" · draft the weekly update") {
		t.Fatalf("a resumed order did not say %q:\n%s", standResumedWord, text)
	}
}

// A REFUSAL IS SAID IN ITS OWN WORDS. What comes back from the seam is a
// sentence written for a person; wrapping it in a second sentence about a key
// that did not work would be the surface talking over the engine.
func TestARefusedWriteSaysTheEnginesOwnSentence(t *testing.T) {
	a, agent := standingPlaceApp(t, []standing.Item{
		standOrder("p1", "draft the weekly update", standing.AltitudeProject),
	}, nil)
	agent.refusal = errors.New("standing orders are not built yet")
	typeLine(t, a, "/standing")
	drive(t, a, key("right"), key("s"))
	text := strings.Join(plainRows(a), "\n")
	if !strings.Contains(text, "standing orders are not built yet") {
		t.Fatalf("the refusal was not said:\n%s", text)
	}
	// AND NOTHING WAS REDRAWN AS THOUGH IT HAD WORKED.
	if screen := standingPlaceScreen(a); !strings.Contains(screen, "draft the weekly update") {
		t.Fatalf("a refused stop took the row off the page anyway:\n%s", screen)
	}
}

// ESC LEAVES THE CONVERSATION EXACTLY AS IT WAS, which is what esc means over
// every modal list on this surface.
func TestEscLeavesTheStandingPage(t *testing.T) {
	a, _ := standingPlaceApp(t, []standing.Item{
		standOrder("p1", "draft the weekly update", standing.AltitudeProject),
	}, nil)
	typeLine(t, a, "/standing")
	drive(t, a, key("esc"))
	if a.at(pageStanding) {
		t.Fatal("esc left the page open")
	}
	if a.overlayHeight() != 0 {
		t.Fatal("the closed page is still taking rows from the frame")
	}
}

// THE VERBS ARE NAMED WHERE A PERSON LOOKS FOR THEM, and NOTHING IS NAMED THAT
// IS NOT BOUND.
//
// The line used to promise four bare letters. Three of them moved onto the row's
// `→` strip when this became a place with a composer under it, so the hint names
// the key that reaches them and the strip names the letters — which is the whole
// of "no key does anything that isn't drawn on screen right now" (verbstrip.go).
func TestTheStandingPageNamesItsVerbs(t *testing.T) {
	a, _ := standingPlaceApp(t, []standing.Item{
		standOrder("p1", "draft the weekly update", standing.AltitudeProject),
	}, nil)
	typeLine(t, a, "/standing")
	hint := a.placeHint()
	for _, want := range []string{"enter", "→", homeItemPauseWord, homeItemStopWord, standNotHereWord, "esc"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("the hint does not name %q: %q", want, hint)
		}
	}
	// AND THE STRIP NAMES THE LETTERS, once `→` has drawn it — ON THE ROW, under
	// the order they act on (SCREEN 3c). The frame is read rather than the strip's
	// own rows, because where it lands is half of what makes a bare letter safe.
	drive(t, a, key("right"))
	rows := strings.Split(placeFrameText(a), "\n")
	at := -1
	for i, row := range rows {
		if strings.Contains(row, "p "+homeItemPauseWord) && strings.Contains(row, "s "+homeItemStopWord) {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("the strip does not offer its letters:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[at], "n "+standNotHereWord) {
		t.Fatalf("the strip is missing a verb: %q", rows[at])
	}
	if at < 1 || !strings.Contains(rows[at-1], "draft the weekly update") {
		t.Fatalf("the strip is not drawn under the order it acts on:\n%s", strings.Join(rows, "\n"))
	}
}

// THE PROVENANCE DOOR SAYS SO WHEN THERE IS NOTHING BEHIND IT. An order made at
// home that never became a conversation has no transcript to open, and saying
// that is more use than a door onto nothing ([app.homeItemEnter]'s rule).
func TestEnterOnAnOrderWithNoConversationSaysSo(t *testing.T) {
	item := standOrder("p1", "draft the weekly update", standing.AltitudeProject)
	item.Origin = standing.Origin{Exchange: "made-at-home"}
	a, _ := standingPlaceApp(t, []standing.Item{item}, nil)
	typeLine(t, a, "/standing")
	drive(t, a, key("enter"))
	if text := strings.Join(plainRows(a), "\n"); !strings.Contains(text, homeItemNoDoor) {
		t.Fatalf("enter on an order with no door said nothing:\n%s", text)
	}
	if !a.at(pageStanding) {
		t.Fatal("a door that led nowhere closed the page")
	}
}

// AND WHEN THE DOOR LEADS WHERE THE PERSON ALREADY IS, it says that instead of
// moving them nowhere.
func TestEnterOnThisConversationsOwnOrderSaysYouAreInIt(t *testing.T) {
	item := standOrder("c1", "keep the tests green", standing.AltitudeConversation)
	item.Origin = standing.Origin{Transcript: "/tmp/lab/transcript.jsonl"}
	a, _ := standingPlaceApp(t, []standing.Item{item}, nil)
	a.file = "/tmp/lab/transcript.jsonl"
	typeLine(t, a, "/standing")
	drive(t, a, key("enter"))
	if text := strings.Join(plainRows(a), "\n"); !strings.Contains(text, standHereWord) {
		t.Fatalf("enter on this conversation's own order said nothing:\n%s", text)
	}
	if a.at(pageStanding) {
		t.Fatal("the page stayed up over the conversation it pointed at")
	}
}

// ── 4. every width ──────────────────────────────────────────────────────────

// THE PAGE HOLDS AT EVERY TIER, phone included: no line wider than the frame,
// and the heading and the shelves still readable.
func TestTheStandingPageHoldsAtEveryWidth(t *testing.T) {
	for _, width := range []int{44, 60, 80, 120, 200} {
		a, _ := standingPlaceApp(t, []standing.Item{
			standOrder("c1", "keep the tests green", standing.AltitudeConversation),
			standOrder("p1", "draft the weekly update", standing.AltitudeProject),
		}, []standing.Item{standOrder("m1", "never touch the public API", standing.AltitudeMachine)})
		a.width, a.height = width, 30
		typeLine(t, a, "/standing")
		if !a.at(pageStanding) {
			t.Fatalf("at %d the page did not open", width)
		}
		// IT IS A PLACE NOW, SO IT IS EXACTLY THE WHOLE TERMINAL — the law every
		// page on this surface holds, and one this list could not hold while it
		// was a twelve-row overlay under the draft (pages.go's [placeFrame]).
		frame, _, _, _ := a.standingPlaceFrame(width, a.height)
		if len(frame) != a.height {
			t.Fatalf("at %d the place drew %d lines into %d rows", width, len(frame), a.height)
		}
		lines := make([]string, 0, len(frame))
		for _, line := range frame {
			lines = append(lines, plain(line))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("at %d a line overflows the frame: %q", width, line)
			}
		}
		screen := strings.Join(lines, "\n")
		for _, want := range []string{standHeading, standInHereWord, standProjectWord} {
			if !strings.Contains(screen, want) {
				t.Fatalf("at %d the page is missing %q:\n%s", width, want, screen)
			}
		}
	}
}

// ── 5. the card ─────────────────────────────────────────────────────────────

// THE CARD NAMES THE REACH BEFORE ANYTHING STANDS, in the person's own words.
// An order agreed to without knowing where it applies is the one thing the
// altitude work may not allow (docs/STANDING-ORDERS.md).
func TestTheRatificationCardNamesWhereItReaches(t *testing.T) {
	for _, one := range []struct {
		level standing.Altitude
		word  string
	}{
		{standing.AltitudeConversation, standJustHereWord},
		{standing.AltitudeProject, standProjectWord},
		{standing.AltitudeMachine, standEverywhereWord},
		// The zero value is a project order, because every item made before
		// reaches were spelled was one ([standing.Item.Level]).
		{"", standProjectWord},
	} {
		a, _, _ := standApp(t)
		item := standItem()
		item.Altitude = one.level
		item.Origin.SessionID = "s1"
		drive(t, a, streamEventMsg{gen: a.gen, ev: standProposal(a, session.StandingNotice{
			Item:      item,
			WhenWords: "Mondays at 9am",
			CostWords: "about $0.02 a run",
		})})
		text := standText(a)
		if !strings.Contains(text, standWhereTag+one.word) {
			t.Fatalf("the card for %q does not say %q:\n%s", one.level, standWhereTag+one.word, text)
		}
	}
}

// ── a rule that never wakes ─────────────────────────────────────────────────

// THE PAGE SAYS `holds` AND THE MARK STAYS THE ORDINARY ONE.
//
// A rule is not a fourth state — it is active, and `◦` is what active looks like
// on every aforge screen. What changes is the tail: where a watch says what it
// last found and a routine says its cadence, a rule says the one thing that is
// true of it. The clause is [standRollup]'s, exactly as every other row's is.
func TestTheStandingPageSaysARuleHolds(t *testing.T) {
	rule := standOrder("h1", "never touch the public API", standing.AltitudeProject)
	rule.When = standing.When{Kind: standing.WhenHold}
	rule.Does, rule.Rails = standing.Action{}, standing.Rails{}
	a, _ := standingPlaceApp(t, []standing.Item{rule}, nil)
	typeLine(t, a, "/standing")
	if !a.at(pageStanding) {
		t.Fatal("/standing opened nothing")
	}
	screen := standingPlaceScreen(a)
	if !strings.Contains(screen, standWaitGlyph+" never touch the public API") {
		t.Fatalf("the rule is not drawn as an ordinary waiting row:\n%s", screen)
	}
	if !strings.Contains(screen, standHoldsWord) {
		t.Fatalf("the page does not say %q for a rule:\n%s", standHoldsWord, screen)
	}
	// AND NOT IN THE MACHINERY'S WORDS. "hold kind" and "no trigger" are this
	// codebase's names for the shape; what a person reads is what it is doing.
	for _, banned := range []string{"hold kind", "no trigger", "whenhold"} {
		if strings.Contains(strings.ToLower(screen), banned) {
			t.Fatalf("the page says %q:\n%s", banned, screen)
		}
	}
}
