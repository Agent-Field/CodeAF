package tui3

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/handoff"
)

func repositoryWithStaleStat(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("unchanged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "tracked.txt")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "Initial file")
	staleRepositoryStat(t, root)
	return root
}

func staleRepositoryStat(t *testing.T, root string) {
	t.Helper()
	old := time.Unix(1600000000, 0)
	if err := os.Chtimes(filepath.Join(root, "tracked.txt"), old, old); err != nil {
		t.Fatal(err)
	}
}

func unchangedRepositoryRead(t *testing.T, root string) error {
	t.Helper()
	index := filepath.Join(root, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		return err
	}
	stat, err := os.Stat(index)
	if err != nil {
		return err
	}
	dir, err := os.Stat(filepath.Dir(index))
	if err != nil {
		return err
	}
	if _, err := homeGitStatus(t.Context(), root); err != nil {
		return err
	}
	after, err := os.ReadFile(index)
	if err != nil {
		return err
	}
	now, err := os.Stat(index)
	if err != nil {
		return err
	}
	currentDir, err := os.Stat(filepath.Dir(index))
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) || !stat.ModTime().Equal(now.ModTime()) {
		return fmt.Errorf("repository reading rewrote the index")
	}
	if !dir.ModTime().Equal(currentDir.ModTime()) {
		return fmt.Errorf("repository reading created or removed an index lock")
	}
	if _, err := os.Stat(index + ".lock"); !os.IsNotExist(err) {
		return fmt.Errorf("repository reading left an index lock: %v", err)
	}
	return nil
}

func TestHomeRepositoryReadingLeavesTheIndexUntouched(t *testing.T) {
	if err := unchangedRepositoryRead(t, repositoryWithStaleStat(t)); err != nil {
		t.Fatal(err)
	}
}

type readingRestore struct {
	cellstore.Transport
	t    *testing.T
	root string
	read bool
}

func (r *readingRestore) Do(ctx context.Context, target cellstore.Target, op cellstore.Op) ([]byte, error) {
	if op.Verb() == "materialize" && target.Tree == r.root {
		r.read = true
		staleRepositoryStat(r.t, r.root)
		if err := unchangedRepositoryRead(r.t, r.root); err != nil {
			return nil, err
		}
	}
	return r.Transport.Do(ctx, target, op)
}

type restoreFetch struct{ engine cellstore.SyncEngine }

func (f restoreFetch) Complete(context.Context, cell.Cell, string) error { return nil }
func (f restoreFetch) Fetch(ctx context.Context, c cell.Cell, head string) error {
	return f.engine.Materialize(ctx, c, head)
}

type cleanRestore struct{ handoff.Local }

func (cleanRestore) Unpublished(cell.Cell, string) (string, uint32, error) { return "", 0, nil }
func (cleanRestore) Dirty(context.Context, cell.Cell) (bool, error)        { return false, nil }

type restoreDirectory struct {
	directory.Client
	view     directory.CellView
	released bool
}

func (d *restoreDirectory) Cell(context.Context, string) (directory.CellView, error) {
	return d.view, nil
}
func (d *restoreDirectory) Acquire(_ context.Context, _ string, opts directory.AcquireOpts) (directory.CellView, error) {
	if opts.Expect == nil || opts.Expect.Fence != d.view.Cell.Lease.Fence {
		return directory.CellView{}, directory.ErrLeaseHeld
	}
	d.view.Cell.Lease.Fence++
	return d.view, nil
}
func (d *restoreDirectory) Release(context.Context, string, uint64) error {
	d.released = true
	return nil
}

func TestInPlaceTakeWithTheHomeRepositoryReading(t *testing.T) {
	celltest.RequireEngine(t)
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Fatal(err)
	}
	root := repositoryWithStaleStat(t)
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	transport := &readingRestore{Transport: cellstore.Spawn{Binary: bin}, t: t, root: root}
	e := cellstore.Engine{Binary: bin, DataRoot: t.TempDir(), Workspace: root, Transport: transport}
	t.Cleanup(cellstore.StopSpawned)
	if err := os.WriteFile(filepath.Join(root, "restored.txt"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sealed, err := e.Seal(t.Context(), c, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "restored.txt"), []byte("later\n"), 0600); err != nil {
		t.Fatal(err)
	}
	syncEngine := e.Sync(cellstore.SyncKeys{}, "test")
	syncEngine.Target = func(at cell.Cell) cellstore.Target {
		tree := at.Root
		if at.Root == c.Root {
			tree = root
		}
		return cellstore.Target{Tree: tree, CellDir: at.Root, DataDir: e.Root()}
	}
	dir := &restoreDirectory{view: directory.CellView{Cell: directory.Cell{Head: sealed.Turn.ID}}}
	taker := handoff.Taker{Dir: dir, Local: cleanRestore{}, Fetch: restoreFetch{syncEngine}, RootFor: func(string) string { return c.Root }, InPlace: func(cell.Cell) bool { return true }}
	taken, err := taker.Take(t.Context(), c.ID)
	if err != nil {
		t.Fatalf("in-place take with repository reading: %v", err)
	}
	if !transport.read || dir.released || taken.Driving.Head != sealed.Turn.ID {
		t.Fatal("take did not finish with the repository read at restore")
	}
	if body, _ := os.ReadFile(filepath.Join(root, "restored.txt")); string(body) != "before\n" {
		t.Fatalf("restored body = %q", body)
	}
}
