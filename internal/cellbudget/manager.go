package cellbudget

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

// Vault is where sealed content lives. Capture snapshots the cell's folder and
// answers a handle; Restore writes that handle back into the folder.
type Vault interface {
	Capture(ctx context.Context, c cell.Cell) (string, error)
	Restore(ctx context.Context, c cell.Cell, handle string, paths []string) error
}

var _ Vault = cellstore.Engine{}

// openGrace is how long a freshly opened cell is safe from eviction.
const openGrace = 10 * time.Minute

// Manager applies the budget to the cells whose ledgers sit under Engine.Root.
// Engine is the store layout only; what a cell seals, and so what eviction may
// remove and restore, is the tree Workspace names.
type Manager struct {
	Engine cellstore.Engine
	// Workspace answers the tree a cell's session seals, "" when it has none.
	Workspace func(cell.Cell) string
	// Vault answers where that tree's sealed content lives.
	Vault func(cell.Cell) Vault
	// Busy reports whether a session holds the cell.
	Busy  func(cell.Cell) bool
	Limit int64
	Now   func() time.Time
}

// New builds the stage 0 manager: the engine is the local directory layout,
// and the vault is that engine over the cell's sealed workspace, so capture and
// restore use the tree the seals use. busy reports whether a session holds the
// cell.
func New(engine cellstore.Engine, workspace func(cell.Cell) string, busy func(cell.Cell) bool) Manager {
	vault := func(c cell.Cell) Vault {
		e := cellstore.EngineFor(workspace(c))
		e.Binary, e.DataRoot = engine.Binary, engine.DataRoot
		return e
	}
	return Manager{Engine: engine, Workspace: workspace, Vault: vault, Busy: busy, Limit: Limit(), Now: time.Now}
}

// rules are what a cell must satisfy to be evicted, cheapest first.
func (m Manager) rules() []Rule {
	return []Rule{idle(m.Busy), justOpened(openGrace, m.Now), owned(m.Workspace), sealed(m.Engine)}
}

func (m Manager) nowMs() int64 { return m.Now().UnixMilli() }

// Open makes the cell ready to run: an evicted cell is written back, and the
// cell is stamped as opened now. It waits for a sweep that holds the cell.
func (m Manager) Open(ctx context.Context, c cell.Cell) error {
	dir := m.Engine.LocalDir(c)
	release, err := lock(dir, true)
	if err != nil {
		return err
	}
	defer release()
	e, err := readEntry(dir)
	if err != nil {
		return err
	}
	if e.Evicted != nil {
		if err := m.materialize(ctx, c, *e.Evicted); err != nil {
			return err
		}
		e.Evicted, e.MeasuredMs = nil, 0
	}
	e.Root, e.OpenedMs = c.Root, m.nowMs()
	return writeEntry(dir, e)
}

// materialize writes back exactly what eviction removed. Only those top-level
// paths are restored, so .cell/ stays as the device has it: the snapshot holds
// it as of the capture, and the transcript or turn log may have grown since.
func (m Manager) materialize(ctx context.Context, c cell.Cell, p Pointer) error {
	if len(p.Paths) == 0 {
		return nil
	}
	return m.Vault(c).Restore(ctx, c, p.Snapshot, p.Paths)
}

// evict removes the working files of a cell's workspace. The caller holds its
// lock, and the rules have said the workspace is the harness's own.
func (m Manager) evict(ctx context.Context, c cell.Cell, e Entry) error {
	paths, err := removable(m.Workspace(c))
	if err != nil {
		return err
	}
	handle, err := m.Vault(c).Capture(ctx, c)
	if err != nil {
		return err
	}
	e.Evicted = &Pointer{Snapshot: handle, AtMs: m.nowMs(), Paths: names(paths)}
	if err := writeEntry(m.Engine.LocalDir(c), e); err != nil {
		return err
	}
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

func names(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}
