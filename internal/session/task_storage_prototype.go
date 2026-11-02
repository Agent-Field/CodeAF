package session

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/taskstorage"
)

// taskStorage owns only this session's task copies, never the caller's project.
func taskStorage(place Place) (taskstorage.Manager, bool) {
	if strings.TrimSpace(place.Dir) == "" {
		return taskstorage.Manager{}, false
	}
	manager, enabled := taskstorage.Prototype(place.Trees())
	if enabled {
		manager.AfterRestore = func(ctx context.Context, path string) error { return restoreTaskGit(ctx, manager, path) }
	}
	return manager, enabled
}

func ensureTaskStorage(ctx context.Context, place Place, dir string) error {
	manager, enabled := taskStorage(place)
	if !enabled || strings.TrimSpace(dir) == "" || !taskStorageOwns(place, dir) {
		return nil
	}
	return manager.Ensure(ctx, dir)
}

func (a *Agent) ensureNodeStorage(ctx context.Context, node *TaskNode) error {
	node.graph.mu.Lock()
	dir := node.worktree
	node.graph.mu.Unlock()
	return ensureTaskStorage(ctx, a.familyPlace(node), dir)
}

// retainTaskStorage postpones the old checkout removal until the full archive
// has independently proved it can restore this owned task directory.
func taskStorageOwns(place Place, dir string) bool {
	rel, err := filepath.Rel(place.Trees(), dir)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

func retainTaskStorage(tree taskTree) bool {
	_, enabled := taskStorage(tree.place)
	if !enabled || tree.merge == mergeInPlace {
		return false
	}
	return taskStorageOwns(tree.place, tree.dir)
}

// rememberTaskWriters runs after child.Close: shutdown is bounded and may have
// sent SIGKILL without joining the process yet, which is not proof of quiescence.
func rememberTaskWriters(node *TaskNode, child *Agent) {
	if _, enabled := taskStorage(child.familyPlace(node)); !enabled {
		return
	}
	blocked := false
	for _, job := range child.jobs.all() {
		if job.running() || job.group.Alive() {
			blocked = true
			break
		}
	}
	if blocked {
		node.graph.mu.Lock()
		node.storageBlocked = true
		node.graph.mu.Unlock()
	}
}

// parkTaskStorage runs after executor teardown but before the completed state is
// announced, so a user cannot reopen the node during the archive transition.
func (a *Agent) parkTaskStorage(node *TaskNode, state TaskState, log io.Writer) {
	if !state.settled() {
		return
	}
	manager, enabled := taskStorage(a.familyPlace(node))
	if !enabled {
		return
	}
	node.graph.mu.Lock()
	dir, blocked, ground := node.worktree, node.storageBlocked, node.Ground
	inPlace := node.merge == mergeInPlace
	node.graph.mu.Unlock()
	if dir == "" || inPlace {
		return
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}
	manager.Busy = func(path string) bool { return blocked || node.graph.taskStorageBusy(node, path) }
	place := a.familyPlace(node)
	c := cell.Cell{ID: place.ID(), Root: place.Dir}
	carry := taskcopy.Carry{Project: ground}
	manager.BeforeCapture = func(ctx context.Context, path string) error {
		if err := carry.PrepareCold(c, path); err != nil {
			return err
		}
		return pinTaskGit(ctx, manager, place, path)
	}
	manager.BeforeRetire = func(ctx context.Context, snapshot, path string) error {
		return carry.MarkCold(c, path, snapshot)
	}
	if err := manager.Park(context.Background(), dir); err != nil {
		releaseTaskGitAfterFailedPark(manager, dir)
		note := fmt.Sprintf("compact storage could not be prepared; its recovery state was retained: %v", err)
		fmt.Fprintln(log, note)
		node.graph.mu.Lock()
		node.landLocked(withReport(node.landed, note))
		node.graph.mu.Unlock()
	}
}

func (g *TaskGraph) taskStorageBusy(finishing *TaskNode, path string) bool {
	if taskStorageHasUncertainWriter() {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, other := range g.nodes {
		if other == finishing {
			if other.storageBlocked {
				return true
			}
			continue
		}
		if other.state.settled() && !other.storageBlocked {
			continue
		}
		if finishing != nil && other.parent == finishing.id {
			return true
		}
		for _, used := range []string{other.worktree, other.Ground} {
			if used == "" {
				continue
			}
			if taskStorageOverlaps(path, used) {
				return true
			}
		}
	}
	return false
}

// TaskStorageQuiescent answers only after this agent has closed every call road.
// A settled job row alone is insufficient while its process group still lives.
func (a *Agent) TaskStorageQuiescent() bool {
	if taskStorageHasUncertainWriter() {
		return false
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if !closed {
		return false
	}
	for _, job := range a.jobs.all() {
		if job.running() || job.group.Alive() {
			return false
		}
	}
	return true
}

func (a *Agent) parkBeltRunStorage(run *beltRun, summary RunSummary) {
	// Multi-root execution copies remain resident until the per-root carrier
	// and ownership lifecycle are ready. Projecting the set onto its primary
	// copy would compact only part of a still-related workspace.
	if run.tree.workspaceSet != nil {
		return
	}

	manager, enabled := taskStorage(a.config.Place)
	if !enabled || !taskStorageOwns(a.config.Place, run.tree.dir) || run.tree.merge == mergeInPlace {
		return
	}
	if _, err := os.Stat(run.tree.dir); os.IsNotExist(err) {
		return
	}
	manager.Busy = func(path string) bool { return a.beltRunStorageBusy(run, summary, path) }
	c := cell.Cell{ID: a.config.Place.ID(), Root: a.config.Place.Dir}
	carry := taskcopy.Carry{Project: run.tree.ground}
	manager.BeforeCapture = func(ctx context.Context, path string) error {
		if err := carry.PrepareCold(c, path); err != nil {
			return err
		}
		return pinTaskGit(ctx, manager, a.config.Place, path)
	}
	manager.BeforeRetire = func(_ context.Context, snapshot, path string) error { return carry.MarkCold(c, path, snapshot) }
	if err := manager.Park(context.Background(), run.tree.dir); err != nil {
		releaseTaskGitAfterFailedPark(manager, run.tree.dir)
		note := "compact storage could not be prepared; its recovery state was retained: " + err.Error()
		if g := a.tasker(); g != nil {
			g.planNote(note)
			if kept, ok := runRowOf(g, run.row); ok && kept.State.settled() {
				kept.Report = withReport(kept.Report, note)
				a.publishRunRow(g, kept)
			}
		}
		_, _ = run.store.AddNote(run.root, run.root, note)
	}
}

func (a *Agent) beltRunStorageBusy(run *beltRun, summary RunSummary, path string) bool {
	a.beltMu.Lock()
	workers := len(run.workers)
	a.beltMu.Unlock()
	if !summary.StorageSafe || workers != 0 {
		return true
	}
	if g := a.tasker(); g != nil && g.taskStorageBusy(nil, path) {
		return true
	}
	for _, job := range a.jobs.all() {
		if !job.running() && !job.group.Alive() {
			continue
		}
		if taskStorageOverlaps(path, job.dir) {
			return true
		}
	}
	return false
}

// A failed initial capture has no cold transition to protect. Give back only
// the lock this attempt acquired; interrupted retirement keeps its pin.
func releaseTaskGitAfterFailedPark(manager taskstorage.Manager, path string) {
	state, err := manager.Inspect(context.Background(), path)
	if err == nil && state.Phase == taskstorage.Resident {
		_ = restoreTaskGit(context.Background(), manager, path)
	}
}

func taskStorageOverlaps(a, b string) bool {
	a, b = canonicalPath(a), canonicalPath(b)
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err == nil && (rel == "." || filepath.IsLocal(rel)) {
			return true
		}
	}
	return false
}

// RestoreTaskPath prepares an explicitly requested task file for normal reading.
// Listing, hovering and rendering never call this filesystem-changing door.
func (a *Agent) RestoreTaskPath(ctx context.Context, path string) error {
	place := a.familyPlace(nil)
	manager, enabled := taskStorage(place)
	if !enabled || !taskStorageOwns(place, path) {
		return nil
	}
	rel, err := filepath.Rel(place.Trees(), path)
	if err != nil {
		return err
	}
	first := strings.Split(rel, string(filepath.Separator))[0]
	return manager.Ensure(ctx, filepath.Join(place.Trees(), first))
}

// An external program is another uncontained writer even if its top-level
// process returns cleanly. Mark before handing execution to its engine.
func startRunWithTaskStorageProof(ctx context.Context, engine RunEngine, spec RunSpec) RunSummary {
	if spec.Delegate != nil {
		if err := markTaskStorageUncertain(spec.Workspace); err != nil {
			return RunSummary{Outcome: "could not run", Result: "could not record program writer uncertainty: " + err.Error()}
		}
	}
	return engine.Start(ctx, spec)
}
