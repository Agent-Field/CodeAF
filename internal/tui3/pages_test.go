package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
func TestTheSevenPlacesAreOneList(t *testing.T) {
	all := pages()
	if len(all) != 7 {
		t.Fatalf("there are %d places, and the design has seven", len(all))
	}
	seen := map[string]bool{}
	for i, id := range all {
		word := id.word()
		if word == "" {
			t.Fatalf("the place at position %d has no word", i+1)
		}
		if seen[word] {
			t.Fatalf("two places are called %q", word)
		}
		seen[word] = true
		// THE NUMBER IS THE POSITION AND NOTHING ELSE.
		got, ok := placeDigit("alt+" + itoa(i+1))
		if !ok || got != id {
			t.Fatalf("alt+%d does not reach %q", i+1, word)
		}
		// AND THE WORD REACHES IT TOO, which is what lets the typed surface
		// offer places beside conversations (SCREEN 1g).
		if back, ok := parsePageWord(word); !ok || back != id {
			t.Fatalf("typing %q does not reach its own place", word)
		}
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
	drive(t, a, key("tab"))
	if a.page != pageTasks && a.page != pageHome {
		t.Fatalf("tab from home landed on %q", a.page.word())
	}
	// alt+5 IS THE SPEND PLACE WHEREVER YOU ARE STANDING.
	drive(t, a, key("alt+5"))
	if a.page != pageSpend || !a.teach.open {
		t.Fatalf("alt+5 did not open the spend place (page %q, teach %v)", a.page.word(), a.teach.open)
	}
	if text := placeFrameText(a); !strings.Contains(text, "What this machine has cost") {
		t.Fatalf("the spend place does not say what it is for:\n%s", text)
	}
	// AND shift+tab IS THE SAME CIRCLE WALKED BACK.
	drive(t, a, key("shift+tab"))
	if a.page != pageMemory && a.page != pageSpend {
		t.Fatalf("shift+tab from spend landed on %q", a.page.word())
	}
}

// A PLACE THAT REFUSES TO OPEN LEAVES THE BAND WHERE IT WAS. A tab bar pointing
// at a room nobody is standing in is worse than no tab bar.
func TestARefusedPlaceDoesNotMoveTheBand(t *testing.T) {
	a := placeApp(t)
	// Memory is off on this surface, so the place cannot open.
	if a.memory != nil {
		t.Skip("this surface has a memory store, so there is nothing to refuse")
	}
	a.showPage(pageMemory)
	if a.page != pageHome {
		t.Fatalf("a refused place took the band anyway: %q", a.page.word())
	}
	if !a.home.open {
		t.Fatal("a refused place closed the one that was up")
	}
}

// ── the frame every place is drawn in ───────────────────────────────────────

// THE FRAME IS EXACTLY THE WHOLE TERMINAL, on every place and at every width,
// with no line running past the edge. It is home's own law, and the router is
// what made it every place's.
func TestEveryPlaceIsExactlyTheWholeFrame(t *testing.T) {
	for _, width := range []int{44, 60, 80, 120, 200} {
		a := placeApp(t)
		a.width, a.height = width, 26
		for _, id := range []page{pageHome, pageSpend, pageSearch} {
			a.showPage(id)
			lines := strings.Split(placeFrameText(a), "\n")
			if len(lines) != a.height {
				t.Fatalf("at %d the %s place drew %d rows into %d",
					width, id.word(), len(lines), a.height)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatalf("at %d the %s place overflows: %q", width, id.word(), line)
				}
			}
		}
	}
}

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
	if !strings.Contains(switcherPaintRow(row, 60, pal, false, false, false), pal.warn(tokens.GlyphNeedsHuman)) {
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
