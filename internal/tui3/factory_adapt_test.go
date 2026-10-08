package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// factoryAdaptedWords is the fixture's adapted item's record as one line.
const factoryAdaptedWords = "plan added security · skipped neaten · why: touches the billing cache"

// WHAT PLAN CHANGED IS ONE DIM LINE UNDER THE STAGES LINE, on the peek in the
// stages block and in the item page's head; an item plan never changed draws
// nothing in its place.
func TestFactoryAdaptedLineUnderTheStages(t *testing.T) {
	a := factoryPlaceLab(t)
	peek := factoryPaneOn(t, a, 10, 150, 20)
	at := -1
	for i, row := range peek {
		if strings.Contains(row, factoryAdaptedWords) {
			at = i
		}
	}
	if at < 1 || !strings.Contains(peek[at-1], "security") || strings.TrimSpace(peek[at+1]) != "" {
		t.Fatalf("the adapted line is not directly under the stages, closing their block:\n%s", strings.Join(peek, "\n"))
	}
	for _, id := range []int{1, 2, 8, 9} {
		peek := strings.Join(factoryPaneOn(t, a, id, 150, 20), "\n")
		if strings.Contains(peek, "plan added") || strings.Contains(peek, "why:") {
			t.Fatalf("item %d was never adapted and draws a record:\n%s", id, peek)
		}
	}

	head := func(id int) []string {
		var out []string
		for _, r := range a.factoryItemBody(*factoryPaneItem(t, a, id), 150, 20) {
			out = append(out, ansi.Strip(r.text))
		}
		return out
	}
	// THE HEAD IS TWO ROWS AND A BLANK on every item; what plan changed is
	// in the issue pane, beside the stages it changed.
	page := head(10)
	if strings.TrimSpace(page[2]) != "" || !strings.Contains(strings.Join(page[3:], "\n"), factoryAdaptedWords) {
		t.Fatalf("the adapted line is not in the issue pane under a two-row head:\n%s", strings.Join(page, "\n"))
	}
	if page := head(9); strings.Contains(strings.Join(page, "\n"), "plan added") || strings.TrimSpace(page[2]) != "" {
		t.Fatalf("an item plan never changed grew a head row:\n%s", strings.Join(page[:5], "\n"))
	}
}

// The adapted line is dim, never the ink of a fact a person must act on.
func TestFactoryAdaptedLineIsDim(t *testing.T) {
	a := factoryPlaceLab(t)
	row := a.factoryAdaptedRow(*factoryPaneItem(t, a, 10), 150)
	if ansi.Strip(row) != factoryAdaptedWords || row != a.pal.dim(factoryAdaptedWords) {
		t.Fatalf("adapted row = %q", row)
	}
	if a.factoryAdaptedRow(*factoryPaneItem(t, a, 9), 150) != "" {
		t.Fatal("an unadapted item has an adapted row")
	}
}
