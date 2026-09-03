package session

// THE FOLDER FAMILY TREE, AS TESTS (issue #230).
//
// The claim under test is one sentence: a family whose ground is a plain folder
// isolates and lands exactly like a family whose ground is a repository. So
// these drive the real doors — the same [prepareTaskTreeOn] a running node
// calls, the same stand a part is grounded on, the same [taskTree.comeHome] a
// part lands through — and assert the four things the issue asks for: a mirror
// that is a repository, a part that works somewhere its parent is not, parts
// that cannot see each other's work, and both parts' work sitting in the mirror
// before the family lands.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// familyOnAFolder is the shape every test here starts from: a person's folder
// with something in it, and the mirrored parent standing on it.
func familyOnAFolder(t *testing.T) (folder string, place Place, parent taskTree) {
	t.Helper()
	folder = t.TempDir()
	writeFile(t, filepath.Join(folder, "notes.md"), "what the parent gathered\n")
	place = Place{Dir: t.TempDir(), Workspace: folder}
	parent, err := prepareTaskTreeOn(context.Background(), place, folder, "aaaa1111bbbb2222", 7,
		"write the report", taskStand{dir: folder, mode: TaskModeMirror})
	if err != nil {
		t.Fatalf("prepareTaskTreeOn for the parent: %v", err)
	}
	return folder, place, parent
}

// partOf grounds one part the way a running family grounds one: on the stand its
// parent's worker resolves from the directory it is standing in.
func partOf(t *testing.T, place Place, parent taskTree, id uint64, title string) taskTree {
	t.Helper()
	stand := (&Agent{config: Config{Workspace: parent.dir}}).resolveTaskGround(
		taskSpec{parent: 7, title: title, brief: "write " + title + ".md", deliverable: "d", acceptance: "a"})
	if stand.mode != TaskModeWorktree || canonicalPath(stand.dir) != canonicalPath(parent.dir) {
		t.Fatalf("a part of a folder family stands %q as %q, want a worktree of the family tree %q",
			stand.dir, stand.mode, parent.dir)
	}
	tree, err := prepareTaskTreeOn(context.Background(), place, parent.dir, "aaaa1111bbbb2222", id, title, stand)
	if err != nil {
		t.Fatalf("prepareTaskTreeOn for %s: %v", title, err)
	}
	return tree
}

// THE MIRROR IS THE FAMILY TREE. It holds the folder as it stood, in a
// repository of its own, with a commit a part can be cut from — and none of that
// reaches the person's folder.
func TestAFolderFamilyOpensItsMirrorAsTheFamilyTree(t *testing.T) {
	folder, _, parent := familyOnAFolder(t)

	if parent.dir == folder {
		t.Fatalf("the family tree is the person's own folder %q", folder)
	}
	if !familyTreeIsOpen(parent.dir) {
		t.Fatalf("the mirror at %q is not a repository its parts could branch from", parent.dir)
	}
	if parent.note != "" {
		t.Fatalf("a family tree that opened cleanly says %q, want nothing", parent.note)
	}
	if tracked := gitOut(t, parent.dir, "ls-files"); !strings.Contains(tracked, "notes.md") {
		t.Fatalf("the baseline holds %q, want the folder as it stood", tracked)
	}
	if log := gitOut(t, parent.dir, "log", "--oneline"); !strings.Contains(log, familyTreeCommitMessage) {
		t.Fatalf("the family tree's history is %q, want its baseline", log)
	}
	// NOTHING OF OURS LIVES IN THE PERSON'S FOLDER. The mirror is harness-owned
	// space; the folder it was copied from is left exactly as it was found.
	if _, err := os.Stat(filepath.Join(folder, ".git")); err == nil {
		t.Fatalf("the person's folder %q was made into a repository", folder)
	}
}

// THE ISSUE'S ACCEPTANCE, END TO END: two parts, two directories, neither
// standing where its parent stands, neither able to see the other's work, and
// both landed in the family tree before the family itself lands.
func TestPartsOfAFolderFamilyWorkApartAndLandInTheFamilyTree(t *testing.T) {
	_, place, parent := familyOnAFolder(t)

	first := partOf(t, place, parent, 8, "the first section")
	second := partOf(t, place, parent, 9, "the second section")

	if first.dir == parent.dir || second.dir == parent.dir {
		t.Fatalf("a part works in its parent's own directory %q", parent.dir)
	}
	if first.dir == second.dir {
		t.Fatalf("both parts work in %q: the siblings are on top of each other", first.dir)
	}
	if canonicalPath(first.root) != canonicalPath(parent.dir) {
		t.Fatalf("the first part's branch merges into %q, want the family tree %q", first.root, parent.dir)
	}
	if first.branch == "" || second.branch == "" {
		t.Fatal("a part of a folder family has no branch to come home on")
	}

	writeFile(t, filepath.Join(first.dir, "one.md"), "the first section\n")
	writeFile(t, filepath.Join(second.dir, "two.md"), "the second section\n")
	// ISOLATION IS THE WHOLE POINT: what one part wrote is not on the other's
	// disk, and neither is on the parent's.
	for _, absent := range []string{
		filepath.Join(first.dir, "two.md"),
		filepath.Join(second.dir, "one.md"),
		filepath.Join(parent.dir, "one.md"),
	} {
		if _, err := os.Stat(absent); err == nil {
			t.Fatalf("%q is on a disk it was never written to: the parts are sharing a tree", absent)
		}
	}
	// And each part woke up holding the family's material.
	if got := readFile(t, filepath.Join(first.dir, "notes.md")); !strings.Contains(got, "what the parent gathered") {
		t.Fatalf("the first part's notes.md is %q; the family's material did not come with it", got)
	}

	for _, part := range []struct {
		tree taskTree
		file string
	}{{first, "one.md"}, {second, "two.md"}} {
		merge, detail, _ := part.tree.comeHome("the section", []string{part.file})
		if merge != mergeMerged {
			t.Fatalf("%s came home as %q (%s), want it merged into the family tree", part.file, merge, detail)
		}
	}

	// BOTH PARTS' WORK IS IN THE FAMILY TREE BEFORE THE FAMILY LANDS, which is
	// what makes the parent's own landing able to ship the whole family product.
	for _, file := range []string{"one.md", "two.md"} {
		if _, err := os.Stat(filepath.Join(parent.dir, file)); err != nil {
			t.Fatalf("%s never reached the family tree: %v", file, err)
		}
	}
	if log := gitOut(t, parent.dir, "log", "--oneline"); !strings.Contains(log, "the section") {
		t.Fatalf("the family tree's history is %q, want the parts' work in it", log)
	}
}

// THE BOUNDARY THE ISSUE DRAWS. A person who said "here" gets "here": an in
// place or a folder family works in the person's own directory, its parts share
// it, and nothing turns that directory into a repository behind their back.
func TestAFamilyToldToWorkHereIsLeftWhereItWasPut(t *testing.T) {
	for _, mode := range []TaskMode{TaskModeInPlace, TaskModeFolder} {
		folder := t.TempDir()
		writeFile(t, filepath.Join(folder, "notes.md"), "the person's own notes\n")
		place := Place{Dir: t.TempDir(), Workspace: folder}

		tree, err := prepareTaskTreeOn(context.Background(), place, folder, "cccc3333dddd4444", 3,
			"work here", taskStand{dir: folder, mode: mode})
		if err != nil {
			t.Fatalf("prepareTaskTreeOn (%s): %v", mode, err)
		}
		if canonicalPath(tree.dir) != canonicalPath(folder) {
			t.Fatalf("a %s task works in %q, want the folder it was told to work in", mode, tree.dir)
		}
		if _, err := os.Stat(filepath.Join(folder, ".git")); err == nil {
			t.Fatalf("a %s task made the person's folder into a repository", mode)
		}
	}
}

// AND THE DEGRADATION IS LOUD. A family tree that could not be opened is still a
// family tree — the work runs, the parts share the directory — but the parent is
// told so in a sentence it can act on, rather than finding out by having its
// siblings write over each other.
func TestAFamilyTreeThatCannotBeOpenedSaysSo(t *testing.T) {
	// A directory that is not there is every reason git init fails, spelled in
	// the one way a test can rely on: no git on the machine, a read-only disk, a
	// folder somebody moved.
	tree := openFamilyTree(taskTree{
		dir:   filepath.Join(t.TempDir(), "gone"),
		mode:  TaskModeMirror,
		merge: mergeInPlace,
	})
	if tree.note == "" {
		t.Fatal("a family that lost its tree says nothing about it")
	}
	if !strings.Contains(tree.note, "in this same folder beside you") {
		t.Fatalf("the sentence is %q; it does not say where the parts will be working", tree.note)
	}
	if !strings.Contains(tree.note, "write different files") {
		t.Fatalf("the sentence is %q; it does not say what to do about it", tree.note)
	}
}

// ── THE RECIPE AS A SCRIPTED NEST ───────────────────────────────────────────

// THE ISSUE'S RECIPE, DRIVEN THROUGH THE REAL DOORS. A mirror-mode parent on a
// temp folder calls `divide_work`; the two parts it names are admitted by the
// real division, started concurrently by the frontier, and run through the whole
// of [Agent.workTaskNode] — the tree preparation, the worker, the ledger, the
// landing. Nothing about the family tree is arranged by hand here, which is the
// point: the hand-driven tests above pin the seam, and this one pins that the
// seam is on the road a running family actually takes.
//
// IT IS THE OTHER ADMISSION DOOR AS WELL. A part admitted by `divide_work`
// carries no ground of its own at all (task_divide.go names no stand), so it
// reaches [prepareTaskTreeAt]'s default road and finds its worktree by asking
// the directory it is standing in — which is the mirror. The hand-driven test
// above covers `propose_task`'s door, which resolves a stand first. Both roads
// have to end in a worktree of the family tree, and neither is the other's test.
func TestAFolderFamilyDividesIntoPartsThatLandInTheFamilyTree(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "notes.md"), "what the family gathered\n")
	t.Setenv("HOME", t.TempDir())

	completer := &folderPartCompleter{}
	session, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = folder
		config.Place = Place{Dir: t.TempDir(), Workspace: folder}
		config.Divide = true
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		// NO CHECKER AND NO REPAIR ROUND. What is under test is where the parts
		// work and how their work comes home; the gate in front of that has its
		// own tests (task_divide_test.go).
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	graph := session.graph()
	// THE PARENT IS THIS TEST'S OWN and stays where it is — it is the dividing
	// worker. Its PARTS run for real.
	graph.run = func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		graph.runOwned(node)
	}

	id := graph.reserve()
	graph.admit(id, taskSpec{title: "the whole report", request: personSentence,
		brief: "write the report", acceptance: "the report is written", depth: 1,
		ground: folder, mode: TaskModeMirror})
	parent := graph.node(id)

	// The two lines the runner runs for a node it is about to start.
	mirror, err := prepareTaskTreeOn(context.Background(), session.config.Place, folder,
		session.journalID(), parent.id, parent.title(), parent.stand())
	if err != nil {
		t.Fatalf("prepareTaskTreeOn for the parent: %v", err)
	}
	parent.setTree(mirror)
	if mirror.dir == folder || !familyTreeIsOpen(mirror.dir) {
		t.Fatalf("the family tree is %q, want a repository of its own beside the person's folder", mirror.dir)
	}

	worker, err := newAgent(Config{
		Workspace: mirror.dir, Model: "test/model", System: "SYSTEM",
		InTask: true, Divide: true, TaskRepairRounds: 0,
		tasker: graph, taskID: id, taskDepth: 1,
	}, completer)
	if err != nil {
		t.Fatalf("newAgent for the worker: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	parent.openRoom().speaking(worker)

	answer, _, err := worker.divideWork(context.Background(), json.RawMessage(fmt.Sprintf(
		`{"evidence":%q,"parts":[`+
			`{"title":"alpha","summary":"s","brief":"write alpha.md","acceptance":"alpha.md is there"},`+
			`{"title":"beta","summary":"s","brief":"write beta.md","acceptance":"beta.md is there"}]}`,
		wideEvidence)))
	if err != nil {
		t.Fatalf("divide_work: %v", err)
	}
	if !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q", answer)
	}

	parts := partsByTitle(t, graph, parent)
	alpha, beta := parts["alpha"], parts["beta"]
	waitDoneNode(t, alpha)
	waitDoneNode(t, beta)

	// EACH PART WORKED SOMEWHERE ITS PARENT IS NOT, AND SOMEWHERE ITS SIBLING IS
	// NOT. This is the whole defect: before the mirror was a repository both of
	// these directories were the parent's own.
	first, second := alpha.notice().Where, beta.notice().Where
	if first == "" || second == "" {
		t.Fatalf("a part has no working copy to name: alpha %q, beta %q", first, second)
	}
	if first == mirror.dir || second == mirror.dir {
		t.Fatalf("a part worked in the family tree itself (%q)", mirror.dir)
	}
	if first == second {
		t.Fatalf("both parts worked in %q: the siblings are on top of each other", first)
	}

	// AND BOTH CAME HOME INTO THE FAMILY TREE, through the branch road every
	// repository part comes home on.
	for _, part := range []*TaskNode{alpha, beta} {
		got := part.notice()
		if got.State != TaskDone || got.Merge != mergeMerged {
			t.Fatalf("the part %s landed %s / %s: %q", got.Title, got.State, got.Merge, got.Report)
		}
		if got.Branch == "" {
			t.Fatalf("the part %s came home without a branch", got.Title)
		}
	}
	for _, file := range []string{"alpha.md", "beta.md"} {
		if _, err := os.Stat(filepath.Join(mirror.dir, file)); err != nil {
			t.Fatalf("%s never reached the family tree: %v", file, err)
		}
	}
	if log := gitOut(t, mirror.dir, "log", "--oneline"); !strings.Contains(log, "task: alpha") || !strings.Contains(log, "task: beta") {
		t.Fatalf("the family tree's history is %q, want both parts' work in it before the family lands", log)
	}

	// AND THE PERSON'S FOLDER IS UNTOUCHED UNTIL THE FAMILY LANDS. The parent
	// has not landed — the whole family's product is laid by name when it does
	// — so nothing of the parts is out there yet, and the folder is still a
	// folder.
	for _, file := range []string{"alpha.md", "beta.md"} {
		if _, err := os.Stat(filepath.Join(folder, file)); !os.IsNotExist(err) {
			t.Fatalf("%s reached the person's folder before the family landed", file)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, ".git")); err == nil {
		t.Fatalf("the person's folder %q was made into a repository", folder)
	}
}

// folderPartCompleter answers every lane one folder family touches, dispatching
// on WHAT IT WAS ASKED rather than on how many calls came before it: two parts
// run at once, and a positional script over concurrent lanes is a test asserting
// about whichever goroutine got there first.
type folderPartCompleter struct{}

func (c *folderPartCompleter) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	system, whole := "", strings.Builder{}
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	tooled := false
	for _, message := range messages {
		whole.WriteString(messageText(message) + "\n")
		if message.Role == "tool" {
			tooled = true
		}
	}
	text := whole.String()
	if system == titleSystem {
		return textResponse("the report"), nil
	}
	// IT READS THE SCOPE AND NOT THE WHOLE PROMPT. Every part is composed with a
	// map of what its SIBLINGS own above its own scope (task_divide_compose.go),
	// so a fixture that dispatched on the whole document would answer for
	// whichever part the map happened to name first — and beta's worker would
	// write alpha's file. What is dispatched on is the section under
	// [divisionThisPart], which is this part's own and nobody else's.
	if _, own, found := strings.Cut(text, divisionThisPart); found {
		text = own
	}
	for _, part := range []string{"alpha", "beta"} {
		if !strings.Contains(text, "write "+part+".md") {
			continue
		}
		if tooled {
			return textResponse("Wrote " + part + ".md."), nil
		}
		return writeResponse("call-"+part, part+".md", "the "+part+" section\n"), nil
	}
	// Everything else — the division review among it — gets nothing it can read,
	// which is the fail-open path and the division exactly as the worker wrote it.
	return textResponse("(unscripted)"), nil
}

// ── THE RESUME ROAD ─────────────────────────────────────────────────────────

// A RESTART REVALIDATES THE FAMILY TREE RATHER THAN ASSUMING IT. A process that
// died before the mirror could be opened would otherwise come back with a tree
// that is not one and nothing said about it, and the parts admitted after the
// restart would share the parent's directory unwarned — the silent degradation
// this seam exists to end, reintroduced by the one road that does not prepare a
// tree.
func TestAResumedFolderFamilyRevalidatesItsTree(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, "notes.md"), "what the family gathered\n")
	mirror := t.TempDir()
	writeFile(t, filepath.Join(mirror, "notes.md"), "what the family gathered\n")

	graph := newTaskGraph()
	// Nothing runs: this is the road a node takes BEFORE it is started again.
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "the whole report", brief: "b", acceptance: "a", depth: 1,
		ground: folder, mode: TaskModeMirror})
	node := graph.node(id)
	graph.mu.Lock()
	node.worktree, node.merge, node.interrupted = mirror, mergeInPlace, true
	graph.mu.Unlock()

	// A MIRROR THE DEAD RUN NEVER OPENED IS OPENED NOW: a restart is a second
	// chance at a disk that may since have become writable.
	tree, resumed := node.resumeTree(Place{}, folder)
	if !resumed {
		t.Fatal("the interrupted node did not resume")
	}
	if !familyTreeIsOpen(tree.dir) {
		t.Fatalf("the resumed mirror at %q is still not a repository its parts could branch from", tree.dir)
	}
	if tree.note != "" {
		t.Fatalf("a family tree that opened on the resume says %q, want nothing", tree.note)
	}

	// AND OPENING IT AGAIN LAYS NO SECOND BASELINE over the work already in it.
	before := gitOut(t, tree.dir, "rev-parse", "HEAD")
	again, _ := node.resumeTree(Place{}, folder)
	if got := gitOut(t, again.dir, "rev-parse", "HEAD"); got != before {
		t.Fatalf("a second resume moved the family tree's history from %q to %q", before, got)
	}
}

// AND A RESUME THAT STILL CANNOT OPEN ONE SAYS SO, exactly as the first
// preparation would have. The sentence is recomputed rather than remembered, so
// there is one account of the fact and it is always about this machine now —
// and the parts admitted after the restart are as warned as the ones admitted
// before it.
func TestAResumedFamilyWithNoTreeStillSaysSo(t *testing.T) {
	folder := t.TempDir()
	mirror := t.TempDir()
	// A directory git will not open, standing in for every real reason it would
	// not: no git on the machine, a read-only disk, a repository somebody broke.
	// It is the one such reason a test can make on demand.
	writeFile(t, filepath.Join(mirror, ".git"), "this is not a repository\n")

	graph := newTaskGraph()
	// Nothing runs: this is the road a node takes BEFORE it is started again.
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "the whole report", brief: "b", acceptance: "a", depth: 1,
		ground: folder, mode: TaskModeMirror})
	node := graph.node(id)
	graph.mu.Lock()
	node.worktree, node.merge, node.interrupted = mirror, mergeInPlace, true
	graph.mu.Unlock()

	tree, resumed := node.resumeTree(Place{}, folder)
	if !resumed {
		t.Fatal("the interrupted node did not resume")
	}
	if tree.note == "" {
		t.Fatal("a resumed family that still has no tree says nothing about it")
	}
	if !strings.Contains(tree.note, "in this same folder beside you") {
		t.Fatalf("the sentence is %q; it does not say where the parts will be working", tree.note)
	}
}
