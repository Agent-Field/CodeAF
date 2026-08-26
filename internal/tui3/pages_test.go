package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE ROUTER, AS A PERSON MEETS IT.
//
// Every test below asserts what is on the screen and what a key does, rather
// than the shape of the code under it (pages.go, placekeys.go, verbstrip.go).

// placeApp is a machine with enough on it for home to open onto.
func placeApp(t *testing.T) *app {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker",
		lab.workspace("alpha"), now.Add(-2*time.Minute))
	lab.session("-beta", "bbbb000000000001", "pricing research",
		lab.workspace("beta"), now.Add(-3*time.Hour))
	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.openHome()
	return a
}

// placeFrameText is whatever place is up, as a reader sees it.
func placeFrameText(a *app) string {
	f, _, _ := a.frame()
	return plain(f)
}

// ── the seven ───────────────────────────────────────────────────────────────

// THE PLACES ARE ONE LIST READ FOUR WAYS: the tab bar's order, the number each
// one answers to, the circle tab walks, and the word a person types. A place
// that fell out of step with itself would be a bar teaching a key that goes
// somewhere else.
//
// THE FOUR READINGS ARE PINNED ON THE REGISTRY NOW
// ([TestEveryPlaceIsRegisteredOnceAndInTabOrder]), because the list they have to
// agree with is the registry rather than a literal seven this file counted. What
// is left here is the two facts that are about the EDGES of that list.
func TestTheSevenPlacesAreOneList(t *testing.T) {
	if len(pages()) != 7 {
		t.Fatalf("there are %d places, and the design has seven", len(pages()))
	}
	// AND alt+8 IS NOTHING, rather than the first place again.
	if _, ok := placeDigit("alt+8"); ok {
		t.Fatal("alt+8 reaches a place that does not exist")
	}
}

// A NUMBER IN FRONT OF A PLACE HAS TO MEAN SOMETHING. A collection can be
// counted; a sum, an act and a set of settings cannot, and a count on one of
// those would be a number about nothing.
func TestOnlyTheCollectionsWearACount(t *testing.T) {
	for _, id := range []page{pageHome, pageTasks, pageStanding, pageMemory} {
		if !id.counted() {
			t.Fatalf("%s holds a pile of things and would not wear a count", id.word())
		}
	}
	for _, id := range []page{pageSpend, pageSearch, pageSettings} {
		if id.counted() {
			t.Fatalf("%s is not a collection and must not wear a count", id.word())
		}
	}
}

// AND A SURFACE THAT HAS NOT COUNTED YET WEARS NOTHING. The counts arrive on
// the clock (placecounts.go); until the first beat the seam is nil, and the
// emptiness law says an unknown number is drawn as nothing, not as a zero.
func TestATabWearsNoCountUntilSomethingAnswersForIt(t *testing.T) {
	a := placeApp(t)
	if a.places != nil {
		t.Fatal("a surface that has not counted yet wired a counter")
	}
	bar := plain(a.placeTabBar(a.width, false, a.pal))
	for _, digit := range "0123456789" {
		if strings.ContainsRune(bar, digit) {
			t.Fatalf("the bar wears a figure with nothing to count: %q", bar)
		}
	}
}

// ── walking between them ────────────────────────────────────────────────────

// tab WALKS THE CIRCLE AND alt+<n> JUMPS. Both are the same list, so a person
// who learns one has learned the other.
func TestTabWalksThePlacesAndTheNumbersJump(t *testing.T) {
	a := placeApp(t)
	if a.page != pageHome {
		t.Fatalf("home did not leave the router standing on itself: %v", a.page)
	}
	// TAB LEAVES THE PLACE IT WAS PRESSED ON, whatever is or is not on this
	// machine. It used to be allowed to land back on home here, which is exactly
	// the defect that let the built binary ship a `tab` that did nothing at all
	// (placemouse.go's [app.walkPage]).
	drive(t, a, key("tab"))
	if a.page == pageHome {
		t.Fatal("tab from home stayed on home")
	}
	// alt+5 IS THE SPEND PLACE WHEREVER YOU ARE STANDING.
	drive(t, a, key("alt+5"))
	if a.page != pageSpend {
		t.Fatalf("alt+5 did not open the spend place: the router is standing on %q", a.page.word())
	}
	if text := placeFrameText(a); !strings.Contains(text, "What this machine has cost") {
		t.Fatalf("the spend place does not say what it is for:\n%s", text)
	}
	// AND shift+tab IS THE SAME CIRCLE WALKED BACK.
	drive(t, a, key("shift+tab"))
	if a.page == pageSpend {
		t.Fatal("shift+tab from the spend place stayed on it")
	}
}

// EVERY PLACE OPENS ON AN EMPTY MACHINE, and each one spends the frame saying
// what it is for.
//
// THIS IS THE DEFECT THE OWNER FOUND BY RUNNING THE BINARY. On a fresh home the
// tab bar drew all seven words and three of the keys did nothing at all: tasks
// refused with no task in the world, standing refused with nothing standing, and
// memory refused with no store behind it — which on a fresh machine is all three
// of them, every time. SCREEN 1f's preamble is the law they broke: an almost-
// empty place is the best teacher on the machine.
//
// It is ONE LOOP OVER THE REGISTRY and not seven cases, so a place added later
// is covered by having been added to [pages].
func TestEveryPlaceOpensOnAnEmptyMachine(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.app("")
	a.width, a.height = 120, 40
	// EVERY SEAM IS NIL AND NOTHING IS ON THE DISK. This is the machine somebody
	// has just installed aforge on, which is the only machine this test is about.
	a.memory, a.stands, a.places = nil, StandingSeam{}, nil
	a.openHome()
	for _, id := range pages() {
		a.showPage(id)
		if a.page != id {
			t.Fatalf("%s did not take the band: the router is standing on %q", id.word(), a.page.word())
		}
		if !a.pageShowing() {
			t.Fatalf("the router says %q and nothing is on the frame", id.word())
		}
		// AND THE FRAME IS THE WHOLE TERMINAL WITH THE PLACE'S OWN WORD ON IT.
		text := placeFrameText(a)
		if lines := strings.Split(text, "\n"); len(lines) != a.height {
			t.Fatalf("the empty %s place drew %d rows into %d", id.word(), len(lines), a.height)
		}
		if !strings.Contains(text, id.word()) {
			t.Fatalf("the empty %s place does not draw its own tab:\n%s", id.word(), text)
		}
	}
}

// AND NOTHING IS EVER PUT BACK, because nothing refuses.
//
// The router used to close what was standing, ask the place to open, and — when
// it would not — re-open what it had just closed. That path is gone with the
// refusals it existed for ([app.showPage]), and this is what replaced the test
// that pinned it: whatever place is asked for is the place a person is left on,
// on a machine with nothing in any of them.
func TestNothingIsEverPutBackBecauseNothingRefuses(t *testing.T) {
	lab := newHomeLab(t)
	a := lab.app("")
	a.width, a.height = 120, 40
	a.memory, a.stands, a.places = nil, StandingSeam{}, nil
	a.openHome()
	for _, id := range []page{pageMemory, pageStanding, pageTasks} {
		was := a.page
		a.showPage(id)
		if a.page == was && was != id {
			t.Fatalf("%s put back %q", id.word(), was.word())
		}
		if a.at(pageHome) && id != pageHome {
			t.Fatalf("%s left home standing under it", id.word())
		}
	}
}

// ── the frame every place is drawn in ───────────────────────────────────────

// THE FRAME IS EXACTLY THE WHOLE TERMINAL, on every place and at every width. It
// is home's own law, and the router is what made it every place's.
//
// IT IS NOT HERE ANY MORE — IT IS ONE LOOP OVER THE REGISTRY. This test named
// three of the seven places and five widths; the law it protects is about ALL of
// them, and a place added later was covered by nobody remembering to add it to a
// list. It is [TestEveryPlaceTakesExactlyTheWholeFrameAtEveryWidth] in
// placelaws_test.go, over every registered place at six widths — [tierPhone]
// among them, which this one never reached for the four places it did not name.

// THE TAB BAR GIVES UP WORDS IN A STATED ORDER RATHER THAN BEING CUT IN HALF. A
// bar trimmed mid-word is a bar lying about how many places there are.
func TestTheTabBarFoldsRatherThanBeingCut(t *testing.T) {
	a := placeApp(t)
	wide := plain(a.placeTabBar(160, false, a.pal))
	for _, id := range pages() {
		if !strings.Contains(wide, id.word()) {
			t.Fatalf("the wide bar is missing %q: %q", id.word(), wide)
		}
	}
	narrow := plain(a.placeTabBar(24, false, a.pal))
	if ansi.StringWidth(narrow) > 24 {
		t.Fatalf("the narrow bar runs past its frame: %q", narrow)
	}
	// AND WHAT SURVIVES IS THE PLACE YOU ARE STANDING IN. Everything else is
	// something you can still reach; this is the one fact the bar exists for.
	if !strings.Contains(narrow, a.page.word()) {
		t.Fatalf("the narrow bar dropped the place you are on: %q", narrow)
	}
}

// THE COMPOSER IS ON EVERY PLACE AND IT ALWAYS SAYS WHERE IT WILL LAND. "Start a
// task from anywhere" is only true if the verb says where anywhere is.
func TestTheComposerAndItsScopeChipAreOnEveryPlace(t *testing.T) {
	a := placeApp(t)
	// A FRAME WIDE ENOUGH FOR BOTH HALVES OF THE BOX ROW. The chip is dropped
	// rather than crowding the sentence beside it (pages.go's [app.placeChipped]),
	// and this suite's own temp directories are seventy cells of path — so a
	// hundred and twenty columns is a frame where the chip's absence would be
	// correct and would prove nothing.
	a.width, a.height = 200, 30
	for _, id := range []page{pageHome, pageSpend, pageSearch} {
		a.showPage(id)
		text := placeFrameText(a)
		if !strings.Contains(text, placeScopeWord+" ") {
			t.Fatalf("the %s place draws no scope chip:\n%s", id.word(), text)
		}
	}
	// AND WHAT IS TYPED ON ONE PLACE IS STILL THERE ON THE NEXT. A composer that
	// forgot on every tab press would be seven boxes rather than one line.
	a.showPage(pageSpend)
	drive(t, a, key("c"), key("u"), key("t"))
	drive(t, a, key("tab"))
	if got := strings.TrimSpace(a.compose.String()); got != "cut" {
		t.Fatalf("the composer lost what was typed across a tab: %q", got)
	}
}

// ── the map ─────────────────────────────────────────────────────────────────

// alt+. DRAWS THE MAP IN THE CELLS THAT WERE ALREADY THERE, and the next key
// takes it away and then does what it was always going to do. A terminal cannot
// see a held modifier, so the mockup's "hold alt" is one chord that lasts one
// keystroke.
func TestTheMapDrawsInTheCellsThatWereAlreadyThere(t *testing.T) {
	a := placeApp(t)
	before := strings.Split(placeFrameText(a), "\n")
	drive(t, a, key("alt+."))
	if !a.mapShowing {
		t.Fatal("alt+. drew no map")
	}
	after := strings.Split(placeFrameText(a), "\n")
	if len(before) != len(after) {
		t.Fatalf("the map moved the frame: %d rows became %d", len(before), len(after))
	}
	// THE NUMBERS ARE ON THE TABS.
	if bar := after[1]; !strings.Contains(bar, "1 home") || !strings.Contains(bar, "7 settings") {
		t.Fatalf("the map put no numbers on the tab bar: %q", bar)
	}
	// AND THE CHORD LIST IS THE HINT LINE.
	if last := after[len(after)-1]; !strings.Contains(last, "→ verbs") {
		t.Fatalf("the map did not become the hint line: %q", last)
	}
	// AND THE NEXT KEY PUTS IT AWAY.
	drive(t, a, key("x"))
	if a.mapShowing {
		t.Fatal("the map outlived the next keystroke")
	}
	if got := a.home.box.String(); got != "x" {
		t.Fatalf("the key that dismissed the map was also swallowed: %q", got)
	}
}

// ── the verb strip ──────────────────────────────────────────────────────────

// A BARE LETTER IS A VERB ONLY WHILE ITS STRIP IS ON SCREEN, and nowhere else on
// this surface. This is the clause the whole key law turns on.
func TestALetterIsAVerbOnlyWhileTheStripIsDrawn(t *testing.T) {
	a := placeApp(t)
	// With no strip up, every letter is a character — including the ones the
	// strips spend.
	for _, letter := range []string{"p", "s", "n", "u", "e", "f"} {
		drive(t, a, key(letter))
	}
	if got := a.home.box.String(); got != "psnuef" {
		t.Fatalf("a letter did something other than type: %q", got)
	}
}

// AND `→` OPENS THE STRIP ONLY WHERE THE ROW HAS VERBS. Everywhere else the
// arrow keeps every meaning it already had, which is what makes this a new claim
// on the key rather than a seizure of it.
//
// A CONVERSATION WITH AN ADDRESS HAS VERBS AND ONE WITHOUT HAS NONE. The strip's
// verbs are the READING's — put it away, and the three doors that need a folder
// to open (switcher.go's [switcherVerbsFor]) — so a row the world recorded no
// workspace for offers nothing, and the arrow goes on meaning what it meant.
func TestTheArrowOnlyOpensAStripWhereTheRowHasVerbs(t *testing.T) {
	a := placeApp(t)
	a.home.box.reset()
	a.home.build()
	drive(t, a, key("right"))
	if !a.strip.open {
		t.Fatal("a conversation with a folder of its own offered no verbs")
	}
	words := ""
	for _, v := range a.strip.verbs {
		words += string(v.key) + " " + v.word + " · "
	}
	for _, want := range []string{"a put it away", "t new chat here", "o open folder", "c copy path"} {
		if !strings.Contains(words, want) {
			t.Fatalf("the strip is missing %q: %s", want, words)
		}
	}
	// AND A ROW WITH NO ADDRESS AT ALL OFFERS NOTHING, which is what keeps a
	// letter safe: the strip cannot offer a verb the row has no way to perform.
	drive(t, a, key("esc"))
	bare := switcherRow{kind: switcherConversation, title: "Nowhere"}
	if got := switcherVerbsFor(bare); len(got) != 1 || got[0].word != "put it away" {
		t.Fatalf("an addressless row offered %v", got)
	}
}

// ── typing offers places (SCREEN 1g) ────────────────────────────────────────

// TYPING OFFERS PLACES BESIDE CHATS, a place ranks first, and each result says
// what kind of thing it is. Nobody has to be told the places exist twice.
func TestTypingOffersAPlaceBesideTheConversations(t *testing.T) {
	a := placeApp(t)
	for _, r := range "sta" {
		drive(t, a, key(string(r)))
	}
	text := placeFrameText(a)
	if !strings.Contains(text, bandFoldGlyph+" standing") {
		t.Fatalf("typing `sta` offered no place:\n%s", text)
	}
	if !strings.Contains(text, placeRowWord) {
		t.Fatalf("the offered place does not say what kind of thing it is:\n%s", text)
	}
	// AND ENTER ON IT GOES THERE.
	at := -1
	for i, line := range a.home.lines {
		if line.kind == homePlace {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the offered place is not a row of the column")
	}
	a.home.cursor = at
	drive(t, a, key("enter"))
	if a.page != pageStanding && a.page != pageHome {
		t.Fatalf("enter on the standing place landed on %q", a.page.word())
	}
}

// A PLACE OUTRANKS EVERY CONVERSATION THE SAME WORDS MATCH (SCREEN 1g:
// "Places rank first when the words match").
//
// FIRST MEANS NEAREST THE BOX, because this column is a DROP-UP: home.go's own
// law over [homeView.buildWorld] says a ranked list read upward out of a box has
// to put its best answer last, or the row somebody wants is the furthest one
// from the key they reach for. So the assertion is a position — the offered
// place is below every conversation row on the screen and above the two rows
// that do something with the SENTENCE — and it is written as a position rather
// than as a line index so that a project heading or a blank appearing between
// them cannot make it pass for the wrong reason.
func TestAPlaceOutranksEveryConversationTheWordsAlsoMatch(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	// Both conversations match `sta` on their own titles, so the place is not
	// winning by being the only row on the list.
	mine := lab.session("-alpha", "aaaa000000000001", "standup notes",
		lab.workspace("alpha"), now.Add(-2*time.Minute))
	lab.session("-beta", "bbbb000000000001", "stacking the deck",
		lab.workspace("beta"), now.Add(-3*time.Hour))
	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.openHome()
	for _, r := range "sta" {
		drive(t, a, key(string(r)))
	}
	place, lastChat, ask, action := -1, -1, -1, -1
	for i, line := range a.home.lines {
		switch line.kind {
		case homePlace:
			place = i
		case homeSession:
			lastChat = i
		case homeAskHere:
			ask = i
		case homeAction:
			action = i
		}
	}
	if place < 0 || lastChat < 0 {
		t.Fatalf("the drop-up offered %d place rows and %d conversation rows", place, lastChat)
	}
	if place < lastChat {
		t.Fatalf("the place is drawn above a conversation it outranks: place at %d, last chat at %d\n%s",
			place, lastChat, placeFrameText(a))
	}
	// AND THE TWO ROWS THAT ACT ON THE SENTENCE STAY UNDER IT. They are one
	// cluster against the box and are not results at all ([homeAction]).
	if ask < place || action < ask {
		t.Fatalf("the sentence cluster moved: place %d, ask here %d, start %d", place, ask, action)
	}
	// AND THE ROW SAYS WHAT IS BEHIND IT, not just what kind of thing it is.
	if !strings.Contains(placeFrameText(a), placeRowWord) {
		t.Fatalf("the offered place does not say what kind of thing it is:\n%s", placeFrameText(a))
	}
}

// AND WITH NO PLACE MATCHED THE CONVERSATIONS KEEP THE LIST TO THEMSELVES. A
// drop-up that grew a place row for words no place answers to would be the
// surface answering a question nobody asked.
func TestWordsNoPlaceAnswersToOfferNoPlaceRow(t *testing.T) {
	a := placeApp(t)
	for _, r := range "pick" {
		drive(t, a, key(string(r)))
	}
	for _, line := range a.home.lines {
		if line.kind == homePlace {
			t.Fatalf("`pick` offered the %q place", line.project)
		}
	}
	if !strings.Contains(placeFrameText(a), "Porting the Picker") {
		t.Fatalf("the conversation the words match is not on the list:\n%s", placeFrameText(a))
	}
}

// AND THE OFFERED PLACE SAYS WHAT IS BEHIND IT — `a place · 2 orders, 1 fired
// today`, which is 1g's own row.
//
// THE CLAUSE IS THE PLACE'S OWN ANSWER and this test asks the place for it as
// well as reading the screen, so a row that drew the words by accident — out of
// a title, out of the tab bar — cannot pass. And the firing half is ABSENT on a
// day nothing fired, which is the emptiness law on a margin four cells wide.
func TestAnOfferedPlaceSaysWhatIsBehindIt(t *testing.T) {
	band := &standBand{}
	lab := newHomeLab(t)
	now := time.Now()
	dir := lab.workspace("alpha")
	mine := lab.session("-alpha", "aaaa000000000001", "porting the picker", dir, now.Add(-2*time.Minute))
	fired := bandItem("one", "tell me when CI goes red", dir, standing.WhenEvery, "every twenty minutes")
	// TODAY IS THE DAY THE TEST RUNS ON, so the firing is stamped at the instant
	// the fixture is built rather than an hour back — an hour back is yesterday
	// on a run that starts just after midnight, and a test that fails once a day
	// is a test nobody believes.
	fired.LastFired = now
	quiet := bandItem("two", "check the release feed", dir, standing.WhenEvery, "every morning at nine")
	band.items = []standing.Item{fired, quiet}
	a := lab.app(mine)
	band.wire(a)
	a.width, a.height = 120, 30
	a.openHome()
	for _, r := range "sta" {
		drive(t, a, key(string(r)))
	}
	if got := (placeStanding{}).summary(a); got != "2 orders, 1 fired today" {
		t.Fatalf("the standing place says %q is behind it", got)
	}
	text := placeFrameText(a)
	if !strings.Contains(text, placeRowWord+" · 2 orders, 1 fired today") {
		t.Fatalf("the offered place does not say what is behind it:\n%s", text)
	}
	// AND A DAY NOTHING FIRED ON SAYS ONLY WHAT IS THERE.
	band.items = []standing.Item{quiet}
	a.refreshHome()
	if got := (placeStanding{}).summary(a); got != "1 order" {
		t.Fatalf("a place with one quiet order says %q", got)
	}
	// AND A MACHINE WITH NOTHING STANDING ON IT SAYS NOTHING AT ALL.
	band.items = nil
	a.refreshHome()
	if got := (placeStanding{}).summary(a); got != "" {
		t.Fatalf("a place with nothing in it says %q", got)
	}
}

// A PREFIX THAT FITS TWO PLACES OFFERS NEITHER, because offering the first would
// be the surface guessing — and a query with a space in it is a sentence rather
// than a name.
func TestAnAmbiguousPrefixOffersNoPlaceAtAll(t *testing.T) {
	if found := placeMatches("s"); len(found) < 2 {
		t.Fatalf("`s` should fit several places, got %d", len(found))
	}
	if _, ok := parsePageWord("s"); ok {
		t.Fatal("`s` resolved to one place")
	}
	if found := placeMatches("standing up a watch"); len(found) != 0 {
		t.Fatalf("a sentence offered %d places", len(found))
	}
	if found := placeMatches("~/aforge-v2"); len(found) != 0 {
		t.Fatalf("a path offered %d places", len(found))
	}
}

// ── the palette ─────────────────────────────────────────────────────────────

// WAITING ON YOU IS ONE COLOUR ON THIS SCREEN. Home used to say it in two — the
// violet question hue on the strip and the amber on the finished-needs-your-look
// glyph — and the wave settled both on the amber (styles.go's [hueWarn]).
func TestWaitingOnYouIsOneColourOnHome(t *testing.T) {
	a := placeApp(t)
	pal := a.pal
	if pal.warnBold(homeAskGlyph) == pal.askBold(homeAskGlyph) {
		t.Fatal("the two hues are the same colour, so this test proves nothing")
	}
	// The one place the mark is painted now is the switcher's own row, where a
	// row that needs a person wears the amber and nothing else on the screen
	// does (switcher.go's [switcherPaintRow]).
	row := switcherRow{kind: switcherConversation, title: "Asking", needs: true}
	if !strings.Contains(switcherPaintRow(row, 60, pal, false, switcherPaint{}), pal.warn(tokens.GlyphNeedsHuman)) {
		t.Fatal("the needs-you row is not the waiting-on-you hue")
	}
	// AND MONEY HAS A HUE OF ITS OWN, which is not the hue of a finished tick.
	if pal.money("$1.00") == pal.add("$1.00") {
		t.Fatal("money and landed work are painted the same colour")
	}
}

// ── the seam ────────────────────────────────────────────────────────────────

// countingPlaces is a stand-in for the per-place look stamps another lane owns.
type countingPlaces map[string]int

func (c countingPlaces) ChangedIn(place string) int { return c[place] }

// A TAB WEARS A COUNT WHEN SOMETHING IN IT CHANGED, and only then — and only
// where a count would mean anything at all.
func TestATabWearsTheCountTheSeamGivesIt(t *testing.T) {
	a := placeApp(t)
	a.places = countingPlaces{
		pageTasks.word():    2,
		pageSpend.word():    9,
		pageStanding.word(): 0,
	}
	bar := plain(a.placeTabBar(160, false, a.pal))
	if !strings.Contains(bar, "tasks 2") {
		t.Fatalf("the tasks tab does not wear its count: %q", bar)
	}
	if strings.Contains(bar, "spend 9") {
		t.Fatalf("spend is a sum and wears a count anyway: %q", bar)
	}
	if strings.Contains(bar, "standing 0") {
		t.Fatalf("a place with nothing new wears a zero: %q", bar)
	}
}

// The compiler is what keeps this honest: the seam is an interface, and a thing
// that answers it is a thing tui3 can be handed without knowing what wrote it.
var _ placeCounts = countingPlaces(nil)

// And the session package's own look stamp is untouched by this wave, which is
// stated here because it is the seam the counts will eventually be built on.
var _ = session.LastLook

// ── the owner's path: every key that moves between places, from everywhere ──

// THE SEVEN KEYS THAT JUMP AND THE TWO THAT WALK ARE TRUE FROM EVERY PLACE, and
// that includes settings — which has a tab bar of its own under the router's —
// and the search place, whose composer takes every printable key.
//
// It is a loop over the registry crossed with itself rather than a list of
// pairs, because "does `alt+4` work from settings" is the question nobody thinks
// to ask until a key does nothing.
func TestEveryPlaceReachesEveryOtherPlace(t *testing.T) {
	a := placeApp(t)
	for _, from := range pages() {
		for _, to := range pages() {
			a.showPage(from)
			drive(t, a, key("alt+"+itoa(placeAt(to)+1)))
			if a.page != to {
				t.Fatalf("alt+%d from %s landed on %q", placeAt(to)+1, from.word(), a.page.word())
			}
		}
		// AND THE CIRCLE WALKS BOTH WAYS FROM HERE.
		a.showPage(from)
		drive(t, a, key("tab"))
		if want := nextPage(from, false); a.page != want {
			t.Fatalf("tab from %s landed on %q, not %q", from.word(), a.page.word(), want.word())
		}
		a.showPage(from)
		drive(t, a, key("shift+tab"))
		if want := nextPage(from, true); a.page != want {
			t.Fatalf("shift+tab from %s landed on %q, not %q", from.word(), a.page.word(), want.word())
		}
		// AND THE MAP DRAWS FROM HERE TOO, which is the one chord that has to
		// survive a place claiming every printable key for its filter.
		a.showPage(from)
		drive(t, a, key("alt+."))
		if !a.mapShowing {
			t.Fatalf("alt+. drew no map on %s", from.word())
		}
		drive(t, a, key("esc"))
	}
}

// placeAt is a place's position in [pages], which is the digit that jumps to it.
func placeAt(id page) int {
	for i, at := range pages() {
		if at == id {
			return i
		}
	}
	return -1
}

// AND THE NUMBERS WORK FROM THE CONVERSATION, which is the screen a person
// spends most of the day on and was the one surface they did not work from.
//
// `tab` and the shift-arrows deliberately do NOT: in the chat those already
// belong to path completion and to the caret, and taking them would be a router
// seizing keys somebody has muscle memory for. The digits are the spare class.
func TestTheNumbersOpenAPlaceFromTheConversationToo(t *testing.T) {
	a := placeApp(t)
	drive(t, a, key("esc"))
	if a.at(pageHome) {
		t.Fatal("esc did not put the conversation back")
	}
	drive(t, a, key("alt+2"))
	if a.page != pageTasks || !a.at(pageTasks) {
		t.Fatalf("alt+2 from the conversation landed on %q (open %v)", a.page.word(), a.at(pageTasks))
	}
	drive(t, a, key("esc"))
	drive(t, a, key("alt+3"))
	if a.page != pageStanding || !a.at(pageStanding) {
		t.Fatalf("alt+3 from the conversation landed on %q", a.page.word())
	}
	// AND `tab` IS STILL THE CONVERSATION'S OWN KEY THERE.
	drive(t, a, key("esc"))
	page := a.page
	drive(t, a, key("tab"))
	if a.page != page || a.at(pageTasks) || a.at(pageStanding) {
		t.Fatal("tab in the conversation opened a place")
	}
}

// THE TAB BAR CARRIES ALL SEVEN WORDS AT EVERY WIDTH A PERSON ACTUALLY USES.
// The ladder that gives words up is for terminals narrower than any of these
// ([app.placeTabBar]); at 80 columns and up nothing is dropped.
func TestTheTabBarCarriesAllSevenAtEveryUsableWidth(t *testing.T) {
	a := placeApp(t)
	for _, width := range []int{80, 120, 200} {
		bar := plain(a.placeTabBar(width, false, a.pal))
		for _, id := range pages() {
			if !strings.Contains(bar, id.word()) {
				t.Fatalf("at %d columns the bar has no %q: %q", width, id.word(), bar)
			}
		}
	}
}
