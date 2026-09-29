package cellstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
)

// dataDirEnv names where the engine keeps its store and catalog. One directory
// per cell means one catalog lock per writer: two writers never wait on each
// other and never share a store.
const dataDirEnv = "FURROW_DATA_DIR"

// maxChangedArgs bounds the changed-path list passed on the command line. A
// longer list is not sent: the engine then walks the tree, which its stat cache
// keeps cheap, and that is always correct.
const maxChangedArgs = 512

// Runner runs one program and returns its stdout. Engine spawns through it so
// tests can count and fake spawns.
type Runner func(ctx context.Context, dir string, env []string, argv ...string) ([]byte, error)

// Engine is the stage 0 Store: one engine spawn per seal.
type Engine struct {
	// Binary is the engine program; empty means the configured or embedded
	// engine, checked to be able to seal (see sealingEngine).
	Binary string
	// DataRoot holds one store directory per cell; empty means the state root's.
	DataRoot string
	// Workspace, when set, is the tree the engine seals: the folder the tools
	// work in, which is not the cell's own. The cell's .cell/ directory is then
	// composed in as the tree's .cell/ entry and nothing is written into the
	// workspace. Empty seals the cell's folder itself.
	Workspace string
	// WorkspaceOf, when set, names the tree per cell and wins over Workspace.
	// The take side needs it: one engine serves every chat a device continues,
	// and each chat works in its own folder.
	WorkspaceOf func(c cell.Cell) string
	Identity    Identity
	// Compose, when set, adds files to the cell's .cell/ directory just before
	// each seal captures it. Nil adds none.
	Compose Composer
	// Guard screens the tree for secrets before each seal (law L3); its zero
	// value is the working guard.
	Guard Guard
	// Run runs the engine program for the spawn transport; setting it means
	// every verb spawns, and the caller sees each spawn.
	Run Runner
	// Transport carries the engine's verbs; nil means transportFor's choice.
	Transport Transport
	Now       func() time.Time
}

var _ Store = Engine{}

// Composer adds what a seal must carry that lives outside the sealed tree, such
// as the task working copies under the session folder's trees/. It writes into
// the cell's own .cell/ directory, which the engine captures with the tree.
type Composer interface {
	Compose(c cell.Cell) error
}

// LocalDir is the device-local directory of a cell: the engine store and the
// call WAL. It is never inside the cell and never synced.
func (e Engine) LocalDir(c cell.Cell) string { return filepath.Join(e.Root(), c.ID) }

// Root is the directory that holds one local directory per cell.
func (e Engine) Root() string {
	if e.DataRoot == "" {
		return home.Join("v3", "stores")
	}
	return e.DataRoot
}

// Seal implements Store: compose the harness-owned files, snapshot the folder,
// record the turn.
func (e Engine) Seal(ctx context.Context, c cell.Cell, info TurnInfo) (Sealed, error) {
	head, err := Head(c)
	if err != nil {
		return Sealed{}, fmt.Errorf("seal: read head: %w", err)
	}
	if err := e.Guard.Screen(c, e.tree(c), e.policyDir(c), info.Changed); err != nil {
		return Sealed{}, fmt.Errorf("seal: %w", err)
	}
	receipt := newReceipt(info, transcriptRange(c, head))
	raw, rid, err := receipt.encode()
	if err != nil {
		return Sealed{}, fmt.Errorf("seal: encode receipt: %w", err)
	}
	if err := e.compose(c, raw, rid, info); err != nil {
		return Sealed{}, fmt.Errorf("seal: compose: %w", err)
	}
	snapshot, err := e.snapshot(ctx, c, rid, info.Changed)
	if err != nil {
		return Sealed{}, err
	}
	turn := newTurn(snapshot, parentOf(head), e.now(), info, rid, e.identity())
	if err := appendTurn(c, turn); err != nil {
		return Sealed{}, fmt.Errorf("seal: record turn: %w", err)
	}
	return Sealed{Turn: turn, Receipt: receipt}, nil
}

func parentOf(head *Sealed) string {
	if head == nil {
		return ""
	}
	return head.Turn.ID
}

func (e Engine) now() time.Time {
	if e.Now == nil {
		return time.Now()
	}
	return e.Now()
}

func (e Engine) identity() Identity {
	id := e.Identity
	if id.Device == "" {
		id.Device = zeroDevice
	}
	return id
}

// snapshot takes one snapshot of the folder through the engine's turn-end
// seal, which attaches a folder it has not seen and seals it in one verb with
// the agent-run trigger. The label carries the receipt id, which is what ties
// a snapshot to its receipt until the engine has a receipt field of its own.
func (e Engine) snapshot(ctx context.Context, c cell.Cell, receipt string, changed []string) (string, error) {
	out, err := e.do(ctx, c, e.cellDir(c), sealOp{Turn: receipt, Changed: changedPaths(changed)})
	if err != nil {
		return "", fmt.Errorf("seal: snapshot: %w", err)
	}
	return parseSnapshot(out)
}

// changedPaths is the paths the engine should visit instead of walking the
// folder, or nil when the caller does not know or the list is too long to
// send. The seal's own writes under the state directory always count as
// changed.
func changedPaths(changed []string) []string {
	if changed == nil || len(changed) > maxChangedArgs {
		return nil
	}
	return append([]string{cell.StateDir}, changed...)
}

// tree is the folder the engine seals and restores.
func (e Engine) tree(c cell.Cell) string {
	if w := e.workspace(c); w != "" {
		return w
	}
	return c.Root
}

// workspace is the folder the tools of c work in, "" when c's own folder is
// what is sealed.
func (e Engine) workspace(c cell.Cell) string {
	if e.WorkspaceOf != nil {
		return e.WorkspaceOf(c)
	}
	return e.Workspace
}

// policyDir is where the harness keeps its exclusions: the cell's directory
// that is composed into a workspace, or the cell's own folder when that is
// what is sealed. The engine reads a .furrowpolicy from either.
func (e Engine) policyDir(c cell.Cell) string {
	if e.workspace(c) == "" {
		return c.Root
	}
	return stateDir(c)
}

// cellDir is the cell's private .cell/ directory when it is composed into a
// tree that is not the cell's own folder, and empty otherwise.
func (e Engine) cellDir(c cell.Cell) string {
	if e.workspace(c) == "" {
		return ""
	}
	return stateDir(c)
}

// cellDirArg names the directory the engine composes in as, and restores, the
// tree's .cell/ entry.
func cellDirArg(dir string) []string { return []string{"--cell-dir", dir} }

// stateDir is the cell's own .cell/ directory.
func stateDir(c cell.Cell) string { return filepath.Join(c.Root, cell.StateDir) }

// do runs one engine verb on the cell's folder against the cell's store, with
// cellDir composed in as the tree's .cell/ entry.
func (e Engine) do(ctx context.Context, c cell.Cell, cellDir string, op Op) ([]byte, error) {
	target := Target{Tree: e.tree(c), DataDir: e.LocalDir(c), CellDir: cellDir}
	return e.transport().Do(ctx, target, op)
}

func (e Engine) transport() Transport {
	if e.Transport != nil {
		return e.Transport
	}
	return transportFor(e.Binary, e.Run)
}

func parseSnapshot(out []byte) (string, error) {
	var doc struct {
		Snapshot string `json:"snapshot"`
	}
	if err := json.Unmarshal(out, &doc); err != nil || doc.Snapshot == "" {
		return "", fmt.Errorf("seal: engine answered %q, not a snapshot id", bytes.TrimSpace(out))
	}
	return doc.Snapshot, nil
}
