package main

// tick.go is the door the operating system's timer knocks on: one bounded pass
// over everything standing, then exit. `aforge tick` is what the launchd agent
// and the systemd user timer in internal/standing/watch.go both run.
//
// IT IS MACHINERY, NOT A COMMAND, and so — exactly like `aforge engine` — it is
// deliberately absent from the usage text. A person accomplishes nothing by
// typing it: it draws nothing, asks nothing, and on an ordinary machine with
// nothing due it prints not one character. Quiet is the design.
//
// IT REFUSES RATHER THAN RACES. Any open aforge window runs the same pass on
// the same cadence and holds the same lock, so a timer that wakes into a live
// window says so on stderr and leaves with a zero: the work is already being
// done by somebody, and that is a success, not a failure.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// tickWall is how long one pass may take. It is the same 120 seconds v1's wake
// allowed itself: long enough for a handful of probes and a small firing, short
// enough that a timer never has two of itself alive.
const tickWall = 120 * time.Second

func runTick(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("aforge tick takes no arguments")
	}
	store, err := standing.Open(home.Join("v3", "standing"))
	if err != nil {
		return err
	}
	ticker, err := v3StandingTicker(store)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), tickWall)
	defer cancel()
	if _, err := ticker.Tick(ctx); err != nil {
		if errors.Is(err, standing.ErrHeld) {
			// Somebody else is already doing this pass. Saying so on stderr and
			// leaving with a zero is the honest answer: nothing went wrong.
			fmt.Fprintln(os.Stderr, "an aforge window is already keeping watch")
			return nil
		}
		return err
	}
	return nil
}

// STUB (lane session replaces): func v3StandingTicker(store *standing.Store) (*standing.Ticker, error)
//
// The real one hands the ticker the session lane's Runner — the thing that runs
// a probe, delivers a line into the conversation that asked, and runs a task in
// its own headless session — along with the model-backed Sentinel and the daily
// rail from settings. Until it exists, this build walks the items, keeps their
// LastChecked honest and writes the wake log, and fires nothing: a capability
// that cannot work is absent, not broken, so the sentinel simply always says no
// and the runner is not there at all.
func v3StandingTicker(store *standing.Store) (*standing.Ticker, error) {
	return &standing.Ticker{
		Store: store,
		Sentinel: func(context.Context, standing.Judgment) (bool, string, float64, error) {
			return false, "no runner in this build", 0, nil
		},
	}, nil
}
