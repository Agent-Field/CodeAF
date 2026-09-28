package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
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

// A successful merge remains successful when cleanup is unavailable. The next
// closed-session sweep retries only the landed copy, not a task kept for review.
func TestLandedForkRetirementRetriesFromCheckpoint(t *testing.T) {
	installFakeFurrow(t)
	binary := os.Getenv(furrow.BinaryEnvVar)
	root, repo, now := t.TempDir(), dirtyRepo(t), time.Now()
	dir, tree := newSweptUniverseSession(t, root, "aaaa5555aaaa5555", repo, now)
	writeFile(t, filepath.Join(tree.dir, "landed.txt"), "saved work\n")
	t.Setenv(furrow.BinaryEnvVar, filepath.Join(t.TempDir(), "missing"))
	furrow.Forget()
	merged, detail, _, _ := tree.comeHome("land it", []string{"landed.txt"}, gitSignature{})
	if merged != mergeMerged || !strings.Contains(detail, "cleanup failed") {
		t.Fatalf("landing = %s: %s", merged, detail)
	}
	if got := readFile(t, filepath.Join(repo, "landed.txt")); got != "saved work\n" {
		t.Fatal(got)
	}
	if _, err := os.Stat(tree.dir); err != nil {
		t.Fatalf("lost retry directory: %v", err)
	}
	document, ok := loadTaskCheckpoint((Place{Dir: dir}).Tasks())
	if !ok {
		t.Fatal("no checkpoint")
	}
	document.Nodes[0].Merge = mergeMerged
	document.Nodes[0].State = TaskDone
	document.Nodes = append(document.Nodes,
		taskRecord{ID: 2, Worktree: repo, Ground: repo, Rung: GroundRungHere},
		taskRecord{ID: 3, Worktree: repo, Ground: repo, Rung: GroundRungHere})
	writeCheckpoint(t, (Place{Dir: dir}).Tasks(), document)
	t.Setenv(furrow.BinaryEnvVar, binary)
	furrow.Forget()
	if !retireCheckpointForks(context.Background(), dir, true, func(line string) { t.Log(line) }) {
		t.Fatal("retry failed")
	}
	if _, err := os.Stat(tree.dir); !os.IsNotExist(err) {
		t.Fatalf("landed copy remains: %v", err)
	}
	if !retireCheckpointForks(context.Background(), dir, true, func(line string) { t.Log(line) }) {
		t.Fatal("repeat retry failed")
	}
}

func TestCheckpointRetirementLeavesUnmergedAndForeignCopies(t *testing.T) {
	for _, mode := range []string{"kept", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			installFakeFurrow(t)
			dir, tree := newSweptUniverseSession(t, t.TempDir(), "bbbb6666bbbb6666", dirtyRepo(t), time.Now())
			document, _ := loadTaskCheckpoint((Place{Dir: dir}).Tasks())
			if mode == "foreign" {
				document.Nodes[0].Merge = mergeMerged
				document.Nodes[0].Worktree = t.TempDir()
			}
			writeCheckpoint(t, (Place{Dir: dir}).Tasks(), document)
			retireCheckpointForks(context.Background(), dir, true, func(line string) { t.Log(line) })
			if _, err := os.Stat(tree.dir); err != nil {
				t.Fatalf("protected copy disappeared: %v", err)
			}
			if len(forkNames(t, tree.ground)) != 1 {
				t.Fatal("protected fork was retired")
			}
		})
	}
}

func TestForkRetirementOrdersSiblingCopiesByTheirGrounds(t *testing.T) {
	nodes := []taskRecord{
		{ID: 1, Worktree: "/session/trees/parent", Ground: "/repo"},
		{ID: 2, Worktree: "/session/trees/child", Ground: "/session/trees/parent"},
		{ID: 3, Worktree: "/session/trees/leaf", Ground: "/session/trees/child"},
	}
	ordered, err := forkRetirementOrder(nodes)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []uint64{3, 2, 1} {
		if ordered[i].ID != want {
			t.Fatalf("retirement order = %v", ordered)
		}
	}
	nodes[0].Ground = nodes[2].Worktree
	if _, err := forkRetirementOrder(nodes); err == nil {
		t.Fatal("cyclic grounds accepted")
	}
}

func TestNestedForkRetirementPreservesKeptChildGround(t *testing.T) {
	installFakeFurrow(t)
	dir, parent := newSweptUniverseSession(t, t.TempDir(), "eeee9999eeee9999", dirtyRepo(t), time.Now())
	child, err := prepareTaskTree(Place{Dir: dir}, parent.dir, "eeee9999eeee9999", 2, "kept child")
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.dropUniverse(); err == nil {
		t.Fatal("retired ground of a kept child")
	}
	if _, err := os.Stat(parent.dir); err != nil {
		t.Fatal(err)
	}
	if err := child.dropUniverse(); err != nil {
		t.Fatal(err)
	}
	if err := parent.dropUniverse(); err != nil {
		t.Fatal(err)
	}
}

// The scripted provider drives the public task door, actual worker tool calls,
// verification and landing. Only the model is a fixture; Furrow is the binary.
func TestRealFurrowWorkerCompletionRetiresTimeline(t *testing.T) {
	binary, data := isolatedRetirementFurrow(t)
	repo := newTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	captured := make(chan string, 1)
	completer := &routedCompleter{
		parent: []step{
			proposeTaskWithAcceptance("Write the note", "write retired.txt", "retired.txt contains completed work", repo, nil),
			finalText("handed off"),
		},
		child: []step{
			func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
				workspace := furrow.Open(ctx, repo)
				if workspace == nil {
					return nil, fmt.Errorf("fixture parent is not attached")
				}
				forks, err := workspace.Forks(ctx)
				if err != nil || len(forks) != 1 {
					return nil, fmt.Errorf("worker forks = %v: %v", forks, err)
				}
				id, err := os.ReadFile(filepath.Join(forks[0].Path, ".furrow", "workspace-id"))
				if err != nil {
					return nil, err
				}
				captured <- strings.TrimSpace(string(id))
				return writeCall("write-result", "retired.txt", "completed work\n")(ctx, messages)
			},
			finalText("Wrote retired.txt."),
		},
		audit: []step{verdict("VERIFIED — retired.txt contains completed work")},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "write the note"))
	node := graph.node(1)
	if node == nil {
		t.Fatal("task was not admitted")
	}
	waitDoneNode(t, node)
	if notice := node.notice(); notice.State != TaskDone {
		t.Fatalf("worker state = %s: %s", notice.State, notice.Report)
	}
	if got := readFile(t, filepath.Join(repo, "retired.txt")); got != "completed work\n" {
		t.Fatal(got)
	}
	var id string
	select {
	case id = <-captured:
	default:
		t.Fatal("worker never ran in a Furrow fork")
	}
	if _, err := os.Stat(filepath.Join(data, "store-v1", "workspaces", id)); !os.IsNotExist(err) {
		t.Fatalf("worker timeline remains: %v", err)
	}
	if names := forkNames(t, repo); len(names) != 0 {
		t.Fatalf("worker fork remains: %v", names)
	}
	if out := retirementFurrowRun(t, binary, repo, "timeline", "--limit", "1"); strings.TrimSpace(string(out)) == "[]" {
		t.Fatal("parent history disappeared")
	}
}
