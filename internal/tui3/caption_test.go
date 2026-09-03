package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

func captionFixture() []entry {
	base := time.Unix(100, 0)
	return []entry{
		{kind: entryUser, text: "why?", turn: 1},
		{kind: entryAssistant, text: "checking where the fold is minted.\nThe details follow.", turn: 1, settled: true},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK, detail: toolDetail{Args: `{"path":"internal/tui3/render.go"}`}, began: base, ended: base.Add(time.Second)},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK, detail: toolDetail{Args: `{"path":"internal/tui3/workfold.go"}`}, began: base, ended: base.Add(2 * time.Second)},
		{kind: entryAssistant, text: "The fold is minted in deckRows.", turn: 1, settled: true},
	}
}

func captionsOf(es []entry, running int) []caption {
	stampHierarchy(es, deriveWorkfolds(es, running))
	return deriveCaptions(es, running)
}

func TestACaptionIsTheFirstLineOfTheProseThatWorkFollows(t *testing.T) {
	es := captionFixture()
	got := captionsOf(es, 0)
	if len(got) != 1 || got[0].text != "checking where the fold is minted" || got[0].source != captionSaid {
		t.Fatalf("caption = %#v", got)
	}
	stampCaptions(es, got)
	if !es[1].capHead || es[1].capCut != strings.IndexByte(es[1].text, '\n')+1 {
		t.Fatalf("head was not lifted: %#v", es[1])
	}
}

func TestTheAnswersFirstLineIsNeverACaption(t *testing.T) {
	es := captionFixture()
	for _, c := range captionsOf(es, 0) {
		if strings.Contains(c.text, "fold is minted in deckRows") {
			t.Fatalf("answer became a caption: %#v", c)
		}
	}
}

func TestASingleCallGetsNoCaption(t *testing.T) {
	es := captionFixture()
	es = append(es[:3], es[4:]...)
	if got := captionsOf(es, 0); len(got) != 0 {
		t.Fatalf("single call got captions: %#v", got)
	}
}

func TestTheCompositeStandsWhenTheModelSaidNothing(t *testing.T) {
	es := captionFixture()
	es = append(es[:1], es[2:]...)
	got := captionsOf(es, 0)
	if len(got) != 1 || got[0].text != "reading 2 files in internal/tui3" || got[0].source != captionMade {
		t.Fatalf("composite = %#v", got)
	}
}

func TestFourReadsUnderOneDirectoryNameThatDirectory(t *testing.T) {
	var es []entry
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		es = append(es, entry{kind: entryTool, tool: "read", turn: 1, status: toolOK,
			detail: toolDetail{Args: `{"path":"internal/tui3/` + name + `"}`}})
	}
	if got := composeCaption(es, 0, len(es)); got != "reading 4 files in internal/tui3" {
		t.Fatalf("composite = %q", got)
	}
}

func TestConsecutiveHeadsWithNoWorkBetweenThemMerge(t *testing.T) {
	es := captionFixture()
	es = append(es[:2], append([]entry{
		{kind: entryAssistant, text: "another paragraph.", turn: 1, settled: true},
	}, es[2:]...)...)
	got := captionsOf(es, 0)
	if len(got) != 1 || got[0].head != 1 {
		t.Fatalf("heads did not merge: %#v", got)
	}
}

func TestExpandingACaptionStopsItsShimmerAndStartsTheRowSpinners(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	c := caption{text: "checking the fold", start: 1, calls: 2, began: time.Unix(100, 0)}
	a.clock = func() time.Time { return time.Unix(104, 0) }
	a.paints = 0
	closed := a.captionRow(c, true, false, 80).text
	a.paints = shimmerPeriod / 2
	if next := a.captionRow(c, true, false, 80).text; next == closed {
		t.Fatal("collapsed live caption did not shimmer")
	}
	a.paints = 0
	open := a.captionRow(c, true, true, 80).text
	a.paints = shimmerPeriod / 2
	if next := a.captionRow(c, true, true, 80).text; next != open {
		t.Fatal("expanded caption kept shimmering")
	}
}

func TestTheLinearTierDrawsNoShimmer(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.linear = true
	a.paints = 0
	first := a.shimmer("checking")
	a.paints = shimmerPeriod / 2
	if second := a.shimmer("checking"); second != first {
		t.Fatalf("linear shimmer moved: %q then %q", first, second)
	}
}

func TestTheChipOpensToTheOutlineAndNotTheMachinery(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, a.workMode = foldFixture(), config.WorkFold
	a.toggleLatestWorkfold()
	page := strings.Join(plainRows(a), "\n")
	if !strings.Contains(page, "I will inspect it") {
		t.Fatalf("outline lost its caption:\n%s", page)
	}
	if strings.Contains(page, "read") || strings.Contains(page, "bash") {
		t.Fatalf("outline exposed machinery:\n%s", page)
	}
}

func TestACaptionKeepsItsWordsAndItsTenseAfterSettle(t *testing.T) {
	es := captionFixture()
	before := captionsOf(es, 1)[0].text
	after := captionsOf(es, 0)[0].text
	if before != after || after != "checking where the fold is minted" {
		t.Fatalf("caption changed from %q to %q", before, after)
	}
}

func TestAnInterruptedTurnLeavesItsLastCaptionStill(t *testing.T) {
	es := captionFixture()
	for i := range es {
		if es[i].turn == 1 && es[i].kind != entryUser {
			es[i].cut = true
		}
	}
	if got := captionsOf(es, 0); len(got) != 1 {
		t.Fatalf("interrupted caption disappeared: %#v", got)
	}
}

func TestATurnWithNoWorkHasNoCaptions(t *testing.T) {
	es := []entry{{kind: entryUser, text: "hello", turn: 1},
		{kind: entryAssistant, text: "hello", turn: 1, settled: true}}
	if got := captionsOf(es, 0); len(got) != 0 {
		t.Fatalf("text-only turn got captions: %#v", got)
	}
}
