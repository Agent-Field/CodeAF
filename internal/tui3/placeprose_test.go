package tui3

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
	// money and nothing else (FIDELITY.md item 1, styles.go's [hueMoney]), so
	// the seam is [palette.money] now and the two greens are two facts again.
	if placeMoneyInk(pal)("$1") != pal.money("$1") {
		t.Fatal("money moved away from its shared ink seam")
	}
	if placeMoneyInk(pal)("$1") == pal.add("$1") {
		t.Fatal("money and the landed tick are one colour again")
	}
}

// ── the time window, on every place that has one (SCREEN 3d) ────────────────

// EVERY PLACE WITH A TIME WINDOW DRAWS THE SAME CONTROL, on its own head row:
// the label between the arrows, which is the control and the reading at once.
//
// The three used to answer this three ways. Standing drew the arrows; spend drew
// a legend that named the keys and never the span; THE TASKS PLACE DREW NOTHING
// AT ALL while binding all four keys, which is the exact defect verbstrip.go's
// law is written against — four keys bound and nothing on screen naming them.
func TestEveryPlaceWithATimeWindowDrawsTheSameControl(t *testing.T) {
	pal := newPalette(tokens.NoColor, false)
	win := session.LastDays(time.Date(2026, time.August, 25, 12, 0, 0, 0, time.Local), 14)
	label := "shift+← " + win.Label() + " →"

	standing := plain(standingHeaderRow(120, win, pal))
	if !strings.Contains(standing, label) {
		t.Fatalf("the standing head row draws no control: %q", standing)
	}
	spend := plain(spendTestReading().rows(120, pal)[0])
	if !strings.Contains(spend, label) {
		t.Fatalf("the spend head row draws no control: %q", spend)
	}
	head := plain(placeHeadRow(120, "work aforge ran on its own. 7.", "", win, pal))
	if !strings.Contains(head, label) {
		t.Fatalf("the shared head row draws no control: %q", head)
	}
	// AND THE SECOND AXIS IS NAMED BESIDE IT, one key at either end of the grain
	// ladder and two in the middle — every direction that would actually move.
	if !strings.Contains(head, placeCoarserWords) || strings.Contains(head, placeFinerWords) {
		t.Fatalf("a window on days named the wrong zoom keys: %q", head)
	}
	monthly := plain(placeHeadRow(120, "work", "", session.UsageWindow{
		From: win.From, To: win.To, Grain: session.GrainMonth}, pal))
	if !strings.Contains(monthly, placeFinerWords) || strings.Contains(monthly, placeCoarserWords) {
		t.Fatalf("a window on months named the wrong zoom keys: %q", monthly)
	}
}

// AND A FRAME WITH NO ROOM FOR THE CONTROL HAS NO WINDOW AT ALL. One predicate
// answers the paint and the keys, separately for each half, so a chord is never
// bound where the clause naming it is off the line.
func TestAWindowIsBoundOnlyWhereItsControlIsDrawn(t *testing.T) {
	win := session.LastDays(time.Date(2026, time.August, 25, 12, 0, 0, 0, time.Local), 14)
	head := "work aforge ran on its own. 7."
	if arrows, grain := placeWindowFits(200, head, win); !arrows || !grain {
		t.Fatalf("a wide frame drew neither half: arrows %v, grain %v", arrows, grain)
	}
	// Wide enough for the arrows, not for the zoom clause beside them.
	narrow := len(head) + len("shift+← "+win.Label()+" →") + placeHeadGap + 2
	if arrows, grain := placeWindowFits(narrow, head, win); !arrows || grain {
		t.Fatalf("a frame with room for the arrows alone answered arrows %v, grain %v", arrows, grain)
	}
	if arrows, grain := placeWindowFits(20, head, win); arrows || grain {
		t.Fatalf("a frame with room for neither answered arrows %v, grain %v", arrows, grain)
	}
	// AND A WINDOW WITH NO SPAN DRAWS NOTHING — not the arrows, not an empty pair.
	if arrows, _ := placeWindowFits(200, head, session.UsageWindow{}); arrows {
		t.Fatal("a window nobody has chosen yet drew a control over no reading")
	}
}
