package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// ── the engine's half ────────────────────────────────────────────────────────

// fakeEngineRecordEnv names a file the stub writes its argv and environment to,
// for the runs whose working directory does not outlive them.
const fakeEngineRecordEnv = "AFORGE_FAKE_SWE_RECORD"

// fakeEngineLeafEnv is which leaf this stub is standing in for. The isolation
// scenario is the only one where two of them run at once and each has to be able
// to recognise its own work.
const fakeEngineLeafEnv = "AFORGE_FAKE_SWE_LEAF"

// fakeIsolationEngine is a coding run written to fail loudly if anything else is
// editing the tree it is working in.
//
// It is the measured defect in miniature. It leaves the shared file in a state
// that does not stand on its own — the half-second in the middle of a real edit
// — waits long enough for a sibling to reach the same window, and then verifies
// the WHOLE tree the way the engine does, by reading what is on disk rather than
// what it thinks it wrote. In one directory the two runs trample each other and
// at least one of them reports somebody else's change as its own breakage; in
// two directories both are green, which is the entire point of a view.
//
// It also litters exactly as the engine litters: a .codeaf/ tree, a plan
// database, and a run of wip(edit) commits that are bookkeeping rather than
// anything a person asked to read.
func fakeIsolationEngine(directory string) int {
	leaf := os.Getenv(fakeEngineLeafEnv)
	out := json.NewEncoder(os.Stdout)
	stage := func(name, status string, data map[string]any) {
		_ = out.Encode(map[string]any{"type": "stage", "stage": name, "status": status, "data": data})
	}
	fail := func(why string) int {
		_ = out.Encode(map[string]any{
			"type": "terminal", "status": "fail", "message": why,
			"data": map[string]any{"cycle": 1, "cost_usd": 0.5},
		})
		return 0
	}
	stage("bootstrap", "ready", map[string]any{"workspace": directory})

	// The engine's own git exclusions, which are what keep its machinery out of
	// the change set. Written here for the same reason the real one writes them:
	// everything below stages with `add -A`.
	fakeExclude(directory, ".codeaf/", ".plandb.db")

	shared := filepath.Join(directory, "shared.txt")
	halfway := "half-edited by " + leaf
	if err := os.WriteFile(shared, []byte(halfway), 0o644); err != nil {
		return fail(err.Error())
	}
	// The window. Long enough that two leaves started together are both inside
	// it, short enough that the test is a test rather than a wait.
	time.Sleep(400 * time.Millisecond)

	stage("verification", "pass", map[string]any{"commands": "go test ./..."})
	if found, err := os.ReadFile(shared); err != nil || string(found) != halfway {
		return fail(fmt.Sprintf("the tree moved under this run: shared.txt reads %q, not %q",
			string(found), halfway))
	}

	// The machinery. 438 files was the real number; three is the same fact.
	_ = os.MkdirAll(filepath.Join(directory, ".codeaf", "plan"), 0o755)
	_ = os.WriteFile(filepath.Join(directory, ".codeaf", "outcomes.jsonl"), []byte("{}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(directory, ".codeaf", "plan", "architecture.md"), []byte("# plan\n"), 0o644)
	_ = os.WriteFile(filepath.Join(directory, ".plandb.db"), []byte("sqlite"), 0o644)

	// The change, finished: the shared file back to something that stands on its
	// own, plus this leaf's own file.
	if err := os.WriteFile(shared, []byte("ok\n"), 0o644); err != nil {
		return fail(err.Error())
	}
	fakeWIPCommit(directory, "wip(edit): "+leaf+" first pass")
	if err := os.WriteFile(filepath.Join(directory, "fix-"+leaf+".txt"), []byte("done by "+leaf+"\n"), 0o644); err != nil {
		return fail(err.Error())
	}
	fakeWIPCommit(directory, "wip(edit): "+leaf+" second pass")

	stage("audit", "pass", map[string]any{"cycle": 1})
	_ = out.Encode(map[string]any{
		"type": "terminal", "status": "pass",
		"message": "The shared file is consistent again and " + leaf + " added its own.",
		"data":    map[string]any{"cycle": 1, "cost_usd": 0.25},
	})
	return 0
}

// fakeExclude writes the engine's own untracked-file exclusions, through git so
// it lands in the right file whether this is a clone or a worktree.
func fakeExclude(directory string, patterns ...string) {
	command := exec.Command("git", "rev-parse", "--git-path", "info/exclude")
	command.Dir = directory
	raw, err := command.Output()
	if err != nil {
		return
	}
	path := strings.TrimSpace(string(raw))
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, path)
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	handle, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer handle.Close()
	for _, pattern := range patterns {
		fmt.Fprintln(handle, pattern)
	}
}

func fakeWIPCommit(directory, message string) {
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.name=engine", "-c", "user.email=engine@example.com",
			"-c", "commit.gpgsign=false", "commit", "--no-verify", "-m", message},
	} {
		command := exec.Command("git", args...)
		command.Dir = directory
		_ = command.Run()
	}
}

// ── the harness ──────────────────────────────────────────────────────────────

// personsRepository is a git repository with a history and a file in it, which
// is what somebody hands `aforge do -w`.
func personsRepository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("the swe worker needs git")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "shared.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"add", "-A"},
		{"-c", "user.name=person", "-c", "user.email=person@example.com",
			"-c", "commit.gpgsign=false", "commit", "--no-verify", "-m", "the work as it stands"},
	} {
		command := exec.Command("git", args...)
		command.Dir = directory
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return directory
}

// isolationLeaf builds one coding leaf pointed at a shared workspace.
func isolationLeaf(t *testing.T, space *Workspace, leaf, title string, record string) (*SWE, Task) {
	t.Helper()
	worker := NewSWE(space, "vendor/model", "sk-test", "", 2*time.Minute).WithAttribution(true)
	worker.binary = os.Args[0]
	worker.pollEvery = 10 * time.Millisecond
	worker.extraEnv = []string{
		fakeEngineEnv + "=isolation",
		fakeEngineLeafEnv + "=" + leaf,
		fakeEngineRecordEnv + "=" + record,
	}
	return worker, Task{NodeKey: leaf, NodeID: 1, Title: title, Brief: title + ", with a test."}
}

func gitOut(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	out, err := command.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, directory, err)
	}
	return strings.TrimSpace(string(out))
}

// ── the tests ────────────────────────────────────────────────────────────────

// TWO LEAVES, ONE REPOSITORY, NEITHER ONE BROKEN BY THE OTHER.
//
// The measured defect: parallel coding leaves share one working directory, each
// runs the repository's whole verification, and a sibling's in-flight edit fails
// an innocent leaf — twice the spend, and the repair loop on the critical path.
// The stub above reproduces it exactly: it leaves the shared file half-edited
// for four hundred milliseconds and then reads it back, which is green in a view
// of one's own and red in a directory somebody else is typing into.
//
// And both of them land. Isolation that delivered one change out of two would be
// a different bug with better manners.
func TestParallelCodingLeavesVerifyInIsolationAndBothLand(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	root := personsRepository(t)
	base := gitOut(t, root, "rev-parse", "HEAD")
	// A person's own directory: the harness's files go somewhere else, which is
	// how this workspace says whose it is.
	space, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(t.TempDir())
	records := t.TempDir()

	type landing struct {
		outcome *Outcome
		err     error
	}
	results := make([]landing, 2)
	leaves := []string{"n1", "n2"}
	titles := []string{"teach the parser trailing commas", "teach the lexer unicode escapes"}
	var wait sync.WaitGroup
	for index, leaf := range leaves {
		wait.Add(1)
		go func() {
			defer wait.Done()
			worker, task := isolationLeaf(t, space, leaf, titles[index],
				filepath.Join(records, leaf+".json"))
			outcome, err := worker.Run(context.Background(), task)
			results[index] = landing{outcome: outcome, err: err}
		}()
	}
	wait.Wait()

	for index, leaf := range leaves {
		if results[index].err != nil {
			t.Fatalf("leaf %s failed: %v\n%s", leaf, results[index].err,
				results[index].outcome.Text)
		}
		if results[index].outcome.Stop != StopDone {
			t.Fatalf("leaf %s stopped %q: %s", leaf, results[index].outcome.Stop,
				results[index].outcome.Text)
		}
	}

	// BOTH RAN SOMEWHERE OF THEIR OWN. A person's directory is never the run
	// directory, whichever leaf got there first.
	for _, leaf := range leaves {
		raw, err := os.ReadFile(filepath.Join(records, leaf+".json"))
		if err != nil {
			t.Fatalf("leaf %s never called the engine: %v", leaf, err)
		}
		var call struct {
			Argv []string `json:"argv"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			t.Fatal(err)
		}
		where := ""
		for index, arg := range call.Argv {
			if arg == "--dir" && index+1 < len(call.Argv) {
				where = call.Argv[index+1]
			}
		}
		if where == "" || where == root {
			t.Fatalf("leaf %s ran the engine in the person's own directory (%q)", leaf, where)
		}
	}

	// BOTH CHANGES ARE HOME, and the shared file is the version that stands on
	// its own rather than either leaf's half-edit.
	for _, leaf := range leaves {
		if _, err := os.Stat(filepath.Join(root, "fix-"+leaf+".txt")); err != nil {
			t.Fatalf("leaf %s did not land: %v", leaf, err)
		}
	}
	if content, err := os.ReadFile(filepath.Join(root, "shared.txt")); err != nil || string(content) != "ok\n" {
		t.Fatalf("shared.txt = %q, %v", string(content), err)
	}

	// THE ENGINE'S LITTER IS NOT IN THE PERSON'S TREE.
	for _, name := range []string{".codeaf", ".plandb.db", ".plandb"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Fatalf("the engine left %s in the person's directory", name)
		}
	}
	// Nor is the harness's own machinery: scratch was pointed elsewhere.
	for _, name := range []string{obsDir, ".aforge"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Fatalf("the harness left %s in the person's directory", name)
		}
	}

	// ONE COMMIT PER LEAF, WITH A REAL MESSAGE. The engine's wip(edit) history
	// stayed on the branch it was written on.
	history := gitOut(t, root, "log", "--format=%s", base+"..HEAD")
	subjects := strings.Split(history, "\n")
	if len(subjects) != 2 {
		t.Fatalf("the person's history grew by %d commits, want one per leaf:\n%s",
			len(subjects), gitOut(t, root, "log", "--format=%s%n%b", base+"..HEAD"))
	}
	if strings.Contains(history, "wip(edit)") {
		t.Fatalf("the engine's bookkeeping reached the person's history:\n%s", history)
	}
	for _, title := range titles {
		if !strings.Contains(history, title) {
			t.Fatalf("no commit is named for %q:\n%s", title, history)
		}
	}
	if body := gitOut(t, root, "log", "--format=%b", base+"..HEAD"); !strings.Contains(body, AttributionTrailer) {
		t.Fatalf("the landing commit carries no provenance:\n%s", body)
	}

	// NEITHER LEAF CLAIMS THE OTHER'S FILES. Both landed on the same shared
	// history, so a change set read as one wide range from either leaf's base to
	// the tip would carry the sibling's work — which is why the range is recorded
	// per pass, at the moment of landing, under the lease.
	for index, leaf := range leaves {
		account := results[index].outcome.Account
		if account == nil || !account.Landed() {
			t.Fatalf("leaf %s derived no change set from the repository: %#v", leaf, account)
		}
		for _, file := range account.Files {
			if strings.HasPrefix(file.Path, "fix-") && file.Path != "fix-"+leaf+".txt" {
				t.Fatalf("leaf %s claimed its sibling's file %s:\n%#v", leaf, file.Path, account.Files)
			}
		}
		if account.Patch == "" {
			t.Fatalf("leaf %s left no readable record of what it changed", leaf)
		}
	}

	// AND NOTHING IS LEFT OVER. A view that succeeded is gone, branch and all.
	if branches := gitOut(t, root, "branch", "--list", "aforge/leaf/*"); branches != "" {
		t.Fatalf("branches survived their leaves:\n%s", branches)
	}
	if trees := gitOut(t, root, "worktree", "list"); strings.Count(trees, "\n") != 0 {
		t.Fatalf("views survived their leaves:\n%s", trees)
	}
}

// Attribution is the operator's row and this worker reads it like every other.
// Off is the absence of the trailer rather than a line saying nobody helped.
func TestTheLandingCommitObeysTheAttributionSetting(t *testing.T) {
	signed := sweLandingMessage(Task{Title: "fix the parser"}, "It parses trailing commas now.", true)
	if !strings.HasPrefix(signed, "fix the parser\n\nIt parses trailing commas now.") {
		t.Fatalf("the message is not the ask over what was done:\n%s", signed)
	}
	if !strings.HasSuffix(signed, AttributionTrailer) {
		t.Fatalf("the trailer is missing:\n%s", signed)
	}
	quiet := sweLandingMessage(Task{Title: "fix the parser"}, "It parses trailing commas now.", false)
	if strings.Contains(quiet, "Co-Authored-By") || strings.Contains(quiet, "aforge") {
		t.Fatalf("attribution is off and the message still signs:\n%s", quiet)
	}
}

// NO OVERHEAD WHERE ISOLATION BUYS NOTHING.
//
// One coding leaf in a directory the harness made for it has no sibling to be
// isolated from and nobody's files to keep out of. It works in the workspace
// itself, exactly as it always has — this is the max-parallelism law read from
// the other end: a view exists to remove a wait, and there is no wait here.
func TestASoleCodingLeafInTheHarnessOwnWorkspaceStillWorksInPlace(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	probe := newSWEProbe(t, "pass")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %q", outcome.Stop)
	}
	argv, _ := probe.engineCall(t)
	where := ""
	for index, arg := range argv {
		if arg == "--dir" && index+1 < len(argv) {
			where = argv[index+1]
		}
	}
	if where != probe.workspace.Root() {
		t.Fatalf("a sole leaf was given a view it did not need: --dir %q, workspace %q",
			where, probe.workspace.Root())
	}
	if !strings.Contains(probe.trace(t), "run in place") {
		t.Fatalf("the trace does not say where the run happened:\n%s", probe.trace(t))
	}
}

// A CONFLICT IS REPORTED, NEVER RESOLVED.
//
// Two views of one repository can produce changes that do not compose, and the
// second one home finds a tree that has moved under its base. Inventing a
// resolution there is a coding worker deciding what somebody's repository should
// say; the mechanical answer is to apply nothing, leave the work whole on its
// branch, and say so in the leaf's own words.
func TestAViewThatCannotBeAppliedRefusesAndSaysWhereTheWorkIs(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	root := personsRepository(t)
	base := gitOut(t, root, "rev-parse", "HEAD")
	space, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(t.TempDir())

	view, _, err := sweOpen(context.Background(), space, "n9", newTracer(space, "n9"))
	if err != nil {
		t.Fatal(err)
	}
	if !view.isolated {
		t.Fatal("a person's own directory was made the run directory")
	}
	// The leaf's own change to the shared file.
	if err := os.WriteFile(filepath.Join(view.dir, "shared.txt"), []byte("the leaf's version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeWIPCommit(view.dir, "wip(edit): the leaf")
	// And an incompatible one landing in the shared tree first.
	if err := os.WriteFile(filepath.Join(root, "shared.txt"), []byte("somebody else's version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeWIPCommit(root, "the other change")
	moved := gitOut(t, root, "rev-parse", "HEAD")

	landing := view.land(context.Background(), true, "the leaf's change")
	if landing.refusal == "" {
		t.Fatal("a conflicting change was applied without a word")
	}
	if !strings.Contains(landing.refusal, view.branch) {
		t.Fatalf("the refusal does not say where the work is:\n%s", landing.refusal)
	}
	if landing.before.top != "" {
		t.Fatal("a refusal reported a landing")
	}
	if head := gitOut(t, root, "rev-parse", "HEAD"); head != moved {
		t.Fatalf("the shared workspace was written to anyway: %s → %s", moved, head)
	}
	if content, err := os.ReadFile(filepath.Join(root, "shared.txt")); err != nil ||
		string(content) != "somebody else's version\n" {
		t.Fatalf("the shared file was rewritten: %q, %v", string(content), err)
	}
	if strings.Contains(gitOut(t, root, "diff"), "<<<<<<<") {
		t.Fatal("the refusal left conflict markers in the shared tree")
	}
	// The work is whole and reachable.
	if branches := gitOut(t, root, "branch", "--list", view.branch); branches == "" {
		t.Fatal("the branch the refusal points at does not exist")
	}
	if kept := gitOut(t, root, "show", view.branch+":shared.txt"); kept != "the leaf's version" {
		t.Fatalf("the leaf's work is not on its branch: %q", kept)
	}
	_ = base
}

// A restarted leaf finds its own view where it left it, checkpoint and all. It
// is the whole of what makes the engine's resume worth having across a restart:
// a fresh empty checkout on the second attempt is a run that starts over.
func TestARestartedLeafKeepsItsOwnView(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	root := personsRepository(t)
	space, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(t.TempDir())

	first, _, err := sweOpen(context.Background(), space, "n3", newTracer(space, "n3"))
	if err != nil {
		t.Fatal(err)
	}
	if !first.isolated {
		t.Fatal("a person's own directory was made the run directory")
	}
	checkpoint := filepath.Join(first.dir, ".codeaf", "resume-checkpoint.json")
	if err := os.MkdirAll(filepath.Dir(checkpoint), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkpoint, []byte(`{"goal":"g","finalStatus":"fail"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	first.release()

	second, _, err := sweOpen(context.Background(), space, "n3", newTracer(space, "n3"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.release()
	if second.dir != first.dir {
		t.Fatalf("the restart was given a different view: %s, was %s", second.dir, first.dir)
	}
	if !resumableCheckpoint(second.dir) {
		t.Fatal("the restart lost the checkpoint it would have resumed from")
	}
}

// A dependency's files are recorded relative to the shared workspace, and a leaf
// reading them from a checkout of its own would find nothing at those names. In
// place the pointer is left exactly as it always was, byte for byte.
func TestAnIsolatedLeafIsToldWhereUpstreamFilesActuallyAre(t *testing.T) {
	inPlace := sweGoal(Task{
		Brief:  "use it",
		Inputs: []Input{{Title: "task-1", Result: "the survey", Artifacts: []string{"01-survey.md"}}},
	}, &sweView{root: "/w", dir: "/w"})
	if !strings.Contains(inPlace, "(files: 01-survey.md") {
		t.Fatalf("the in-place prompt changed:\n%s", inPlace)
	}
	isolated := sweGoal(Task{
		Brief:  "use it",
		Inputs: []Input{{Title: "task-1", Result: "the survey", Artifacts: []string{"01-survey.md"}}},
	}, &sweView{root: "/w", dir: "/views/n1", isolated: true})
	if !strings.Contains(isolated, "(files: "+filepath.Join("/w", "01-survey.md")) {
		t.Fatalf("an isolated leaf was pointed at a file it cannot open:\n%s", isolated)
	}
}
