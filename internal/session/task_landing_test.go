package session

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// task_landing_test.go is written from two measured defects, both of them a
// task that SETTLED AS DONE over something nobody could use.
//
// The first: a node ran its test suite, the suite needed a virtualenv, and the
// landing swept the virtualenv onto the person's branch — twenty-six hunks of
// vendored noise and not one line of the change it was briefed for. A second
// run committed three thousand files of a `.venv_test` the same way. Both were
// `git add -A` calling a directory a deliverable.
//
// The second: a node's branch would not merge, and the node landed anyway. The
// patch was an unresolved merge and the report said as much, while the card
// read finished.
//
// Everything below is one repository in a temp directory and the real git
// underneath it, because both defects were about what git was actually asked to
// do rather than about anything a fake could have modelled.

// A LANDING BRINGS HOME WHAT THE WORKER WROTE. The two files its own hands made
// go onto the person's branch; the environment a command built in the same
// directory does not, however much of the directory it fills.
func TestALandingBringsHomeOnlyWhatTheWorkerWrote(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "aaaa1111aaaa1111", 1, "add the parser")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	// What the worker's own write and edit calls made.
	writeFile(t, filepath.Join(tree.dir, "parser.py"), "def parse():\n    return 1\n")
	writeFile(t, filepath.Join(tree.dir, "parser_test.py"), "def test_parse():\n    assert True\n")
	// And what a command made while the tests ran, which nobody wrote.
	for _, name := range []string{"lib/site.py", "bin/activate", "pyvenv.cfg"} {
		writeFile(t, filepath.Join(tree.dir, ".venv", filepath.FromSlash(name)), "vendored\n")
	}
	writeFile(t, filepath.Join(tree.dir, ".pytest_cache", "CACHEDIR.TAG"), "cache\n")

	merge, detail := tree.comeHome("add the parser", []string{"parser.py", "parser_test.py"})
	if merge != mergeMerged {
		t.Fatalf("merge = %q (%s), want it to come home", merge, detail)
	}
	for _, want := range []string{"parser.py", "parser_test.py"} {
		if _, err := os.Stat(filepath.Join(repo, want)); err != nil {
			t.Fatalf("%s did not land on the person's branch: %v", want, err)
		}
	}
	// THE WHOLE POINT: the person's branch is the change and nothing else.
	landed := gitOut(t, repo, "ls-tree", "-r", "--name-only", "HEAD")
	for _, unwanted := range []string{".venv", ".pytest_cache"} {
		if strings.Contains(landed, unwanted) {
			t.Fatalf("%s was landed on the person's branch:\n%s", unwanted, landed)
		}
	}
	// AND THE PERSON IS TOLD, rather than left to find a directory of somebody
	// else's leavings in a worktree they never opened.
	if !strings.Contains(detail, "went with its working copy") || !strings.Contains(detail, ".venv") {
		t.Fatalf("the landing said nothing about what it left: %q", detail)
	}
}

// A PATH THE REPOSITORY IGNORES IS STILL IGNORED, and the one path git refuses
// does not cost the node everything else it wrote. This is the batch falling
// back to one add per path, stated as the behaviour it buys.
func TestAnIgnoredPathTheWorkerWroteDoesNotCostItTheRest(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), "*.log\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "ignore logs")

	tree, err := prepareTaskTree(Place{}, repo, "bbbb2222bbbb2222", 1, "write the report")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "report.md"), "# what happened\n")
	writeFile(t, filepath.Join(tree.dir, "run.log"), "noise\n")

	saved := commitTaskWork(tree.dir, "write the report", []string{"run.log", "report.md"})
	if !containsString(saved, "report.md") {
		t.Fatalf("committed %v, want the report on it", saved)
	}
	if containsString(saved, "run.log") {
		t.Fatalf("committed %v, want the ignored path left where it is", saved)
	}
}

// UNTRACKED LEAVINGS STAY IN THE WORKTREE. A node whose branch is kept rather
// than merged leaves its mess exactly where it fell — the branch holds the work
// and nothing else, and the files are still on disk for anybody who wants them.
func TestWhatTheNodeDidNotWriteStaysInItsWorktree(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "cccc3333cccc3333", 1, "build it")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "main.go"), "package main\n")
	writeFile(t, filepath.Join(tree.dir, "build", "binary"), "elf\n")

	merge, changed := keptWork(tree, "build it", []string{"main.go"})
	if merge != mergeAborted {
		t.Fatalf("merge = %q, want the branch kept", merge)
	}
	if !containsString(changed, "main.go") || containsString(changed, "build/binary") {
		t.Fatalf("changed = %v, want the file it wrote and not the one it built", changed)
	}
	listed := gitOut(t, repo, "ls-tree", "-r", "--name-only", tree.branch)
	if !strings.Contains(listed, "main.go") {
		t.Fatalf("branch %s holds:\n%s\nwant main.go on it", tree.branch, listed)
	}
	if strings.Contains(listed, "build/binary") {
		t.Fatalf("branch %s carries what a command built:\n%s", tree.branch, listed)
	}
	// STILL THERE. Not landed is not deleted.
	if _, err := os.Stat(filepath.Join(tree.dir, "build", "binary")); err != nil {
		t.Fatalf("the leavings were destroyed rather than left: %v", err)
	}
	if left := leftBehind(tree.dir); !containsString(left, "build/binary") {
		t.Fatalf("leftBehind = %v, want it to name what is still sitting there", left)
	}
}

// A NODE THAT RESUMES STILL OWNS WHAT ITS FIRST ATTEMPT WROTE. Staging by name
// means the list has to outlive the process that made it, so it rides in the
// checkpoint and the second attempt starts holding it.
func TestAResumedNodeStillOwnsWhatItWroteBeforeTheProcessDied(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "write the parser"}, state: TaskRunning}
	graph.mu.Lock()
	graph.nodes[node.id] = node
	graph.order = append(graph.order, node.id)
	graph.mu.Unlock()
	node.noteWrote("parser.go")
	node.noteWrote("parser_test.go")

	// The process dies here, and the checkpoint is all that is left of it.
	record := func() taskRecord {
		graph.mu.Lock()
		defer graph.mu.Unlock()
		return node.recordLocked()
	}()
	if len(record.Wrote) != 2 {
		t.Fatalf("the checkpoint carries %v, want both files the node wrote", record.Wrote)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var back taskRecord
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}

	resumed := &TaskNode{graph: graph, id: 1, wrote: back.Wrote, changed: back.Changed}
	held := resumed.rememberedWrites()
	for _, want := range []string{"parser.go", "parser_test.go"} {
		if !containsString(held, want) {
			t.Fatalf("the resumed node holds %v, want it to still own %s", held, want)
		}
	}
}

// A MERGE THAT CONFLICTS LEAVES THE PERSON'S CHECKOUT CLEAN. No markers, no
// half-finished merge, and the branch kept with the conflicting file named in
// the words a person reads.
func TestAConflictedMergeLeavesHomeCleanAndNamesTheFile(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "dddd4444dddd4444", 1, "edit the shared file")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "the node's line\n")
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's line\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "person")

	merge, detail := tree.comeHome("edit the shared file", []string{"shared.txt"})
	if merge != mergeConflicted {
		t.Fatalf("merge = %q (%s), want conflicted", merge, detail)
	}
	if !strings.Contains(detail, "shared.txt") || !strings.Contains(detail, "both sides") {
		t.Fatalf("the detail does not name what clashed: %q", detail)
	}
	// NO MARKERS ON THE PERSON'S BRANCH, EVER.
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != "the person's line\n" {
		t.Fatalf("the person's file reads %q — a landing wrote over it", got)
	}
	if _, err := git(repo, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err == nil {
		t.Fatal("the person's checkout was left mid-merge")
	}
	// And the work is where the report says it is.
	if branches := gitOut(t, repo, "branch", "--list", tree.branch); !strings.Contains(branches, tree.branch) {
		t.Fatal("the conflicted branch was deleted: the node's work is gone")
	}
	if listed := gitOut(t, repo, "ls-tree", "-r", "--name-only", tree.branch); !strings.Contains(listed, "shared.txt") {
		t.Fatalf("branch %s does not hold the node's work:\n%s", tree.branch, listed)
	}
}

// AND A CONFLICTED MERGE IS NOT "DONE". It needs a look, in the same words
// every other undecided landing uses, with the file named and the branch kept.
func TestAConflictedMergeNeedsYourLookRatherThanDone(t *testing.T) {
	repo := newTestRepo(t)
	// The landing touches nothing on the agent but the log it writes to, so a
	// bare one is the whole of what this needs.
	agent := &Agent{}
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "edit the shared file"}, state: TaskRunning}
	graph.mu.Lock()
	graph.nodes[node.id] = node
	graph.order = append(graph.order, node.id)
	graph.mu.Unlock()

	tree := taskTree{dir: filepath.Join(repo, "tree"), root: repo, branch: "task/edit-the-shared-file"}
	detail := conflictSentence(tree.branch, []string{"shared.txt"}, "")
	state := agent.landConflicted(node, tree, []string{"shared.txt"},
		"the parser now takes the shared line", detail, io.Discard)

	if state != TaskUnverified {
		t.Fatalf("a conflicted landing is %q, want it to need a look", state)
	}
	report, changed, branch, merge := node.leavings()
	if merge != mergeConflicted {
		t.Fatalf("merge = %q, want it to say the branch would not go", merge)
	}
	if branch != tree.branch || !containsString(changed, "shared.txt") {
		t.Fatalf("the landing lost the branch or the files: %q %v", branch, changed)
	}
	if !strings.HasPrefix(report, needsLookLead) {
		t.Fatalf("the report does not lead with the person's own words:\n%s", report)
	}
	if !strings.Contains(report, "shared.txt") {
		t.Fatalf("the report does not name what clashed:\n%s", report)
	}
	// THE WORK'S OWN ACCOUNT SURVIVES. Somebody has to decide this, and they
	// need both halves.
	if !strings.Contains(report, "the parser now takes the shared line") {
		t.Fatalf("the node's own words were dropped:\n%s", report)
	}
	// AND NOT ONE WORD OF MACHINERY. The card is read by a person.
	for _, banned := range []string{"conflicted", "verdict", "auditor", "unverified"} {
		if strings.Contains(strings.ToLower(report), banned) {
			t.Fatalf("the report says %q to a person:\n%s", banned, report)
		}
	}
}

// A DELIVERABLE A COMMAND GENERATED COMES HOME WHEN THE NODE NAMES IT. That is
// the one door out of "only what you wrote", and it is closed to a name with
// nothing behind it.
func TestAReportCanNameTheFilesACommandGenerated(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "site", "index.html"), "<h1>hi</h1>\n")
	writeFile(t, filepath.Join(dir, "site", "app.css"), "body{}\n")
	if err := os.MkdirAll(filepath.Join(dir, "site", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}

	said := "The scaffold is generated and the theme is applied.\n\n" +
		"files: site/index.html, `site/app.css`, site/missing.js, site/assets, /etc/passwd, ../escape.txt"
	declared := declaredFiles(said, dir)

	want := []string{"site/index.html", "site/app.css"}
	if len(declared) != len(want) {
		t.Fatalf("declared = %v, want exactly %v", declared, want)
	}
	for _, path := range want {
		if !containsString(declared, path) {
			t.Fatalf("declared = %v, want it to name %s", declared, path)
		}
	}
	// A report that never says the word declares nothing, which is every report
	// written before this existed.
	if got := declaredFiles("I wrote the files and they are good.", dir); got != nil {
		t.Fatalf("declared = %v off a report with no declaration", got)
	}
	// And the bullet-and-bold spelling a model reaches for is the same sentence.
	if got := declaredFiles("- **files:** site/index.html", dir); !containsString(got, "site/index.html") {
		t.Fatalf("declared = %v off a bulleted declaration", got)
	}
}

// ── WHERE A VERDICT IS REACHED ──────────────────────────────────────────────
//
// A PASSING CHECK MAY NOT DEPEND ON STATE THE DELIVERABLE DOES NOT CARRY, and
// the measured failure is what these pin. A node was asked to make a scorer pass;
// the scorer addressed files at a path the repository did not keep them at, and
// the node made the path exist with `ln -sf` instead of changing the source. It
// re-ran the scorer against its own symlink, watched it pass, and said the job
// was done — and the auditor, standing in the same directory with the same
// symlink under it, saw the same pass.
//
// So the audit is reached in a CLEAN RESTORE: the tree as it was, with exactly
// the files the node wrote laid over it, and nothing else the run left behind.

// THE RESTORE IS THE BRANCH PLUS WHAT THE NODE WROTE, and nothing the run made
// on the side is in it — not an environment a check leans on, not build output.
func TestTheAuditIsReachedInACleanRestoreOfWhatShips(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "src", "main.go"), "package main\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "the tree as it was")

	tree, err := prepareTaskTree(Place{}, repo, "dddd4444dddd4444", 1, "make the scorer pass")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	// What the node's own hands wrote — the deliverable.
	writeFile(t, filepath.Join(tree.dir, "src", "parser.go"), "package main // the parser\n")
	// And what it did NOT write: the fixture it dropped where the scorer looks,
	// the link it made so a path would resolve, and the output of a build.
	writeFile(t, filepath.Join(tree.dir, "test-files", "case.json"), "{}\n")
	if err := os.Symlink(filepath.Join(tree.dir, "src"), filepath.Join(tree.dir, "fixtures")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree.dir, "target", "debug", "binary"), "elf\n")

	node := loneTestNode(t, "make the scorer pass")
	ground := auditGroundFor(node, tree, []string{"src/parser.go"}, io.Discard)
	defer ground.drop()
	if !ground.restored {
		t.Fatalf("no restore was made: %s", ground.why)
	}
	if ground.dir == tree.dir {
		t.Fatal("the verdict would be reached in the node's own working copy")
	}
	// What ships is there: the tree it started from, and the change over it.
	for _, want := range []string{"src/main.go", "src/parser.go"} {
		if _, err := os.Stat(filepath.Join(ground.dir, filepath.FromSlash(want))); err != nil {
			t.Fatalf("%s is not in the restore: %v", want, err)
		}
	}
	// What does not ship is not.
	for _, unwanted := range []string{"test-files/case.json", "fixtures", "target/debug/binary"} {
		if _, err := os.Lstat(filepath.Join(ground.dir, filepath.FromSlash(unwanted))); err == nil {
			t.Fatalf("%s followed the work into the restore", unwanted)
		}
	}
	// AND THE CHANGE READS AS A CHANGE THERE. The sentence the auditor is given
	// says `git diff --cached` shows all of it, new files included, and that has
	// to be true of the tree it is actually standing in.
	staged := gitOut(t, ground.dir, "diff", "--cached", "--name-only")
	if !strings.Contains(staged, "src/parser.go") {
		t.Fatalf("the restore's index does not hold the change:\n%s", staged)
	}
	// And it takes itself away again.
	ground.drop()
	if _, err := os.Stat(ground.dir); err == nil {
		t.Fatal("the restore was left behind")
	}
}

// AND A WORKSPACE WITH NO REPOSITORY BEHIND IT IS RESTORED BY ITS OWN CLOCK —
// which is the shape the failure was actually measured in.
func TestARestoreOfANonRepositoryLeavesWhatTheRunMade(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "src", "main.rs"), "fn main() {}\n")
	writeFile(t, filepath.Join(workspace, "Cargo.toml"), "[package]\n")
	// The original tree predates the work; everything below is made after it.
	before := time.Now().Add(-2 * time.Hour)
	for _, path := range []string{"src/main.rs", "Cargo.toml", "src", "."} {
		if err := os.Chtimes(filepath.Join(workspace, filepath.FromSlash(path)), before, before); err != nil {
			t.Fatal(err)
		}
	}
	node := loneTestNode(t, "make the scorer pass")
	node.graph.mu.Lock()
	node.started = time.Now().Add(-time.Hour)
	node.graph.mu.Unlock()

	writeFile(t, filepath.Join(workspace, "src", "parser.rs"), "// the parser\n")
	writeFile(t, filepath.Join(workspace, "test-files", "case.json"), "{}\n")
	writeFile(t, filepath.Join(workspace, "target", "debug", "binary"), "elf\n")

	tree := taskTree{dir: workspace, merge: mergeInPlace}
	ground := auditGroundFor(node, tree, []string{"src/parser.rs"}, io.Discard)
	defer ground.drop()
	if !ground.restored {
		t.Fatalf("no restore was made: %s", ground.why)
	}
	for _, want := range []string{"src/main.rs", "Cargo.toml", "src/parser.rs"} {
		if _, err := os.Stat(filepath.Join(ground.dir, filepath.FromSlash(want))); err != nil {
			t.Fatalf("%s is not in the restore: %v", want, err)
		}
	}
	for _, unwanted := range []string{"test-files/case.json", "target/debug/binary"} {
		if _, err := os.Lstat(filepath.Join(ground.dir, filepath.FromSlash(unwanted))); err == nil {
			t.Fatalf("%s followed the work into the restore", unwanted)
		}
	}
}

// AND THE AUDIT FAILS ON IT, naming what is not there. This is the whole point
// of the restore: the auditor did not have to be told the rule, it simply could
// not find the thing the check was leaning on.
func TestACheckLeaningOnAFileTheNodeDidNotWriteFailsItsAudit(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "eeee5555eeee5555", 1, "make the scorer pass")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "parser.go"), "package main\n")
	// The fixture the node dropped where the scorer looks, and never wrote.
	writeFile(t, filepath.Join(tree.dir, "test-files", "case.json"), "the-scorers-fixture\n")

	completer := &routedCompleter{audit: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("look", "bash", `{"command":"cat test-files/case.json"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("REFUTED — test-files/case.json is not in the change, so the check passes only in the copy the work made"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = repo })
	node := loneTestNode(t, "make the scorer pass")
	node.graph.mu.Lock()
	node.spec.acceptance = "the scorer passes"
	node.graph.mu.Unlock()

	verdict := agent.auditNode(context.Background(), node, tree, []string{"parser.go"}, "the scorer passes now", io.Discard)
	if !verdict.answered || verdict.verified {
		t.Fatalf("the audit came back %s, want the finding", verdict.report())
	}
	if !strings.Contains(strings.Join(verdict.evidence, " "), "test-files/case.json") {
		t.Fatalf("the finding does not name the missing path: %v", verdict.evidence)
	}
	// THE LOAD-BEARING HALF: the auditor could not read the file, because it was
	// never standing where the file is. A test that only asserted the scripted
	// verdict would pass over an auditor judging the dirty working copy.
	completer.mu.Lock()
	defer completer.mu.Unlock()
	if len(completer.auditRequests) < 2 {
		t.Fatalf("the auditor made %d requests, so it never read a result", len(completer.auditRequests))
	}
	for _, request := range completer.auditRequests {
		for _, message := range request {
			if message.Role == "tool" && strings.Contains(messageText(message), "the-scorers-fixture") {
				t.Fatal("the auditor read a file the node never wrote, so it judged the working copy")
			}
		}
	}
	// And it was told where it stands, in words it can act on.
	asked := messageText(completer.auditRequests[0][len(completer.auditRequests[0])-1])
	if !strings.Contains(asked, "CLEAN RESTORE") {
		t.Fatalf("the auditor was not told it is in a restore:\n%s", asked)
	}
}

// loneTestNode is one node in a graph of its own, for the tests that drive a
// landing or an audit directly rather than through a whole run.
func loneTestNode(t *testing.T, title string) *TaskNode {
	t.Helper()
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: title}, state: TaskRunning}
	graph.mu.Lock()
	graph.nodes[node.id] = node
	graph.order = append(graph.order, node.id)
	graph.mu.Unlock()
	return node
}

// ── WHAT THE AUDITOR IS SHOWN OF WHAT ALREADY HAPPENED ──────────────────────

// THE AUDITOR READS EVIDENCE; IT DOES NOT NECESSARILY REDO IT. A check is often
// the most expensive thing in a task, and an auditor made to rediscover and
// re-run it from nothing inside [auditDeadline] is an auditor that times out —
// which is what was measured: a node landed unchecked because its scorer was
// re-run twice by a judge that then ran out of its five minutes.
func TestTheAuditorIsShownWhatTheWorkAlreadyRan(t *testing.T) {
	node := loneTestNode(t, "make the scorer pass")
	node.graph.mu.Lock()
	node.spec.acceptance = "the scorer passes"
	node.graph.mu.Unlock()
	node.keepReceipts([]toolReceipt{{
		tool:   "bash",
		args:   `{"command":"./score.sh"}`,
		result: "score: 41/60 — 19 cases failed",
	}})

	question := auditQuestion(node, taskTree{}, auditGround{dir: "/restore", restored: true}, []string{"parser.go"}, "it passes")
	if !strings.Contains(question, "WHAT THE WORK ALREADY RAN") {
		t.Fatalf("the auditor was shown nothing of what happened:\n%s", question)
	}
	for _, want := range []string{"./score.sh", "score: 41/60 — 19 cases failed"} {
		if !strings.Contains(question, want) {
			t.Errorf("the packet is missing %q:\n%s", want, question)
		}
	}
	// AND THE PROVENANCE IS STATED, because it is the whole safety of showing it
	// at all: in a restore those results came from ANOTHER tree — the one a
	// verdict may not rest on — so they may settle a refutation and never an
	// acceptance.
	if !strings.Contains(question, "not the copy you are standing in") {
		t.Errorf("the packet does not say where its evidence came from:\n%s", question)
	}
	// Standing in the work's own copy the warning is the opposite one.
	inPlace := auditQuestion(node, taskTree{}, auditGround{dir: "/work"}, []string{"parser.go"}, "it passes")
	if !strings.Contains(inPlace, "They came from this same copy.") {
		t.Errorf("the packet misstates its own provenance:\n%s", inPlace)
	}
}

// AND THEY ARE THE WORKER'S REAL RESULTS, READ OFF ITS TRANSCRIPT — the text the
// model actually read, not the capped display copy that carries a contract
// saying it is only ever for showing a person.
func TestTheReceiptsAreTheWorkersOwnToolResultsAndAreBounded(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "note.txt"), "PELICAN-42\n")

	var turn int
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			turn++
			return toolResponse("read-1", "read", `{"path":"note.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("read it"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = workspace })
	collect(t, mustSubmit(t, agent, "read the note"))

	receipts := lastToolReceipts(agent, auditReceiptCount)
	if len(receipts) == 0 {
		t.Fatal("nothing was read off the worker's transcript")
	}
	if len(receipts) > auditReceiptCount {
		t.Fatalf("%d receipts, want at most %d", len(receipts), auditReceiptCount)
	}
	last := receipts[len(receipts)-1]
	if last.tool != "read" {
		t.Errorf("the receipt names %q rather than the call that made it", last.tool)
	}
	if !strings.Contains(last.result, "PELICAN-42") {
		t.Errorf("the receipt does not carry what the tool actually answered: %q", last.result)
	}
	if !strings.Contains(last.args, "note.txt") {
		t.Errorf("the receipt does not say what was asked for: %q", last.args)
	}
	// AND EACH ONE IS BOUNDED, so a worker whose last command printed a megabyte
	// cannot spend the auditor's whole context before it has read the acceptance.
	for _, receipt := range receipts {
		if len(receipt.result) > auditReceiptLimit+64 {
			t.Fatalf("a receipt weighs %d bytes, over the bound of %d", len(receipt.result), auditReceiptLimit)
		}
	}
}
