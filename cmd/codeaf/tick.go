package main

// tick.go is the door the operating system's timer knocks on: one bounded pass
// over everything standing, then exit. `codeaf tick` is what the launchd agent
// and the systemd user timer in internal/standing/watch.go both run.
//
// IT IS MACHINERY, NOT A COMMAND, and so — exactly like `codeaf engine` — it is
// deliberately absent from the usage text. A person accomplishes nothing by
// typing it: it draws nothing, asks nothing, and on an ordinary machine with
// nothing due it prints not one character. Quiet is the design.
//
// IT REFUSES RATHER THAN RACES. Any open codeaf window runs the same pass on
// the same cadence and holds the same lock, so a timer that wakes into a live
// window says so on stderr and leaves with a zero: the work is already being
// done by somebody, and that is a success, not a failure.

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/standing"
)

func runTick(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("codeaf tick takes no arguments")
	}
	store, err := standing.Open(home.Join("v3", "standing"))
	if err != nil {
		return err
	}
	ticker, release, err := v3StandingTicker(store)
	if err != nil {
		return err
	}
	// The pass's model catalog is joined before this command returns, so its
	// warm cannot write a cache after the process has said it is done.
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), standing.TickWindow)
	defer cancel()
	if _, err := ticker.Tick(ctx); err != nil {
		if errors.Is(err, standing.ErrHeld) {
			// Somebody else is already doing this pass. Saying so on stderr and
			// leaving with a zero is the honest answer: nothing went wrong.
			fmt.Fprintln(os.Stderr, "a codeaf window is already keeping watch")
			return nil
		}
		return err
	}
	return nil
}
