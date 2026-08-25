package tui3

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// THE STANDING PLACE, DRIVEN AS A PERSON DRIVES IT: /standing typed into a real
// surface with both seams under it, keys pressed, and the frame read back.
//
// standingplace_test.go is the layer below — the pure reading, with fixtures and
// no app at all. standingpage_test.go is what stands over the conversation
// somebody is sitting in. These are the other half of that page: the orders this
// MACHINE holds that do not reach here, which a person who set one up last week
// in another project comes to this screen to find.

// standFarItem is one plausible standing order living in a workspace, as the
// STORE holds it. It is deliberately not [standOrder]: that one is handed over
// by the conversation seam and carries a reach, while this one is a document in
// a folder and its reach is nobody's business on this shelf.
func standFarItem(id, title, workspace string) standing.Item {
	return standing.Item{
		ID:        id,
		Words:     title + ", every time, without me asking",
		Brief:     standing.Brief{Title: title},
		Workspace: workspace,
		When:      standing.When{Kind: standing.WhenEvery, Words: "Mondays at 9am", Every: "168h"},
		Does:      standing.Action{Kind: standing.ActionSay, Say: "said"},
		Rails:     standing.Rails{PerRunUSD: 0.05, MaxPerDay: 1},
		Status:    standing.StatusActive,
	}
}

// standFarLab is a surface with BOTH seams under it, because the page needs
// both and the wave that had it need only one is the wave these tests exist
// for: the conversation's own engine answers what stands HERE, and the store
// answers what stands anywhere at all.
//
// The workspace it answers is a REAL folder outside the places root, because a
// row whose recorded folder is not on the disk is a different row with a
// different tail ([homeLab.workspace] states that law).
type standFarLab struct {
	a     *app
	agent *standPageFake
	band  *standBand
	lab   *homeLab
	// far is the workspace the machine's own orders live in, and it has a
	// conversation of its own so that it is an ORDINARY project rather than the
	// bare kind [standFarLab.bare] makes.
	far string
}

// newStandFarLab builds that surface: `here` stands over this conversation,
// `far` stands in another project on the same machine.
func newStandFarLab(t *testing.T, here []standing.Item, far ...standing.Item) *standFarLab {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	dir := lab.workspace("alpha")
	lab.session("alpha", "s1", "Alpha", dir, now.Add(-time.Hour))
	for i := range far {
		if far[i].Workspace == "" {
			far[i].Workspace = dir
		}
	}
	a, agent := standPageApp(t, here, nil)
	a.homeRoot = lab.root
	a.width, a.height = 100, 30
	band := &standBand{items: far}
	band.wire(a)
	return &standFarLab{a: a, agent: agent, band: band, lab: lab, far: dir}
}

// bare points the window at a workspace that has standing orders and NO
// conversation at all — the state [app.readBareBands] exists for, and the one a
// machine-wide reading that walked only the projects would draw as nothing.
func (l *standFarLab) bare(t *testing.T, items ...standing.Item) {
	t.Helper()
	dir := l.lab.workspace("nowhere")
	for i := range items {
		items[i].Workspace = dir
	}
	l.band.items = items
	l.a.workspace = dir
}

// open is /standing, and the page as a reader sees it.
func (l *standFarLab) open(t *testing.T) string {
	t.Helper()
	typeLine(t, l.a, "/standing")
	if !l.a.standPage.up {
		t.Fatal("/standing opened nothing")
	}
	return standPageScreen(l.a)
}

// ── 1. the machine reaches the page ─────────────────────────────────────────

// AN ORDER IN ANOTHER PROJECT REACHES THIS PLACE, AND OPENS IT ON ITS OWN.
//
// This is the whole of the fault the fourth shelf repairs. `/standing` asked the
// conversation seam what stood here, got nothing, and said "nothing stands here
// yet" over a machine holding three orders the person had answered a card for —
// a screen that exists to say what is true, denying what is true.
func TestAnOrderInAnotherProjectReachesTheStandingPage(t *testing.T) {
	lab := newStandFarLab(t, nil,
		standFarItem("f1", "watch the release feed", ""),
		standFarItem("f2", "keep the changelog index fresh", ""))
	screen := lab.open(t)
	for _, want := range []string{
		standHeading,
		standOtherWord,
		standWaitGlyph + " watch the release feed",
		standWaitGlyph + " keep the changelog index fresh",
		// THE ROW IS DRAWN IN THE SAME GRAMMAR AS EVERY OTHER ROW: the mark every
		// aforge screen agrees on, and home's own clause ([standRollup]) — not a
		// second derivation of the same record in a second set of words.
		"Mondays at 9am",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the page does not say %q:\n%s", want, screen)
		}
	}
	// AND NOTHING ON IT SPEAKS THE MACHINERY'S OWN WORDS, the new heading least
	// of all — `elsewhere on this machine` is the sentence a machine would write.
	for _, banned := range []string{"altitude", "scope", "machine", "conversation altitude"} {
		if strings.Contains(strings.ToLower(screen), banned) {
			t.Fatalf("the page says the machinery's word %q:\n%s", banned, screen)
		}
	}
	// The cursor is on the first order of the only shelf there is, so the strip
	// has something to act on the moment the page opens.
	item, ok := lab.a.standPage.current()
	if !ok || item.ID != "f1" {
		t.Fatalf("the cursor is on %q (%v), not on the first order", item.ID, ok)
	}
}

// A WORKSPACE WITH ORDERS AND NO CONVERSATION REACHES IT TOO.
//
// [app.readBareBands] says why in its own words: "a watch is content", and a
// workspace whose only content is something keeping an eye on it had no
// heading, no band and no row — "the person set a thing up and the screen that
// exists to show them what is true showed them nothing". A machine-wide list
// that walked only the projects with conversations in them would be that same
// fault said a second time.
func TestABareWorkspacesOrderReachesTheStandingPage(t *testing.T) {
	lab := newStandFarLab(t, nil)
	lab.bare(t, standFarItem("b1", "watch the deploy log", ""))
	screen := lab.open(t)
	if !strings.Contains(screen, standOtherWord) {
		t.Fatalf("the shelf is missing:\n%s", screen)
	}
	if !strings.Contains(screen, standWaitGlyph+" watch the deploy log") {
		t.Fatalf("a bare workspace's order is not on the page:\n%s", screen)
	}
}

// THE THREE SHELVES COME FIRST AND THE MACHINE COMES LAST, which is the same
// outward reading the three already are, carried one step further: what is true
// where you are sitting, then what is true elsewhere.
func TestTheMachinesShelfIsDrawnUnderTheConversationsOwn(t *testing.T) {
	lab := newStandFarLab(t,
		[]standing.Item{standOrder("p1", "draft the weekly update", standing.AltitudeProject)},
		standFarItem("f1", "watch the release feed", ""))
	screen := lab.open(t)
	here, other := strings.Index(screen, standProjectWord), strings.Index(screen, standOtherWord)
	if here < 0 || other < 0 || here > other {
		t.Fatalf("the shelves are out of order (%d, %d):\n%s", here, other, screen)
	}
}

// AN ORDER IS ON ONE SHELF AND ONLY ONE. Everything standing over this
// conversation is also a document in a folder the store enumerates, so a page
// that did not deduplicate would draw the same order twice under two different
// claims about where it stands.
func TestAnOrderOverThisConversationIsNotDrawnTwice(t *testing.T) {
	here := standOrder("p1", "draft the weekly update", standing.AltitudeProject)
	far := standFarItem("p1", "draft the weekly update", "")
	lab := newStandFarLab(t, []standing.Item{here}, far)
	screen := lab.open(t)
	if n := strings.Count(screen, "draft the weekly update"); n != 1 {
		t.Fatalf("one order was drawn %d times:\n%s", n, screen)
	}
	if strings.Contains(screen, standOtherWord) {
		t.Fatalf("a shelf with nothing left on it drew its heading anyway:\n%s", screen)
	}
}

// AND AN ORDER KEPT OUT OF HERE STAYS KEPT OUT. `n` files it as one dim line
// under its own shelf; pushing it back onto the machine's shelf would answer
// that gesture by redrawing the thing the person had just pushed away.
func TestAnExceptedOrderDoesNotReappearOnTheMachinesShelf(t *testing.T) {
	excepted := standOrder("m1", "never touch the public API", standing.AltitudeMachine)
	lab := newStandFarLab(t,
		[]standing.Item{standOrder("p1", "draft the weekly update", standing.AltitudeProject)},
		standFarItem("m1", "never touch the public API", ""))
	lab.agent.excepted = []standing.Item{excepted}
	screen := lab.open(t)
	if !strings.Contains(screen, standNotHereWord+": never touch the public API") {
		t.Fatalf("the exception is not on the page:\n%s", screen)
	}
	if strings.Contains(screen, standOtherWord) {
		t.Fatalf("an excepted order came back as another project's:\n%s", screen)
	}
}

// ── 2. the whole frame, and no fold ─────────────────────────────────────────

// EVERY ORDER IS REACHABLE, HOWEVER MANY THERE ARE.
//
// The list showed four rows and hid the rest behind a `▸ N more` line that no
// key answered — a door onto nothing, on a screen that takes the whole
// terminal. A place has no reason to fold: the cursor walks the list and the
// window follows it, so the sixth order is one `down` further away than the
// fifth and not behind anything.
func TestTheCursorWalksPastTheFifthOrderAndTheWindowFollows(t *testing.T) {
	var far []standing.Item
	for _, one := range []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh"} {
		far = append(far, standFarItem(one, "watch the "+one+" thing", ""))
	}
	lab := newStandFarLab(t, nil, far...)
	lab.open(t)
	for range 6 {
		drive(t, lab.a, key("down"))
	}
	item, ok := lab.a.standPage.current()
	if !ok || item.ID != "seventh" {
		t.Fatalf("six downs reached %q (%v), not the seventh order", item.ID, ok)
	}
	// AND WHAT THE CURSOR IS ON IS ON THE SCREEN. A cursor that walked past the
	// bottom edge is a cursor nobody can see, which is the fold again wearing
	// different clothes.
	screen := standPageScreen(lab.a)
	if !strings.Contains(screen, "watch the seventh thing") {
		t.Fatalf("the row under the cursor is not drawn:\n%s", screen)
	}
	// AND NO LINE OFFERS A DOOR THERE IS NO KEY FOR.
	if strings.Contains(screen, "▸") || strings.Contains(screen, " more") {
		t.Fatalf("the page folded rows behind a line no key answers:\n%s", screen)
	}
}

// A PAGE KEY STEPS BY THE WINDOW THE FRAME ACTUALLY GAVE IT, not by a constant
// left over from when this was a twelve-row overlay under the draft.
func TestAPageKeyStepsByTheWindowTheFrameGave(t *testing.T) {
	var far []standing.Item
	for _, one := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		far = append(far, standFarItem(one, "watch the "+one+" thing", ""))
	}
	lab := newStandFarLab(t, nil, far...)
	lab.open(t)
	drive(t, lab.a, key("pgdown"))
	item, ok := lab.a.standPage.current()
	if !ok || item.ID != "h" {
		t.Fatalf("pgdown on a thirty-row frame reached %q (%v), not the last order", item.ID, ok)
	}
	drive(t, lab.a, key("pgup"))
	if item, _ := lab.a.standPage.current(); item.ID != "a" {
		t.Fatalf("pgup came back to %q, not the first order", item.ID)
	}
}

// THE CURSOR STOPS ONLY ON ORDERS. A heading, a `not here` line and a `last
// look` paragraph are things to READ: they are furniture the page chose, and a
// selection on one is a selection no verb has anything to do with.
func TestTheStandingCursorStopsOnlyOnOrders(t *testing.T) {
	lab := newStandFarLab(t,
		[]standing.Item{standOrder("p1", "draft the weekly update", standing.AltitudeProject)},
		standFarItem("f1", "watch the release feed", ""))
	lab.open(t)
	seen := map[string]bool{}
	for range 8 {
		row, ok := lab.a.standPage.choice()
		if !ok {
			t.Fatal("the cursor came to rest on nothing")
		}
		if row.kind != standRowItem {
			t.Fatalf("the cursor rests on a %v, which is not an order", row.kind)
		}
		seen[row.view.Item.ID] = true
		drive(t, lab.a, key("down"))
	}
	if !seen["p1"] || !seen["f1"] {
		t.Fatalf("walking down reached %v, not both shelves' orders", seen)
	}
}

// AND A CLICK RESOLVES AGAINST THE LINES THAT WERE ACTUALLY PAINTED. One layout
// answers both the paint and the pointer, so a press can never open the row
// above the one under the finger.
func TestPressingARowOfTheMachinesShelfLandsOnThatRow(t *testing.T) {
	lab := newStandFarLab(t, nil,
		standFarItem("f1", "watch the release feed", ""),
		standFarItem("f2", "keep the changelog index fresh", ""))
	lab.open(t)
	lines, _, _, _ := lab.a.standPageFrame(lab.a.width, lab.a.height)
	found := -1
	for y, line := range lines {
		if strings.Contains(plain(line), "keep the changelog index fresh") {
			found = y
			break
		}
	}
	if found < 0 {
		t.Fatal("the second order is not on the frame")
	}
	lab.a.standPage.press(lab.a, found)
	if item, ok := lab.a.standPage.current(); !ok || item.ID != "f2" {
		t.Fatalf("the press landed on %q (%v), not on the row it was aimed at", item.ID, ok)
	}
	// A heading answers to no row, so pressing one moves nothing at all.
	for y, line := range lines {
		if strings.TrimSpace(plain(line)) == standOtherWord {
			lab.a.standPage.press(lab.a, y)
			break
		}
	}
	if item, _ := lab.a.standPage.current(); item.ID != "f2" {
		t.Fatalf("pressing a heading moved the cursor to %q", item.ID)
	}
}

// ── 3. one grammar, one set of words ────────────────────────────────────────

// THE ROW'S TAIL IS HOME'S OWN CLAUSE AND NEVER A SECOND ONE.
//
// The place drew its own column of rope, cadence and cost from the same record
// home draws a rollup from — two readings of one item, in front of one person,
// on two screens that will drift the first time either is edited. There is one
// derivation ([standRollup]) and this page uses it, whichever shelf the row is
// filed under.
func TestAStandingRowsTailIsHomesOwnClause(t *testing.T) {
	watched := standFarItem("f1", "the 6am watch", "")
	watched.When = standing.When{Kind: standing.WhenProbe, Words: "daily"}
	watched.LastChecked = time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC)
	watched.LastCheckLine = "nothing had changed since yesterday"
	lab := newStandFarLab(t, nil, watched)
	screen := lab.open(t)
	want := standRollup(StandingItemView{Item: watched}, lab.a.now())
	if want == "" {
		t.Fatal("the fixture has no clause to compare against")
	}
	if !strings.Contains(screen, want) {
		t.Fatalf("the row does not carry home's clause %q:\n%s", want, screen)
	}
}

// AND ITS MARK IS THE ONE EVERY AFORGE SCREEN AGREES ON — the store's own
// answer ([standing.Item.Glyph]), so that a row on this shelf and the same row
// on home cannot lead with two different characters.
func TestTheMachinesShelfWearsTheStoresOwnMarks(t *testing.T) {
	paused := standFarItem("f1", "keep the top-movers sheet fresh", "")
	paused.Status = standing.StatusPaused
	asking := standFarItem("f2", "file anything that looks like an invoice", "")
	asking.NeedsPerson = "send this invoice?"
	lab := newStandFarLab(t, nil, paused, asking)
	screen := lab.open(t)
	for _, want := range []string{
		homeAskGlyph + " file anything that looks like an invoice",
		standOffGlyph + " keep the top-movers sheet fresh",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the page does not draw %q:\n%s", want, screen)
		}
	}
}

// THE SHELF KEEPS HOME'S TRIAGE ORDER: what needs somebody, then what is
// moving, then everything else. The mark and the row's position are two
// statements of one fact, and a list that sorted them apart would put the row
// this screen exists for below three that could have waited.
func TestTheMachinesShelfKeepsHomesTriageOrder(t *testing.T) {
	quiet := standFarItem("quiet", "watch the release feed", "")
	asking := standFarItem("asking", "file anything that looks like an invoice", "")
	asking.NeedsPerson = "send this invoice?"
	lab := newStandFarLab(t, nil, quiet, asking)
	screen := lab.open(t)
	at, over := strings.Index(screen, "file anything"), strings.Index(screen, "watch the release feed")
	if at < 0 || over < 0 || at > over {
		t.Fatalf("the order that needs somebody is not first (%d, %d):\n%s", at, over, screen)
	}
}

// THE ONE HEADING COUNTS NOTHING.
//
// The page said "things aforge does without being asked. 7 standing, 1 waiting
// to be stood up." — a screen counting what a person can see, and counting it
// beside a list they are about to scroll. The heading names the page and the
// rows say the rest.
func TestTheStandingPagesHeadingCountsNothing(t *testing.T) {
	lab := newStandFarLab(t, nil,
		standFarItem("f1", "watch the release feed", ""),
		standFarItem("f2", "keep the changelog index fresh", ""))
	screen := lab.open(t)
	if !strings.Contains(screen, standHeading) {
		t.Fatalf("the page lost its heading:\n%s", screen)
	}
	for _, banned := range []string{"2 standing", "waiting to be stood up", "without being asked"} {
		if strings.Contains(screen, banned) {
			t.Fatalf("the heading counts what a person can see (%q):\n%s", banned, screen)
		}
	}
}

// THE LAST LOOK FOLLOWS THE CURSOR, AND SAYS NOTHING WHEN THERE IS NOTHING TO
// SAY. It is the one thing a place can draw that an overlay could not: the row's
// tail is a clause sharing a line with the order's name, while this is the whole
// account of what was actually seen.
func TestTheLastLookFollowsTheCursorAndIsSilentWhenUnknown(t *testing.T) {
	now := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	looked := standFarItem("looked", "the 6am watch", "")
	looked.When = standing.When{Kind: standing.WhenProbe, Words: "daily"}
	looked.LastChecked = now.Add(-3 * time.Hour)
	looked.LastCheckLine = "nothing had changed since yesterday"
	never := standFarItem("never", "watch the release feed", "")
	lab := newStandFarLab(t, nil, looked, never)
	screen := lab.open(t)
	for _, want := range []string{
		"the 6am watch, last look",
		"looked 3h ago · nothing had changed since yesterday, so nothing was done",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the page does not say %q:\n%s", want, screen)
		}
	}
	// AND AN ORDER THAT WAS NEVER LOOKED AT DRAWS NO PARAGRAPH AT ALL — not a
	// heading, not a blank line, not "never run".
	drive(t, lab.a, key("down"))
	screen = standPageScreen(lab.a)
	if strings.Contains(screen, "last look") {
		t.Fatalf("an order that has never been looked at drew a last look:\n%s", screen)
	}
}

// ── 4. the verbs the row can actually perform ───────────────────────────────

// THE STRIP OFFERS ONLY WHAT THE ROW CAN BE ASKED TO DO, AND EVERY LETTER ON IT
// IS BOUND.
//
// The old table put `y let it`, `n not this time` and `t trust it alone` on the
// strip with nothing behind any of them — words on a screen that no key
// answered. A capability that cannot work is absent, not broken: an order in
// another project has the two writes the store can make and no others, because
// `not here` names a place it was never over.
func TestTheMachinesShelfOffersOnlyTheStoresOwnWrites(t *testing.T) {
	paused := standFarItem("f2", "keep the top-movers sheet fresh", "")
	paused.Status = standing.StatusPaused
	lab := newStandFarLab(t, []standing.Item{
		standOrder("p1", "draft the weekly update", standing.AltitudeProject),
	}, standFarItem("f1", "watch the release feed", ""), paused)

	words := func() string {
		verbs := lab.a.standRowVerbs()
		parts := make([]string, 0, len(verbs))
		for _, v := range verbs {
			if v.do == nil {
				t.Fatalf("the strip offers %q with nothing behind it", v.word)
			}
			parts = append(parts, string(v.key)+" "+v.word)
		}
		return strings.Join(parts, " · ")
	}
	lab.open(t)
	if got, want := words(), "p pause · s stop · n "+standNotHereWord; got != want {
		t.Fatalf("this conversation's own order offers %q, want %q", got, want)
	}
	drive(t, lab.a, key("down"))
	if got, want := words(), "p pause · s stop"; got != want {
		t.Fatalf("another project's order offers %q, want %q", got, want)
	}
	drive(t, lab.a, key("down"))
	if got, want := words(), "p "+standResumeWord+" · s stop"; got != want {
		t.Fatalf("a paused order offers %q, want %q", got, want)
	}
	// AND THE HINT NAMES EXACTLY THOSE. Nothing is named that is not bound, so
	// the line over a row that cannot make an exception does not promise one.
	hint := lab.a.standPage.hint(lab.a)
	if !strings.Contains(hint, standResumeWord) || strings.Contains(hint, standNotHereWord) {
		t.Fatalf("the hint over a paused order in another project says %q", hint)
	}
}

// A WINDOW WITH NO WAY TO WRITE OFFERS NOTHING, rather than offering keys that
// would fail. It is the same law the belt keeps for a tool with nothing behind
// it.
func TestAWindowThatCannotWriteOffersNoVerbsOnTheMachinesShelf(t *testing.T) {
	lab := newStandFarLab(t, nil, standFarItem("f1", "watch the release feed", ""))
	lab.a.stands.Save = nil
	lab.open(t)
	if verbs := lab.a.standRowVerbs(); len(verbs) != 0 {
		t.Fatalf("a window that cannot write offered %d verbs", len(verbs))
	}
	if hint := lab.a.standPage.hint(lab.a); strings.Contains(hint, "→") {
		t.Fatalf("the hint named a strip with nothing on it: %q", hint)
	}
}

// THE WRITE GOES THROUGH THE STORE AND THE PAGE IS READ AGAIN FROM IT. What a
// person sees after a keystroke is what the disk says, never what the surface
// wished ([app.homeItemWrite] states the law this borrows).
func TestPausingAnOrderInAnotherProjectGoesThroughTheStore(t *testing.T) {
	lab := newStandFarLab(t, nil, standFarItem("f1", "watch the release feed", ""))
	lab.open(t)
	drive(t, lab.a, key("right"), key("p"))
	if len(lab.band.saved) != 1 || lab.band.saved[0].Status != standing.StatusPaused {
		t.Fatalf("p wrote %+v", lab.band.saved)
	}
	if text := strings.Join(plainRows(lab.a), "\n"); !strings.Contains(text, homeItemPaused+" · watch the release feed") {
		t.Fatalf("no receipt for the pause:\n%s", text)
	}
	if screen := standPageScreen(lab.a); !strings.Contains(screen, standOffGlyph+" watch the release feed") {
		t.Fatalf("the paused order kept its old mark:\n%s", screen)
	}
	// AND STARTING IT AGAIN SAYS THE WORD THE TRANSCRIPT ALREADY USES.
	drive(t, lab.a, key("right"), key("p"))
	if text := strings.Join(plainRows(lab.a), "\n"); !strings.Contains(text, standResumedWord+" · watch the release feed") {
		t.Fatalf("a resumed order did not say %q:\n%s", standResumedWord, text)
	}
}

// STOPPING ONE TAKES IT OFF THE PAGE AND RECORDS WHY IN THE PERSON'S OWN TERMS.
func TestStoppingAnOrderInAnotherProjectTakesItOffThePage(t *testing.T) {
	lab := newStandFarLab(t, nil,
		standFarItem("f1", "watch the release feed", ""),
		standFarItem("f2", "keep the changelog index fresh", ""))
	lab.open(t)
	drive(t, lab.a, key("right"), key("s"))
	if len(lab.band.saved) != 1 || lab.band.saved[0].RetiredWhy != homeStoppedWhy {
		t.Fatalf("s wrote %+v", lab.band.saved)
	}
	screen := standPageScreen(lab.a)
	if strings.Contains(screen, "watch the release feed") {
		t.Fatalf("a stopped order is still on the page:\n%s", screen)
	}
	if !strings.Contains(screen, "keep the changelog index fresh") {
		t.Fatalf("the rest of the shelf went with it:\n%s", screen)
	}
}

// A REFUSAL IS SAID IN THE STORE'S OWN WORDS, and nothing is redrawn as though
// the write had worked.
func TestARefusedStoreWriteSaysTheStoresOwnSentence(t *testing.T) {
	lab := newStandFarLab(t, nil, standFarItem("f1", "watch the release feed", ""))
	lab.band.err = errors.New("the standing folder is read-only")
	lab.open(t)
	drive(t, lab.a, key("right"), key("p"))
	if text := strings.Join(plainRows(lab.a), "\n"); !strings.Contains(text, "the standing folder is read-only") {
		t.Fatalf("the refusal was not said:\n%s", text)
	}
	if screen := standPageScreen(lab.a); !strings.Contains(screen, standWaitGlyph+" watch the release feed") {
		t.Fatalf("a refused pause repainted the row anyway:\n%s", screen)
	}
}

// ── 5. every width, and the emptiness law ───────────────────────────────────

// THE MACHINE'S SHELF HOLDS AT EVERY TIER, phone included: the place is exactly
// the whole terminal, no line overflows it, and the heading and the rows are
// still readable.
func TestTheMachinesShelfHoldsAtEveryWidth(t *testing.T) {
	for _, width := range []int{44, 60, 80, 120, 200} {
		lab := newStandFarLab(t, nil,
			standFarItem("f1", "watch the release feed", ""),
			standFarItem("f2", "keep the changelog index fresh", ""))
		lab.a.width, lab.a.height = width, 30
		lab.open(t)
		frame, _, _, _ := lab.a.standPageFrame(width, lab.a.height)
		if len(frame) != lab.a.height {
			t.Fatalf("at %d the place drew %d lines into %d rows", width, len(frame), lab.a.height)
		}
		lines := make([]string, 0, len(frame))
		for _, line := range frame {
			line = plain(line)
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("at %d a line is %d cells: %q", width, got, line)
			}
			lines = append(lines, line)
		}
		screen := strings.Join(lines, "\n")
		for _, want := range []string{standHeading, standOtherWord, "watch the release feed"} {
			if !strings.Contains(screen, want) {
				t.Fatalf("at %d the page is missing %q:\n%s", width, want, screen)
			}
		}
	}
}

// AND SO DOES A ROW WHOSE AUTHOR WROTE IT IN WIDE CHARACTERS WITH AN UNBOUNDED
// CADENCE. The person's own words are kept whole in the record; what a narrow
// frame does is clip them for the drawing, never overflow.
func TestAStandingRowFitsAuthoredUnicodeAndAnUnboundedCadence(t *testing.T) {
	wide := standFarItem("wide", strings.Repeat("東京🧭", 30), "")
	wide.Brief = standing.Brief{}
	wide.When = standing.When{Kind: standing.WhenEvery, Words: strings.Repeat("界", 250)}
	for _, width := range []int{44, 60, 80, 120, 200} {
		lab := newStandFarLab(t, nil, wide)
		lab.a.width, lab.a.height = width, 30
		lab.open(t)
		frame, _, _, _ := lab.a.standPageFrame(width, lab.a.height)
		for _, line := range frame {
			if got := ansi.StringWidth(plain(line)); got > width {
				t.Fatalf("width %d drew %d cells: %q", width, got, plain(line))
			}
		}
	}
}

// THE EMPTINESS LAW REACHES EVERY FIGURE ON THE PAGE — no `$0.00`, no count of
// what a person can see, and no shelf heading over nothing.
func TestTheStandingPageObeysTheEmptinessLaw(t *testing.T) {
	lab := newStandFarLab(t, nil, standFarItem("f1", "watch the release feed", ""))
	screen := lab.open(t)
	for _, banned := range []string{"$0.00", "0 tok", "0 standing", standInHereWord, standProjectWord, standEverywhereWord} {
		if strings.Contains(screen, banned) {
			t.Fatalf("the page draws %q over nothing:\n%s", banned, screen)
		}
	}
	// AND A MACHINE WITH NOTHING ON IT OPENS NO PAGE AT ALL: one sentence in the
	// conversation, and no screen to dismiss before it can be told it was
	// useless.
	bare := newStandFarLab(t, nil)
	typeLine(t, bare.a, "/standing")
	if bare.a.standPage.up {
		t.Fatal("a machine nothing stands on opened a page anyway")
	}
	if text := strings.Join(plainRows(bare.a), "\n"); !strings.Contains(text, standNothingWord) {
		t.Fatalf("nothing was said:\n%s", text)
	}
}

// THE READING IS TAKEN ON THE OPEN AND NEVER ON A DRAW. The bands are a walk of
// every project's documents; a page that took it inside a frame would touch the
// disk on every keystroke, every resize and every blink of the caret.
func TestTheStandingPageReadsTheStoreOnlyWhenItOpens(t *testing.T) {
	lab := newStandFarLab(t, nil, standFarItem("f1", "watch the release feed", ""))
	asked := 0
	items := lab.a.stands.Items
	lab.a.stands.Items = func(workspace string) []standing.Item {
		asked++
		return items(workspace)
	}
	lab.open(t)
	if asked == 0 {
		t.Fatal("opening the page asked the store nothing")
	}
	after := asked
	for range 3 {
		lab.a.standPageFrame(lab.a.width, lab.a.height)
		drive(t, lab.a, key("down"))
	}
	if asked != after {
		t.Fatalf("drawing and moving the cursor asked the store %d more times", asked-after)
	}
}

// ── 6. the tab bar's count ──────────────────────────────────────────────────

func TestStandingChangedSinceCountsFiringsAfterTheLook(t *testing.T) {
	seen := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	a := &app{}
	a.home.items = map[string][]StandingItemView{"p": {
		{Item: standing.Item{ID: "new", LastFired: seen.Add(time.Minute)}},
		{Item: standing.Item{ID: "old", LastFired: seen.Add(-time.Minute)}},
	}}
	if got := a.standingChangedSince(seen); got != 1 {
		t.Fatalf("changed standing items = %d, want 1", got)
	}
	// AND IT COUNTS THE BARE WORKSPACES TOO, because [app.readBareBands] writes
	// their bands into the same map and a count that walked only the projects
	// with conversations in them would be under by whatever those hold.
	bare := filepath.Join(t.TempDir(), "nowhere")
	a.home.items[bare] = []StandingItemView{{Item: standing.Item{ID: "far", LastFired: seen.Add(time.Minute)}}}
	a.home.bare = []homeBare{{project: session.Project{Dir: bare, Path: bare}}}
	if got := a.standingChangedSince(seen); got != 2 {
		t.Fatalf("with a bare workspace the count is %d, want 2", got)
	}
}

// ── 7. the place's own contract ─────────────────────────────────────────────

// THE STOPS THE PLACE OFFERS ARE THE ROWS THE CURSOR ACTUALLY WALKS. They are
// one answer to "which lines may be selected" — the frame will move the cursor
// by them once the registry lands, and a second list would be a page that could
// be walked onto a heading it swears is furniture.
func TestThePlaceOffersTheSameStopsItsCursorWalks(t *testing.T) {
	lab := newStandFarLab(t,
		[]standing.Item{standOrder("p1", "draft the weekly update", standing.AltitudeProject)},
		standFarItem("f1", "watch the release feed", ""))
	lab.open(t)
	walked := map[int]bool{lab.a.standPage.cursor: true}
	for range 6 {
		drive(t, lab.a, key("down"))
		walked[lab.a.standPage.cursor] = true
	}
	stops := lab.a.standPage.stops()
	if len(stops) != len(walked) {
		t.Fatalf("the place offers %d stops and the cursor walked %d", len(stops), len(walked))
	}
	for _, at := range stops {
		if !walked[at] {
			t.Fatalf("row %d is offered as a stop and the cursor never rests there", at)
		}
	}
}

// AND THE PLACE SAYS NOTHING IN THE FRAME'S NOTE SLOT. That line is for facts
// about the WHOLE body — a count, a filter, an open editor's label — and every
// fact this place has is about one order, which is what the row and the strip
// are for. A count of what a person can already see is the emptiness law broken
// from the other end.
func TestTheStandingPlaceSaysNothingInTheNoteSlot(t *testing.T) {
	lab := newStandFarLab(t, nil, standFarItem("f1", "watch the release feed", ""))
	lab.open(t)
	if note := lab.a.standPage.note(lab.a, lab.a.width); len(note) != 0 {
		t.Fatalf("the place wrote %q into the note slot", note)
	}
}

// THE THREE CONVERSATION VERBS REFUSE AN ORDER THEY ARE NOT OVER, AND THEY SAY
// SO. The strip never offers them on the machine's shelf, so this is the second
// lock on the same door — and a key that did nothing and said nothing would be
// indistinguishable from a key that is broken.
func TestAConversationVerbOnAnotherProjectsOrderSaysSo(t *testing.T) {
	lab := newStandFarLab(t, nil, standFarItem("f1", "watch the release feed", ""))
	lab.open(t)
	lab.a.standPage.ask(lab.a, standExcept)
	if text := strings.Join(plainRows(lab.a), "\n"); !strings.Contains(text, standNotOursWord) {
		t.Fatalf("the refusal was not said:\n%s", text)
	}
	if len(lab.agent.excepts) != 0 {
		t.Fatalf("the engine was asked to except %v", lab.agent.excepts)
	}
}

// ── 8. the window: when it fired ────────────────────────────────────────────

// THE WINDOW KEY MOVES THE LABEL AND NEVER TOUCHES THE STORE. The orders are
// already in memory — the open collected them — so paging the window is
// arithmetic over a slice, which is what lets a person hold the arrow down. A
// window key that reached the disk would break the law this place is built on
// with the one gesture most likely to repeat.
func TestTheWindowKeyMovesTheLabelWithoutTouchingTheStore(t *testing.T) {
	fired := standFarItem("f1", "watch the release feed", "")
	fired.LastFired = time.Date(2026, 8, 16, 9, 0, 0, 0, time.Local)
	lab := newStandFarLab(t, nil, fired)

	asked := 0
	items := lab.a.stands.Items
	lab.a.stands.Items = func(workspace string) []standing.Item {
		asked++
		return items(workspace)
	}
	lab.open(t)
	after := asked
	before := lab.a.standPage.win

	head := func() string {
		frame, _, _, _ := lab.a.standPageFrame(lab.a.width, lab.a.height)
		for _, line := range frame {
			if strings.Contains(plain(line), standHeading) {
				return plain(line)
			}
		}
		t.Fatal("the header is not on the frame")
		return ""
	}
	was := head()
	if !strings.Contains(was, "shift+←") {
		t.Fatalf("the header draws no window control: %q", was)
	}

	drive(t, lab.a, key("shift+left"))
	if lab.a.standPage.win == before {
		t.Fatal("shift+left moved nothing")
	}
	if now := head(); now == was {
		t.Fatalf("the label did not change: %q", now)
	}
	// AND BACK AGAIN LANDS WHERE IT STARTED, which is what makes the arrows a
	// pair rather than two separate gestures.
	drive(t, lab.a, key("shift+right"))
	if lab.a.standPage.win != before {
		t.Fatalf("shift+right came back to %q, not to %q",
			lab.a.standPage.win.Label(), before.Label())
	}
	// The grain zoom is the other axis, and it is the same four keys everywhere.
	drive(t, lab.a, key("shift+up"))
	if lab.a.standPage.win.Grain == before.Grain {
		t.Fatalf("shift+up did not zoom the grain: %v", lab.a.standPage.win.Grain)
	}
	if asked != after {
		t.Fatalf("moving the window asked the store %d more times", asked-after)
	}
}

// NARROWING THE WINDOW SCOPES WHAT IS LISTED — that is what the control is for —
// AND AN ORDER THAT HAS NEVER FIRED IS NEVER SCOPED AWAY, because it has no
// firing to be outside anything.
func TestNarrowingTheWindowScopesWhatFiredAndKeepsWhatNeverDid(t *testing.T) {
	old := standFarItem("old", "watch the release feed", "")
	old.LastFired = time.Date(2026, 5, 17, 9, 0, 0, 0, time.Local)
	never := standFarItem("never", "never touch the public API", "")
	lab := newStandFarLab(t, nil, old, never)
	screen := lab.open(t)
	for _, want := range []string{"watch the release feed", "never touch the public API"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the opening frame already hid %q:\n%s", want, screen)
		}
	}
	// Walk the window back off the old firing. Its span reaches from May to
	// today, so one step back is a window with nothing that fired in it.
	drive(t, lab.a, key("shift+left"))
	screen = standPageScreen(lab.a)
	if strings.Contains(screen, "watch the release feed") {
		t.Fatalf("an order that fired outside the window is still listed:\n%s", screen)
	}
	if !strings.Contains(screen, "never touch the public API") {
		t.Fatalf("an order that has never fired was scoped away:\n%s", screen)
	}
	// AND THE CURSOR IS BACK ON A ROW THAT EXISTS. A list that lost rows under a
	// cursor left where it was would put a verb on whatever slid into its line.
	item, ok := lab.a.standPage.current()
	if !ok || item.ID != "never" {
		t.Fatalf("the cursor is on %q (%v) after the list changed under it", item.ID, ok)
	}
}

// A FRAME TOO NARROW TO DRAW THE CONTROL HAS NO WINDOW AT ALL: one predicate
// answers the paint and the keys, so the arrows are never bound where they are
// not drawn.
func TestTheWindowKeysAreUnboundOnAFrameTooNarrowToDrawThem(t *testing.T) {
	fired := standFarItem("f1", "watch the release feed", "")
	fired.LastFired = time.Date(2026, 8, 16, 9, 0, 0, 0, time.Local)
	lab := newStandFarLab(t, nil, fired)
	lab.a.width, lab.a.height = 44, 30
	lab.open(t)
	before := lab.a.standPage.win
	drive(t, lab.a, key("shift+left"))
	if lab.a.standPage.win != before {
		t.Fatal("a phone frame moved a window it never drew")
	}
	if screen := standPageScreen(lab.a); strings.Contains(screen, "shift+") {
		t.Fatalf("a phone frame drew the control:\n%s", screen)
	}
}

// AND THE ROPE REACHES THE SCREEN. The rung is derived in internal/standing and
// drawn on the row's tail; this is the end-to-end that the two are actually
// joined up, because a derivation nothing draws is a column that does not exist.
func TestTheRopeIsOnTheStandingPlacesRows(t *testing.T) {
	trusted := standFarItem("trusted", "watch the release feed", "")
	trusted.Grant, trusted.CleanRuns = "summarise without asking", standing.TrustAfter
	earning := standFarItem("earning", "keep the changelog index fresh", "")
	earning.Grant, earning.CleanRuns = "refresh without asking", 3
	bare := standFarItem("bare", "file anything that looks like an invoice", "")
	lab := newStandFarLab(t, nil, trusted, earning, bare)
	screen := lab.open(t)
	for _, want := range []string{standing.RopeTrusted, "earning trust 3/5", standing.RopeAsksFirst} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the page does not draw %q:\n%s", want, screen)
		}
	}
	// AND NEVER THE RESIDENT'S WORD FOR THE SAME MECHANISM. `tenure` is that
	// other product's name for a charter earning the same thing, and vocabulary
	// does not travel between the two.
	if strings.Contains(strings.ToLower(screen), "tenure") {
		t.Fatalf("the page says the resident's word:\n%s", screen)
	}
}
