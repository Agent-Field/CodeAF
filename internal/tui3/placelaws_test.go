package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/store"
)

// ── THE LAWS THE PLACES ARE BUILT ON ────────────────────────────────────────
//
// docs/design/home-rethink/ARCHITECTURE.md states six, and every one of them is
// a shape a person could not see going wrong until a room was already broken.
// They are pinned here because the whole argument for the `place` interface is
// that a room added later cannot half-exist — and "cannot" has to be something
// the build says out loud rather than something a reviewer remembers.

// placeSourceFiles is every non-test Go file of this package.
func placeSourceFiles(t *testing.T) []string {
	t.Helper()
	found, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("could not list the package: %v", err)
	}
	kept := make([]string, 0, len(found))
	for _, name := range found {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		kept = append(kept, name)
	}
	if len(kept) < 50 {
		t.Fatalf("only %d source files — this test is looking in the wrong directory", len(kept))
	}
	return kept
}

// LAW 1 · NOTHING SWITCHES ON A PAGE ID OUTSIDE THE REGISTRY.
//
// This is the whole of what the interface bought. The seven rooms used to be
// arms of eleven switches across five files — the frame, the note line, the
// hint, the composer, the alt letters, the time window, the verbs, the counts
// and the three pointer gestures — so a place added later was eleven edits, and
// a place that answered ten of them was a room with no pointer or a tab that
// never wore its number. Both shipped.
//
// A FEATURE IS A NEW PLACE FILE, A NEW READING FUNCTION OR A NEW SEAM — NEVER A
// NEW `case`. That sentence is ARCHITECTURE.md's and this is what makes it true.
func TestNothingSwitchesOnAPageIdOutsideTheRegistry(t *testing.T) {
	// The three shapes a dispatch takes in Go, spelled as this codebase spells
	// them. `a.at(pageHome)` is deliberately NOT one of them: a predicate asking
	// "am I up" is one fact read once, where a switch over the ids is this
	// router's job done somewhere else.
	shapes := []string{"case page", "switch a.page", "== page", "!= page"}
	for _, name := range placeSourceFiles(t) {
		if name == "pages.go" {
			// The registry's own file may know every place by name — it is the one
			// file that is allowed to, and in fact no longer needs to.
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("could not read %s: %v", name, err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			code := line
			if at := strings.Index(code, "//"); at >= 0 {
				// A COMMENT MAY SAY `case pageHome` WHILE TELLING THE STORY OF THE
				// switch that used to be there, and several of them do. What is
				// forbidden is the code.
				code = code[:at]
			}
			for _, shape := range shapes {
				if strings.Contains(code, shape) {
					t.Errorf("%s switches on a page id outside the registry: %q", name, strings.TrimSpace(line))
				}
			}
		}
	}
}

// LAW 2 · EVERY PLACE IS REGISTERED ONCE AND IS IN THE TAB ORDER.
//
// The registry and [placeOrder] are two lists that have to be the same list. A
// place in the registry and not in the order is a room with no tab and no
// number; a place in the order and not in the registry is a tab that opens
// nothing, which is exactly what the owner met on a fresh machine.
func TestEveryPlaceIsRegisteredOnceAndInTabOrder(t *testing.T) {
	if len(placeRegistry) != len(placeOrder) {
		t.Fatalf("%d places are registered and %d are on the bar", len(placeRegistry), len(placeOrder))
	}
	seen := map[page]bool{}
	words := map[string]bool{}
	for at, id := range placeOrder {
		pl := placeFor(id)
		if pl == nil {
			t.Fatalf("position %d of the bar names a place nothing registered", at+1)
		}
		if pl.id() != id {
			t.Fatalf("the place filed under %q calls itself %q", id.word(), pl.id().word())
		}
		if seen[id] {
			t.Fatalf("%q is on the bar twice", id.word())
		}
		seen[id] = true
		word := pl.word()
		if word == "" {
			t.Fatalf("the place at position %d has no word", at+1)
		}
		if words[word] {
			t.Fatalf("two places are called %q", word)
		}
		words[word] = true
		// AND THE NUMBER IS THE POSITION AND NOTHING ELSE, which is the promise
		// `alt+1`…`alt+7` and the map both make.
		if got, ok := placeDigit("alt+" + itoa(at+1)); !ok || got != id {
			t.Fatalf("alt+%d does not reach %q", at+1, word)
		}
		// AND THE WORD REACHES IT TOO, which is what lets the typed surface offer
		// places beside conversations (SCREEN 1g).
		if back, ok := parsePageWord(word); id != pageChats && (!ok || back != id) {
			t.Fatalf("typing %q does not reach its own place", word)
		}
	}
	// AND THE CONVERSATION IS NOT A PLACE. [pageNone] is the zero value and it
	// has no room behind it, which is what makes `a.page` one answer to "is
	// anything up" rather than a label on six flags.
	if placeFor(pageNone) != nil {
		t.Fatal("the conversation registered itself as a place")
	}
}

// LAW 6 · ONE PLACE, ONE FILE.
//
// A room's struct, its handle, its body, its keys and its verbs live in
// `place_<word>.go` and nowhere else, so that reading a place is opening one
// file — and so that the `init` which registers it sits beside the thing it is
// registering.
func TestOnePlaceOneFile(t *testing.T) {
	for _, id := range pages() {
		name := "place_" + id.word() + ".go"
		body, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("the %s place has no %s: %v", id.word(), name, err)
			continue
		}
		if !strings.Contains(string(body), "registerPlace(") {
			t.Errorf("%s does not register its own place", name)
		}
	}
	// AND NOTHING ELSE REGISTERS ONE. A registration in another file is a room
	// whose front door is in somebody else's house.
	for _, name := range placeSourceFiles(t) {
		if strings.HasPrefix(name, "place_") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("could not read %s: %v", name, err)
		}
		if strings.Contains(string(body), "registerPlace(place") {
			t.Errorf("%s registers a place that is not its own", name)
		}
	}
}

// LAW 7 · NO PLACE FILE MENTIONS THE TAB BAR.
//
// The bar is drawn on all seven places, in the same cells, by one function, and
// the cursor that stands on it is frame state (pages.go's [barCursor]). A place
// that held a flag about the cursor having left it — or that bound `←`/`→` for
// the bar, or drew a word of it — would be a seventh answer to a question the
// router already answers, and the seventh would be the one that forgot.
//
// IT IS THE SAME ARGUMENT AS LAW 1 asked of a feature rather than of a switch: a
// feature is a new place file, a new reading function or a new seam — never a
// new `case` — and this one is none of the three, because it belongs to the
// frame. [TestTheBarIsARowOnEveryPlace] is the other half: what the frame owes
// every place, one loop over the registry.
func TestNoPlaceFileMentionsTheBar(t *testing.T) {
	// `bar` ON ITS OWN IS NOT THE WORD. Every place answers a `bar(a, width)` —
	// the thumb foot that stands in for the hint line at [tierPhone] — and home
	// keeps a `bar []hudSpan` of its own for the phone tier's action bar. What is
	// forbidden is the ROUTER'S bar reaching into a place file, and each of these
	// names one piece of it.
	forbidden := []string{"barCursor", "a.bar.", "barRaise", "barDrop", "barWalk",
		"barEnter", "barReach", "barKey(", "tabHover", "navLine", "navPress"}
	for _, name := range placeSourceFiles(t) {
		if !strings.HasPrefix(name, "place_") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("could not read %s: %v", name, err)
		}
		for at, line := range strings.Split(string(body), "\n") {
			code := line
			if cut := strings.Index(code, "//"); cut >= 0 {
				// A COMMENT MAY SAY WHERE THE BAR LIVES, and one of them does:
				// place_home.go tells the story of the rest state the bar
				// replaced. What is forbidden is the code.
				code = code[:cut]
			}
			for _, word := range forbidden {
				if strings.Contains(code, word) {
					t.Errorf("%s:%d reaches into the router's tab bar: %q",
						name, at+1, strings.TrimSpace(line))
				}
			}
		}
	}
}

// LAW 5 · THE READING LAYERS SEE NO *app.
//
// A reading is (data, window, width, palette) → rows. It cannot start a clock,
// cannot read a disk and cannot ask the surface a question — which is what makes
// it testable with fixtures at five widths and what stops a paint turning into a
// directory walk. The moment one of them takes an `*app` the layer has collapsed
// and nobody notices until a frame is slow.
func TestTheReadingLayersImportNoApp(t *testing.T) {
	readings := []string{
		"switcher.go", "tasksplace.go", "standingplace.go",
		"memoryplace.go", "spendplace.go", "searchplace.go", "placeprose.go",
	}
	fset := token.NewFileSet()
	for _, name := range readings {
		// PARSED AND NOT GREPPED, because these files talk about the law in their
		// own headers — switcher.go's says "Nothing here takes an *app" in as many
		// words — and a test that read comments would fail on the sentence that
		// states the rule it is enforcing.
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("could not parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if ok && ident.Name == "app" {
				t.Errorf("%s names the app at %s — a reading is data in, rows out",
					name, fset.Position(ident.Pos()))
			}
			return true
		})
	}
}

// LAW 3 · EVERY PLACE IS EXACTLY THE WHOLE FRAME, AT EVERY WIDTH.
//
// It is home's own law, and the router is what made it every place's: the frame
// is the terminal, with no line running past the edge and no row short of the
// bottom. It is ONE LOOP OVER THE REGISTRY and not seven copies, so a place
// added later is covered by having been registered.
func TestEveryPlaceTakesExactlyTheWholeFrameAtEveryWidth(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			// FORTY-FOUR IS IN THE LADDER because it is [tierPhone], where home is
			// an inbox and the tasks place's foot is a band rather than a legend.
			// A place that took the frame at every width a person can read and not
			// at the one a thumb holds would be a place with a broken shape on the
			// only screen that cannot spare a row.
			for _, width := range []int{44, 60, 80, 120, 160, 200} {
				a := place.open(t)
				a.width, a.height = width, 26
				lines, _, _, _ := a.placeDraw(placeFor(place.id), width, a.height)
				if len(lines) != a.height {
					t.Fatalf("at %d the %s place drew %d rows into %d",
						width, place.id.word(), len(lines), a.height)
				}
				for at, line := range lines {
					if got := ansi.StringWidth(ansi.Strip(line)); got > width {
						t.Fatalf("at %d the %s place overflows on row %d by %d cells: %q",
							width, place.id.word(), at, got-width, ansi.Strip(line))
					}
				}
			}
		})
	}
}

// LAW 3a · EVERY PLACE SPENDS THE SAME HEAD AND THE SAME FOOT, full or empty.
//
// LAW 3 asks that a place fill the frame; this asks WHERE its edges are. The
// bar, the head rule, the foot rule, the box and the hint are the furniture a
// person's eye finds without looking, and a foot rule that stood one row higher
// on a place with a note than on one without moved under the eye on every `tab`
// — and moved again inside tasks the moment its first piece of work landed
// (PLACES-AUDIT.md finding 1). One loop over the registry and over every place
// that can be empty, at three sizes, and every edge has to be on the same row.
//
// [placeFootRows] is that foot at rest, counted where it is drawn: the blank
// over the rule, the rule with the note on it, the composer's [homeDraftFloor]
// rows and the hint. This law reads the number back off the frame rather than
// keeping a second copy of it, because a law that owned its own arithmetic
// would go on passing after the foot moved.
//
// THE COMPOSER IS THE SAME HEIGHT AT REST AS IN USE, which is what makes this
// number a constant at all. It was one row at rest and grew to three with what
// was typed into it; a box that changed height moved this whole foot under the
// hand, and the law below could only be stated about a resting screen. The
// sizes here are all tall enough to hold that floor ([placeBoxFloor] gives it
// back on a frame that is not).

func TestEveryPlaceSpendsTheSameHeadAndFoot(t *testing.T) {
	type edges struct{ bar, headRule, blank, footRule, box, hint int }
	sizes := [][2]int{{80, 24}, {120, 45}, {180, 45}}
	labs := append(everyPlaceTable(), everyEmptyPlace()...)
	// TWO SHAPES, AND EVERY PLACE HAS ONE OF THEM. Home's foot carries the box —
	// as many rows as the height can afford ([boxFloor]), the rule directly over
	// the first of them; every other place's foot is the blank, the rule and
	// the hint, with no box at all ([placeBareFootRows]): only home starts
	// things (pages.go's [place.box]). Both are derived from the frame's own
	// doors rather than counted out again here.
	wantFor := func(id page, size [2]int) edges {
		height := size[1]
		got := edges{bar: navRow, headRule: 1, blank: placeHeadRows - 1,
			footRule: height - placeFootRowsFor(id, height) + 1, box: -1, hint: height - 1}
		if id == pageHome {
			got.box = height - 1 - boxFloor(height)
		}
		return got
	}
	for _, lab := range labs {
		for _, size := range sizes {
			a := lab.open(t)
			a.width, a.height = size[0], size[1]
			lines, _, _, _ := a.placeDraw(placeFor(lab.id), a.width, a.height)
			rows := make([]string, len(lines))
			for i, line := range lines {
				rows[i] = ansi.Strip(line)
			}
			got := edges{bar: a.tabRow, headRule: -1, blank: -1, footRule: -1, box: -1, hint: len(rows) - 1}
			if len(rows) > 1 && strings.HasPrefix(rows[1], "──") {
				got.headRule = 1
			}
			if strings.TrimSpace(rows[placeHeadRows-1]) == "" {
				got.blank = placeHeadRows - 1
			}
			// THE RULE AND THE BOX ARE FOUND BY COUNTING BACK THROUGH THE
			// COMPOSER'S OWN HEIGHT, not by two fixed offsets. The box is
			// [homeDraftFloor] rows and its FIRST row is the one carrying the
			// prompt, so the rule sits one above that and the rows between the
			// prompt and the hint are the composer's own.
			floor := 0
			if lab.id == pageHome {
				floor = boxFloor(size[1])
			}
			if at := len(rows) - 2 - floor; at >= 0 && strings.HasPrefix(rows[at], "─") {
				got.footRule = at
			}
			if at := len(rows) - 1 - floor; lab.id == pageHome && at >= 0 && strings.HasPrefix(rows[at], " "+prompt) {
				got.box = at
			}
			want := wantFor(lab.id, size)
			if got != want {
				t.Errorf("the %s place at %dx%d puts its edges at %+v, and its shape puts them at %+v\n%s",
					lab.id.word(), size[0], size[1], got, want, strings.Join(rows, "\n"))
			}
		}
	}
}

// LAW 4 · A PLACE NEVER READS THE DISK ON A DRAW — OR ON A KEYSTROKE.
//
// Every fact a place shows was read on the way in or on the three-second beat
// and is held in that place's own cache; a body builds rows out of what those
// left behind. A draw that reached a seam would reach it on every keystroke, on
// every resize and on every frame of an animation — which is how a still page
// ends up walking a directory sixty times a second.
//
// AND THE LAW'S OWN ARGUMENT IS WHY IT COVERS MORE THAN THE BODY NOW. "A draw
// that reached a seam would reach it on every keystroke" is a sentence about
// keystrokes, and for one wave the test only walked `body`, `note` and `hint` —
// so the spend place's window arrows walked the standing store N+1 times per
// press, at key-repeat rate, with a law written against exactly that defect
// passing green. Everything a hand can do without asking for anything to happen
// is fenced here: the window arrows, the alt letters, the verbs a row offers,
// and the three things a pointer does. What is NOT fenced is `enter`, which is
// a person asking for something and is allowed to pay for it.
//
// THE SEAMS ARE MADE TO PANIC RATHER THAN COUNTED. A counter says "it read the
// disk twice" and leaves somebody to argue about whether twice is fine; a panic
// says which seam, on which place, at which width, and there is nothing to argue
// about.
func TestAPlaceNeverReadsTheDiskOnADraw(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			pl := placeFor(place.id)

			// The one syscall home makes about a project folder, and the one it is
			// already held to on a per-reading basis
			// (TestHomeStatsAFolderOncePerReadingAndNotPerFrame).
			was := homeFolderThere
			homeFolderThere = func(where string) bool {
				t.Fatalf("the %s place stat'd %q while drawing its body", place.id.word(), where)
				return false
			}
			t.Cleanup(func() { homeFolderThere = was })

			// AND EVERY SEAM THE DOOR WIRED. A store, a standing document, a
			// full-text index: each is a file or a database, and none of them may
			// be touched between one frame and the next.
			a.memory = panicMemory{t: t, place: place.id.word()}
			// THE STANDING SEAM IS WIRED WHETHER THE LAB WIRED IT OR NOT. A fence
			// that was installed only where the lab had already put a seam asked
			// nothing at all of the labs that leave it nil — which is how the spend
			// place came to walk the whole standing store on every window
			// keystroke with this test green. A place reads `nil` as "there is no
			// store to ask", so a nil seam does not exercise the question; this
			// hands every place a store that is a fault to touch.
			a.stands.Items = func(string) []standing.Item {
				t.Fatalf("the %s place read the standing store on a draw or a keystroke", place.id.word())
				return nil
			}
			a.stands.All = func() []standing.Item {
				t.Fatalf("the %s place walked the standing store on a draw or a keystroke", place.id.word())
				return nil
			}
			a.searchStore = panicSearch{t: t, place: place.id.word()}

			for _, width := range []int{44, 60, 120, 200} {
				a.width = width
				pl.body(a, width, 12)
				// AND THE FOOT TOO, because the note line and the hint are drawn on
				// the same frame and by the same rule.
				pl.note(a, width)
				pl.hint(a)
			}

			// AND EVERY KEY AND GESTURE THAT ONLY MOVES OR RE-GROUPS WHAT IS
			// ALREADY HELD. Each of these repeats — an arrow held down, a wheel
			// spun, a pointer dragged across the rows — so a seam reached from one
			// of them is a seam reached at the speed of a hand.
			for _, key := range []string{"shift+left", "shift+right", "shift+up", "shift+down"} {
				pl.window(a, key)
			}
			for letter := 'a'; letter <= 'z'; letter++ {
				pl.alt(a, letter)
			}
			// The verbs are what `→` draws, and they are asked for on the frame
			// that draws the strip.
			pl.verbs(a)
			pl.rowID(a)
			// THE POINTER RESTING IS HERE AND THE PRESS IS NOT: a press on a row
			// is `enter` on it (pages.go's [place.press]), and a door may read what
			// it opens, as the tab bar's own press does.
			for y := 0; y < 12; y++ {
				pl.hover(a, y)
			}
			pl.wheel(a, 1)
			pl.wheel(a, -1)
			// AND THE ROWS ARE BUILT ONCE MORE AFTERWARDS, because a keystroke
			// that only marked something dirty would otherwise pay for the seam on
			// the next draw instead — which is the same read, one frame later.
			pl.body(a, a.width, 12)
		})
	}
}

// panicMemory is a memory store that is a fault to touch. Every method fails the
// test rather than answering, so a body that asked one question fails on the
// question rather than on a count somebody has to interpret.
type panicMemory struct {
	t     *testing.T
	place string
}

func (p panicMemory) blame(what string) {
	p.t.Fatalf("the %s place asked the memory store to %s while drawing its body", p.place, what)
}

func (p panicMemory) Snapshot(limit int) (store.MemoryShelves, error) {
	p.blame("take a snapshot")
	return store.MemoryShelves{}, nil
}

func (p panicMemory) ForgetMemory(id string) error {
	p.blame("forget a line")
	return nil
}

func (p panicMemory) RestoreMemory(id string) error {
	p.blame("put a line back")
	return nil
}

func (p panicMemory) UpdateMemory(id, title, text string, tags []string) error {
	p.blame("rewrite a line")
	return nil
}

func (p panicMemory) MemoryProvenance(id string) (string, string, time.Time, error) {
	p.blame("look up where a line came from")
	return "", "", time.Time{}, nil
}

func (p panicMemory) ChangedSince(since time.Time) (int, int, error) {
	p.blame("count what changed")
	return 0, 0, nil
}

func (p panicMemory) ListMemories(scope string, limit int) ([]store.Memory, error) {
	p.blame("list what is remembered")
	return nil, nil
}

// panicSearch is the conversation index on the same terms.
type panicSearch struct {
	t     *testing.T
	place string
}

func (p panicSearch) SearchConversations(terms string, limit int) ([]store.ConversationHit, error) {
	p.t.Fatalf("the %s place searched what was said while drawing its body", p.place)
	return nil, nil
}

// A FACT SPELLED TWICE IS A BUG (ARCHITECTURE.md), pinned for the words a strip
// puts in front of a person.
//
// The memory reading carried a second copy of two of them — a `memoryReading.verbs`
// with no caller, naming `e fix the wording` with the letter baked into the word
// while the live strip pairs the letter and the word separately (verbstrip.go:
// `pal.data(string(v.key)) + pal.dim(" "+v.word)`). Revived, it would have drawn
// `e e fix the wording`. Dead code is not inert when it holds a second answer to
// a question somebody will ask again.
//
// The words are STRING LITERALS in the code and quoted in the manual, so this
// counts the literals: exactly one, the constant's own declaration. A comment
// that mentions a verb is not a spelling of it, so the source is parsed rather
// than grepped — the same call [TestTheReadingLayersImportNoApp] makes and for
// the same reason.
func TestEachVerbWordIsSpelledOnce(t *testing.T) {
	words := []string{memoryCardWord, memoryFixWord, memoryForgetWord, memoryUndoWord}
	seen := map[string]int{}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("could not parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, word := range words {
				// CONTAINS AND NOT EQUALS, because the second copy that was here
				// did not spell the word the same way — it was `e fix the wording`,
				// the letter baked into the word. A test that asked for equality
				// would have watched it go past.
				if !strings.Contains(text, word) {
					continue
				}
				seen[word]++
				if text != word || name != "verbstrip.go" {
					t.Errorf("%q is spelled again inside %q at %s — the strip's words live in verbstrip.go and nowhere else",
						word, text, fset.Position(lit.Pos()))
				}
			}
			return true
		})
	}
	for _, word := range words {
		if seen[word] == 0 {
			t.Errorf("%q is spelled nowhere at all", word)
		}
	}
}
