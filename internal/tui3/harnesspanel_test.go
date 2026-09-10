package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// The harness panel and the strip's chip.
//
// Everything here drives the real surface against a real registry in a
// temporary directory: the panel's whole job is to say what is on disk, and a
// test that stubbed the store would be a test of the stub.

// harnessApp is a surface with a registry behind /harness.
func harnessApp(t *testing.T, saved ...subharness.Harness) (*app, *subharness.Store) {
	t.Helper()
	store := subharness.At(t.TempDir())
	for _, one := range saved {
		if _, err := store.Save(one); err != nil {
			t.Fatalf("seed %s: %v", one.Id.Name, err)
		}
	}
	a := newTestApp(&fakeAgent{})
	a.harn = store
	a.width = 100
	return a, store
}

func demoHarness(name, desc string) subharness.Harness {
	return subharness.Harness{
		Id: subharness.Id{Name: name, Desc: desc, Version: 1},
		Program: subharness.Program{
			Nodes: []subharness.Node{
				{Id: "look", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{
					"brief": "look at it", "tools": "read",
				}},
				{Id: "land", Kind: subharness.KindHumanGate, Fields: subharness.Fields{"ask": "land it?"}},
			},
			Edges: []subharness.Edge{{"look", "land"}},
		},
		Whitelist: []string{"read"},
	}
}

// EACH ROW IS THE MARK, THE NAME AND THE VERSION, with what it is for and what
// it has done beside it.
func TestTheHarnessPanelDrawsWhatIsRegistered(t *testing.T) {
	a, _ := harnessApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeLine(t, a, "/harness")
	if !a.harnPanel.open {
		t.Fatal("/harness opened nothing")
	}
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, glyphHarness+" triage-flake v1") {
		t.Fatalf("a row is not a mark, a name and a version:\n%s", screen)
	}
	if !strings.Contains(screen, "chase a flaky test") {
		t.Fatalf("a row does not say what it is for:\n%s", screen)
	}
	// AND A HARNESS NOBODY HAS RUN SAYS NOTHING THERE. `never run` was a column of
	// apologies under every fresh row; the emptiness law says a row with nothing
	// to report reports nothing, and `/subharness` has always drawn it that way.
	if strings.Contains(screen, "never run") || strings.Contains(screen, "0 runs") {
		t.Fatalf("a harness with no history counted its own silence:\n%s", screen)
	}
}

func TestTheHarnessPanelSaysWhatTheHistorySays(t *testing.T) {
	a, store := harnessApp(t, demoHarness("triage-flake", "chase a flaky test"))
	for _, status := range []subharness.Status{subharness.StatusOK, subharness.StatusDeclined} {
		if _, err := store.SaveRun(subharness.Trace{
			Id:      subharness.Id{Name: "triage-flake", Version: 1},
			Started: time.Now().Add(-2 * time.Hour),
			Status:  status,
		}); err != nil {
			t.Fatal(err)
		}
	}
	typeLine(t, a, "/harness")
	screen := strings.Join(plainOverlay(a), "\n")
	// The count is every trace, and the LAST RUN IS SPELLED THE WAY /subharness
	// spells it — the same two words for the same fact about the same program,
	// rather than this door's old `last declined, 2h ago`.
	if !strings.Contains(screen, "2 runs") || !strings.Contains(screen, "2h · incomplete") {
		t.Fatalf("the row does not carry the history in the shared words:\n%s", screen)
	}
	// AND NOT ONE MACHINE WORD FOR HOW IT ENDED. `declined` is the trace's own
	// status; on a row somebody is scanning, a person who said no at a gate did
	// not fail and is not told they did.
	for _, word := range []string{"declined", "cancelled", "intervened", "failed"} {
		if strings.Contains(screen, word) {
			t.Fatalf("the row says %q about a run:\n%s", word, screen)
		}
	}
}

// ENTER PRINTS THE CARD INTO THE CONVERSATION, where prose lives on this
// surface.
func TestEnterOnAHarnessPrintsItsCard(t *testing.T) {
	a, _ := harnessApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeLine(t, a, "/harness")
	drive(t, a, key("enter"))
	if a.harnPanel.open {
		t.Fatal("the panel stayed up over the card it printed")
	}
	screen := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"triage-flake · v1", "look at it", "land it?", "can use · reads files"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the card does not say %q:\n%s", want, screen)
		}
	}
}

// THE RUN HISTORY RIDES WITH IT: "what is this shape" and "what did it do last
// time" are one question asked twice.
func TestTheCardCarriesTheLastRun(t *testing.T) {
	a, store := harnessApp(t, demoHarness("triage-flake", "chase a flaky test"))
	if _, err := store.SaveRun(subharness.Trace{
		Id:      subharness.Id{Name: "triage-flake", Version: 1},
		Started: time.Now(),
		Status:  subharness.StatusDeclined,
		Trail: []subharness.Trail{
			{Step: 1, Id: "look", Kind: subharness.KindAgentLoop, Out: "it is the reconciler"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	typeLine(t, a, "/harness")
	drive(t, a, key("enter"))
	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, "last run") || !strings.Contains(screen, "declined") {
		t.Fatalf("the card does not carry the last run:\n%s", screen)
	}
}

// THE CARD REACHES THE SCREEN WITH ITS OWN LINE STRUCTURE INTACT.
//
// It went through the ordinary note, which re-flows every paragraph to the frame
// — and the card says which step belongs to which lane by INDENTING it, so the
// one thing a person opens the card to read was the one thing this door
// destroyed on the way there. Each line is now fitted where it stands and cut if
// it has to be, never wrapped.
func TestThePrintedCardKeepsItsOwnIndentation(t *testing.T) {
	branching := demoHarness("triage-flake", "chase a flaky test")
	branching.Program = subharness.Program{
		Nodes: []subharness.Node{
			{Id: "pick", Kind: subharness.KindBranch, Fields: subharness.Fields{"when": "contains flaky"}},
			{Id: "rerun", Kind: subharness.KindToolCall, Fields: subharness.Fields{
				"tool": "bash", "args": "go test -run TestFoo -count 20",
			}},
			{Id: "explain", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{
				"brief": "say why it is not flaky",
			}},
		},
		Edges: []subharness.Edge{{"pick", "rerun"}, {"pick", "explain"}},
	}
	// A choice is a shape only a harness allowed to decide may hold, which the
	// store checks on the way in.
	branching.Dyn = subharness.Dyn{Ladder: subharness.DynBranch, Cap: 2}
	branching.Whitelist = []string{"read", "bash"}
	a, _ := harnessApp(t, branching)
	// A NARROW FRAME IS THE WHOLE TEST. At a hundred cells nothing is long enough
	// to wrap and the two paths agree; the flattening only ever showed up on the
	// frame a person actually reads a long step on.
	a.width = 44
	typeLine(t, a, "/harness")
	drive(t, a, key("enter"))

	at := -1
	for i := range a.entries {
		if a.entries[i].kind == entryNote {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the card never reached the transcript")
	}
	rows := a.renderEntry(at, &a.entries[at], a.width)

	// ONE ROW PER LINE OF THE CARD. Re-flowing would turn a long step into two
	// rows, the second of which sits at the margin and claims to be a step of the
	// run rather than the tail of one.
	lines := strings.Split(a.entries[at].text, "\n")
	if len(rows) != len(lines) {
		t.Fatalf("the card was re-flowed: %d lines became %d rows at width %d:\n%s",
			len(lines), len(rows), a.width, plain(strings.Join(rows, "\n")))
	}
	// AND EACH ROW KEEPS THE COLUMN THE CARD PUT IT IN, measured past the note's
	// own two-cell lead ("· " on the first row, two spaces after it).
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		row := plain(rows[i])
		if len([]rune(row)) < 2 {
			t.Fatalf("line %d came back as %q", i, row)
		}
		body, want := string([]rune(row)[2:]), len(line)-len(strings.TrimLeft(line, " "))
		if got := len(body) - len(strings.TrimLeft(body, " ")); got != want {
			t.Fatalf("line %d sits in column %d and the card put it in %d: %q", i, got, want, row)
		}
	}
	// Nothing runs past the frame either.
	for _, row := range rows {
		if width := len([]rune(plain(row))); width > a.width {
			t.Fatalf("a card row ran past the frame (%d cells): %q", width, plain(row))
		}
	}
}

func TestAnEmptyRegistrySaysSo(t *testing.T) {
	a, _ := harnessApp(t)
	typeLine(t, a, "/harness")
	if !a.harnPanel.open {
		t.Fatal("/harness opened nothing over an empty registry")
	}
	if screen := strings.Join(plainOverlay(a), "\n"); !strings.Contains(screen, "no harnesses are registered") {
		t.Fatalf("an empty registry drew:\n%s", screen)
	}
}

// A SURFACE WITH NO REGISTRY SAYS SO rather than opening an empty list, which
// would be a lie about what is saved.
func TestNoRegistryIsSaidOutLoud(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width = 100
	typeLine(t, a, "/harness")
	if a.harnPanel.open {
		t.Fatal("a surface with no registry opened the list anyway")
	}
	if screen := strings.Join(plainRows(a), "\n"); !strings.Contains(screen, harnessUnavailableWord) {
		t.Fatalf("nothing was said:\n%s", screen)
	}
}

func TestHarnessIsOnTheCommandList(t *testing.T) {
	found := false
	for _, one := range commands {
		if one.name == "harness" {
			found = true
		}
	}
	if !found {
		t.Fatal("/harness is not on the command list")
	}
	if !strings.Contains(helpText("", chordSpelling{}), "/harness") {
		t.Fatal("/harness is not in /help")
	}
}

// ── the strip ───────────────────────────────────────────────────────────────

// runningAgent is a surface's agent that is executing a harness.
type runningAgent struct {
	*fakeAgent
	name string
}

func (r *runningAgent) RunningHarness() (string, bool) { return r.name, r.name != "" }

// A RUNNING HARNESS RAISES THE STRIP AND LEADS IT. Nothing else on the surface
// would say so: the run happens inside one tool call, so the transcript shows a
// row that has not come back yet and nothing more.
func TestTheStripCarriesTheRunningHarness(t *testing.T) {
	a := newTestApp(&runningAgent{fakeAgent: &fakeAgent{}, name: "triage-flake"})
	a.width, a.height = 100, 30
	a.harn = subharness.At(t.TempDir())

	if !a.stripShowing() {
		t.Fatal("a running harness did not raise the strip")
	}
	row := plain(a.stripRow(a.width))
	if !strings.Contains(row, "triage-flake") {
		t.Fatalf("the strip does not name the harness: %q", row)
	}
	if a.stripHarn.to <= a.stripHarn.from {
		t.Fatalf("the chip recorded no columns to be pressed: %+v", a.stripHarn)
	}
	// AND THE CHIP IS A DOOR: pressing it opens the registry, which is the only
	// place a run can be looked at.
	if _, took := a.stripPress(a.stripHarn.from+1, a.headHeight()); !took {
		t.Fatal("the strip did not take the press")
	}
	if !a.harnPanel.open {
		t.Fatal("pressing the harness chip did not open the panel")
	}
}

// AND IT GOES AWAY when the run does — the strip is the live set, never a
// permanent bar.
func TestTheStripDropsTheHarnessWhenItEnds(t *testing.T) {
	agent := &runningAgent{fakeAgent: &fakeAgent{}, name: "triage-flake"}
	a := newTestApp(agent)
	a.width, a.height = 100, 30
	if !a.stripShowing() {
		t.Fatal("a running harness did not raise the strip")
	}
	agent.name = ""
	if a.stripShowing() {
		t.Fatal("the strip stayed up after the run ended")
	}
	if row := a.stripRow(a.width); row != "" {
		t.Fatalf("the strip drew a row for nothing: %q", row)
	}
}

// THE TWO HARNESS SURFACES ARE SIBLINGS AND DO NOT COLLIDE: the offer is a
// question about this turn (harness.go), the panel is a list of what is saved.
func TestTheOfferAndThePanelAreDifferentThings(t *testing.T) {
	a, _ := harnessApp(t, demoHarness("triage-flake", "chase a flaky test"))
	typeLine(t, a, "/harness")
	if !a.harnPanel.open {
		t.Fatal("/harness opened nothing")
	}
	if a.asksHarness() {
		t.Fatal("opening the list raised the offer question")
	}
	if a.questionHeight() != 0 {
		t.Fatal("the list took the question block's rows")
	}
}
