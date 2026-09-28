package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/furrow"
)

// The actual landing must retire the timeline, not just the fork-list entry.
// Each test owns its store so this proof cannot capture the developer's files.
func TestRealFurrowLandedTaskRetiresTimeline(t *testing.T) {
	binary, data := isolatedRetirementFurrow(t)
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}
	for id := uint64(1); id <= 3; id++ {
		tree, err := prepareTaskTree(place, repo, "aaaabbbbccccdddd", id, "land and retire")
		if err != nil {
			t.Fatal(err)
		}
		if tree.rung != GroundRungUniverse {
			t.Fatalf("rung = %s, want the real furrow", tree.rung)
		}
		workspaceID := strings.TrimSpace(readFile(t, filepath.Join(tree.dir, ".furrow", "workspace-id")))
		writeFile(t, filepath.Join(tree.dir, "done.txt"), strings.Repeat("done\n", int(id)))
		retirementFurrowRun(t, binary, tree.dir, "snap")
		merged, detail, _, _ := tree.comeHome("land and retire", []string{"done.txt"}, gitSignature{})
		if merged != mergeMerged {
			t.Fatalf("landing = %s: %s", merged, detail)
		}
		if got := readFile(t, filepath.Join(repo, "done.txt")); got != strings.Repeat("done\n", int(id)) {
			t.Fatalf("landed result = %q", got)
		}
		if _, err := os.Stat(filepath.Join(data, "store-v1", "workspaces", workspaceID)); !os.IsNotExist(err) {
			t.Fatalf("landed task still has a retained timeline: %v", err)
		}
		if names := forkNames(t, repo); len(names) != 0 {
			t.Fatalf("landed forks remain: %v", names)
		}
	}
	// Removing only this fixture's parent history isolates leaked child roots.
	retirementFurrowRun(t, binary, repo, "forget", "--purge")
	var report struct {
		Reachable uint64 `json:"reachable_objects"`
	}
	if err := json.Unmarshal(retirementFurrowRun(t, binary, repo, "gc", "--dry-run"), &report); err != nil {
		t.Fatal(err)
	}
	if report.Reachable != 0 {
		t.Fatalf("completed children retain %d objects after the fixture parent is purged", report.Reachable)
	}
}

func isolatedRetirementFurrow(t *testing.T) (string, string) {
	t.Helper()
	binary := strings.TrimSpace(os.Getenv(realFurrowEnvVar))
	if binary == "" {
		t.Skip("set " + realFurrowEnvVar + " to run the real fork-retirement proof")
	}
	data := t.TempDir()
	t.Setenv("FURROW_DATA_DIR", data)
	t.Setenv(furrow.BinaryEnvVar, binary)
	furrow.Forget()
	t.Cleanup(furrow.Forget)
	return binary, data
}

func retirementFurrowRun(t *testing.T, binary, repo string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binary, append([]string{"--json", "--repo", repo}, args...)...)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("furrow %v: %v\n%s", args, err, out)
	}
	return out
}
