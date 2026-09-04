package main

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// THE THREE ROAD-DEPENDENT CAPABILITIES, DECIDED ONCE.
//
// A harness design, a subharness intake card and an adaptive run's fuel gate
// each reach a person on a standing subscription, so each is a question about
// the ROAD between the session and the screen rather than about the machine.
// chatv3.go used to answer them one way and engine.go another, in prose, which
// is how a lane comes to be lit on one door and dark on the other with nothing
// saying so. These pin that there is one answer and that both doors take it.

// builderName says which of the two session builders a shape chose. Comparing
// the closures by their code pointer is the only way to ask the question from
// outside, and the question is worth asking: "runs are on" and "build through
// v3OpenSession" are one fact, and a shape that set the lane and picked the
// plain builder would turn the verb on with nothing behind it.
func builderName(t *testing.T, open func(session.Config) (*session.Agent, error)) string {
	t.Helper()
	switch reflect.ValueOf(open).Pointer() {
	case reflect.ValueOf(v3OpenSession).Pointer():
		return "v3OpenSession"
	case reflect.ValueOf(session.New).Pointer():
		return "session.New"
	}
	return "something else"
}

func TestASurfaceInThisProcessGetsEveryStandingLane(t *testing.T) {
	if lanes := v3LanesHere(); !lanes.Designs || !lanes.Cards || !lanes.Runs {
		t.Fatalf("v3LanesHere() = %+v, want every lane: the surface and the session are one program", lanes)
	}
	store := &subharness.Store{}
	cfg, open := v3Shape(session.Config{HarnessStore: store}, v3LanesHere())
	if cfg.HarnessStore != store {
		t.Fatal("the designer was taken away from a surface that is in this process")
	}
	if !cfg.HarnessCards {
		t.Fatal("the intake cards were left off for a surface that holds the harness lane")
	}
	if got := builderName(t, open); got != "v3OpenSession" {
		t.Fatalf("the builder was %s, want the one that wires the adaptive runner", got)
	}
}

// A ROAD THAT CARRIES NOTHING TAKES ALL THREE AWAY, and takes them away as
// ABSENCE rather than as breakage: a nil store is building off on internal/
// session's own terms, and the model is not handed a verb it cannot complete.
func TestARoadWithNoLanesLeavesTheCapabilitiesAbsent(t *testing.T) {
	cfg, open := v3Shape(session.Config{HarnessStore: &subharness.Store{}, HarnessCards: true}, v3Lanes{})
	if cfg.HarnessStore != nil {
		t.Fatal("the designer was left on over a road that carries no design card")
	}
	if cfg.HarnessCards {
		t.Fatal("the intake cards were left on over a road that cannot answer one")
	}
	if got := builderName(t, open); got != "session.New" {
		t.Fatalf("the builder was %s, want the plain one: nothing may start a run whose gate cannot be answered", got)
	}
}

// AND OVER A CONNECTION THE WIRE IS ASKED, never guessed at. This is the line
// that made the hosted conversation a lesser one for as long as the protocol
// carried neither lane, and the line that lights it up now that it does.
func TestTheLanesOverAConnectionAreTheOnesTheWireSaysItCarries(t *testing.T) {
	carried := remote.StandingLanes()
	lanes := v3LanesOverWire()
	if lanes.Designs != carried.Designs || lanes.Runs != carried.Runs {
		t.Fatalf("v3LanesOverWire() = %+v, but the wire carries %+v", lanes, carried)
	}
	// The intake card needs BOTH halves — the subscription its card arrives on
	// and the door its answer goes back through — so it is never on because half
	// of it is.
	if want := carried.Designs && carried.Cards; lanes.Cards != want {
		t.Fatalf("intake cards = %v, want %v: the card needs its lane and its answer door", lanes.Cards, want)
	}
	if !lanes.Designs || !lanes.Cards {
		t.Fatal("this build's wire carries the harness lane, so a hosted conversation must have those cards")
	}
	// AND THE RUNNER STAYS OFF UNTIL THE WHOLE ROOM CROSSES. internal/remote's
	// lanes.go lists what is missing — a session lane with no replay, and a run
	// page whose journal door answers a path on the engine's disk — and a build
	// that lit this up early would put a fuel gate on a screen nobody could
	// answer.
	if lanes.Runs {
		t.Fatal("a hosted conversation was given the adaptive runner while the run room does not cross")
	}
}
