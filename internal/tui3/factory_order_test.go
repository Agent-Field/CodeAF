package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// factoryOrderLab is the still floor with the cheap read's priorities and
// reasons set on its five new items.
func factoryOrderLab(t *testing.T) *app {
	t.Helper()
	a := factoryPlaceLab(t)
	snap := factory.Fixture(factoryTestNow)
	set := map[string]struct {
		p      int
		reason string
	}{
		"#1662": {3, "small claims, all testable"},
		"ci":    {1, "main is red"},
		"#702":  {3, ""},
		"#1540": {2, "crashes nightly"},
	}
	for i, it := range snap.Items {
		if s, ok := set[it.Ref()]; ok {
			snap.Items[i].Triage.Priority, snap.Items[i].Triage.Reason = s.p, s.reason
		}
	}
	a.factoryFold(snap)
	// The lab's priorities are a fresh floor's, so the rows are placed by
	// them as a first read would place them (factory_order.go).
	a.factoryPlace(true)
	return a
}

// factoryNewRefs is the walk's refs inside the `new` section, top to bottom.
func factoryNewRefs(a *app) []string {
	var out []string
	for _, i := range a.factoryWalkNow() {
		if it := a.fp.snap.Items[i]; it.State == factory.StateNew {
			out = append(out, it.Ref())
		}
	}
	return out
}

// `O` CYCLES priority → first → age → cost → priority, sorting only
// within each section, and the hint names the order the floor is in.
func TestFactoryOrderCyclesAndSortsWithinSections(t *testing.T) {
	a := factoryOrderLab(t)
	a.width = 200
	steps := []struct {
		word string
		new  string
	}{
		{"priority", "ci,#1540,#1662,#702,#31"},
		{"first", "ci,#1540,#702,#1662,#31"},
		{"age", "ci,#31,#702,#1662,#1540"},
		{"cost", "ci,#1662,#1540,#31,#702"},
		{"priority", "ci,#1540,#1662,#702,#31"},
	}
	for n, s := range steps {
		if n > 0 {
			drive(t, a, key("O"))
		}
		if got := a.fp.order.word(); got != s.word {
			t.Fatalf("after %d presses the order is %q, want %q", n, got, s.word)
		}
		if got := strings.Join(factoryNewRefs(a), ","); got != s.new {
			t.Fatalf("order %q sorts new as %s, want %s", s.word, got, s.new)
		}
		if sheet := factorySheetText(a); !strings.Contains(sheet, "O order · "+s.word) {
			t.Fatalf("order %q: the ? sheet does not name it: %q", s.word, sheet)
		}
		// THE SECTIONS NEVER MOVE: the question is first in every order.
		if first, _ := a.factoryWalkNow(), 0; len(first) == 0 || a.fp.snap.Items[first[0]].State != factory.StateNeedsYou {
			t.Fatalf("order %q moved the needs-you section", s.word)
		}
	}
}

// A ROW IN THE `first` ORDER DRAWS ITS REASON dim before the age, exactly, and
// a narrow row drops the reason before it drops any fact.
func TestFactoryOrderFirstRowDrawsTheReasonAndDropsItFirst(t *testing.T) {
	a := factoryOrderLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	a.fp.columns = true
	var ci factory.Item
	for _, it := range a.fp.snap.Items {
		if it.Ref() == "ci" {
			ci = it
		}
	}
	plainBefore := ansi.Strip(a.factoryRailItem(ci, 150, false))
	drive(t, a, key("O"))
	wide := ansi.Strip(a.factoryRailItem(ci, 150, false))
	if got := ansi.StringWidth(wide); got != 150 {
		t.Fatalf("the row is %d cells, want 150", got)
	}
	want := "✕ ▇     ci main is red · sync · TestCompactKeepsFilters         whisper      ci red · ~$1.50 · ci                                    main is red    6h"
	if wide != want {
		t.Fatalf("the first-order row is\n%q\nwant\n%q\n(before O it was %q)", wide, want, plainBefore)
	}
	// Narrow enough that the reason no longer fits beside every fact: the
	// reason goes and the facts stay exactly as they were in the default
	// order at that width.
	for width := 150; width >= 90; width-- {
		row := ansi.Strip(a.factoryRailItem(ci, width, false))
		if strings.Contains(row, "main is red  ") && !strings.Contains(row, "~$1.50") {
			t.Fatalf("at %d a fact was dropped before the reason: %q", width, row)
		}
	}
	// No reason, no space taken: #31 has none.
	for _, it := range a.fp.snap.Items {
		if it.Ref() == "#31" {
			a.fp.order = factoryOrderPriority
			before := ansi.Strip(a.factoryRailItem(it, 150, false))
			a.fp.order = factoryOrderFirst
			if after := ansi.Strip(a.factoryRailItem(it, 150, false)); after != before {
				t.Fatalf("a row with no reason changed in the first order:\n%q\n%q", before, after)
			}
		}
	}
}

// `O` IS A FLOOR KEY: on the item page it does nothing, and the order is not
// changed underneath.
func TestFactoryOrderKeyIsTheFloorsOnly(t *testing.T) {
	a := factoryOrderLab(t)
	drive(t, a, key("enter"))
	if !a.fp.open {
		t.Skip("enter did not open the item page on this fixture")
	}
	drive(t, a, key("O"))
	if a.fp.order != factoryOrderPriority {
		t.Fatalf("O on the item page changed the order to %q", a.fp.order.word())
	}
}

// AT 160 COLUMNS THE `first` ORDER SHOWS ITS REASON at a row's end and the hint
// still names the order: the reason takes the cells the facts leave free at
// this width, and the hint sheds `/ filter` and `A backlog` before the order.
func TestFactoryOrderFirstShowsReasonAndHintAt160(t *testing.T) {
	a := factoryOrderLab(t)
	a.width, a.height = 160, 44
	drive(t, a, key("O"))
	if a.fp.order != factoryOrderFirst {
		t.Fatalf("O did not reach the first order: %q", a.fp.order.word())
	}
	frame := ansi.Strip(strings.Join(factoryFrameLines(a), "\n"))
	if !strings.Contains(frame, "main is red") {
		t.Fatalf("no reason on any row at 160 columns:\n%s", frame)
	}
	if sheet := factorySheetText(a); !strings.Contains(sheet, "O order · first") {
		t.Fatalf("the ? sheet lost the order at 160: %q", sheet)
	}
}

// `tab` ON THE FLOOR WALKS TO THE NEXT PLACE, as its hint says.
func TestFactoryTabWalksToTheNextPlace(t *testing.T) {
	a := factoryOrderLab(t)
	drive(t, a, key("tab"))
	if a.at(pageFactory) {
		t.Fatal("tab on the floor stayed on the floor")
	}
}
