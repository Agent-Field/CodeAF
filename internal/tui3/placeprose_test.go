package tui3

import (
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func TestPlaceProseKeepsOneFoldGrammarAndOneSectionBreath(t *testing.T) {
	if got := foldLine(1234, "on this shelf"); got != tokens.GlyphCollapsed+" 1,234 more, on this shelf" {
		t.Fatalf("foldLine = %q", got)
	}
	rows := appendPlaceSection([]string{"first", "", ""}, "second")
	rows = appendPlaceSection(rows, "third")
	if got := strings.Join(rows, "|"); got != "first||second||third" {
		t.Fatalf("section rhythm = %q", got)
	}
}

func TestTheSixPlacesDoNotRegrowRetiredProseHelpers(t *testing.T) {
	files := []string{"switcher.go", "tasksplace.go", "standingplace.go", "memoryplace.go", "spendplace.go", "searchplace.go"}
	retired := []string{"tasksMoneyInk", "standingMoneyInk", "spendMoneyInk", "formatMemoryNumber", "commaInt"}
	for _, name := range files {
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, word := range retired {
			if strings.Contains(string(body), word) {
				t.Errorf("%s regrew %s", name, word)
			}
		}
	}
}

func TestSettledTasksStayNeutralWhileMoneyKeepsItsMeaning(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	item := tasksItem{section: tasksToday}
	item.entry.Status = "done"
	_, ink := tasksGlyph(item, pal)
	if ink(tokens.GlyphSettled) != pal.muted(tokens.GlyphSettled) {
		t.Fatal("a settled task did not match the neutral done-row ink")
	}
	// MONEY IS ITS OWN INK AND IT IS NO LONGER THE TICK'S. This assertion used to
	// pin [placeMoneyInk] to [palette.add] — the green a finished tick wears —
	// which said that landing and paying are one event. The design spends green on
	// money and nothing else (FIDELITY.md item 1, styles.go's [hueMoneyPlace]), so
	// the seam is [palette.money] now and the two greens are two facts again.
	if placeMoneyInk(pal)("$1") != pal.money("$1") {
		t.Fatal("money moved away from its shared ink seam")
	}
	if placeMoneyInk(pal)("$1") == pal.add("$1") {
		t.Fatal("money and the landed tick are one colour again")
	}
}
