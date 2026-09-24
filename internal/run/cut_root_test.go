package run

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// A RUN THE CALLER CUT IS LEFT OPEN, WHICHEVER WAY THE SELECT FELL. When the
// caller's context ends, the root worker comes home with the context's own
// error, and that return and the context's end are ready at the loop's select
// together. The select took the return first here, as Go may: the pass that
// follows must answer incomplete and leave the root as the run left it, the
// same ending the caller's wall gives, and never write `context canceled` over
// the run's task as though its work had failed.
func TestARootTheCallerCutIsNotFailedInItsStore(t *testing.T) {
	for _, cut := range []error{context.Canceled, fmt.Errorf("the program stopped: %w", context.DeadlineExceeded)} {
		store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "cut", "root", "The run", "cut")
		if err != nil {
			t.Fatal(err)
		}
		s := NewSupervisor(store, t.TempDir(), 1, Limits{}, nil)
		s.dispatchedRoot = true
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		s.absorb(workerReturn{task: *store.Task("root"), err: cut})
		if got := s.pass(ctx, "root"); got != OutcomeIncomplete {
			t.Fatalf("a cut root's pass answered %q, want %q", got, OutcomeIncomplete)
		}
		if root := store.Task("root"); root.Status == plandb.StatusFailed || root.Error != "" {
			t.Fatalf("the caller's cut was written as the run failing: %s (%q)", root.Status, root.Error)
		}
		_ = store.Close()
	}
}
