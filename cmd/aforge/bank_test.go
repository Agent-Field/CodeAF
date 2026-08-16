package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// The incident, end to end at the surface.
//
// A craft job's root leaf ran a benchmark campaign for the better part of an
// hour: four algorithms implemented, three datasets benchmarked, a progress row
// at every step, and at the finish line "The comparison writeup for all four
// algorithms is now pulled together into one document". Then its first attempt
// hit its time ceiling. The node watchdog returns no outcome at all in that
// case, so everything the attempt reached lived in exactly two places — the
// journal and the job directory — and the retry read neither. It started cold.
//
// It does not any more. The bank is what those two places say, composed under
// the same headers a re-decomposed leaf already gets.
func TestTheRetryOfALeafThatDiedOnTheClockCarriesItsBank(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "craft-4958", Brief: "Deliver the result of ideate novel classification algorithms", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1",
		Intent: "ideate novel classification algorithms"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	node, _, err := graph.Node("craft-4958")
	if err != nil {
		t.Fatalf("read node: %v", err)
	}

	// What it posted as it went — the rows a long worker writes to its own
	// record, which for a craft root is the node itself.
	for _, latest := range []string{
		"AdaptiveKernel and three others implemented",
		"benchmarked against RBF-SVM on wine, digits and covertype",
		"The comparison writeup for all four algorithms is now pulled together into one document",
	} {
		if _, err := thread.Record(graph, store.Message{
			Role: store.RoleSystem, NodeID: "craft-4958", Body: "benchmarking",
			Progress: &store.MessageProgress{Phase: "benchmarking", Latest: latest},
		}); err != nil {
			t.Fatalf("record progress: %v", err)
		}
	}

	// And what it left on disk.
	jobDir := filepath.Join(t.TempDir(), "craft-4958")
	space, err := exec.NewWorkspace(jobDir)
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	writeFile(t, filepath.Join(jobDir, "04-comparison.md"), "# Comparison of four algorithms")
	writeFile(t, filepath.Join(jobDir, "kernels.py"), "def adaptive_kernel(): ...")
	if err := os.MkdirAll(filepath.Join(jobDir, ".obs"), 0o755); err != nil {
		t.Fatalf("obs dir: %v", err)
	}
	writeFile(t, filepath.Join(jobDir, ".obs", "spill-1.txt"), "machinery")

	// The watchdog's ending: an error that says the clock, and no outcome.
	bank := leafBank(graph, node, space, jobDir, true, nil, nil)
	if bank.Empty() {
		t.Fatal("a leaf that shared progress and wrote two files banked nothing")
	}
	instruction := bank.Input().Result

	if !strings.Contains(instruction, resident.ContinuationPartialHeader) {
		t.Fatalf("the retry's instruction lost the continuation header:\n%s", instruction)
	}
	if !strings.Contains(instruction, resident.ContinuationFilesHeader) {
		t.Fatalf("the retry's instruction lost the files header:\n%s", instruction)
	}
	if !strings.Contains(instruction, "The comparison writeup for all four algorithms is now pulled together into one document") {
		t.Fatalf("the retry was not told the writeup was already assembled:\n%s", instruction)
	}
	if !strings.Contains(instruction, "AdaptiveKernel and three others implemented") {
		t.Fatalf("the retry was not told the algorithms were already implemented:\n%s", instruction)
	}
	if !strings.Contains(instruction, filepath.Join(jobDir, "04-comparison.md")) {
		t.Fatalf("the retry was not handed the document already on disk:\n%s", instruction)
	}
	if !strings.Contains(instruction, filepath.Join(jobDir, "kernels.py")) {
		t.Fatalf("the retry was not handed the code already on disk:\n%s", instruction)
	}
	// The harness's own directories are not the job's work and are never offered.
	if strings.Contains(instruction, ".obs") {
		t.Fatalf("the machinery's spill directory was banked as produced work:\n%s", instruction)
	}
	// And it arrives as a result the leaf already holds, not as an errand.
	if bank.Input().Title != resident.BankInputTitle {
		t.Fatalf("bank input title = %q", bank.Input().Title)
	}

	// The class does not move, because the clock is not evidence about it.
	if resident.MayReclassify(nil, &exec.Abandoned{After: 17 * time.Minute}) {
		t.Fatal("a leaf abandoned by the watchdog was reclassified")
	}
	if resident.MayReclassify(&exec.Outcome{Stop: exec.StopDeadline}, nil) {
		t.Fatal("a leaf that ran out of wall clock was reclassified")
	}
	if node.Subharness != "" {
		t.Fatalf("the leaf's promised worker changed before it ever ran: %q", node.Subharness)
	}
}

// An errand works directly in a person's own folder. The files in it are theirs,
// not a previous attempt's, and naming them to a retry as "files already
// produced, to reuse rather than recreate" would tell a worker its job was
// half-done by work nobody did.
func TestAnErrandsOwnDirectoryIsNeverBankedAsProducedWork(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "fix the failing test", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "fix the failing test"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	node, _, err := graph.Node("task-1")
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	theirDir := t.TempDir()
	space, err := exec.NewWorkspace(theirDir)
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	writeFile(t, filepath.Join(theirDir, "main.go"), "package main")

	bank := leafBank(graph, node, space, theirDir, false, nil, nil)
	if !bank.Empty() {
		t.Fatalf("an errand banked the person's own project as produced work: %+v", bank)
	}
}

// A leaf that is not its own job root shares to the board, which anchors to the
// job — and the journal cannot say which leaf of a job wrote which note. What it
// said, it remembers, and the retry gets it either way.
func TestALeafsOwnSharedLinesRideIntoItsRetryEvenWhenTheBoardCannotAttributeThem(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "the job", Stage: 2},
		{ID: "task-1-a", Parent: "task-1", Brief: "one part of it", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "the job"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	node, _, err := graph.Node("task-1-a")
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	// The note went to the job root, exactly as the board writes it.
	if _, err := thread.Record(graph, store.Message{
		Role: store.RoleAgent, NodeID: "task-1",
		Body: jobNoteBody("the revenue column is duplicated in the source"),
	}); err != nil {
		t.Fatalf("record note: %v", err)
	}

	shared := &sharedLines{}
	shared.add("the revenue column is duplicated in the source")

	bank := leafBank(graph, node, nil, "", false, nil, shared.lines())
	instruction := bank.Input().Result
	if !strings.Contains(instruction, "the revenue column is duplicated in the source") {
		t.Fatalf("the leaf's own shared line did not reach its retry:\n%s", instruction)
	}
	// Said once, however many sources it came from.
	if strings.Count(instruction, "the revenue column is duplicated") != 1 {
		t.Fatalf("the same finding was banked twice:\n%s", instruction)
	}
}

// The sentence the surface says when the startup sweep finds work a dead process
// left claimed. It used to promise "each starts again from the beginning", which
// was accurate and was the bug; the behaviour changed, so the words did.
func TestTheLaunchPickupSaysWhatActuallyHappensNow(t *testing.T) {
	line := pickedUpMessage(2)
	if !strings.Contains(line, "picked up 2 piece(s) of work that were interrupted") {
		t.Fatalf("the pickup line stopped saying what it found: %q", line)
	}
	if strings.Contains(line, "starts again from the beginning") {
		t.Fatalf("the pickup line still promises a cold restart: %q", line)
	}
	if !strings.Contains(line, "continues from what it had already reached") {
		t.Fatalf("the pickup line does not say the work continues: %q", line)
	}
	if !strings.Contains(line, "still where it left them") {
		t.Fatalf("the pickup line stopped saying where the files are: %q", line)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
