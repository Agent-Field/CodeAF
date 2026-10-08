package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A FLOOR THAT IS READ AND HOLDS NOTHING SAYS WHAT ARRIVES THERE in one dim
// line under the handover, never blank rows and never a sentence saying it is
// empty, and `n` still opens the new-work row under it.
func TestFactoryBareFloorNamesWhatArrivesAndNStillWorks(t *testing.T) {
	f := &factoryFake{shape: func(s *factory.Snapshot) {
		s.Items, s.Repos = nil, nil
	}}
	a := factoryVerbLab(t, f)
	for _, width := range []int{150, 100, 80} {
		a.width = width
		text := strings.Join(strings.Fields(factoryFrameText(a)), " ")
		if !strings.Contains(text, factoryBareWords) {
			t.Fatalf("a bare floor at %d columns does not say what arrives here:\n%s", width, factoryFrameText(a))
		}
		if strings.Contains(strings.ToLower(text), "empty") {
			t.Fatalf("a bare floor at %d columns says it is empty:\n%s", width, factoryFrameText(a))
		}
	}
	a.width = 150
	// No row of the line is a place a press lands.
	for y := 0; y < a.height; y++ {
		if a.factoryPress(y) {
			t.Fatalf("a press on row %d of a bare floor landed on an item", y)
		}
	}
	drive(t, a, key("n"))
	if text := factoryFrameText(a); !strings.Contains(text, "new work ›") || !strings.Contains(strings.Join(strings.Fields(text), " "), factoryBareWords) {
		t.Fatalf("n on a bare floor did not open the new-work row under the line:\n%s", text)
	}

	// A floor that has not been read yet draws no such line: it may be false.
	b := placeApp(t)
	b.factory = f.seam()
	b.width, b.height = 150, 44
	b.fp.loaded = false
	if b.factoryBare() {
		t.Fatal("a floor that was never read reads as bare")
	}
}

// THE PEEK'S KEY LINE SAYS `r run` ONLY WHERE A LAUNCH STANDS BEHIND IT,
// and the bottom bar never says it: the bar is the floor's navigation.
func TestFactoryPeekKeysNeedALaunch(t *testing.T) {
	// The still fixture has no engine door, like the person's own floor.
	a := factoryPlaceLab(t)
	it := *factoryPaneItem(t, a, 4)
	words := a.factoryActionWords(it)
	for _, gone := range []string{"r run", "L run"} {
		if strings.Contains(words, gone) {
			t.Fatalf("with no launch the peek says %q: %q", gone, words)
		}
	}
	if !strings.HasPrefix(words, "enter open") || !strings.Contains(words, "space select") {
		t.Fatalf("the peek lost the keys that need no launch: %q", words)
	}
	rows := factoryPaneOn(t, a, 4, factoryPaneW(150), 20)
	if last := rows[len(rows)-1]; strings.Contains(last, "r run") {
		t.Fatalf("the drawn peek says r run with no launch: %q", last)
	}
	if hint := (placeFactory{}).hint(a); strings.Contains(hint, "r run") {
		t.Fatalf("the hint says r run with no launch: %q", hint)
	}

	// Over a seam that can launch, both say it.
	f := &factoryFake{}
	b := factoryVerbLab(t, f)
	factoryOn(t, b, 4)
	got, _ := b.factoryCursorItem()
	if words := b.factoryActionWords(got); !strings.HasPrefix(words, "enter open · r run") {
		t.Fatalf("with a launch the peek does not say r run: %q", words)
	}
	if hint := (placeFactory{}).hint(b); strings.Contains(hint, "r run") {
		t.Fatalf("with a launch the bottom bar repeats the strip: %q", hint)
	}
}

// THE TITLE HAS A ROW OF ITS OWN AND THE META THE ROW UNDER IT: at 150
// columns the title keeps its words, and a meta too wide for the column drops
// its facts from the right, whole, before its first one is cut.
func TestFactoryPeekTitleOutlastsTheMeta(t *testing.T) {
	a := factoryPlaceLab(t)
	it := *factoryPaneItem(t, a, 4)
	it.Title = "fix the double count in the ledger"
	it.Repo = "ledger"
	it.Origin = factory.OriginChat
	it.URL = ""
	measure := factoryPaneW(150) - factoryMargin
	block := a.factoryPeekTitle(it, measure)
	if len(block) != 2 {
		t.Fatalf("the title block is %d rows: %q", len(block), block)
	}
	if got := ansi.Strip(block[0]); got != it.Ref()+" "+it.Title {
		t.Fatalf("the title row is %q", got)
	}
	meta := a.factoryMeta(it)
	if got := ansi.Strip(block[1]); got != strings.Join(meta, rowSep) {
		t.Fatalf("the meta row is %q, want %q", got, strings.Join(meta, rowSep))
	}
	if block[1] != a.pal.dim(ansi.Strip(block[1])) {
		t.Fatalf("the meta row is not dim: %q", block[1])
	}
	// Narrow, the meta drops from the right and keeps its first fact.
	narrow := ansi.Strip(a.factoryPeekTitle(it, 14)[1])
	if !strings.HasPrefix(narrow, meta[0]) || strings.Contains(narrow, meta[len(meta)-1]) {
		t.Fatalf("a narrow meta did not drop from the right: %q", narrow)
	}
	// A title too long for the column is cut on its own row.
	it.Title = strings.Repeat("a long title ", 10)
	if row := ansi.Strip(a.factoryPeekTitle(it, measure)[0]); ansi.StringWidth(row) > measure {
		t.Fatalf("a long title overflowed its row: %q", row)
	}
}
