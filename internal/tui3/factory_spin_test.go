package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── the floor's spinners turn (owner's screenshot, 2026-10-08) ─────────────

// factorySpinClock swaps the surface's tick for one that records the delay
// each arming asked for, and arms nothing.
func factorySpinClock(t *testing.T) *[]time.Duration {
	t.Helper()
	old := surfaceTick
	t.Cleanup(func() { surfaceTick = old })
	var delays []time.Duration
	surfaceTick = func(d time.Duration, cb func(time.Time) tea.Msg) tea.Cmd {
		delays = append(delays, d)
		return func() tea.Msg { return nil }
	}
	return &delays
}

// factorySpinLab is the polish lab with nothing else on the surface moving,
// so the floor is the whole of what can hold the clock.
func factorySpinLab(t *testing.T) (*app, *factoryPolishLab) {
	t.Helper()
	a, lab := newFactoryPolishLab(t)
	a.linear = false
	return a, lab
}

// factoryNotePlain is the place's note line, plain.
func factoryNotePlain(a *app) string {
	return ansi.Strip(strings.Join((placeFactory{}).note(a, 150), ""))
}

// A DOOR A KEY ASKED, OUT: the floor arms the clock at the spinner's own
// cadence, and the note line's glyph moves from one step to the next.
func TestFactoryDoorSpinnerTurns(t *testing.T) {
	a, _ := factorySpinLab(t)
	delays := factorySpinClock(t)
	a.fp.act.doing = "asking gh…"
	if !a.factorySpinning() {
		t.Fatal("a door out is not a floor in flight")
	}
	first := factoryNotePlain(a)
	if !strings.Contains(first, "asking gh…") {
		t.Fatalf("the note line while the door is out is %q", first)
	}
	*delays = nil
	for range spinnerStep {
		a.paint()
	}
	if len(*delays) == 0 || (*delays)[len(*delays)-1] != a.frameEvery()*spinnerStep {
		t.Fatalf("the floor asked for frames at %v, not the spinner's cadence", *delays)
	}
	second := factoryNotePlain(a)
	if second == first || !strings.Contains(second, "asking gh…") {
		t.Fatalf("the spinner stood still: %q then %q", first, second)
	}
	a.fp.act.doing = ""
	a.paint()
	if a.factorySpinning() || a.painting {
		t.Fatal("the door answered and the clock kept turning")
	}
}

// AN ITEM BEING READ, ARRIVING FROM A READ OFF THE LOOP: the reply that
// folds the busy entry in starts the clock from still; a row only waiting its
// turn turns nothing.
func TestFactoryBusyReadStartsTheSpinner(t *testing.T) {
	a, lab := factorySpinLab(t)
	delays := factorySpinClock(t)
	lab.busy[6] = factory.BusyWaiting
	factoryLabRead(t, a)
	if a.factorySpinning() {
		t.Fatal("a row waiting its turn turned the clock")
	}
	lab.busy[6] = factory.BusyReading
	a.painting = false
	a.Update(doorMsg{front: a.frontGen, fold: func(bool) tea.Cmd {
		factoryLabRead(t, a)
		return nil
	}})
	if !a.factorySpinning() || !a.painting {
		t.Fatal("a read arriving from off the loop did not wake the still clock")
	}
	*delays = nil
	a.paint()
	if len(*delays) == 0 || (*delays)[len(*delays)-1] != a.frameEvery()*spinnerStep {
		t.Fatalf("a busy item needs only spinner-cadence frames: %v", *delays)
	}
}

// A SOURCE MID-POLL spins the handover's clause, so it holds the clock too.
func TestFactoryPollingSourceSpins(t *testing.T) {
	a, lab := factorySpinLab(t)
	factorySpinClock(t)
	lab.polled = true
	factoryLabRead(t, a)
	if !a.factorySpinning() {
		t.Fatal("a source mid-poll is not a floor in flight")
	}
	lab.polled = false
	factoryLabRead(t, a)
	if a.factorySpinning() {
		t.Fatal("the poll ended and the floor still says it is in flight")
	}
}

// NOTHING IN FLIGHT ARMS NOTHING, and neither does a floor nobody is looking
// at.
func TestFactoryStillFloorArmsNoClock(t *testing.T) {
	a, _ := factorySpinLab(t)
	delays := factorySpinClock(t)
	if a.factorySpinning() {
		t.Fatal("a still floor says something is in flight")
	}
	a.painting = true
	*delays = nil
	a.paint()
	for _, d := range *delays {
		if d == a.frameEvery()*spinnerStep {
			t.Fatalf("a still floor armed the spinner's clock: %v", *delays)
		}
	}
	if a.painting {
		t.Fatal("a still floor kept the paint clock turning")
	}
	a.fp.act.doing = "asking gh…"
	a.page = pageHome
	if a.factorySpinning() {
		t.Fatal("a floor off screen turned the clock")
	}
}
