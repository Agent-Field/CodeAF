package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/taskstorage"
)

// An archive failure must not turn successful task retirement into data loss.
// This drives the same task executor a person starts through the conversation.
func TestTaskStorageFailureKeepsFinishedCopyForRecovery(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	t.Setenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY", filepath.Join(t.TempDir(), "missing-archive"))
	completer := &routedCompleter{
		parent: []step{proposeCall("Add greeting", "write greet.go"), finalText("handed off")},
		child: nodeLane(4, func(_, wrote bool) *ai.Response {
			if !wrote {
				return writeResponse("greet", "greet.go", "package greet\nfunc Greet() string { return \"hi\" }\n")
			}
			return pricedResponse("greet.go is ready", 0.01)
		}),
	}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Workspace = repo
		c.Place = Place{Dir: t.TempDir(), Workspace: repo}
		c.AskConsent = false
		c.TaskAutoApproveSeconds = 0
		c.TaskAudit = false
	})
	notice := landedNode(t, agent, agent.graph())
	if notice.State != TaskDone {
		t.Fatalf("task did not finish: %+v", notice)
	}
	node := agent.graph().node(1)
	node.graph.mu.Lock()
	dir := node.worktree
	node.graph.mu.Unlock()
	if _, err := os.Stat(filepath.Join(dir, "greet.go")); err != nil {
		t.Fatalf("archive failure removed the full working copy: %v", err)
	}
	if !strings.Contains(notice.Report, "compact storage") {
		t.Fatalf("archive failure hidden from report: %q", notice.Report)
	}
}

func TestDefaultRunWithoutWriterProofKeepsItsFullWorkingCopy(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	t.Setenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY", filepath.Join(t.TempDir(), "missing-archive"))
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("finished")
	double.real = true
	double.work = func(dir string) { writeFile(t, filepath.Join(dir, "proof.txt"), "finished bytes\n") }
	registerBeltRunEngine(t, double)
	place := Place{Dir: t.TempDir()}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText("finished")}}, func(c *Config) {
		c.Workspace = newTestRepo(t)
		c.Place = place
		c.SessionFile = place.Transcript()
		c.AskConsent = false
	})
	id, _, _, err := agent.StartTask(context.Background(), "write proof.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	endBeltRun(t, agent, double)
	row, ok := runRowOf(agent.graph(), id)
	if !ok || row.Copy == nil {
		t.Fatalf("run has no saved working copy: %+v", row)
	}
	if b, e := os.ReadFile(filepath.Join(row.Copy.Dir, "proof.txt")); e != nil || string(b) != "finished bytes\n" {
		t.Fatalf("uncertain writer teardown lost task bytes: %q %v", b, e)
	}
	if !strings.Contains(row.Report, "compact storage") {
		t.Fatalf("retained-copy reason was hidden: %q", row.Report)
	}
}

func TestColdLinkedRunKeepsStagedAndUnstagedWorkAcrossPruneAndContinue(t *testing.T) {
	binary := env.Get("CODEAF_PROTOTYPE_ARCHIVE_BINARY")
	if binary == "" {
		t.Skip("prototype archive binary is not configured")
	}
	for _, name := range []string{"ordinary prune", "missing administration repair", "existing user lock"} {
		t.Run(name, func(t *testing.T) {
			repair := name == "missing administration repair"
			prelocked := name == "existing user lock"
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
			t.Setenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY", binary)
			agent, g, repo, double := continueAgent(t)
			tree, e := cutWorktreeAt(agent.config.Place, repo, filepath.Join(agent.config.Place.Trees(), "7"), "task/cold-index", 0700)
			if e != nil {
				t.Fatal(e)
			}
			writeFile(t, filepath.Join(tree.dir, ".gitignore"), "ignored/\n")
			mustGit(t, tree.dir, "add", ".gitignore")
			mustGit(t, tree.dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "task input policy")
			writeFile(t, filepath.Join(tree.dir, "shared.txt"), "staged version\n")
			mustGit(t, tree.dir, "add", "shared.txt")
			writeFile(t, filepath.Join(tree.dir, "shared.txt"), "unstaged version\n")
			writeFile(t, filepath.Join(tree.dir, "untracked.txt"), "untracked bytes\n")
			writeFile(t, filepath.Join(tree.dir, "ignored", "cache.txt"), "ignored bytes\n")
			head := strings.TrimSpace(gitOut(t, tree.dir, "rev-parse", "HEAD"))
			staged := gitOut(t, tree.dir, "show", ":shared.txt")
			status := gitOut(t, tree.dir, "status", "--porcelain=v1", "--untracked-files=all")
			if prelocked {
				mustGit(t, tree.dir, "worktree", "lock", "--reason", "the user is keeping this", tree.dir)
			}
			keepInterruptedRun(t, agent, g, 7, "continue preserved state", runCopyOf(tree))
			manager, _ := taskStorage(agent.config.Place)
			manager.Busy = func(string) bool { return false }
			c := cell.Cell{ID: agent.config.Place.ID(), Root: agent.config.Place.Dir}
			carry := taskcopy.Carry{}
			manager.BeforeCapture = func(ctx context.Context, path string) error {
				if e := carry.PrepareCold(c, path); e != nil {
					return e
				}
				return pinTaskGit(ctx, manager, agent.config.Place, path)
			}
			manager.BeforeRetire = func(_ context.Context, snapshot, path string) error { return carry.MarkCold(c, path, snapshot) }
			if e := manager.Park(context.Background(), tree.dir); e != nil {
				t.Fatal(e)
			}
			if _, e := os.Stat(tree.dir); !os.IsNotExist(e) {
				t.Fatalf("working tree was not cold: %v", e)
			}
			pin, e := readTaskGitPin(manager, tree.dir)
			if e != nil {
				t.Fatal(e)
			}
			mustGit(t, repo, "worktree", "prune", "--expire", "now")
			if _, e := os.Stat(pin.Admin); e != nil {
				t.Fatalf("normal pruning removed pinned Git state: %v", e)
			}
			if repair {
				if e := os.RemoveAll(pin.Admin); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := agent.ContinueRun(context.Background(), 7); e != nil {
				t.Fatal(e)
			}
			beltRunWaitFor(t, "the restored run to start", double.didRun)
			if got := strings.TrimSpace(gitOut(t, tree.dir, "rev-parse", "HEAD")); got != head {
				t.Fatalf("commit changed: %q != %q", got, head)
			}
			if got := gitOut(t, tree.dir, "show", ":shared.txt"); got != staged {
				t.Fatalf("staged bytes changed: %q != %q", got, staged)
			}
			if got := gitOut(t, tree.dir, "status", "--porcelain=v1", "--untracked-files=all"); got != status {
				t.Fatalf("staging state changed:\n%s\nwant:\n%s", got, status)
			}
			for path, want := range map[string]string{"shared.txt": "unstaged version\n", "untracked.txt": "untracked bytes\n", "ignored/cache.txt": "ignored bytes\n"} {
				got, e := os.ReadFile(filepath.Join(tree.dir, path))
				if e != nil || string(got) != want {
					t.Fatalf("%s lost: %q %v", path, got, e)
				}
			}
			lock, e := os.ReadFile(filepath.Join(pin.Admin, "locked"))
			if prelocked {
				if e != nil || strings.TrimSpace(string(lock)) != "the user is keeping this" {
					t.Fatalf("the user's Git lock changed: %q %v", lock, e)
				}
			} else if !os.IsNotExist(e) {
				t.Fatalf("our archive lock was not released: %v", e)
			}
			endBeltRun(t, agent, double)
		})
	}
}

func TestTaskStorageQuiescenceWaitsForClosedBackgroundJobs(t *testing.T) {
	agent, _ := jobsAgent(t)
	text, failed := runTool(t, agent, "bash", `{"command":"sleep 30","background":true}`)
	if failed {
		t.Fatalf("background launch: %s", text)
	}
	if agent.TaskStorageQuiescent() {
		t.Fatal("a live worker was called safe to archive")
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the closed worker's processes to be reaped", agent.TaskStorageQuiescent)
}

func TestExplicitReadRestoresACompletedColdTaskFile(t *testing.T) {
	binary := env.Get("CODEAF_PROTOTYPE_ARCHIVE_BINARY")
	if binary == "" {
		t.Skip("prototype archive binary is not configured")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	place := Place{Dir: t.TempDir()}
	repo := newTestRepo(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = repo; c.Place = place })
	tree, e := cutWorktreeAt(place, repo, filepath.Join(place.Trees(), "completed"), "task/closed-answer", 0700)
	if e != nil {
		t.Fatal(e)
	}
	writeFile(t, filepath.Join(tree.dir, "answer.txt"), "a completed task's retained answer\n")
	manager, _ := taskStorage(place)
	manager.Busy = func(string) bool { return false }
	carry := taskcopy.Carry{}
	c := cell.Cell{ID: place.ID(), Root: place.Dir}
	manager.BeforeCapture = func(ctx context.Context, path string) error {
		if e := carry.PrepareCold(c, path); e != nil {
			return e
		}
		return pinTaskGit(ctx, manager, place, path)
	}
	manager.BeforeRetire = func(_ context.Context, snapshot, path string) error { return carry.MarkCold(c, path, snapshot) }
	if e := manager.Park(context.Background(), tree.dir); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(tree.dir); !os.IsNotExist(e) {
		t.Fatalf("copy was not cold: %v", e)
	}
	args, e := json.Marshal(map[string]string{"path": filepath.Join(tree.dir, "answer.txt")})
	if e != nil {
		t.Fatal(e)
	}
	text, failed := runTool(t, agent, "read", string(args))
	if failed || !strings.Contains(text, "a completed task's retained answer") {
		t.Fatalf("reading an archived task failed: %q error=%v", text, failed)
	}
}

func TestActiveAncestorWorkerPreventsColdRetirement(t *testing.T) {
	root := t.TempDir()
	trees := filepath.Join(root, "trees")
	dir := filepath.Join(trees, "child")
	writeFile(t, filepath.Join(dir, "answer.txt"), "still mutable by its parent\n")
	graph := newTaskGraph()
	graph.nodes[1] = &TaskNode{id: 1, graph: graph, state: TaskRunning, worktree: trees}
	manager := taskstorage.Manager{
		Root: filepath.Join(root, "storage"), Trees: trees,
		Vault: taskstorage.CLIBackend{Binary: filepath.Join(root, "must-not-run"), Store: filepath.Join(root, "vault")},
		Busy:  func(path string) bool { return graph.taskStorageBusy(nil, path) },
	}
	if e := manager.Park(context.Background(), dir); e == nil || !strings.Contains(e.Error(), "writer") {
		t.Fatalf("active ancestor was not refused before capture: %v", e)
	}
	if b, e := os.ReadFile(filepath.Join(dir, "answer.txt")); e != nil || string(b) != "still mutable by its parent\n" {
		t.Fatalf("ancestor-owned bytes changed: %q %v", b, e)
	}
}

func TestDefaultCompletedRunArchivesAndExplicitWorkTabRestoresIt(t *testing.T) {
	binary := env.Get("CODEAF_PROTOTYPE_ARCHIVE_BINARY")
	if binary == "" {
		t.Skip("prototype archive binary is not configured")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("finished")
	double.summary.StorageSafe = true
	double.real = true
	double.work = func(dir string) { writeFile(t, filepath.Join(dir, "proof.txt"), "automatically archived result\n") }
	registerBeltRunEngine(t, double)
	place := Place{Dir: t.TempDir()}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText("finished")}}, func(c *Config) {
		c.Workspace = newTestRepo(t)
		c.Place = place
		c.SessionFile = place.Transcript()
		c.AskConsent = false
	})
	id, _, _, e := agent.StartTask(context.Background(), "write proof.txt", false)
	if e != nil {
		t.Fatal(e)
	}
	endBeltRun(t, agent, double)
	row, ok := runRowOf(agent.graph(), id)
	if !ok || row.Copy == nil || row.State != TaskDone {
		t.Fatalf("completed run record: %+v", row)
	}
	if _, e := os.Stat(row.Copy.Dir); !os.IsNotExist(e) {
		t.Fatalf("normal completion did not archive the working copy: %v", e)
	}
	key := planStoreID(strconv.FormatUint(id, 10))
	_, _ = agent.PlanTaskWork(key)
	if _, e := os.Stat(row.Copy.Dir); !os.IsNotExist(e) {
		t.Fatalf("background work query expanded the cold copy: %v", e)
	}
	work, ok := agent.OpenPlanTaskWork(key)
	if !ok || !work.Read || work.Error != "" {
		t.Fatalf("explicit work reopen: %+v %v", work, ok)
	}
	if b, e := os.ReadFile(filepath.Join(row.Copy.Dir, "proof.txt")); e != nil || string(b) != "automatically archived result\n" {
		t.Fatalf("completed task bytes lost: %q %v", b, e)
	}
	after, _ := runRowOf(agent.graph(), id)
	if after.State != TaskDone {
		t.Fatalf("opening a completed copy restarted its execution: %+v", after)
	}
}

func TestPreviousShellAttemptCannotBeRetiredByACleanContinuation(t *testing.T) {
	binary := env.Get("CODEAF_PROTOTYPE_ARCHIVE_BINARY")
	if binary == "" {
		t.Skip("prototype archive binary is not configured")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	place := Place{Dir: t.TempDir()}
	dir := filepath.Join(place.Trees(), "one")
	writeFile(t, filepath.Join(dir, "answer.txt"), "must remain resident\n")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = dir; c.Place = place })
	if text, failed := runTool(t, agent, "bash", `{"command":"printf shell-finished"}`); failed {
		t.Fatal(text)
	}
	graph := agent.graph()
	node := &TaskNode{id: 1, graph: graph, state: TaskFailed, worktree: dir, storageBlocked: true, done: make(chan struct{})}
	graph.nodes[1] = node
	graph.order = []uint64{1}
	graph.seq = 1
	graph.run = func(n *TaskNode) { graph.complete(n, TaskDone) }
	if e := agent.ContinueTask(1, "only file work now"); e != nil {
		t.Fatal(e)
	}
	waitDoneNode(t, node)
	manager, _ := taskStorage(place)
	manager.Busy = func(path string) bool { return graph.taskStorageBusy(nil, path) }
	if e := manager.Park(context.Background(), dir); e == nil || !strings.Contains(e.Error(), "writer") {
		t.Fatalf("a clean retry erased shell uncertainty: %v", e)
	}
	if b, e := os.ReadFile(filepath.Join(dir, "answer.txt")); e != nil || string(b) != "must remain resident\n" {
		t.Fatalf("uncertain task bytes changed: %q %v", b, e)
	}
}

func TestSettledShellChildPreventsParentRetirementAfterRestart(t *testing.T) {
	binary := env.Get("CODEAF_PROTOTYPE_ARCHIVE_BINARY")
	if binary == "" {
		t.Skip("prototype archive binary is not configured")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	place := Place{Dir: t.TempDir()}
	parent := filepath.Join(place.Trees(), "parent")
	child := filepath.Join(parent, "child")
	writeFile(t, filepath.Join(child, "answer.txt"), "late writer could still change this\n")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = child; c.Place = place })
	if text, failed := runTool(t, agent, "bash", `{"command":"printf shell-finished"}`); failed {
		t.Fatal(text)
	}
	if e := agent.Close(); e != nil {
		t.Fatal(e)
	}
	// A fresh graph has no old in-memory blocked bit. Retirement must still see
	// the device-local launch uncertainty that deliberately survives restart.
	restarted, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = parent; c.Place = place })
	manager, _ := taskStorage(place)
	manager.Busy = func(path string) bool { return restarted.graph().taskStorageBusy(nil, path) }
	if e := manager.Park(context.Background(), parent); e == nil || !strings.Contains(e.Error(), "writer") {
		t.Fatalf("a restart forgot the settled child shell: %v", e)
	}
	if _, e := os.Stat(filepath.Join(child, "answer.txt")); e != nil {
		t.Fatalf("parent retirement removed uncertain child: %v", e)
	}
}

func TestUncontainedShellBlocksEvenUnrelatedTaskCopiesInItsProfile(t *testing.T) {
	if env.Get("CODEAF_PROTOTYPE_ARCHIVE_BINARY") == "" {
		t.Skip("prototype archive binary is not configured")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	agent, _ := jobsAgent(t)
	if text, failed := runTool(t, agent, "bash", `{"command":"printf finished"}`); failed {
		t.Fatal(text)
	}
	if e := agent.Close(); e != nil {
		t.Fatal(e)
	}
	if agent.TaskStorageQuiescent() {
		t.Fatal("a closed shell's missing process group was treated as escaped-writer proof")
	}
	place := Place{Dir: t.TempDir()}
	dir := filepath.Join(place.Trees(), "unrelated")
	writeFile(t, filepath.Join(dir, "answer.txt"), "host-wide access remains uncertain\n")
	restarted, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = dir; c.Place = place })
	manager, _ := taskStorage(place)
	manager.Busy = func(path string) bool { return restarted.graph().taskStorageBusy(nil, path) }
	if e := manager.Park(context.Background(), dir); e == nil || !strings.Contains(e.Error(), "writer") {
		t.Fatalf("an unrelated path bypassed uncontained host-writer uncertainty: %v", e)
	}
	if _, e := os.Stat(filepath.Join(dir, "answer.txt")); e != nil {
		t.Fatal(e)
	}
}

func TestShellDoesNotRunWhenItsUncertaintyCannotBeRecorded(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	writeFile(t, filepath.Join(profile, "task-storage", "uncertain-writers"), "not a directory")
	agent, dir := jobsAgent(t)
	text, failed := runTool(t, agent, "bash", `{"command":"printf must-not-run > started.txt"}`)
	if !failed || !strings.Contains(text, "writer uncertainty") {
		t.Fatalf("an unrecorded shell was allowed: %q %v", text, failed)
	}
	if _, e := os.Stat(filepath.Join(dir, "started.txt")); !os.IsNotExist(e) {
		t.Fatalf("the shell ran before its durable guard: %v", e)
	}
}
