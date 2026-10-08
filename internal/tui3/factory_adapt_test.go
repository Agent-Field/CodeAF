package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// factoryAdaptedWords is the fixture's adapted item's record as one line.
const factoryAdaptedWords = "plan added security · skipped neaten · why: touches the billing cache"

// WHAT PLAN CHANGED IS ONE DIM LINE UNDER THE STAGES LINE, on the peek and in
// the item page's head; an item plan never changed draws nothing in its place.
func TestFactoryAdaptedLineUnderTheStages(t *testing.T) {
	a := factoryPlaceLab(t)
	peek := factoryPaneOn(t, a, 10, 150, 20)
	at := -1
	for i, row := range peek {
		if strings.Contains(row, factoryAdaptedWords) {
			at = i
		}
	}
	if at != 4 || !strings.Contains(peek[3], "security") || !strings.Contains(peek[5], "─") {
		t.Fatalf("the adapted line is not row five, under the stages and over the rule:\n%s", strings.Join(peek, "\n"))
	}
	for _, id := range []int{1, 2, 8, 9} {
		peek := strings.Join(factoryPaneOn(t, a, id, 150, 20), "\n")
		if strings.Contains(peek, "plan added") || strings.Contains(peek, "why:") {
			t.Fatalf("item %d was never adapted and draws a record:\n%s", id, peek)
		}
		if rows := a.factoryPeekFixed(*factoryPaneItem(t, a, id), 80); len(rows) != factoryPaneFixed {
			t.Fatalf("item %d's fixed rows are %d", id, len(rows))
		}
	}

	head := func(id int) []string {
		var out []string
		for _, r := range a.factoryItemBody(*factoryPaneItem(t, a, id), 150, 20) {
			out = append(out, ansi.Strip(r.text))
		}
		return out
	}
	page := head(10)
	if !strings.Contains(page[2], factoryAdaptedWords) || strings.TrimSpace(page[3]) != "" {
		t.Fatalf("the item page head does not carry the adapted line on its third row:\n%s", strings.Join(page[:5], "\n"))
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
