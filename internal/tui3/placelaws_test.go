package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/store"
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
		if back, ok := parsePageWord(word); !ok || back != id {
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

// LAW 4 · A PLACE NEVER READS THE DISK ON A DRAW.
//
// Every fact a place shows was read on the way in or on the three-second beat
// and is held in that place's own cache; a body builds rows out of what those
// left behind. A draw that reached a seam would reach it on every keystroke, on
// every resize and on every frame of an animation — which is how a still page
// ends up walking a directory sixty times a second.
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
			if a.stands.Items != nil {
				a.stands.Items = func(string) []standing.Item {
					t.Fatalf("the %s place read the standing store while drawing its body", place.id.word())
					return nil
				}
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
