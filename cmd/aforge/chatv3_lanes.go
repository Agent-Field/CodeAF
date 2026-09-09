package main

// chatv3_lanes.go is the one statement of which standing capabilities a
// conversation's surface can hold, and the assembly that applies it.
//
// Three capabilities depend on the road between the session and the screen
// rather than on the machine. A design card, a subharness intake card and an
// adaptive run's fuel gate are raised on subscriptions that outlive their turn
// ([session.Agent.WatchHarnessDesigns], [session.Agent.WatchOrchestrations]), so
// a question raised on a road that cannot carry it is work nobody will be shown.
// In this process every road carries; over a connection the wire says which do
// ([remote.StandingLanes]).
//
// It is one file because it used to be two places: chatv3.go set HarnessCards and
// built through [v3OpenSession], engine.go nilled HarnessStore and built through
// session.New. Two doors answering the same question apart is how a capability
// gets fixed on one road and stays dark on the other.

import (
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// v3Lanes is what a conversation may do, in the three answers that depend on
// the road rather than on the machine.
//
// The zero value is all three off, which is the honest posture for a road
// nobody has stated: a capability that cannot reach a person is absent, and
// internal/session already reads each of these as absent rather than broken
// (nil HarnessStore is building off, nil OrchestrateRunner is orchestration
// off, and false HarnessCards is simply not offering the verb).
type v3Lanes struct {
	// Designs is the harness designer: a design outlives the turn that asked
	// for it, and its card arrives on the design subscription.
	Designs bool
	// Cards is chat offering a saved program with an intake card
	// (internal/session's canProposeSubharness). It travels the design
	// subscription too, and its answer is ResolveSubharness.
	Cards bool
	// Runs is an adaptive run: the planner's notes, the gauge, and the FUEL
	// GATE, which is a question. A run whose gate cannot be answered stops at
	// its cap in silence hours later, so this one is never on hopefully — over a
	// connection it is off, and [remote.Lanes] lists what is left to build.
	Runs bool
}

// v3LanesHere is a surface in THIS process: the session and the screen are one
// program, so every lane is a channel and nothing has to cross anything.
func v3LanesHere() v3Lanes {
	return v3Lanes{Designs: true, Cards: true, Runs: true}
}

// v3LanesOverWire is a surface on the other end of a connection, and it asks
// internal/remote rather than restating what that protocol carries. A wire that
// grows a lane lights the capability up on every road that goes over one —
// `--host`, `--at`, and this machine's own session host — in one place.
func v3LanesOverWire() v3Lanes {
	carried := remote.StandingLanes()
	return v3Lanes{
		Designs: carried.Designs,
		// The intake card and the design card ride ONE subscription, so the card
		// half also needs the door its answer goes back through. Both facts are
		// the wire's; neither is guessed at here.
		Cards: carried.Designs && carried.Cards,
		Runs:  carried.Runs,
	}
}

// v3Shape applies the lanes to a config and answers the builder to open the
// session with.
//
// The builder is part of the answer rather than a second decision: an adaptive
// run is wired by a closure that has to be told which agent it belongs to
// (chatv3_orchestrate.go), so "runs are on" and "build through [v3OpenSession]"
// are one fact. A door that took the lanes and then picked its own builder could
// turn the verb on with nothing behind it.
func v3Shape(cfg session.Config, lanes v3Lanes) (session.Config, func(session.Config) (*session.Agent, error)) {
	if !lanes.Designs {
		// Nil is the honest way to say the designer is not among the things this
		// conversation can do, so the model says as much instead of starting two
		// model calls that end with a page nobody is ever shown. RUNNING a
		// harness that already exists is untouched — that rides Harnesses and
		// RunHarness, which are properties of the machine.
		cfg.HarnessStore = nil
	}
	cfg.HarnessCards = lanes.Cards
	if !lanes.Runs {
		return cfg, session.New
	}
	return cfg, v3OpenSession
}
