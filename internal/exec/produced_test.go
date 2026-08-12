package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The defect this file exists for: the artifact registry was fed by the write
// family alone, so a file a subprocess created under the sh tool was invisible
// to everything downstream — the files footer, the delivery gate's evidence, a
// continuation node's inputs. A run rendered a 204KB plot, delivered without
// mentioning it, and was failed for a message that "did not contain the
// script".
func TestASubprocessCreatedFileIsRecordedAsAnArtifact(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "7", nil)

	// Nothing here types the file into a tool call: a program the leaf ran
	// writes it, which is how a chart, a build output or a converted document
	// actually arrives in a workspace.
	result := tools.Execute(t.Context(), "sh",
		`{"cmd":"printf 'plot bytes' > stochastic_fit_plot.png"}`)
	if result.IsError {
		t.Fatalf("command failed: %s", result.Content)
	}

	artifacts := space.Artifacts("7")
	if !slices.Contains(artifacts, "stochastic_fit_plot.png") {
		t.Fatalf("a file created by the command is not in the node's artifacts: %v", artifacts)
	}
}

// A file the command only read is not a thing the command produced, or every
// leaf would deliver its own inputs back to the person who supplied them.
func TestAnUntouchedFileIsNotClaimedAsProduced(t *testing.T) {
	space := workspace(t)
	given := filepath.Join(space.Root(), "given.csv")
	if err := os.WriteFile(given, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Older than the slack the scan allows for coarse filesystem timestamps,
	// so this stands in for material that was in the workspace all along.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(given, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	tools := NewToolbox(space, "4", nil)

	if result := tools.Execute(t.Context(), "sh", `{"cmd":"wc -l given.csv"}`); result.IsError {
		t.Fatalf("command failed: %s", result.Content)
	}

	if artifacts := space.Artifacts("4"); len(artifacts) != 0 {
		t.Fatalf("a command that read a file claimed it as output: %v", artifacts)
	}
}

// The harness's own machinery must never surface as a deliverable. The spill
// directory is written by the very call that would otherwise record it, so this
// is not a hypothetical: without the dot-file rule every large sh result would
// name its own spill file as an artifact of the job.
func TestTheHarnessesOwnFilesAreNotDeliverables(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "5", nil)

	result := tools.Execute(t.Context(), "sh",
		`{"cmd":"printf 'LINE%s\\n' 1 2 3 4 5 6 7 8 9 10 | awk '{for(i=0;i<200;i++) print}'"}`)
	if result.IsError {
		t.Fatalf("command failed: %s", result.Content)
	}
	if !strings.Contains(result.Content, obsDir) {
		t.Fatalf("this test needs a spilled result to be meaningful: %q", result.Content)
	}

	for _, path := range space.Artifacts("5") {
		if strings.HasPrefix(path, ".") {
			t.Errorf("machinery named as a deliverable: %q", path)
		}
	}
}

// Bounded, because this runs after every shell call and a workspace's size is
// nobody's plan: a command that unpacked a dependency tree must not turn into
// three hundred named deliverables.
func TestProducedFilesAreBounded(t *testing.T) {
	space := workspace(t)
	outputs := filepath.Join(space.Root(), "out")
	if err := os.MkdirAll(outputs, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for index := range producedPerCall + 10 {
		name := filepath.Join(outputs, fmt.Sprintf("part-%03d.txt", index))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// Files under a dependency tree are somebody else's, however new they are.
	skipped := filepath.Join(space.Root(), "node_modules", "left-pad", "index.js")
	if err := os.MkdirAll(filepath.Dir(skipped), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(skipped, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	produced := space.producedSince(producedMark(time.Now()))
	if len(produced) > producedPerCall {
		t.Fatalf("scan returned %d files, cap is %d", len(produced), producedPerCall)
	}
	for _, path := range produced {
		if strings.Contains(path, "node_modules") {
			t.Errorf("a dependency tree was claimed as output: %q", path)
		}
	}
}
