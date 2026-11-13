package tui3

import (
	"context"
	"sync"
)

// modelSelections holds only unfinished asks, keyed by their owning agent.
// Commands can start in any order, including after replay or a chat switch.
var modelSelections sync.Map

type modelSelection struct {
	done      chan struct{}
	model     string
	window    int
	temporary bool
}

func pendingModelSelection(agent Agent) *modelSelection {
	if selected, ok := modelSelections.Load(agent); ok {
		return selected.(*modelSelection)
	}
	return nil
}

// waitModelSelection runs only inside a send command, never on the update loop.
// A refusal still completes the ask, letting the engine answer on its current
// model. A later selection queued during the wait must also precede this send.
func waitModelSelection(ctx context.Context, agent Agent) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		selected := pendingModelSelection(agent)
		if selected == nil {
			return nil
		}
		select {
		case <-selected.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
