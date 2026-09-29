package cellstore

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// planOp asks what restoring a snapshot would change, and changes nothing.
type planOp struct {
	Snapshot string `json:"snapshot"`
}

func (planOp) Verb() string { return "plan" }

func (o planOp) Args() []string { return []string{"--json", "rewind", o.Snapshot, "--dry-run"} }

// Dirty says whether the cell's tree holds work its newest sealed state does
// not: the engine's own plan for restoring its workspace head lists exactly the
// paths that differ. The head is the engine's word for it, not the turn log's:
// a tree that arrived from another machine holds the head that was imported,
// whose own turn line is not in the log it carried. The harness's own .cell/
// entry is not work, so it never counts.
//
// The daemon does not serve the plan, so this asks a spawned engine, which is
// always correct and runs once per takeover.
func (e Engine) Dirty(ctx context.Context, c cell.Cell) (bool, error) {
	target := Target{Tree: e.tree(c), DataDir: e.LocalDir(c), CellDir: e.cellDir(c)}
	out, err := Spawn{Binary: e.Binary}.Do(ctx, target, planOp{Snapshot: "head"})
	if err != nil {
		return false, fmt.Errorf("dirty: %w", err)
	}
	return workChanged(out)
}

// workChanged reads a plan and says whether any change is outside .cell/.
func workChanged(plan []byte) (bool, error) {
	var doc struct {
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(plan, &doc); err != nil {
		return false, fmt.Errorf("dirty: unreadable plan: %w", err)
	}
	for _, ch := range doc.Changes {
		if top, _, _ := strings.Cut(path.Clean(ch.Path), "/"); top != cell.StateDir {
			return true, nil
		}
	}
	return false, nil
}
