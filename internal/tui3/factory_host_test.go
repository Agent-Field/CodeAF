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

// THE PEEK'S KEY LINE SAYS `r run` AND `p plan first` ONLY WHERE A LAUNCH
// STANDS BEHIND THEM, by the same predicate as the hint line under the page.
func TestFactoryPeekKeysNeedALaunch(t *testing.T) {
	// The still fixture has no engine door, like the person's own floor.
	a := factoryPlaceLab(t)
	it := *factoryPaneItem(t, a, 4)
	words := a.factoryActionWords(it)
	for _, gone := range []string{"r run", "p plan first", "L launch"} {
		if strings.Contains(words, gone) {
			t.Fatalf("with no launch the peek says %q: %q", gone, words)
		}
	}
	if !strings.HasPrefix(words, "enter open") || !strings.Contains(words, "space mark") {
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
	if words := b.factoryActionWords(got); !strings.Contains(words, "r run · p plan first") {
		t.Fatalf("with a launch the peek does not say r run: %q", words)
	}
	if hint := (placeFactory{}).hint(b); !strings.Contains(hint, "r run") {
		t.Fatalf("with a launch the hint does not say r run: %q", hint)
	}
}

// AT 150 COLUMNS THE TITLE KEEPS ITS WORDS AND THE META YIELDS, dropping its
// facts from the right, before the title is cut at all.
func TestFactoryPeekTitleOutlastsTheMeta(t *testing.T) {
	a := factoryPlaceLab(t)
	it := *factoryPaneItem(t, a, 4)
	it.Title = "fix the double count in the ledger"
	it.Repo = "ledger"
	it.Origin = factory.OriginChat
	measure := factoryPaneW(150) - factoryPaneLead
	row := ansi.Strip(a.factoryTitleRow(it, measure))
	if ansi.StringWidth(row) != measure {
		t.Fatalf("the title row is %d cells, want %d: %q", ansi.StringWidth(row), measure, row)
	}
	if !strings.Contains(row, it.Ref()+" "+it.Title) {
		t.Fatalf("the title was cut while the meta kept facts: %q", row)
	}
	meta := a.factoryMeta(it)
	if !strings.Contains(row, meta[0]) {
		t.Fatalf("the meta dropped its first fact while there was room for it: %q", row)
	}
	if strings.Contains(row, "from a chat") && !strings.Contains(row, strings.Join(meta, rowSep)) {
		t.Fatalf("the meta dropped from the left rather than the right: %q", row)
	}

	// A title too long for any meta beside it is cut, and only then.
	it.Title = strings.Repeat("a long title ", 10)
	row = ansi.Strip(a.factoryTitleRow(it, measure))
	if strings.Contains(row, meta[0]) || ansi.StringWidth(row) > measure {
		t.Fatalf("a title with no room kept meta beside it: %q", row)
	}
}
