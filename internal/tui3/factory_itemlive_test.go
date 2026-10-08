package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── the item page is alive while a stage runs (owner's screenshot, 2026-10-08)

// factoryPlanLab is the verb lab with item 2 running its plan stage, every
// stage after it still to come, its log what *log holds on each read, and the
// surface's clock at *now. The item page is open on it, on the plan row.
func factoryPlanLab(t *testing.T) (*app, *factoryFake, *time.Time, *[]factory.LogLine) {
	t.Helper()
	now := factoryTestNow
	log := []factory.LogLine{
		{At: factoryTestNow, Tone: "said", Text: "queued"},
		{At: factoryTestNow, Tone: "shell", Text: "ls"},
	}
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		s := *it.Stream
		s.Phases = append([]factory.Phase(nil), s.Phases...)
		for i := range s.Phases {
			s.Phases[i].State = factory.PhasePending
		}
		s.Phases[0].State = factory.PhaseRunning
		s.Log = append([]factory.LogLine(nil), log...)
		it.Stream = &s
	})
	a := factoryVerbLab(t, f)
	a.linear = false
	a.clock = func() time.Time { return now }
	factoryLabRead(t, a)
	factoryOn(t, a, 2)
	drive(t, a, key("enter"))
	if !a.fp.open {
		t.Fatal("enter did not open the item page")
	}
	if r := factoryRowNamed(t, a, "plan"); r.view.kind != factoryMarkRunning {
		t.Fatalf("the lab's plan stage is not running: %+v", r.view)
	}
	return a, f, &now, &log
}

// factoryPlanCell is the plan stage's rail cell as the page draws it, plain.
func factoryPlanCell(t *testing.T, a *app) string {
	t.Helper()
	for _, row := range factoryBodyPlain(a, a.width, 30) {
		left, _, div := factorySplitAt(row)
		if div < 0 {
			continue
		}
		if f := strings.Fields(left); len(f) >= 2 && f[1] == "plan" {
			return strings.TrimSpace(left)
		}
	}
	t.Fatalf("no plan row on the rail:\n%s", strings.Join(factoryBodyPlain(a, a.width, 30), "\n"))
	return ""
}

// factoryPlanPane is the item page's pane, plain, one line per row.
func factoryPlanPane(a *app) string {
	var pane []string
	for _, row := range factoryBodyPlain(a, a.width, 30) {
		if _, right, div := factorySplitAt(row); div >= 0 {
			pane = append(pane, strings.TrimSpace(right))
		}
	}
	return strings.Join(pane, "\n")
}

// THE RUNNING STAGE TURNS: its mark on the rail and at the head of its pane's
// running line is the transcript's spinner, and it moves from one spinner
// step to the next. The floor's row keeps the working mark.
func TestFactoryItemPageRunningStageSpins(t *testing.T) {
	a, _, _, _ := factoryPlanLab(t)
	if !a.factorySpinning() {
		t.Fatal("an open page with a running stage holds no clock")
	}
	first := a.factorySpin()
	cell := factoryPlanCell(t, a)
	if !strings.HasPrefix(cell, first+" plan") {
		t.Fatalf("the running plan row does not wear the spinner %q: %q", first, cell)
	}
	if pane := factoryPlanPane(a); !strings.Contains(pane, first) {
		t.Fatalf("the running pane does not lead with the spinner %q:\n%s", first, pane)
	}
	for range spinnerStep {
		a.paint()
	}
	second := a.factorySpin()
	if second == first {
		t.Fatal("the spinner did not step across a spinner step of paints")
	}
	if cell2 := factoryPlanCell(t, a); cell2 == cell || !strings.HasPrefix(cell2, second+" plan") {
		t.Fatalf("the running plan row stood still: %q then %q", cell, cell2)
	}
	if pane := factoryPlanPane(a); !strings.Contains(pane, second) {
		t.Fatalf("the pane's spinner stood still:\n%s", pane)
	}
	it, _ := a.factoryCursorItem()
	if lead := ansi.Strip(a.itemLeadMark(it)); lead != a.icon(tokens.GWorking) {
		t.Fatalf("the floor's running lead changed: %q", lead)
	}
}

// THE SECOND BEAT is armed while the page is open on a running stage, and
// asks nothing once the page closes or the item lands.
func TestFactoryItemPageSecondBeat(t *testing.T) {
	a, f, _, _ := factoryPlanLab(t)
	if !a.factoryWantsSecondBeat() {
		t.Fatal("an open page with a running stage wants no one-second beat")
	}
	if !a.fp.readingArmed {
		t.Fatal("opening the page on a running stage armed no beat")
	}
	if a.factoryReadSoonWake() != nil {
		t.Fatal("a second beat was armed beside the one in the air")
	}
	if a.factoryReadSoon(a.fp.readingGen) == nil {
		t.Fatal("the beat on an open running page asked nothing")
	}
	drive(t, a, key("esc"))
	if a.fp.open || a.factoryWantsSecondBeat() {
		t.Fatal("the closed page still wants the one-second beat")
	}
	if a.factoryReadSoon(a.fp.readingGen) != nil || a.fp.readingArmed {
		t.Fatal("the beat went on after the page closed")
	}

	drive(t, a, key("enter"))
	if !a.fp.readingArmed {
		t.Fatal("opening the page again armed no beat")
	}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		it.State = factory.StateLanded
		s := *it.Stream
		s.Phases = append([]factory.Phase(nil), s.Phases...)
		for i := range s.Phases {
			s.Phases[i].State = factory.PhaseDone
		}
		it.Stream = &s
	})
	factoryLabRead(t, a)
	if a.factoryWantsSecondBeat() {
		t.Fatal("a landed item's page still wants the one-second beat")
	}
	if a.factoryReadSoon(a.fp.readingGen) != nil {
		t.Fatal("the beat went on after the item landed")
	}
}

// THE TIME COUNTS FROM THE MOMENT AND NOW: a second on the surface's clock is
// a second more on the rail, with no new snapshot read.
func TestFactoryItemPageElapsedCounts(t *testing.T) {
	a, _, now, _ := factoryPlanLab(t)
	*now = now.Add(15 * time.Second)
	if cell := factoryPlanCell(t, a); !strings.HasSuffix(cell, "plan · 15s") {
		t.Fatalf("fifteen seconds in, the plan row says %q", cell)
	}
	*now = now.Add(time.Second)
	if cell := factoryPlanCell(t, a); !strings.HasSuffix(cell, "plan · 16s") {
		t.Fatalf("a second later with no read, the plan row says %q", cell)
	}
	if pane := factoryPlanPane(a); !strings.Contains(pane, "16s") {
		t.Fatalf("the pane's time did not count:\n%s", pane)
	}
}

// THE LOG GROWS: a line the runner appended is on the running pane after the
// beat's read, newest last.
func TestFactoryItemPageLogGrows(t *testing.T) {
	a, _, _, log := factoryPlanLab(t)
	const fresh = "read ×4 · factory_item.go"
	if strings.Contains(factoryPlanPane(a), fresh) {
		t.Fatal("the lab's log already holds the fresh line")
	}
	*log = append(*log, factory.LogLine{At: factoryTestNow, Tone: "shell", Text: fresh})
	spend(t, a, a.factoryReadSoon(a.fp.readingGen))
	pane := factoryPlanPane(a)
	if !strings.Contains(pane, fresh) {
		t.Fatalf("the beat's read did not grow the log:\n%s", pane)
	}
	if strings.Index(pane, fresh) < strings.Index(pane, "queued") {
		t.Fatalf("the newest line does not stand below the oldest:\n%s", pane)
	}
}
