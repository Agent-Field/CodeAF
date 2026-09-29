package cellbudget

import (
	"context"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

var ctx = context.Background()

// rig is a manager over the real engine, with a clock the test moves and a
// busy set the test edits. It skips when no engine binary resolves.
type rig struct {
	t    *testing.T
	m    Manager
	busy map[string]bool
	now  time.Time
	dirs string
	work map[string]string // cell id -> the workspace its seals cover
	bin  string
}

func newRig(t *testing.T, limit int64) *rig {
	t.Helper()
	bin, err := furrow.ResolveBinary()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	r := &rig{t: t, busy: map[string]bool{}, now: time.Now(), dirs: t.TempDir(), work: map[string]string{}, bin: bin}
	r.m = New(cellstore.Engine{Binary: bin, DataRoot: filepath.Join(r.dirs, "stores")},
		func(c cell.Cell) string { return r.work[c.ID] },
		func(c cell.Cell) bool { return r.busy[c.ID] })
	r.m.Limit = limit
	r.m.Now = func() time.Time { return r.now }
	return r
}

// cell makes a sealed cell whose workspace is its own work/ folder holding one
// file of n bytes, opened at the rig's current time, then moves the clock past
// the just-opened grace.
func (r *rig) cell(name string, n int) cell.Cell {
	r.t.Helper()
	c := r.newCell()
	return r.seal(c, filepath.Join(c.Root, "work"), name, n)
}

// yours is the same cell over a project folder outside it.
func (r *rig) yours(name string, n int) (cell.Cell, string) {
	r.t.Helper()
	c, dir := r.newCell(), r.t.TempDir()
	return r.seal(c, dir, name, n), dir
}

func (r *rig) newCell() cell.Cell {
	c, err := cell.CreateIn(filepath.Join(r.dirs, "cells"), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		r.t.Fatal(err)
	}
	return c
}

func (r *rig) seal(c cell.Cell, workspace, name string, n int) cell.Cell {
	r.work[c.ID] = workspace
	write(r.t, filepath.Join(workspace, name), strings.Repeat("x", n))
	write(r.t, filepath.Join(workspace, "src", "deep", "f.txt"), name)
	write(r.t, filepath.Join(c.Root, cell.TranscriptPath), `{"role":"user"}`+"\n")
	engine := cellstore.EngineFor(workspace)
	engine.Binary, engine.DataRoot = r.bin, r.m.Engine.DataRoot
	if _, err := engine.Seal(ctx, c, cellstore.TurnInfo{Trigger: cellstore.AgentRun}); err != nil {
		r.t.Fatal(err)
	}
	if err := r.m.Open(ctx, c); err != nil {
		r.t.Fatal(err)
	}
	r.now = r.now.Add(time.Hour)
	return c
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) collect() Report {
	r.t.Helper()
	rep, err := r.m.Collect(ctx, false)
	if err != nil {
		r.t.Fatal(err)
	}
	return rep
}

func (r *rig) outcomes(rep Report) map[string]Outcome {
	out := map[string]Outcome{}
	for _, a := range rep.Actions {
		out[a.ID] = a.Outcome
	}
	return out
}

// digest lists every path with its mode and content hash, .git and .furrow aside.
func digest(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return err
		}
		if rel == ".git" || rel == ".furrow" {
			return fs.SkipDir
		}
		info, _ := d.Info()
		sum := [8]byte{}
		if !d.IsDir() {
			raw, _ := os.ReadFile(p)
			h := sha256.Sum256(raw)
			copy(sum[:], h[:8])
		}
		lines = append(lines, rel+" "+info.Mode().String()+" "+string(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestEvictThenOpenRestoresByteIdentical(t *testing.T) {
	r := newRig(t, 1)
	c := r.cell("big", 4096)
	grown := filepath.Join(c.Root, cell.TranscriptPath)
	write(t, grown, `{"role":"user"}`+"\n"+`{"role":"assistant"}`+"\n") // grew after the seal
	work := r.work[c.ID]
	want, wantCell := digest(t, work), digest(t, c.Root)

	rep := r.collect()
	if r.outcomes(rep)[c.ID] != Evicted {
		t.Fatalf("not evicted: %+v", rep)
	}
	left, _ := os.ReadDir(work)
	for _, e := range left {
		if !kept[e.Name()] {
			t.Fatalf("working file %q survived eviction", e.Name())
		}
	}
	if err := r.m.Open(ctx, c); err != nil {
		t.Fatal(err)
	}
	if got := digest(t, work); got != want {
		t.Fatalf("reopened tree differs.\nwant:\n%s\ngot:\n%s", want, got)
	}
	if got := digest(t, c.Root); got != wantCell {
		t.Fatalf("the cell's own folder changed.\nwant:\n%s\ngot:\n%s", wantCell, got)
	}
}

func TestYourFolderIsNeverEvicted(t *testing.T) {
	r := newRig(t, 1)
	c, dir := r.yours("big", 4096) // opened first, so considered first
	mine := r.cell("big", 4096)
	want := digest(t, dir)
	dry, err := r.m.Collect(ctx, true)
	if err != nil || r.outcomes(dry)[mine.ID] != WouldEvict {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	if a := dry.Actions[0]; a.ID != c.ID || a.Outcome != Skipped || a.Why != "workspace is yours" {
		t.Fatalf("dry run must say why: %+v", a)
	}
	if got := r.outcomes(r.collect()); got[c.ID] != Skipped || got[mine.ID] != Evicted {
		t.Fatalf("outcomes: %v", got)
	}
	if got := digest(t, dir); got != want {
		t.Fatalf("your folder changed.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestYourFolderDoesNotCountAgainstTheBudget(t *testing.T) {
	r := newRig(t, 6000)
	r.yours("huge", 1<<20)
	mine := r.cell("a", 4096)
	rep := r.collect()
	if rep.Total >= 1<<20 {
		t.Fatalf("a folder of yours was counted: %+v", rep)
	}
	if got := r.outcomes(rep); len(got) != 0 {
		t.Fatalf("under budget, nothing to do: %v (%s)", got, mine.ID)
	}
}

func TestRunningCellIsNeverEvicted(t *testing.T) {
	r := newRig(t, 1)
	c := r.cell("big", 4096)
	r.busy[c.ID] = true
	rep := r.collect()
	if r.outcomes(rep)[c.ID] != Skipped || rep.Actions[0].Why != "running" {
		t.Fatalf("running cell was not skipped: %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(r.work[c.ID], "big")); err != nil {
		t.Fatal("running cell lost its files")
	}
}

func TestUnsealedCellIsNeverEvicted(t *testing.T) {
	r := newRig(t, 1)
	never := r.cell("never", 4096)
	if err := os.Remove(filepath.Join(never.Root, cellstore.TurnsPath)); err != nil {
		t.Fatal(err)
	}
	pending := r.cell("pending", 4096)
	wal, _, err := cellstore.OpenWAL(r.m.Engine.WALPath(pending))
	if err != nil {
		t.Fatal(err)
	}
	if err := wal.Begin(cellstore.Intent{V: 1, Tool: "bash", ArgsHash: "h", Started: 1}); err != nil {
		t.Fatal(err)
	}
	got := r.outcomes(r.collect())
	if got[never.ID] != Skipped || got[pending.ID] != Skipped {
		t.Fatalf("unsealed cells must be skipped: %v", got)
	}
}

func TestEvictionIsLeastRecentlyOpenedFirst(t *testing.T) {
	r := newRig(t, 6000)
	oldest, middle, newest := r.cell("a", 4096), r.cell("b", 4096), r.cell("c", 4096)
	if err := r.m.Open(ctx, oldest); err != nil { // reopening makes it the newest
		t.Fatal(err)
	}
	r.now = r.now.Add(time.Hour)
	got := r.outcomes(r.collect())
	if got[middle.ID] != Evicted || got[newest.ID] != Evicted || len(got) != 2 {
		t.Fatalf("want the two least recently opened evicted, got %v", got)
	}
	_ = oldest
}

func TestJustOpenedCellIsProtected(t *testing.T) {
	r := newRig(t, 1)
	c := r.cell("big", 4096)
	if err := r.m.Open(ctx, c); err != nil {
		t.Fatal(err)
	}
	if got := r.outcomes(r.collect()); got[c.ID] != Skipped {
		t.Fatalf("a cell opened this instant was evicted: %v", got)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	r := newRig(t, 1)
	c := r.cell("big", 4096)
	rep, err := r.m.Collect(ctx, true)
	if err != nil || r.outcomes(rep)[c.ID] != WouldEvict {
		t.Fatalf("dry run: %+v %v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(r.work[c.ID], "big")); err != nil {
		t.Fatal("dry run removed files")
	}
}

func TestUnderBudgetMeasuresOnceAndTouchesNothing(t *testing.T) {
	r := newRig(t, 1<<30)
	c := r.cell("big", 4096)
	first := r.collect()
	if len(first.Actions) != 0 || first.Total < 4096 {
		t.Fatalf("under budget: %+v", first)
	}
	dir := r.m.Engine.LocalDir(c)
	before, _ := readEntry(dir)
	r.collect()
	if after, _ := readEntry(dir); after.MeasuredMs != before.MeasuredMs {
		t.Fatal("size was measured again although the transcript had not moved")
	}
}

func TestLimitReadsTheSetting(t *testing.T) {
	t.Setenv(BudgetEnv, "3")
	if Limit() != 3<<30 {
		t.Fatalf("limit %d", Limit())
	}
	t.Setenv(BudgetEnv, "0")
	if Limit() != 0 {
		t.Fatal("0 must mean no limit")
	}
	t.Setenv(BudgetEnv, "junk")
	if Limit() != defaultGiB<<30 {
		t.Fatal("junk must fall back to the default")
	}
}
