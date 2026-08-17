package orchestrate

import (
	"context"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMeter(t *testing.T) {
	if got, want := MeterCall(1_000_000, 0, "openai/gpt-5"), 1.25; math.Abs(got-want) > 1e-9 {
		t.Fatalf("input tokens: got %v want %v", got, want)
	}
	if got, want := MeterCall(0, 1_000_000, "anthropic/claude-opus"), 75.0; math.Abs(got-want) > 1e-9 {
		t.Fatalf("output tokens: got %v want %v", got, want)
	}
	// Meter has one number and charges it at the dear rate on purpose.
	if Meter(1_000, "z-ai/glm-5.2") != MeterCall(0, 1_000, "z-ai/glm-5.2") {
		t.Fatalf("Meter is the output rate")
	}
	if _, known := PriceOf("z-ai/glm-5.2:free"); !known {
		t.Fatalf("an endpoint suffix is not a different model")
	}
	if _, known := PriceOf("kimi-k3"); !known {
		t.Fatalf("a bare model name matches its vendor's row")
	}
	price, known := PriceOf("somebody/new-model")
	if known {
		t.Fatalf("that model is not in the table")
	}
	if price.Out <= 0 {
		t.Fatalf("an unpriced model is not a free model")
	}
}

func TestGauge(t *testing.T) {
	if got := (Fuel{Cap: 2, Spent: 1.6}).Gauge(); got != "$1.60 of $2.00" {
		t.Fatalf("got %q", got)
	}
	if got := (Fuel{Spent: 0.5}).Gauge(); got != "$0.50" {
		t.Fatalf("an uncapped run shows what it spent: %q", got)
	}
	if !(Fuel{Cap: 2, Spent: 1.6}).Low() || (Fuel{Cap: 2, Spent: 1.0}).Low() {
		t.Fatalf("the warning mark is %v", WarnMark)
	}
}

// gated is the run every test below drives: one node at a time, each one
// costing what the test says, and the planner adding the next one until it is
// told to stop.
type gated struct {
	mu    sync.Mutex
	added int
	limit int
	cost  float64
	ran   []string
}

func (g *gated) Plan(_ context.Context, v View) (Amendment, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.added >= g.limit {
		return done("wrap up"), nil
	}
	g.added++
	return Amendment{Add: []Node{{ID: nodeName(g.added), Goal: "work"}}}, nil
}

// Exec records the NODES it ran; the synthesis is a call the run makes about
// itself, and counting it as work would make "nothing ran after the gate"
// impossible to state.
func (g *gated) Exec(_ context.Context, n Node, _ []NodeStatus) (string, float64, error) {
	g.mu.Lock()
	if n.ID != SynthesisID {
		g.ran = append(g.ran, n.ID)
	}
	g.mu.Unlock()
	return n.ID + " did the thing", g.cost, nil
}

func (g *gated) done() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.ran...)
}

func nodeName(at int) string { return "n" + string(rune('0'+at)) }

// atTheGate starts a run that will pause on its second node, and hands back
// the run, the work it is doing, and the snapshot it paused with.
func atTheGate(t *testing.T) (*Orchestrator, *gated, chan Snapshot) {
	t.Helper()
	work := &gated{limit: 4, cost: 0.6}
	var (
		warned = make(chan Fuel, 4)
		paused = make(chan Fuel, 4)
		ended  = make(chan Snapshot, 1)
	)
	run := New("the goal", work, work, Options{
		Cap:     1.00,
		Lanes:   1,
		OnFuel:  func(f Fuel) { warned <- f },
		OnPause: func(f Fuel) { paused <- f },
	})
	go func() {
		snap, _ := run.Run(testContext(t))
		ended <- snap
	}()

	select {
	case <-warned:
	case <-time.After(5 * time.Second):
		t.Fatalf("the tank crossed %v%% and said nothing", WarnMark*100)
	}
	select {
	case gauge := <-paused:
		if gauge.Spent < gauge.Cap {
			t.Fatalf("paused at %v of %v", gauge.Spent, gauge.Cap)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the tank emptied and the run did not stop")
	}
	// The pause is a state, not a race: it is on the snapshot before anybody
	// answers it, and it stays there until somebody does.
	deadline := time.After(2 * time.Second)
	for !run.Snapshot().Paused {
		select {
		case <-deadline:
			t.Fatalf("the run is not marked paused")
		case <-time.After(2 * time.Millisecond):
		}
	}
	return run, work, ended
}

// TestFuelPausesAndResumesOnTopup.
func TestFuelPausesAndResumesOnTopup(t *testing.T) {
	run, work, ended := atTheGate(t)
	before := len(work.done())

	// Nothing new launches while the gate is up.
	time.Sleep(50 * time.Millisecond)
	if got := len(work.done()); got != before {
		t.Fatalf("%d nodes launched while the run was paused", got-before)
	}
	if err := run.Resolve("nonsense"); err == nil {
		t.Fatalf("the gate takes three answers and that is not one")
	}
	if err := run.Resolve("topup:2.00"); err != nil {
		t.Fatal(err)
	}

	snap := waitForEnd(t, ended)
	if !snap.Done || snap.Answer == "" {
		t.Fatalf("a topped-up run runs to its synthesis: %+v", snap)
	}
	if snap.Paused {
		t.Fatalf("an answered gate is not still up")
	}
	if snap.Fuel.Cap != 3.00 {
		t.Fatalf("the top-up raises the tank to %v, want 3.00", snap.Fuel.Cap)
	}
	if len(work.done()) <= before {
		t.Fatalf("the run did not carry on past the gate")
	}
	if !strings.Contains(strings.Join(snap.Notes, " | "), "topped up") {
		t.Fatalf("the top-up said nothing: %+v", snap.Notes)
	}
}

// TestFuelFinishSynthesizesOverPartialWork.
func TestFuelFinishSynthesizesOverPartialWork(t *testing.T) {
	run, work, ended := atTheGate(t)
	before := len(work.done())
	if err := run.Resolve(GateFinish); err != nil {
		t.Fatal(err)
	}
	snap := waitForEnd(t, ended)
	if !snap.Done {
		t.Fatalf("finish ends the run: %+v", snap)
	}
	if snap.Answer == "" {
		t.Fatalf("finish is a synthesis over what is there, not a silence")
	}
	// The synthesis is a call, and the only new work there was.
	if got := len(work.done()); got != before {
		t.Fatalf("%d nodes ran after finish", got-before)
	}
}

// TestFuelStopSettlesWithoutSynthesis.
func TestFuelStopSettlesWithoutSynthesis(t *testing.T) {
	run, _, ended := atTheGate(t)
	if err := run.Resolve(GateStop); err != nil {
		t.Fatal(err)
	}
	snap := waitForEnd(t, ended)
	if !snap.Done {
		t.Fatalf("stop settles the run: %+v", snap)
	}
	if snap.Answer != "" {
		t.Fatalf("stop is somebody declining to pay for one more call, got %q", snap.Answer)
	}
	var landed int
	for _, node := range snap.Nodes {
		if node.State == Done {
			landed++
		}
	}
	if landed == 0 {
		t.Fatalf("a stopped run keeps what it finished")
	}
}

// TestGateRefusesWhatNobodyAsked.
func TestGateRefusesWhatNobodyAsked(t *testing.T) {
	run := bare()
	if err := run.Resolve(GateStop); err == nil {
		t.Fatalf("a run that is not at the gate has no gate to answer")
	}
	if err := run.Resolve("topup:not-money"); err == nil {
		t.Fatalf("that is not an amount")
	}
}

// TestUncappedRunNeverPauses: zero is a person who did not set a tank, and it
// is not a tank of zero dollars.
func TestUncappedRunNeverPauses(t *testing.T) {
	work := &gated{limit: 2, cost: 5}
	run := New("goal", work, work, Options{Lanes: 1})
	snap, err := run.Run(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Paused || !snap.Done {
		t.Fatalf("an uncapped run runs: %+v", snap)
	}
	if snap.Fuel.Spent < 10 {
		t.Fatalf("an uncapped run still meters: %v", snap.Fuel.Spent)
	}
}

func waitForEnd(t *testing.T, ended chan Snapshot) Snapshot {
	t.Helper()
	select {
	case snap := <-ended:
		return snap
	case <-time.After(10 * time.Second):
		t.Fatalf("the run never ended")
		return Snapshot{}
	}
}
