package cellstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── AN EDIT MADE IN PLACE BETWEEN TWO TURNS TRAVELS WITH THE NEXT ONE ───────
//
// Contract C1.1 (stampscan.go says why): a file edited in place by something
// other than a tool call, after the chat has sealed once in this process, is in
// the next turn's snapshot even when that turn ran no tool and the edit left
// the folder's own modified time where it was.

// sealedFile is what the snapshot a seal made holds for name, read the way a
// computer taking the chat would: restored into a folder of its own.
func sealedFile(t *testing.T, e Engine, r *guardRig, snapshot, name string) string {
	t.Helper()
	arrived := newCell(t)
	receiver := e
	receiver.Workspace = t.TempDir()
	if err := receiver.Sync(SyncKeys{}, LedgerName("http://local.invalid", "in-place")).Materialize(t.Context(), arrived, snapshot); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(receiver.tree(arrived), name))
	if err != nil {
		t.Fatalf("the snapshot has no %s: %v", name, err)
	}
	return string(raw)
}

func inPlaceEngine(t *testing.T, r *guardRig) Engine {
	t.Helper()
	real := realEngine(t)
	e := EngineFor(r.tree)
	e.Binary, e.Transport, e.DataRoot = real.Binary, real.Transport, real.DataRoot
	e.Guard.Home, e.Guard.Notify = r.guard.Home, r.guard.Notify
	return e
}

func TestAnInPlaceAppendBetweenTurnsIsSealedWithTheNextTurn(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		r := newRig(t)
		e := inPlaceEngine(t, r)
		r.write("README.md", "one\n")
		r.write("other.txt", "untouched\n")
		if _, err := e.Seal(t.Context(), r.cell, TurnInfo{}); err != nil {
			t.Fatal(err)
		}
		// A turn that ran nothing, so the chat has a seal in this process and
		// the next one is the delta road.
		if _, err := e.Seal(t.Context(), r.cell, TurnInfo{Changed: []string{}}); err != nil {
			t.Fatal(err)
		}

		before, err := os.Stat(r.tree)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(filepath.Join(r.tree, "README.md"), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("edited in place\n"); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if after, err := os.Stat(r.tree); err != nil || !after.ModTime().Equal(before.ModTime()) {
			t.Fatalf("the folder's own modified time moved (%v, %v); the fixture no longer shows an in-place edit", after.ModTime(), err)
		}

		sealed, err := e.Seal(t.Context(), r.cell, TurnInfo{Changed: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		if got := sealedFile(t, e, r, sealed.Turn.ID, "README.md"); got != "one\nedited in place\n" {
			t.Fatalf("the next turn sealed README.md as %q; the edit made in place was left out", got)
		}
	})
}

// A rewrite that keeps the file's size is the same edit with nothing to notice
// but its modified time.
func TestAnInPlaceRewriteOfTheSameSizeIsSealedWithTheNextTurn(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		r := newRig(t)
		e := inPlaceEngine(t, r)
		r.write("notes.txt", "aaaa\n")
		if _, err := e.Seal(t.Context(), r.cell, TurnInfo{}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.Seal(t.Context(), r.cell, TurnInfo{Changed: []string{}}); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(r.tree, "notes.txt")
		if err := os.WriteFile(path, []byte("bbbb\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		later := time.Now().Add(time.Minute)
		if err := os.Chtimes(path, later, later); err != nil {
			t.Fatal(err)
		}
		sealed, err := e.Seal(t.Context(), r.cell, TurnInfo{Changed: []string{"unrelated.txt"}})
		if err != nil {
			t.Fatal(err)
		}
		if got := sealedFile(t, e, r, sealed.Turn.ID, "notes.txt"); got != "bbbb\n" {
			t.Fatalf("the turn sealed notes.txt as %q; a same-size rewrite made in place was left out", got)
		}
	})
}

// Contract C1.4: what the stat index says of a file is what the snapshot holds,
// so an edit that was never sealed is never vouched for. The index is taken
// before the engine reads, so a file written after it differs from it.
func TestTheStatIndexNeverVouchesForABytesTheSealDidNotRead(t *testing.T) {
	r := newRig(t)
	e := inPlaceEngine(t, r)
	r.write("README.md", "one\n")
	sealed, err := e.Seal(t.Context(), r.cell, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if !e.checkMaterializedStat(r.cell, sealed.Turn.ID) {
		t.Fatal("a folder untouched since its seal is not vouched for; the index lost the fast road")
	}
	f, err := os.OpenFile(filepath.Join(r.tree, "README.md"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("after\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if e.checkMaterializedStat(r.cell, sealed.Turn.ID) {
		t.Fatal("the stat index vouched for a file edited after its seal")
	}
}
