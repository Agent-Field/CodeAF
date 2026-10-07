package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// factoryHeadLab is the factory place over the fixture with snap folded in
// instead, on the plain floor so a row reads as its words.
func factoryHeadLab(t *testing.T, snap factory.Snapshot) *app {
	t.Helper()
	a := factoryPlaceLab(t)
	a.pal = newPalette(tokens.NoColor, false)
	a.fp.snap = snap
	return a
}

// factoryHeadPlain is the strip at width, each row stripped of its paint.
func factoryHeadPlain(a *app, width int) []string {
	rows := a.factoryHead(width)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r)
	}
	return out
}

// THE FIXTURE'S HANDOVER CARRIES THE FIXTURE'S COUNTS: the shift's start and
// length and spend on the heading, what shipped by name and what arrived, the
// one item waiting, and the floor's repos, benches and money.
func TestFactoryHeadCarriesTheFixturesCounts(t *testing.T) {
	a := factoryHeadLab(t, factory.Fixture(factoryTestNow))
	rows := factoryHeadPlain(a, 110)
	if len(rows) != 5 {
		t.Fatalf("the strip is %d rows, want four and a blank: %q", len(rows), rows)
	}
	for i, want := range []string{
		"handover · since 04:00 · 8h · $8.44 ─",
		"1 shipped #1663 · 4 arrived",
		"? 1 waiting on you",
		"3 repos · benches 1/6",
	} {
		if !strings.Contains(rows[i], want) {
			t.Errorf("row %d is missing %q:\n%q", i, want, rows[i])
		}
	}
	if !strings.HasSuffix(strings.TrimRight(rows[2], " "), "24h "+strings.Repeat("▁", 17)+"▅▁█▁▁▄▁") {
		t.Errorf("the sparkline does not end at the current hour:\n%q", rows[2])
	}
	if !strings.HasSuffix(strings.TrimRight(rows[3], " "), "$11.31 / $60 today") {
		t.Errorf("the day's money is not at the right of the floor's facts:\n%q", rows[3])
	}
	if strings.TrimSpace(rows[4]) != "" {
		t.Errorf("the strip's last row is not blank: %q", rows[4])
	}
}

// EVERY ZERO CLAUSE IS ABSENT: no handled clause when nothing was handled, no
// spend on the heading when nothing was spent, no figure on the day's money
// when nothing was spent today, and no speed for a real engine.
func TestFactoryHeadDrawsNoZeroClause(t *testing.T) {
	snap := factory.Fixture(factoryTestNow)
	snap.Shift.Shipped, snap.Shift.Shipping = 0, nil
	snap.Shift.Spent = 0
	snap.Daily = 0
	a := factoryHeadLab(t, snap)
	rows := factoryHeadPlain(a, 110)
	text := strings.Join(rows, "\n")
	for _, never := range []string{"shipped", "handled", "$0", "$—", " 0 ", "×"} {
		if strings.Contains(text, never) {
			t.Errorf("the strip says %q over a zero:\n%s", never, text)
		}
	}
	if !strings.Contains(rows[1], "4 arrived") {
		t.Errorf("the arrived clause went with the zeros:\n%q", rows[1])
	}
	if !strings.HasSuffix(strings.TrimRight(rows[3], " "), "/ $60 today") {
		t.Errorf("a day with nothing spent does not draw the rail alone:\n%q", rows[3])
	}

	// And each clause that does count is said, a question in the singular.
	snap = factory.Fixture(factoryTestNow)
	snap.Shift.Shipped, snap.Shift.Shipping, snap.Shift.Handled = 2, []string{"#1661", "#1663"}, 1
	snap.Speed = 30 * time.Second
	rows = factoryHeadPlain(factoryHeadLab(t, snap), 110)
	if !strings.Contains(rows[1], "2 shipped #1661 #1663 · 4 arrived · 1 question handled") {
		t.Errorf("the shift row is %q", rows[1])
	}
	if !strings.HasSuffix(strings.TrimRight(rows[3], " "), "$11.31 / $60 today · 150×") {
		t.Errorf("the mock clock's speed is not after the money:\n%q", rows[3])
	}
}

// AN ALL-ZERO SHIFT SAYS ONE QUIET SENTENCE, and a floor with nothing waiting
// says that in words rather than a zero.
func TestFactoryHeadQuietShift(t *testing.T) {
	snap := factory.Fixture(factoryTestNow)
	snap.Shift = factory.Shift{Since: snap.Shift.Since}
	snap.Items = nil
	a := factoryHeadLab(t, snap)
	rows := factoryHeadPlain(a, 110)
	if got := strings.TrimSpace(rows[1]); got != factoryHeadQuietWords {
		t.Errorf("an empty shift draws %q, want %q", got, factoryHeadQuietWords)
	}
	if got := strings.TrimSpace(rows[2]); got != factoryHeadNoWaitWords {
		t.Errorf("nothing waiting draws %q, want %q", got, factoryHeadNoWaitWords)
	}
	if strings.Contains(rows[0], "$") || strings.Contains(rows[2], factoryHeadHoursWord) {
		t.Errorf("an empty shift drew a spend or a sparkline:\n%s", strings.Join(rows, "\n"))
	}
}

// EVERY ROW IS EXACTLY THE WIDTH, at a wide pane, a middling one and one under
// the sparkline's floor, and under that floor the sparkline is gone.
func TestFactoryHeadRowsAreExactlyTheWidth(t *testing.T) {
	snap := factory.Fixture(factoryTestNow)
	snap.Speed = 30 * time.Second
	snap.Sources = []factory.SourceInfo{
		{Name: "chat"},
		{Name: "github", Writes: true, Polled: factoryTestNow.Add(-4 * time.Minute)},
	}
	a := factoryHeadLab(t, snap)
	for _, width := range []int{110, 80, 50} {
		rows := a.factoryHead(width)
		for i, r := range rows {
			if got := ansi.StringWidth(r); got != width {
				t.Errorf("at %d row %d is %d cells: %q", width, i, got, ansi.Strip(r))
			}
		}
		spark := strings.Contains(ansi.Strip(rows[2]), factoryHeadHoursWord)
		if want := width >= factoryHeadSparkFloor; spark != want {
			t.Errorf("at %d the sparkline is drawn=%v, want %v: %q", width, spark, want, ansi.Strip(rows[2]))
		}
	}
	if row := ansi.Strip(a.factoryHead(110)[3]); !strings.Contains(row, "3 repos · github · chat · benches 1/6 · polled 4m ago") {
		t.Errorf("the floor's facts are %q", row)
	}
}

// THE PLAIN FLOOR DRAWS NO SGR, on a full shift and on an empty one.
func TestFactoryHeadPlainFloorHasNoSGR(t *testing.T) {
	full := factory.Fixture(factoryTestNow)
	full.Speed = 30 * time.Second
	quiet := full
	quiet.Shift, quiet.Daily = factory.Shift{}, 0
	for _, snap := range []factory.Snapshot{full, quiet} {
		a := factoryHeadLab(t, snap)
		for _, width := range []int{110, 80, 50} {
			for i, r := range a.factoryHead(width) {
				if strings.Contains(r, "\x1b") {
					t.Errorf("at %d row %d carries SGR at the plain floor: %q", width, i, r)
				}
			}
		}
	}
}

// THE PAGE STACKS THE HANDOVER ABOVE THE PANE in the pane column.
func TestFactoryPageStacksTheHandoverAboveThePane(t *testing.T) {
	a := factoryPlaceLab(t)
	a.height = 40
	head, pane := -1, -1
	for i, line := range strings.Split(factoryFrameText(a), "\n") {
		cut := strings.Index(line, "│")
		if cut < 0 {
			continue
		}
		right := line[cut:]
		if head < 0 && strings.Contains(right, "handover · since") {
			head = i
		}
		if pane < 0 && strings.Contains(right, "#1538  agentfield/codeaf") {
			pane = i
		}
	}
	if head < 0 || pane < 0 || pane != head+5 {
		t.Fatalf("the pane does not start five rows under the handover (head %d, pane %d):\n%s", head, pane, factoryFrameText(a))
	}
}
