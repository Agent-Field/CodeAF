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

// factoryPaneW is the peek's width beside the rows at a terminal width.
func factoryPaneW(width int) int { return width - factoryRowsCols(width) - 1 }

// EVERY ITEM OF THE FIXTURE DRAWS, in every state, at the two widths that
// draw a peek, as exactly its width and its room, and nothing a person reads
// says a word of the machinery.
func TestFactoryPaneDrawsEveryStateExactly(t *testing.T) {
	a := factoryPlaceLab(t)
	states := map[factory.State]bool{}
	for _, width := range []int{150, 120} {
		paneW := factoryPaneW(width)
		for _, room := range []int{1, 2, 6, 8, 40} {
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
				// THE ACTION LINE IS PINNED TO THE LAST ROW whenever there are two.
				if room >= 2 {
					if last := strings.TrimSpace(rows[room-1]); !strings.HasPrefix(a.factoryActionWords(it), last[:min(len(last), 8)]) {
						t.Fatalf("%s at %d×%d: the last row is not the action line: %q", it.Ref(), paneW, room, rows[room-1])
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

// A DISMISSED ITEM IS NOT ON THE ROWS, but asked for, it reads as a new one:
// its fixed rows draw and its keys are a new item's.
func TestFactoryPaneDrawsADismissedItemAsNew(t *testing.T) {
	a := factoryPlaceLab(t)
	it := *factoryPaneItem(t, a, 8)
	it.State = factory.StateDismissed
	// The still fixture has no launch, so a new item's keys are the ones that
	// need none ([app.factoryCanRun]); a seam that can launch says `r run`.
	if got := a.factoryActionWords(it); !strings.Contains(got, "enter open · space mark · d hide") {
		t.Fatalf("a dismissed item's keys are %q", got)
	}
	a.factory = (&factoryFake{}).seam()
	if got := a.factoryActionWords(it); !strings.Contains(got, "r run · p plan first") {
		t.Fatalf("a dismissed item's keys over a launch are %q", got)
	}
	if rows := a.factoryPeekFixed(it, 60); len(rows) != factoryPaneFixed || !strings.Contains(ansi.Strip(rows[0]), "#1540") {
		t.Fatalf("a dismissed item's fixed rows are %q", rows)
	}
}

// AT THE PLAIN FLOOR THE PEEK IS BYTES A SCREEN READER CAN SAY: no colour, no
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
		for _, r := range a.factoryPane(62, 40) {
			if strings.Contains(r, "\x1b[") {
				t.Fatalf("%s at the plain floor carries SGR: %q", it.Ref(), r)
			}
		}
	}
}

// THE FIVE FIXED ROWS: what it is, the read, the chips, the stages, the rule,
// in that order on a new item, and its keys on the last row.
func TestFactoryPeekNewItem(t *testing.T) {
	a := factoryPlaceLab(t)
	rows := factoryPaneOn(t, a, 4, 105, 20)
	for i, want := range []string{
		"#1662 fix(media): tree rails on narrow widths",
		"claims are testable; three checks cover them",
		"gate ship · cap $3 · effort —",
		"read · checks · review",
		"────",
	} {
		if !strings.Contains(rows[i], want) {
			t.Fatalf("row %d is missing %q:\n%s", i+1, want, strings.Join(rows, "\n"))
		}
	}
	if !strings.Contains(rows[0], "codeaf · pr · priya (collaborator) · 1h · github") {
		t.Fatalf("row 1 has no meta: %q", rows[0])
	}
	for _, key := range []string{"[t]", "[c]", "[e]"} {
		if strings.Contains(rows[2], key) {
			t.Fatalf("the peek's chips name a key: %q", rows[2])
		}
	}
	if !strings.Contains(rows[5], "Claims:") {
		t.Fatalf("the body does not follow the rule:\n%s", strings.Join(rows, "\n"))
	}
	// The still fixture cannot launch, so its new item's keys leave off `r run`
	// and `p plan first` (TestFactoryPeekKeysNeedALaunch holds the other side).
	if last := strings.TrimSpace(rows[19]); last != "enter open · space mark · d hide" {
		t.Fatalf("the action line is %q", last)
	}
	// A thin item from a stranger names its questions and the stranger rule.
	text := strings.Join(strings.Fields(strings.Join(factoryPaneOn(t, a, 6, 105, 20), " ")), " ")
	for _, want := range []string{"thin · it would ask olu: which network, and how slow?", "never shipped on"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the thin stranger's peek is missing %q:\n%s", want, text)
		}
	}
}

// A STAGE WHOSE CONDITION DOES NOT FIT THE ITEM IS DIM ON THE PEEK'S STAGE
// LINE AND SKIPPED ON THE ITEM PAGE, with its reason; on an item it fits it is
// neither.
func TestFactoryPaneSkipsAStageThatDoesNotFit(t *testing.T) {
	a := factoryPlaceLab(t)
	it := factoryPaneItem(t, a, 8) // ready 75
	it.Stages = append(append([]factory.Stage{}, it.Stages...), factory.Stage{Name: "ask", Ask: "ask the author what is missing", When: "thin", On: true})
	last := func() factoryStageView {
		views := a.factoryItemStages(*it)
		return views[len(views)-1]
	}
	ready := a.factoryPeekStrip(*it, 100)
	if v := last(); !v.skipped {
		t.Fatal("on a ready item the thin stage is not skipped")
	}
	if label, _ := a.factoryStageLabel(last()); !strings.Contains(label, "skipped") {
		t.Fatalf("the skipped stage's label is %q", label)
	}
	it.Triage.Readiness = 40
	if v := last(); v.skipped {
		t.Fatal("on a thin item the thin stage is still skipped")
	}
	if thin := a.factoryPeekStrip(*it, 100); thin == ready {
		t.Fatal("the lit stage and the skipped stage are painted the same")
	}
}

// THE STREAM: the phase strip, the log's tail with the activity and spend on
// its first line; NEEDS-YOU puts the question and its keys under the rule;
// QUEUED says what frees it.
func TestFactoryPaneStreamStates(t *testing.T) {
	a := factoryPlaceLab(t)
	text := strings.Join(factoryPaneOn(t, a, 2, 105, 20), "\n")
	for _, want := range []string{"#1551 filters lost on compact", "write ×3", "review 1/2 · 4m left", "review 1/2: 3 findings · fixing", "$1.42/$5", "enter open · s steer · p pause · x stop"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the running peek is missing %q:\n%s", want, text)
		}
	}
	// THE LOG IS A TAIL: in a short room the newest line stays and the oldest goes.
	short := strings.Join(factoryPaneOn(t, a, 2, 105, 8), "\n")
	if !strings.Contains(short, "review 1/2: 3 findings") || strings.Contains(short, "reading #1551") {
		t.Fatalf("a short room did not keep the log's tail:\n%s", short)
	}
	text = strings.Join(factoryPaneOn(t, a, 1, 105, 20), "\n")
	for _, want := range []string{"plan is ready · go, or change it?", "[y] yes · [n] no · [a] in words", "y n answer · a in words · x stop"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the needs-you peek is missing %q:\n%s", want, text)
		}
	}
	text = strings.Join(factoryPaneOn(t, a, 3, 105, 20), "\n")
	if !strings.Contains(text, "queued · benches full · a bench frees it") {
		t.Fatalf("the queued peek does not say what frees it:\n%s", text)
	}
}

// A LANDED ITEM'S TAIL IS ITS CLAIMS, A FAILED ONE SAYING SO, then the policy
// rows; its keys are open, ship anyway, send back and check again.
func TestFactoryPaneLandedDrawsTheClaims(t *testing.T) {
	a := factoryPlaceLab(t)
	text := strings.Join(factoryPaneOn(t, a, 9, 105, 20), "\n")
	for _, want := range []string{
		"fires on first true, never again",
		"survives a codeaf restart — not shown",
		"go.mod unchanged · policy",
		"view]",
		"enter open · a ship anyway · c send back · o check again",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the landed peek is missing %q:\n%s", want, text)
		}
	}
}

// THE SHIPPED LINE says when it merged and what it cost.
func TestFactoryPaneShipped(t *testing.T) {
	a := factoryPlaceLab(t)
	text := strings.Join(factoryPaneOn(t, a, 10, 105, 10), "\n")
	for _, want := range []string{"#1663 spend row shows stale after compact", "merged 06:00 · $1.90", "enter open"} {
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
