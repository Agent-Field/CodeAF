package cellstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// planOp asks what restoring a snapshot would change, and changes nothing.
type planOp struct {
	Snapshot string `json:"snapshot"`
	Repo     string `json:"-"`
}

func (planOp) Verb() string { return "plan" }

func (o planOp) Args() []string {
	return []string{"--json", "rewind", o.Snapshot, "--dry-run", "--repo", o.Repo}
}

// plan keeps the workspace in an argument so long Windows copy paths never
// become a spawned process's working directory, including on later take-backs.
func (e Engine) plan(ctx context.Context, c cell.Cell, snapshot string) ([]byte, error) {
	target := e.target(c)
	if err := os.MkdirAll(target.DataDir, 0700); err != nil {
		return nil, err
	}
	tree := target.Tree
	target.Tree = target.DataDir
	return (Spawn{Binary: e.Binary, Run: e.Run}).Do(ctx, target, planOp{Snapshot: snapshot, Repo: tree})
}

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
	_, recovery, err := OpenWAL(e.WALPath(c))
	if err != nil {
		return false, err
	}
	// External output can be the only effect of a finished call. Take-back
	// must branch it even when no workspace file or transcript changed (D4/L9).
	if len(recovery.Completed) > 0 {
		return true, nil
	}
	if err := e.screenCapture(ctx, c); err != nil {
		return false, err
	}
	// The plan deliberately ignores harness bookkeeping under .cell, but a
	// reply already written to its journal is work even before the next seal.
	head, err := Head(c)
	if err != nil {
		return false, err
	}
	if head != nil && sizeOf(rel(c, cell.TranscriptPath)) != head.Receipt.Transcript.End {
		return true, nil
	}
	out, err := e.plan(ctx, c, "head")
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

// rebindOp tells the engine that the tree it restored at From now stands at Repo.
type rebindOp struct {
	From string `json:"from"`
	Repo string `json:"-"`
}

func (rebindOp) Verb() string { return "rebind" }

func (o rebindOp) Args() []string {
	return []string{"--json", "rebind", "--from", o.From, "--repo", o.Repo}
}

// rebind runs from the local store rather than a moved workspace. Empty trees
// may need creating, and Windows cannot start a process in a long copy path
// even when filesystem operations can open that directory.
func (e Engine) rebind(ctx context.Context, target Target, from string) error {
	if err := os.MkdirAll(target.Tree, 0700); err != nil {
		return fmt.Errorf("workspace %q: %w", target.Tree, err)
	}
	if err := os.MkdirAll(target.DataDir, 0700); err != nil {
		return err
	}
	tree := target.Tree
	target.Tree, target.CellDir = target.DataDir, ""
	_, err := (Spawn{Binary: e.Binary, Run: e.Run}).Do(ctx, target, rebindOp{From: from, Repo: tree})
	return err
}

// Follow registers the tree of to as the tree of from, which was restored and
// then moved into place. The engine knows a workspace by its path, so without
// this the moved folder is one it has never heard of: it cannot say what the
// folder holds that its newest sealed state does not, until something seals it.
// The daemon does not serve the verb, so this asks a spawned engine, once per
// takeover.
func (e Engine) Follow(ctx context.Context, from, to cell.Cell) error {
	target := e.target(to)
	if err := e.rebind(ctx, target, e.tree(from)); err != nil {
		return fmt.Errorf("follow %q via %q: %w", target.Tree, target.DataDir, err)
	}
	if err := os.MkdirAll(e.LocalDir(to), 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(e.LocalDir(to), "engine-tree"), []byte(target.Tree), 0600)
}
