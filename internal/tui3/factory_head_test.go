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
	// These tests read the full strip, which is `h` (factory_head.go); the
	// one line is factory_polish_test.go's.
	a.fp.headFull = true
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
// two items waiting (a question and a landed sheet), and the floor's repos, benches and money.
func TestFactoryHeadCarriesTheFixturesCounts(t *testing.T) {
	a := factoryHeadLab(t, factory.Fixture(factoryTestNow))
	rows := factoryHeadPlain(a, 110)
	if len(rows) != 5 {
		t.Fatalf("the strip is %d rows, want four and a blank: %q", len(rows), rows)
	}
	for i, want := range []string{
		"handover · since 04:00 · 8h · $8.44 ─",
		"1 shipped #1663 · 4 arrived",
		"? 2 waiting on you",
		"3 repos · github · chat · benches 1/6 · polled 14s ago",
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
	rows = factoryHeadPlain(factoryHeadLab(t, snap), 110)
	if !strings.Contains(rows[1], "2 shipped #1661 #1663 · 4 arrived · 1 question handled") {
		t.Errorf("the shift row is %q", rows[1])
	}
	if !strings.HasSuffix(strings.TrimRight(rows[3], " "), "$11.31 / $60 today") {
		t.Errorf("the money is not at the right of the floor's facts:\n%q", rows[3])
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

// THE HANDOVER SPANS THE WHOLE WIDTH ABOVE BOTH COLUMNS: its heading starts
// at the frame's left edge with no rule to its left, and the two columns start
// on the row after its blank, with the peek's first row on that same row.
func TestFactoryPageStacksTheHandoverAboveTheColumns(t *testing.T) {
	a := factoryPlaceLab(t)
	a.width, a.height = 150, 40
	head, cols := -1, -1
	for i, line := range strings.Split(factoryFrameText(a), "\n") {
		if head < 0 && strings.Contains(line, "◆ ") {
			if strings.Contains(line, "│") {
				t.Fatalf("the handover shares its row with the column rule: %q", line)
			}
			head = i
		}
		if cols < 0 && strings.Contains(line, "│") {
			cols = i
		}
	}
	// THE ONE-LINE HANDOVER is its line and a blank, and the columns start
	// on the row after (factory_head.go).
	if head < 0 || cols != head+2 {
		t.Fatalf("the columns do not start two rows under the handover (head %d, columns %d):\n%s", head, cols, factoryFrameText(a))
	}
}

// A source whose last read failed says so beside its name and keeps the
// time of its last good read.
func TestFactoryHeadSaysASourceIsInTrouble(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	snap := factory.Snapshot{Now: now, Sources: []factory.SourceInfo{
		{Name: "chat"}, {Name: "terminal"},
		{Name: "github", Polled: now.Add(-4 * time.Minute), Trouble: "not reachable"},
	}}
	got := ansi.Strip(rowTail(factoryHeadFacts(snap), 200))
	if !strings.Contains(got, "github · not reachable") || !strings.Contains(got, "polled 4m ago") {
		t.Fatalf("the facts line reads %q", got)
	}
}

// A LANDED ITEM WAITS ON YOU: its sheet waits for the sign-off, so a floor
// whose only item has landed does not say `nothing waits on you`, and the
// handover, its full form and the tab bar count it alike. The 2026-10-08
// hand run landed #1 under `nothing waits on you`.
func TestFactoryALandedItemCountsAsWaiting(t *testing.T) {
	full := factory.Fixture(factoryTestNow)
	var landed factory.Item
	for _, it := range full.Items {
		if it.State == factory.StateLanded {
			landed = it
		}
	}
	snap := full
	snap.Items = []factory.Item{landed}
	a := factoryHeadLab(t, snap)
	a.fp.loaded = true
	text := strings.Join(factoryHeadPlain(a, 110), "\n")
	if strings.Contains(text, factoryHeadNoWaitWords) || !strings.Contains(text, "? 1 waiting on you") {
		t.Fatalf("a landed item is not counted as waiting:\n%s", text)
	}
	a.fp.headFull = false
	if line := strings.Join(factoryHeadPlain(a, 150), "\n"); !strings.Contains(line, "? 1 waiting") {
		t.Fatalf("the one-line handover does not count the landed item:\n%s", line)
	}
	if n := a.factoryWaiting(); n != 1 {
		t.Fatalf("the tab bar counts %d waiting, want 1", n)
	}
	a.fp.snap = full
	if n, want := a.factoryWaiting(), full.Count(factory.StateNeedsYou)+full.Count(factory.StateLanded); n != want || n != 2 {
		t.Fatalf("the fixture counts %d waiting, want %d", n, want)
	}
}
