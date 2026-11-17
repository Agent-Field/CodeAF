package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// scaleFolders writes `owners` conversation folders, each with a checkpoint of
// `each` nodes (every one waiting on the one before it), the way the engine
// writes them.
func scaleFolders(tb testing.TB, owners, each int) []string {
	tb.Helper()
	root := tb.TempDir()
	var dirs []string
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for o := 0; o < owners; o++ {
		dir := filepath.Join(root, fmt.Sprintf("agent%04d", o))
		doc := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: uint64(each)}
		for n := 1; n <= each; n++ {
			record := taskRecord{
				ID: uint64(n), Title: fmt.Sprintf("task %d of %d", n, o), Brief: "do the work", Acceptance: "it works",
				State: TaskDone, Merge: mergeMerged, Changed: []string{"a.go", "b.go"}, CostUSD: 0.01, Input: 100, Output: 20,
				AdmittedAt: at, StateAt: at, StartedAt: at, EndedAt: at,
			}
			if n > 1 {
				record.DependsOn = []uint64{uint64(n - 1)}
			}
			doc.Nodes = append(doc.Nodes, record)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatal(err)
		}
		encoded, err := json.Marshal(doc)
		if err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(Place{Dir: dir}.Tasks(), encoded, 0o600); err != nil {
			tb.Fatal(err)
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

// 200 000 TASKS ARE READ ONCE: cold, they are parsed within a bounded time and
// memory; warm, asking again is stat calls and no parse.
func TestTheSourceReadsTwoHundredThousandTasksOnceAndThenOnlyStats(t *testing.T) {
	if testing.Short() {
		t.Skip("writes and reads 200 000 tasks")
	}
	dirs := scaleFolders(t, 200, 1000)
	// The budget is raised past the default so the whole scope is remembered;
	// with the default, the least recently used conversations are dropped and
	// read again (workgraph_bounds_test.go).
	source := NewWorkSourceLimits(WorkLimits{CacheBytes: 1 << 30, CacheOwners: 1000})
	ctx := context.Background()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	total := 0
	for _, dir := range dirs {
		facts, err := source.Owner(ctx, filepath.Base(dir), dir)
		if err != nil {
			t.Fatal(err)
		}
		total += len(facts.Facts)
	}
	cold := time.Since(start)
	runtime.GC()
	runtime.ReadMemStats(&after)
	t.Logf("cold: %d tasks in %v, %d MiB held", total, cold, (after.HeapAlloc-before.HeapAlloc)>>20)
	parses := source.Parses()
	start = time.Now()
	for _, dir := range dirs {
		if _, err := source.Owner(ctx, filepath.Base(dir), dir); err != nil {
			t.Fatal(err)
		}
	}
	warm := time.Since(start)
	t.Logf("warm: 200 conversations again in %v", warm)
	if total != 200_000 || source.Parses() != parses {
		t.Fatalf("total = %d, parses went %d -> %d", total, parses, source.Parses())
	}
	if warm > cold/5 {
		t.Fatalf("an unchanged read cost %v against %v cold", warm, cold)
	}
}
