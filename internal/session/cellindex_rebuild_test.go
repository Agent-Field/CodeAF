package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
	// No task landed, no memory was said and nothing was made, so those three have nothing to build.
	if err != nil || len(built) != len(cellindex.Indexes)-3 {
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

// addRunRecords writes run records into the session's checkpoint the way the
// live run writers leave them, beside the nodes already there.
func addRunRecords(t *testing.T, c cell.Cell, runs []map[string]any) {
	t.Helper()
	path := session.Place{Dir: c.Root}.Tasks()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	document["runs"], document["seq"] = runs, 8
	content, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A chat that moves to a new bucket with a landed task, a settled hand-off run,
// a settled adaptive run and a background job gets the rows of the first three
// and none for the job, even when the bucket already names the chat in one row.
// The bucket named the session through its task row alone, and the page it draws
// listed one of the three.
func TestTaskIndexRebuildsRunsBesideNodes(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	session.RunScriptedSession(t, c.Root, "audit the pricing code")
	session.RunLandedTasks(t, c.Root, "map the parser")
	index := session.TaskIndexPath(session.Place{Dir: c.Root}.Transcript())
	if got := session.ReadTaskIndex(index); len(got) != 1 {
		t.Fatalf("bucket holds %d rows before the moved runs, want the node's one", len(got))
	}
	started := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	ended := started.Add(90 * time.Second)
	addRunRecords(t, c, []map[string]any{
		{"id": 4, "title": "audit the pricing code", "state": "done", "report": "Pricing is sound.\nMore.",
			"model": "m-1", "costUsd": 0.5, "startedAt": started, "endedAt": ended, "elapsed_ms": 90000,
			"copy": map[string]any{"dir": "/machine/a/copy"}},
		{"id": 5, "run": "r1", "title": "ship the adaptive plan", "state": "done", "model": "m-2",
			"costUsd": 1.25, "startedAt": started, "endedAt": ended, "report": "Shipped."},
		{"id": 6, "run": "r1", "node": "n1", "parent": 5, "title": "write the migration", "state": "failed", "costUsd": 0.1, "endedAt": ended},
		{"id": 7, "kind": "job", "title": "watch the build", "state": "done"},
		{"id": 8, "title": "still moving", "state": "running"},
	})

	built, err := cellindex.Rebuild(c, "")
	if err != nil || !slices.Contains(built, "tasks.jsonl") {
		t.Fatalf("Rebuild built %v, err %v; the run rows are missing from the bucket", built, err)
	}
	rows := map[string]session.TaskIndexEntry{}
	for _, row := range session.ReadTaskIndex(index) {
		rows[row.ID] = row
	}
	if len(rows) != 4 || rows["7"].ID != "" || rows["8"].ID != "" {
		t.Fatalf("rows after rebuild = %+v, want ids 1, 4, 5 and 6 only", rows)
	}
	run := rows["4"]
	want := session.TaskIndexEntry{
		ID: "4", Name: "audit-the-pricing-code", Label: "audit the pricing code", Title: "audit the pricing code",
		Status: "done", Outcome: "Pricing is sound.", Cost: 0.5, DurationMS: 90000,
		StartedAt: started, EndedAt: ended, SessionID: c.ID,
	}
	if !reflect.DeepEqual(run, want) {
		t.Fatalf("rebuilt run row:\n got  %+v\n want %+v", run, want)
	}
	if root := rows["5"]; root.Kind != "adaptive" || root.Model != "m-2" || root.Cost != 1.25 || root.Parent != "" {
		t.Fatalf("rebuilt adaptive root = %+v", root)
	}
	if node := rows["6"]; node.Kind != "adaptive" || node.Parent != "5" || node.Status != "failed" || node.Model != "" {
		t.Fatalf("rebuilt adaptive node = %+v", node)
	}
	if again, err := cellindex.Rebuild(c, ""); err != nil || slices.Contains(again, "tasks.jsonl") {
		t.Fatalf("second Rebuild built %v, want no task index (err %v)", again, err)
	}
	if lines := rawLines(t, index); lines != 4 {
		t.Fatalf("index file holds %d lines, want 4: a row was written twice", lines)
	}
}

func rawLines(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Split(strings.TrimSpace(string(content)), "\n"))
}
