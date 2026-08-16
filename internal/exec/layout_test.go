package exec

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// THE LEAF'S WORKING DIRECTORY IS BORING, and this is the test that says so.
//
// It used to be anything but. `.obs/` held every spilled tool result,
// `.aforge/trace/` held the worker's own turn-by-turn transcript, its raw event
// stream and its patch, and `.aforge/jobs/` held its background logs — all of it
// in the one directory the worker was told to work in and told to look at. A
// measured atomic leaf spent five of its eleven turns listing that machinery and
// reading its own trace log back into its own context: orientation at full
// price, of bytes it had written itself a second earlier.
//
// What stays is work product. A sibling's NN-title.md is what the next leaf is
// meant to find, and moving it would be moving the wrong thing.
func TestTheLeafsWorkingDirectoryHoldsNoMachinery(t *testing.T) {
	root, scratch := t.TempDir(), t.TempDir()
	space, err := NewWorkspace(root)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	space = space.WithScratch(scratch)

	// One deliverable, written the way a leaf writes one.
	deliverable, err := space.Resolve("07-vendors.md")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := os.WriteFile(deliverable, []byte("Vendor A: 41ms.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	space.Record("n7", deliverable)

	// The three machinery writers, driven through the same calls the loop makes.
	trace := newTracer(space, "n7")
	trace.note("contract: read every result in full")
	trace.stream(`{"type":"message.part.delta"}`)
	trace.close()

	tools := NewToolbox(space, "n7", nil)
	spilled := tools.spill(Result{Content: strings.Repeat("observation bytes ", 4000)})
	if !strings.Contains(spilled.Content, "Whole output:") {
		t.Fatalf("the oversized result was never spilled: %q", spilled.Content)
	}

	logPath, _, err := space.ScratchPath(filepath.Join(jobsDir, jobLogName("n7", space.nextJobID())))
	if err != nil {
		t.Fatalf("ScratchPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("build output\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := rootEntries(t, root); len(got) != 1 || got[0] != "07-vendors.md" {
		t.Fatalf("the leaf's own directory holds %v, want only its deliverable", got)
	}
	// And the machinery is not merely absent — it exists, one directory across,
	// where every reader of it now looks.
	for _, expected := range []string{obsDir, traceDir, jobsDir} {
		if _, err := os.Stat(filepath.Join(scratch, expected)); err != nil {
			t.Fatalf("%s did not land in the scratch home: %v", expected, err)
		}
	}
	// The spill pointer is the one machinery path a leaf is deliberately sent to,
	// so it has to name somewhere the leaf can actually open from its own cwd.
	if !strings.Contains(spilled.Content, scratch) {
		t.Fatalf("the spill pointer is not an absolute path into the scratch home: %q", spilled.Content)
	}
}

// rootEntries lists what an agent listing its own workspace would see, which is
// the only listing that matters here: the leaf reads the directory with a shell,
// and a hidden entry is one `ls -a` away rather than gone. Everything the
// harness writes is dot-prefixed AND outside the root, and this asserts the
// second half — the first was never enough on its own.
func rootEntries(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", root, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// Two leaves, one scratch home, one background job each. The log number is
// unique only within a Workspace handle and the surfaces build one per claimed
// node, so before the leaf carried its own name into the file both of them wrote
// `1.log` and the second silently appended to the first's output.
func TestConcurrentLeavesDoNotShareOneBackgroundJobLog(t *testing.T) {
	scratch := t.TempDir()
	names := map[string]bool{}
	for _, leaf := range []string{"task-9-n1", "task-9-n2"} {
		space, err := NewWorkspace(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		space = space.WithScratch(scratch)
		path, _, err := space.ScratchPath(filepath.Join(jobsDir, jobLogName(leaf, space.nextJobID())))
		if err != nil {
			t.Fatal(err)
		}
		if names[path] {
			t.Fatalf("both leaves claim the same background log %s", path)
		}
		names[path] = true
	}
}

// The measurement behind the brief's sufficiency claim. A directory is the one
// thing a Task cannot reason about — a fresh job folder and somebody's project
// are the same shape from up there — so it is read, and read cheaply: the first
// unaccounted file ends the walk.
func TestHoldsNothingButReadsTheDirectoryRatherThanAssumingIt(t *testing.T) {
	root := t.TempDir()
	space, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(t.TempDir())

	if !space.HoldsNothingBut(nil) {
		t.Fatal("an empty job directory reported material in it")
	}
	if err := os.WriteFile(filepath.Join(root, "07-vendors.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !space.HoldsNothingBut([]string{"07-vendors.md"}) {
		t.Fatal("a sibling's own deliverable, already in the leaf's hands, counted as something to discover")
	}
	if space.HoldsNothingBut(nil) {
		t.Fatal("a file nobody named counted as nothing to discover")
	}
	// Machinery would have made every directory in the product look occupied, and
	// so would the tooling's own state.
	if err := os.MkdirAll(filepath.Join(root, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !space.HoldsNothingBut([]string{"07-vendors.md"}) {
		t.Fatal("the tooling's own dot-directory counted as the job's material")
	}
	// A person's project answers no, which is the case the whole guard exists for.
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if space.HoldsNothingBut([]string{"07-vendors.md"}) {
		t.Fatal("a source file in the workspace was reported as nothing to discover")
	}
}

// A workspace nobody moved the machinery out of keeps the old shape. It is the
// control for the test above — the separation is the caller's act, and a test
// double that never makes it must not silently change what it is testing.
func TestAWorkspaceWithNoScratchHomeStillWritesBesideTheWork(t *testing.T) {
	root := t.TempDir()
	space, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if space.PersonalRoot() {
		t.Fatal("a harness-made directory reported itself as a person's")
	}
	trace := newTracer(space, "n1")
	trace.note("x")
	trace.close()
	if _, err := os.Stat(filepath.Join(root, traceDir)); err != nil {
		t.Fatalf("without a scratch home the recorder went somewhere unexpected: %v", err)
	}
	if space.OwnedByPerson(); !space.PersonalRoot() {
		t.Fatal("the person's-directory fact is not carried")
	}
}

// The loop's own end-to-end shape: a leaf runs, writes its deliverable, and
// leaves the directory it worked in holding that and nothing else.
func TestARunLeavesTheWorkAndTakesTheMachineryWithIt(t *testing.T) {
	root, scratch := t.TempDir(), t.TempDir()
	space, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(scratch)
	client := &scriptedCompleter{}
	linear := NewLinear(client, space, nil, 10, 150_000, time.Minute)
	if _, err := linear.Run(context.Background(), Task{NodeID: 3, Brief: "answer it"}); err != nil {
		t.Fatal(err)
	}
	if got := rootEntries(t, root); len(got) != 0 {
		t.Fatalf("a finished run left %v in the directory it worked in", got)
	}
	if _, err := os.Stat(TraceFile(scratch, "3")); err != nil {
		t.Fatalf("the recorder is not in the scratch home either: %v", err)
	}
}
