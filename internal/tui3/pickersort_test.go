package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/lane"
)

// sortCatalog is a list with a value and a GAP in every sortable column, because
// the gaps are what the laws below are about.
var sortCatalog = []Model{
	{ID: "b/cheap", PromptPrice: 1e-7, CompletionPrice: 2e-7, ContextLength: 8_000, ArenaElo: 900},
	{ID: "a/dear", PromptPrice: 9e-6, CompletionPrice: 9e-5, ContextLength: 1_000_000, ArenaElo: 1400},
	{ID: "c/middle", PromptPrice: 1e-6, CompletionPrice: 1e-6, ContextLength: 200_000, ArenaElo: 1200},
	// Publishes nothing at all: the row every law here has to place last.
	{ID: "d/silent"},
}

func sortApp(t *testing.T) *app {
	t.Helper()
	a := pickerApp(t, &fakeAgent{model: "c/middle"}, sortCatalog)
	a.width, a.height = 120, 40
	typeLine(t, a, "/model")
	return a
}

// EACH COLUMN IS READ THE WAY THE QUESTION IS ASKED. Nobody opens a price column
// to find the most expensive model, so the first press is the useful order every
// time.
func TestTheSortKeyOrdersEachColumnTheWayItIsRead(t *testing.T) {
	for _, probe := range []struct {
		key  pickerSortKey
		want []string
	}{
		{pickerByName, []string{"a/dear", "b/cheap", "c/middle", "d/silent"}},
		// Cheapest first, and the row with no price last.
		{pickerByIn, []string{"b/cheap", "c/middle", "a/dear", "d/silent"}},
		{pickerByOut, []string{"b/cheap", "c/middle", "a/dear", "d/silent"}},
		// Biggest window and highest score first.
		{pickerByWindow, []string{"a/dear", "c/middle", "b/cheap", "d/silent"}},
		{pickerByElo, []string{"a/dear", "c/middle", "b/cheap", "d/silent"}},
	} {
		t.Run(probe.key.word(), func(t *testing.T) {
			a := sortApp(t)
			a.pick.sortBy(probe.key)
			if got := pickerIDs(a); strings.Join(got, ",") != strings.Join(probe.want, ",") {
				t.Fatalf("%s gave %v, want %v", probe.key.word(), got, probe.want)
			}
		})
	}
}

// WHAT NOBODY PUBLISHED SORTS LAST IN BOTH DIRECTIONS. This is the emptiness law
// said about order: a model with no published price is not the cheapest model, and
// turning the column round must not make it the most expensive one either — it is
// simply not in the comparison. The `cheap` filter word this replaces got the rule
// right in one direction only, by calling an unknown price infinity.
func TestWhatNobodyPublishedSortsLastEitherWayRound(t *testing.T) {
	for _, key := range []pickerSortKey{pickerByIn, pickerByOut, pickerByWindow, pickerByElo} {
		t.Run(key.word(), func(t *testing.T) {
			a := sortApp(t)
			a.pick.sortBy(key) // natural order
			forward := pickerIDs(a)
			if forward[len(forward)-1] != "d/silent" {
				t.Fatalf("%s put the silent row at %v", key.word(), forward)
			}
			a.pick.sortBy(key) // and turned round
			if !a.pick.sort.back {
				t.Fatal("the second press did not turn the column round")
			}
			back := pickerIDs(a)
			if back[len(back)-1] != "d/silent" {
				t.Fatalf("%s reversed put the silent row at %v", key.word(), back)
			}
			// AND THE KNOWN ROWS DID turn round, so this is not a sort that
			// quietly refused. The three that published a figure come back in the
			// opposite order while the silent one stays where it is — which is the
			// whole claim: it is not at either end, it is out of the comparison.
			if strings.Join(back[:3], ",") != strings.Join(reversedOf(forward[:3]), ",") {
				t.Fatalf("%s: forward %v, reversed %v — the known rows did not turn round",
					key.word(), forward, back)
			}
		})
	}
}

// THE CYCLE COMES BACK TO THE LIST'S OWN ORDER, which is what makes the sort
// undoable: the name search's best-match ordering is a rung, not something a press
// throws away for the life of the list.
func TestTheSortCycleComesBackToTheListsOwnOrder(t *testing.T) {
	a := sortApp(t)
	opened := strings.Join(pickerIDs(a), ",")

	seen := map[pickerSortKey]bool{}
	for range int(pickerSortKeyCount) * 2 {
		drive(t, a, key(tasksSortKeyChord))
		seen[a.pick.sort.key] = true
		if a.pick.sort.key == pickerByList {
			break
		}
	}
	if a.pick.sort.key != pickerByList {
		t.Fatalf("the cycle never came back to the list's order (saw %v)", seen)
	}
	if got := strings.Join(pickerIDs(a), ","); got != opened {
		t.Fatalf("back at the list's order the rows are %q, want the opening %q", got, opened)
	}
}

// A COLUMN NOBODY PUBLISHED IS NOT A RUNG. With nothing measured there is no
// `first` and no `t/s` column, and a rung for one would be a press that moves no
// row and paints no arrow — indistinguishable from the key being broken.
func TestTheSortCycleSkipsAColumnNobodyPublished(t *testing.T) {
	forgetLanes()
	lane.Default().Reset()
	a := sortApp(t)

	for range int(pickerSortKeyCount) * 2 {
		drive(t, a, key(tasksSortKeyChord))
		if a.pick.sort.key == pickerByFirst || a.pick.sort.key == pickerByRate {
			t.Fatalf("the cycle stopped on %s, which this list has no column for", a.pick.sort.key.word())
		}
		if a.pick.sort.key == pickerByList {
			break
		}
	}

	// AND IT IS A RUNG AGAIN once something has been measured, because the
	// question is about the data and not about the key.
	laneLab(t, threeLanes())
	b := laneApp(t)
	b.width, b.height = 120, 40
	typeLine(t, b, "/model")
	if !b.pick.sortable(pickerByFirst) {
		t.Fatal("first is not sortable on a list whose providers have been measured")
	}
}

// THE SORTED COLUMN WEARS THE ARROW, and it is MEASURED rather than appended: a
// head that grew two cells after the columns were budgeted would push the block
// past the measure and move every row under it sideways.
func TestTheSortedColumnWearsTheArrowInsideItsOwnWidth(t *testing.T) {
	a := sortApp(t)
	plainFit := a.pick.tableFit(a.width)
	before := ansi.StringWidth(plainFit.header())

	a.pick.sortBy(pickerByElo)
	marked := a.pick.tableFit(a.width)
	head := marked.header()
	if !strings.Contains(head, "elo "+tasksSortDown) {
		t.Fatalf("the heading does not carry the arrow on elo: %q", head)
	}
	if got := ansi.StringWidth(head); got != before {
		t.Fatalf("the heading is %d cells with the arrow and %d without — the block moved", got, before)
	}
	// AND EVERY ROW IS STILL THE HEADING'S WIDTH, which is the alignment itself.
	row := marked.row(a.pick.rowCells(sortCatalog[0], ""))
	if got, want := ansi.StringWidth(row), marked.facts+marked.pad; got != want {
		t.Fatalf("a row is %d cells and the table is %d", got, want)
	}

	// TURNED ROUND, THE ARROW TURNS and the width does not move again.
	a.pick.sortBy(pickerByElo)
	if head := a.pick.tableFit(a.width).header(); !strings.Contains(head, "elo "+tasksSortUp) {
		t.Fatalf("the reversed heading reads %q", head)
	}
}

// A SORTED LIST DRAWS NO SERVICE HEADINGS. The rows are in a column's order now,
// so a service's name over a run of them is a claim about the structure that
// stopped being true — and with the services interleaved it would land on nearly
// every row.
func TestASortedListDrawsNoServiceHeadings(t *testing.T) {
	a := sortApp(t)
	for at := range a.pick.list {
		a.pick.all[a.pick.hits[a.pick.list[at].hit]].Group = "a service"
	}
	if a.pick.groupBefore(0) == "" {
		t.Fatal("the unsorted list draws no heading, so this test is not about the change")
	}
	a.pick.sortBy(pickerByElo)
	for at := range a.pick.list {
		if got := a.pick.groupBefore(at); got != "" {
			t.Fatalf("row %d of a sorted list carries the heading %q", at, got)
		}
	}
}

// THE CURSOR LANDS ON THE TOP OF A SORTED LIST, because the top is the answer.
// Going back to the model in use — which is what an empty filter box does — shows
// somebody who asked for the cheapest model their own row's neighbourhood instead,
// and made reversing a column look like it had done nothing.
func TestSortingPutsTheCursorOnTheAnswer(t *testing.T) {
	a := sortApp(t)
	if chosen, _ := a.pick.choice(); chosen.ID != "c/middle" {
		t.Fatalf("the list did not open on the model in use: %q", chosen.ID)
	}
	a.pick.sortBy(pickerByIn)
	chosen, ok := a.pick.choice()
	if !ok || chosen.ID != "b/cheap" {
		t.Fatalf("after sorting by price the cursor is on %q, want the cheapest", chosen.ID)
	}
	// AND EMPTYING THE BOX STILL GOES BACK TO THE MODEL IN USE once the list is
	// in its own order again, which is the rule this one is carved out of.
	a.pick.sortBy(pickerByList)
	if chosen, _ := a.pick.choice(); chosen.ID != "c/middle" {
		t.Fatalf("back in the list's own order the cursor is on %q", chosen.ID)
	}
}

// THE SORT COMES AFTER THE NAME RANKING, so `dear` then a sort is the matching
// rows in price order rather than the cheapest rows that happen to match.
func TestTheSortOrdersWhatTheFilterKept(t *testing.T) {
	a := sortApp(t)
	typeInto(t, a, "e")
	kept := pickerIDs(a)
	if len(kept) < 2 {
		t.Fatalf("the filter kept %v, too few to order", kept)
	}
	a.pick.sortBy(pickerByIn)
	sorted := pickerIDs(a)
	if len(sorted) != len(kept) {
		t.Fatalf("the sort changed which rows were kept: %v then %v", kept, sorted)
	}
	if sorted[0] != "b/cheap" {
		t.Fatalf("the sorted hits start %v, want the cheapest of them", sorted)
	}
}

// AND THE FOOT NAMES THE KEY, not the column: taskstable.go's own ruling, whose
// measurement was that `alt+s sort: out/M` cost the five cells that made the foot
// drop this clause and the filter hint beside it at a hundred columns.
func TestTheFootNamesTheSortKeyAndNotTheColumn(t *testing.T) {
	a := sortApp(t)
	want := tasksSortKeyChord + " sort"
	if got := a.hintWord(); !strings.Contains(got, want) {
		t.Fatalf("the foot reads %q, want it to name %q", got, want)
	}
	a.pick.sortBy(pickerByElo)
	if got := a.hintWord(); !strings.Contains(got, want) {
		t.Fatalf("with a sort on, the foot reads %q", got)
	}
	if got := a.hintWord(); strings.Contains(got, want+":") || strings.Contains(got, "sort: ") {
		t.Fatalf("the foot names the column as well as the key: %q", got)
	}
}

// reversedOf is a copy of a slice back to front, for a test that is about two
// orders being each other's opposite.
func reversedOf(in []string) []string {
	out := make([]string, len(in))
	for at, one := range in {
		out[len(in)-1-at] = one
	}
	return out
}
