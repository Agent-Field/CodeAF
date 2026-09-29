package session_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellindex"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A session written by the real writers, stripped of every derived file and
// rebuilt from its transcript, reads back the same.
//
// Compared semantically, not byte for byte: meta.json carries Build (the codeaf
// that wrote it) and instants stamped a moment after the journal's own
// (Created, LastUserAt), and a ledger row carries lane timings no transcript
// line holds. Every fact the transcript does hold must match exactly.
func TestDerivedIndexesRebuildFromTheTranscript(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	session.RunScriptedSession(t, c.Root, "map the parser migration failures")

	wantMeta, err := session.LoadMeta(c.Root)
	if err != nil || wantMeta.ID == "" {
		t.Fatalf("real writer left no meta: %+v %v", wantMeta, err)
	}
	wantRows := ledgerRows(t, c.ID)
	if wantRows.calls == 0 {
		t.Fatal("real writer left no ledger rows for the session")
	}

	workspace := wantMeta.Workspace // the opener knows it; a sealed transcript does not
	if err := os.Remove(filepath.Join(c.Root, "meta.json")); err != nil {
		t.Fatal(err)
	}
	dropLedger(t)

	built, err := cellindex.Rebuild(c, workspace)
	// No task landed and no memory was said, so those two have nothing to build.
	if err != nil || len(built) != len(cellindex.Indexes)-2 {
		t.Fatalf("Rebuild built %v, err %v", built, err)
	}
	assertMetaEquivalent(t, wantMeta, c.Root)
	assertSameTotals(t, wantRows, ledgerRows(t, c.ID))

	again, err := cellindex.Rebuild(c, workspace)
	if err != nil || len(again) != 0 {
		t.Fatalf("second Rebuild built %v, want nothing (err %v)", again, err)
	}
}

func assertMetaEquivalent(t *testing.T, want session.Meta, dir string) {
	t.Helper()
	got, err := session.LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	exact := got.ID == want.ID && got.Title == want.Title && got.Workspace == want.Workspace &&
		got.Model == want.Model && got.SpentUSD == want.SpentUSD && got.Tokens == want.Tokens
	near := within(got.Created, want.Created) && within(got.LastUserAt, want.LastUserAt)
	if !exact || !near {
		t.Fatalf("rebuilt meta differs:\n got  %+v\n want %+v", got, want)
	}
}

func within(a, b time.Time) bool { return a.Sub(b).Abs() < 5*time.Second }

type totals struct {
	in, out, calls int
	usd            float64
}

func assertSameTotals(t *testing.T, want, got totals) {
	t.Helper()
	if want != got {
		t.Fatalf("ledger totals differ: got %+v want %+v", got, want)
	}
}

func ledgerRows(t *testing.T, id string) totals {
	t.Helper()
	rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		return totals{}
	}
	var sum totals
	for _, r := range rows {
		if r.Session == id {
			sum.in, sum.out, sum.calls, sum.usd = sum.in+r.Input, sum.out+r.Output, sum.calls+r.Calls, sum.usd+r.USD
		}
	}
	return sum
}

func dropLedger(t *testing.T) {
	t.Helper()
	if err := os.Remove(session.UsageLedgerPath()); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// The bucket's task index, deleted, is rebuilt from the checkpoint: the rows
// the live landings wrote come back, and the session's own copy is left alone.
//
// Compared field for field. Nothing in a row is stamped at write time: the
// landing instant and duration are the record's own, so the rebuilt row is the
// live row exactly.
func TestTaskIndexRebuildsFromTheCheckpoint(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	session.RunScriptedSession(t, c.Root, "map the parser migration failures")
	session.RunLandedTasks(t, c.Root, "map the parser", "fix the reconciler")

	index := session.TaskIndexPath(session.Place{Dir: c.Root}.Transcript())
	want := session.ReadTaskIndex(index)
	if len(want) != 2 {
		t.Fatalf("live landings wrote %d rows, want 2", len(want))
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}

	built, err := cellindex.Rebuild(c, "")
	if err != nil || !slices.Contains(built, "tasks.jsonl") {
		t.Fatalf("Rebuild built %v, err %v", built, err)
	}
	if got := session.ReadTaskIndex(index); !reflect.DeepEqual(got, want) {
		t.Fatalf("rebuilt rows differ:\n got  %+v\n want %+v", got, want)
	}
	if again, err := cellindex.Rebuild(c, ""); err != nil || slices.Contains(again, "tasks.jsonl") {
		t.Fatalf("second Rebuild built %v, want no task index (err %v)", again, err)
	}
}
