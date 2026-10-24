package cellstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// syncOp is the engine's durability boundary, separate from a read-complete
// seal acknowledgement. A boundary accepts only an explicit durable response.
type syncOp struct{}

func (syncOp) Verb() string   { return "sync" }
func (syncOp) Args() []string { return []string{"--json", "sync"} }

func syncEngine(ctx context.Context, tr Transport, target Target) error {
	target.CellDir = ""
	raw, err := tr.Do(ctx, target, syncOp{})
	if err != nil {
		return fmt.Errorf("save chat files: %w", err)
	}
	var reply struct {
		Durable bool `json:"durable"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil || !reply.Durable {
		return fmt.Errorf("save chat files: engine did not confirm durable storage")
	}
	return nil
}

type engineDebt struct {
	transport  Transport
	target     Target
	root       string
	generation uint64
}

var engineCommits = struct {
	sync.Mutex
	next  uint64
	cells map[string]engineDebt
}{cells: map[string]engineDebt{}}

// markEngine retains the exact routing used by this capture, including stores
// from an older installation. Retaining routing instead of the Engine lets a
// closed chat release its scan ledger. Generations keep a concurrent seal owed.
func markEngine(c cell.Cell, e Engine) {
	engineCommits.Lock()
	defer engineCommits.Unlock()
	engineCommits.next++
	engineCommits.cells[c.Root] = engineDebt{e.transport(), e.target(c), c.Root, engineCommits.next}
}

func flushEngines(ctx context.Context, root string) error { return syncEngines(ctx, root, false) }

func syncEngines(ctx context.Context, root string, boundary bool) error {
	engineCommits.Lock()
	var batch []engineDebt
	for path, debt := range engineCommits.cells {
		if (root == "" || path == root) && (boundary || debt.generation != 0) {
			batch = append(batch, debt)
		}
	}
	engineCommits.Unlock()
	for _, debt := range batch {
		// Removed temporary homes have no remaining local state to acknowledge.
		if _, err := os.Stat(debt.root); !os.IsNotExist(err) {
			if err := syncEngine(ctx, debt.transport, debt.target); err != nil {
				return err
			}
		}
		engineCommits.Lock()
		if engineCommits.cells[debt.root].generation == debt.generation {
			debt.generation = 0
			engineCommits.cells[debt.root] = debt
		}
		engineCommits.Unlock()
	}
	return nil
}

// durable joins both halves even when the engine acknowledged a previous group
// already. Keeping this explicit makes every exit and ownership boundary fail
// closed when either local records or saved files cannot reach disk.
func durable(ctx context.Context, root string) error {
	if err := syncEngines(ctx, root, true); err != nil {
		return err
	}
	return commits.flush(ctx, root)
}
