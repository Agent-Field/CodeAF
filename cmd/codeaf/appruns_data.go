package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// The data place calls its hooks in commit order. A separate reader keeps
// expression evaluation and run journaling off its delivery goroutine.
func watchAppRunCommits(ctx context.Context, place *data.Place, engine *runs.Engine) (stop func(), done <-chan struct{}) {
	batches := make(chan data.Change, 128)
	unsubscribe := place.OnCommit(func(batch data.Change) {
		if batch.Draft != "" {
			return
		}
		select {
		case batches <- batch:
		case <-ctx.Done():
		}
	})
	stopped := make(chan struct{})
	guard.Go("app/run-triggers", func() {
		defer close(stopped)
		for {
			select {
			case <-ctx.Done():
				return
			case batch := <-batches:
				changes := make([]runs.Change, 0, len(batch.Records))
				for _, row := range batch.Records {
					changes = append(changes, runs.Change{Type: batch.Type, ID: row.ID, Before: row.Before, After: row.After})
				}
				if err := engine.RecordChanges(changes); err != nil {
					fmt.Fprintln(os.Stderr, "app record trigger:", err)
				}
			}
		}
	})
	return unsubscribe, stopped
}
