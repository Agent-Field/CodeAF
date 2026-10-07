package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// factoryPaneOn puts the cursor on the item with id and answers the pane at
// width and room, plain, one string per row.
func factoryPaneOn(t *testing.T, a *app, id, width, room int) []string {
	t.Helper()
	for at, i := range factoryWalk(a.fp.snap) {
		if a.fp.snap.Items[i].ID == id {
			a.fp.cursor = at
			rows := a.factoryPane(width, room)
			out := make([]string, len(rows))
			for j, r := range rows {
				out[j] = ansi.Strip(r)
			}
			return out
		}
	}
	t.Fatalf("item %d is not on the floor", id)
	return nil
}

// factoryPaneItem finds the fixture item with id.
func factoryPaneItem(t *testing.T, a *app, id int) *factory.Item {
	t.Helper()
	for i := range a.fp.snap.Items {
		if a.fp.snap.Items[i].ID == id {
			return &a.fp.snap.Items[i]
		}
	}
	t.Fatalf("no item %d in the fixture", id)
	return nil
}

// EVERY ITEM OF THE FIXTURE DRAWS, in every state, at a wide and a middling
// terminal, as exactly its width and its room, and nothing a person reads says
// a word of the machinery.
func TestFactoryPaneDrawsEveryStateExactly(t *testing.T) {
	a := factoryPlaceLab(t)
	states := map[factory.State]bool{}
	for _, width := range []int{150, 100} {
		paneW := width - factoryRailCols(width)
		for _, room := range []int{1, 8, 40} {
			for _, it := range a.fp.snap.Items {
				if it.State == factory.StateDismissed {
					continue
				}
				states[it.State] = true
				rows := factoryPaneOn(t, a, it.ID, paneW, room)
				if len(rows) != room {
					t.Fatalf("%s at %d×%d drew %d rows", it.Ref(), paneW, room, len(rows))
				}
				for i, r := range rows {
					if got := ansi.StringWidth(r); got != paneW {
						t.Fatalf("%s at %d×%d: row %d is %d cells: %q", it.Ref(), paneW, room, i, got, r)
					}
					low := strings.ToLower(r)
					for _, banned := range []string{"verified", "verdict", "auditor", "refuted"} {
						if strings.Contains(low, banned) {
							t.Fatalf("%s says %q: %q", it.Ref(), banned, r)
						}
					}
				}
			}
		}
	}
	for _, st := range []factory.State{factory.StateNew, factory.StateQueued, factory.StateRunning, factory.StateNeedsYou, factory.StateLanded, factory.StateShipped} {
		if !states[st] {
			t.Errorf("the fixture drew no item in state %q", st)
		}
	}
}

// A DISMISSED ITEM IS NOT ON THE RAIL, but a pane asked for one draws its card
// rather than nothing.
func TestFactoryPaneDrawsADismissedItemAsItsCard(t *testing.T) {
	a := factoryPlaceLab(t)
	factoryPaneItem(t, a, 8).State = factory.StateDismissed
	lines := a.factoryCard(*factoryPaneItem(t, a, 8), 80)
	if text := ansi.Strip(strings.Join(lines, "\n")); !strings.Contains(text, "[enter] go") {
		t.Fatalf("a dismissed item's card has no go line:\n%s", text)
	}
}

// AT THE PLAIN FLOOR THE PANE IS BYTES A SCREEN READER CAN SAY: no colour, no
// weight, and every mark from the ASCII column of the vocabulary.
func TestFactoryPaneAtThePlainFloorHasNoSGR(t *testing.T) {
	a := factoryPlaceLab(t)
	a.pal = newPalette(tokens.NoColor, true)
	a.linear = true
	for _, it := range a.fp.snap.Items {
		if it.State == factory.StateDismissed {
			continue
		}
		for at, i := range factoryWalk(a.fp.snap) {
			if a.fp.snap.Items[i].ID == it.ID {
				a.fp.cursor = at
			}
		}
		for _, r := range a.factoryPane(100, 40) {
			if strings.Contains(r, "\x1b[") {
				t.Fatalf("%s at the plain floor carries SGR: %q", it.Ref(), r)
			}
		}
	}
}

// THE CARD says what the item is, what the factory makes of it, the chips, the
// stages, the policy and the go line.
func TestFactoryPaneCardReadsTheItem(t *testing.T) {
	a := factoryPlaceLab(t)
	text := strings.Join(factoryPaneOn(t, a, 4, 105, 40), "\n")
	for _, want := range []string{
		"#1662 fix(media): tree rails on narrow widths",
		"codeaf · pr · priya (collaborator) · 1h · github",
		"pr · +218 −44 · 6 files · ci",
		"claims are testable; three checks cover them",
		"gate ship [t]   cap $3 [c]   effort — [e]",
		"[1]", "read", "the diff and its claims",
		"must show: complexity within +10% of main · no new dependencies without asking",
		"[enter] go  runs to a PR · you sign off",
		"[s] add a stage in words",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the card is missing %q:\n%s", want, text)
		}
	}
	// A thin item from a stranger names its questions and the stranger rule.
	text = strings.Join(factoryPaneOn(t, a, 6, 105, 40), "\n")
	for _, want := range []string{"thin · it would ask olu: which network, and how slow?", "nothing posts without you", "never shipped on"} {
		if !strings.Contains(strings.Join(strings.Fields(text), " "), want) {
			t.Fatalf("the thin stranger's card is missing %q:\n%s", want, text)
		}
	}
}

// A STAGE WHOSE CONDITION DOES NOT FIT THE ITEM DRAWS SKIPPED, WITH ITS REASON;
// the same stage on an item it fits draws lit.
func TestFactoryPaneSkipsAStageThatDoesNotFit(t *testing.T) {
	a := factoryPlaceLab(t)
	it := factoryPaneItem(t, a, 8) // ready 75
	it.Stages = append(append([]factory.Stage{}, it.Stages...), factory.Stage{Name: "ask", Ask: "ask the author what is missing", When: "thin", On: true})
	row := func() string {
		for _, r := range a.factoryCard(*it, 100) {
			if strings.Contains(ansi.Strip(r), "ask the author what is missing") {
				return r
			}
		}
		t.Fatal("the thin stage has no row")
		return ""
	}
	ready := row()
	if !strings.Contains(ansi.Strip(ready), "· skipped: not thin") {
		t.Fatalf("on a ready item the thin stage is not skipped: %q", ansi.Strip(ready))
	}
	if !factory.Fits(it.Stages[0], *it) || factory.Fits(it.Stages[len(it.Stages)-1], *it) {
		t.Fatal("Fits disagrees with the row")
	}
	it.Triage.Readiness = 40
	thin := row()
	if strings.Contains(ansi.Strip(thin), "skipped") {
		t.Fatalf("on a thin item the thin stage is still skipped: %q", ansi.Strip(thin))
	}
	if thin == ready {
		t.Fatal("the lit row and the skipped row are painted the same")
	}
}

// THE STREAM draws the phases, the recipe and the tail of its log; NEEDS-YOU
// puts the question and its keys under it; QUEUED says what frees it.
func TestFactoryPaneStreamStates(t *testing.T) {
	a := factoryPlaceLab(t)
	text := strings.Join(factoryPaneOn(t, a, 2, 105, 40), "\n")
	for _, want := range []string{"#1551 filters lost on compact", "bench 1", "26m", "$1.42 / $5", "write ×3", "review 1/2 · 4m left", "plan · write · test · review · neaten · proof", "review 1/2: 3 findings · fixing"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the running stream is missing %q:\n%s", want, text)
		}
	}
	// THE LOG IS A TAIL: in a short room the newest line stays and the oldest goes.
	short := strings.Join(factoryPaneOn(t, a, 2, 105, 6), "\n")
	if !strings.Contains(short, "review 1/2: 3 findings") || strings.Contains(short, "reading #1551") {
		t.Fatalf("a short room did not keep the log's tail:\n%s", short)
	}
	text = strings.Join(factoryPaneOn(t, a, 1, 105, 40), "\n")
	for _, want := range []string{"plan is ready · go, or change it?", "[y] yes · [n] no · [a] answer in words · it waits; the other benches do not"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the needs-you stream is missing %q:\n%s", want, text)
		}
	}
	// The question survives a room too short for the log.
	short = strings.Join(factoryPaneOn(t, a, 1, 105, 8), "\n")
	if !strings.Contains(short, "[y] yes") {
		t.Fatalf("the question's keys were pushed off a short room:\n%s", short)
	}
	text = strings.Join(factoryPaneOn(t, a, 3, 105, 40), "\n")
	if !strings.Contains(text, "queued · benches full · a bench frees it") {
		t.Fatalf("the queued stream does not say what frees it:\n%s", text)
	}
}

// A FAILED CLAIM PUTS SEND BACK ON ENTER, naming the claim, and shipping takes
// another key; every claim green puts ship on enter.
func TestFactoryPaneSheetPutsSendBackOnEnterWhenAClaimFailed(t *testing.T) {
	a := factoryPlaceLab(t)
	text := strings.Join(factoryPaneOn(t, a, 9, 105, 40), "\n")
	for _, want := range []string{
		"sign-off · #1661 · probes fire once, then retire",
		"its claims",
		"survives a codeaf restart — not shown",
		"[enter] send back — \"prove survives a codeaf restart\"",
		"[a] ship anyway",
		"a failed claim makes the blocking action the default key",
		"policy · every PR on codeaf must show",
		"go.mod unchanged · policy",
		"view]",
		"diff +218 −44 — the appendix",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the sheet with a failed claim is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[enter] ship") {
		t.Fatalf("a failed claim left ship on enter:\n%s", text)
	}
	it := factoryPaneItem(t, a, 9)
	it.Proof = append([]factory.Claim{}, it.Proof...)
	it.Proof[3].OK = true
	text = strings.Join(factoryPaneOn(t, a, 9, 105, 40), "\n")
	if !strings.Contains(text, "[enter] ship") || strings.Contains(text, "send back —") {
		t.Fatalf("a green sheet does not put ship on enter:\n%s", text)
	}
}

// THE SHIPPED LINE says when it merged and how to reach its room.
func TestFactoryPaneShipped(t *testing.T) {
	a := factoryPlaceLab(t)
	text := strings.Join(factoryPaneOn(t, a, 10, 105, 10), "\n")
	for _, want := range []string{"#1663 spend row shows stale after compact", "merged 06:00 · $1.90", "[enter] the room"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the shipped item is missing %q:\n%s", want, text)
		}
	}
}

// FITS reads the item's triage, and a word it was never taught fits.
func TestFactoryFits(t *testing.T) {
	it := factory.Item{Triage: factory.Triage{Readiness: 80, Size: "M", Area: "tui"}}
	for _, c := range []struct {
		when string
		want bool
	}{
		{"", true}, {"always", true}, {"thin", false}, {"large", false},
		{"touches auth", false}, {"has ui", true}, {"on a full moon", true},
	} {
		if got := factory.Fits(factory.Stage{When: c.when}, it); got != c.want {
			t.Errorf("Fits(%q) = %v, want %v", c.when, got, c.want)
		}
	}
	it.Triage = factory.Triage{Readiness: 30, Size: "L", Area: "billing"}
	for _, when := range []string{"thin", "large", "touches auth"} {
		if !factory.Fits(factory.Stage{When: when}, it) {
			t.Errorf("Fits(%q) refused an item it describes", when)
		}
	}
}
