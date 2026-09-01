package session

// THE FAMILY'S WORLD, AS TESTS (issue #232).
//
// Two claims, and every test here drives the real doors for them — the real
// `divide_work`, the real frontier starting the parts, the real
// [prepareTaskTreeOn] preparing their working copies, the real landing merging
// their branches home:
//
//  1. A parent that has written something and then divides puts that work ON THE
//     FAMILY BRANCH, so every part wakes up with it on disk and the family's own
//     history says so.
//  2. Every part starts from THAT ONE COMMIT, however far apart their working
//     copies were prepared and however much the parent wrote in between.
//
// What is scripted is only the provider. The parts are real workers on a real
// graph, and they land through the branch road a repository part has always
// landed on.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the rig ─────────────────────────────────────────────────────────────────

// wipFamily is a conversation, a parent standing on its own family tree, the
// worker that IS that parent, and a graph whose PARTS run for real.
type wipFamily struct {
	ground string
	tree   taskTree
	place  Place
	graph  *TaskGraph
	parent *TaskNode
	worker *Agent
	script *wipPartCompleter
	// journal is the worker's own session file, which is where a node writes
	// down what the division road decided (sessionfile.go's [journalDivision]).
	journal string
}

// newWipFamily builds that shape on either ground there is: a repository the
// person owns, or a plain folder whose mirror #230 opens as the family tree.
//
// gate runs on every part's goroutine before it starts, which is the one hook a
// test needs to hold one sibling back while the parent keeps writing — the
// window this whole seam is about.
func newWipFamily(t *testing.T, mode TaskMode, gate func(*TaskNode)) *wipFamily {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	family := &wipFamily{script: &wipPartCompleter{seen: map[string]string{}}}
	switch mode {
	case TaskModeMirror:
		family.ground = t.TempDir()
		writeFile(t, filepath.Join(family.ground, "notes.md"), "what the family gathered\n")
	default:
		family.ground = newTestRepo(t)
	}
	family.place = Place{Dir: t.TempDir(), Workspace: family.ground}

	session, _ := newTestAgent(t, family.script, func(config *Config) {
		config.Workspace = family.ground
		config.Place = family.place
		config.Divide = true
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		// NO CHECKER AND NO REPAIR ROUND. What is under test is the world the
		// parts start in and how their work comes home; the gate in front of that
		// has its own tests (task_divide_test.go).
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	family.graph = session.graph()
	// THE PARENT IS THIS TEST'S OWN and stays where it is — it is the dividing
	// worker. Its PARTS run for real.
	family.graph.run = func(node *TaskNode) {
		if node.parent == 0 {
			return
		}
		if gate != nil {
			gate(node)
		}
		family.graph.runOwned(node)
	}

	id := family.graph.reserve()
	family.graph.admit(id, taskSpec{title: "the whole job", request: personSentence,
		brief: "do the whole job", acceptance: "it is done", depth: 1,
		ground: family.ground, mode: mode})
	family.parent = family.graph.node(id)

	// The two lines the runner runs for a node it is about to start.
	tree, err := prepareTaskTreeOn(context.Background(), family.place, family.ground,
		session.journalID(), family.parent.id, family.parent.title(), family.parent.stand())
	if err != nil {
		t.Fatalf("prepareTaskTreeOn for the parent: %v", err)
	}
	family.parent.setTree(tree)
	family.tree = tree
	if canonicalPath(tree.dir) == canonicalPath(family.ground) {
		t.Fatalf("the family tree is the person's own directory %q", family.ground)
	}

	family.journal = filepath.Join(t.TempDir(), "worker.jsonl")
	worker, err := newAgent(Config{
		Workspace: tree.dir, Model: "test/model", System: "SYSTEM",
		SessionFile: family.journal,
		InTask:      true, Divide: true, TaskRepairRounds: 0,
		tasker: family.graph, taskID: id, taskDepth: 1,
	}, family.script)
	if err != nil {
		t.Fatalf("newAgent for the worker: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	family.parent.openRoom().speaking(worker)
	family.worker = worker
	return family
}

// wrote is what a saving call does, said the way the run says it: the file on
// disk AND the path on the node's ledger, which is the one list the harness
// stages ([stageTaskWork]).
func (f *wipFamily) wrote(t *testing.T, name, content string) {
	t.Helper()
	writeFile(t, filepath.Join(f.tree.dir, name), content)
	f.parent.noteWrote(name)
}

// divide is the real tool call, with two parts that own separate files.
func (f *wipFamily) divide(t *testing.T) string {
	t.Helper()
	answer, _, err := f.worker.divideWork(context.Background(), json.RawMessage(fmt.Sprintf(
		`{"evidence":%q,"parts":[`+
			`{"title":"alpha","summary":"s","brief":"write alpha.md","acceptance":"alpha.md is there"},`+
			`{"title":"beta","summary":"s","brief":"write beta.md","acceptance":"beta.md is there"}]}`,
		wideEvidence)))
	if err != nil {
		t.Fatalf("divide_work: %v", err)
	}
	return answer
}

// onlyDivision is the one line a division writes down about itself
// (sessionfile.go's [journalDivision]), which is where the world it froze and
// the commit it wrote are read from — off the record the road keeps rather than
// off anything a test arranged.
func onlyDivision(t *testing.T, journal string) journalDivision {
	t.Helper()
	lines := journaledDivisions(t, journal)
	if len(lines) != 1 {
		t.Fatalf("the journal holds %d division lines, want the one this call wrote", len(lines))
	}
	return lines[0]
}

// wipPartCompleter answers every lane a family touches, dispatching on WHAT IT
// WAS ASKED rather than on how many calls came before it: two parts run at once,
// and a positional script over concurrent lanes is a test asserting about
// whichever goroutine got there first.
//
// EACH PART READS THE PARENT'S FILE AND THEN THE FILE THE PARENT WROTE LATER,
// through the real `read` hand aimed at its own working copy. That is the
// issue's acceptance said from inside: not "the directory contains it" checked
// by a test after the fact, but the worker asking for it at its first step and
// being given it.
type wipPartCompleter struct {
	mu   sync.Mutex
	seen map[string]string
}

func (c *wipPartCompleter) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	system, whole := "", strings.Builder{}
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	tools, last := 0, ""
	for _, message := range messages {
		whole.WriteString(messageText(message) + "\n")
		if message.Role == "tool" {
			tools++
			last = messageText(message)
		}
	}
	text := whole.String()
	if system == titleSystem {
		return textResponse("the part"), nil
	}
	// WHICH PART THIS IS is read off the part's OWN scope and never off the whole
	// brief: the harness composes a part's brief with its siblings' scopes in it
	// so that it knows what not to touch (task_divide_compose.go), so a lane
	// dispatched on "write beta.md" appearing anywhere would answer beta's script
	// on alpha's lane.
	own := text
	if at := strings.Index(own, divisionThisPart); at >= 0 {
		own = own[at+len(divisionThisPart):]
	}
	if at := strings.Index(own, divisionOtherParts); at >= 0 {
		own = own[:at]
	}
	for _, part := range []string{"alpha", "beta"} {
		if !strings.Contains(own, "write "+part+".md") {
			continue
		}
		switch tools {
		case 0:
			return toolResponse("repro-"+part, "read", `{"path":"repro.txt"}`), nil
		case 1:
			c.record(part+"/repro", last)
			return toolResponse("later-"+part, "read", `{"path":"later.txt"}`), nil
		case 2:
			c.record(part+"/later", last)
			return writeResponse("write-"+part, part+".md", "the "+part+" section\n"), nil
		default:
			return textResponse("Wrote " + part + ".md."), nil
		}
	}
	// Everything else — the division review among it — gets nothing it can read,
	// which is the fail-open path and the division exactly as the worker wrote it.
	return textResponse("(unscripted)"), nil
}

func (c *wipPartCompleter) record(key, answer string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, already := c.seen[key]; !already {
		c.seen[key] = answer
	}
}

func (c *wipPartCompleter) answer(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen[key]
}

// parentOfCommitSaying is the commit one landing built ON: the first parent of
// the commit in this repository whose subject is exactly `subject`.
//
// IT IS READ OFF THE HISTORY AND NOT OFF A BRANCH NAME because a landed part has
// no branch left — a merged worktree's branch is deleted the moment its work is
// in ([taskTree.releaseLanded]) — and the question outlives it: what the part's
// own commit stands on is the world the part was given.
func parentOfCommitSaying(t *testing.T, dir, subject string) string {
	t.Helper()
	for _, line := range nonEmptyLines(gitOut(t, dir, "log", "--format=%H|%P|%s")) {
		fields := strings.SplitN(line, "|", 3)
		if len(fields) != 3 || strings.TrimSpace(fields[2]) != subject {
			continue
		}
		parents := strings.Fields(fields[1])
		if len(parents) == 0 {
			t.Fatalf("the commit saying %q has no parent at all", subject)
		}
		return parents[0]
	}
	t.Fatalf("no commit in %s says %q:\n%s", dir, subject, gitOut(t, dir, "log", "--oneline"))
	return ""
}

// ── the issue's recipe, on both grounds ─────────────────────────────────────

// THE WHOLE ACCEPTANCE, END TO END. A parent writes `repro.txt`, divides, and:
// each part reads that file off its own disk at its first step; the family
// branch holds the checkpoint commit BEFORE the parts' merges; and the parent's
// own landing puts all of it on the person's branch in ONE merge.
func TestAFamilyPutsItsWorkOnTheFamilyBranchBeforeItsPartsAreCut(t *testing.T) {
	family := newWipFamily(t, TaskModeWorktree, nil)
	family.wrote(t, "repro.txt", "the failing case the parent built\n")

	before := strings.TrimSpace(gitOut(t, family.tree.dir, "rev-parse", "HEAD"))
	if answer := family.divide(t); !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q", answer)
	}

	// THE COMMIT IS ON THE FAMILY BRANCH, which is the whole difference between
	// this and the ground ladder's seal: the seal moves no ref and is rebased
	// back out at the landing (groundladder.go), and this is history.
	frozen := strings.TrimSpace(gitOut(t, family.tree.dir, "rev-parse", "HEAD"))
	if frozen == before {
		t.Fatal("the family branch never moved: the parent's work is still uncommitted and no part can see it")
	}
	if got := strings.TrimSpace(gitOut(t, family.tree.dir, "log", "-1", "--format=%s")); got != wipCheckpointMessage(family.parent.title()) {
		t.Fatalf("the checkpoint says %q, want %q", got, wipCheckpointMessage(family.parent.title()))
	}
	if got := gitOut(t, family.tree.dir, "show", frozen+":repro.txt"); !strings.Contains(got, "the failing case") {
		t.Fatalf("the checkpoint does not hold the parent's file: %q", got)
	}

	parts := partsByTitle(t, family.graph, family.parent)
	for _, name := range []string{"alpha", "beta"} {
		waitDoneNode(t, parts[name])
		got := parts[name].notice()
		if got.State != TaskDone || got.Merge != mergeMerged {
			t.Fatalf("the part %s landed %s / %s: %q", name, got.State, got.Merge, got.Report)
		}
		if got.Branch == "" {
			t.Fatalf("the part %s came home without a branch", name)
		}
	}

	// EACH PART READ THE PARENT'S FILE OFF ITS OWN DISK AT ITS FIRST STEP.
	for _, name := range []string{"alpha", "beta"} {
		if answer := family.script.answer(name + "/repro"); !strings.Contains(answer, "the failing case") {
			t.Fatalf("the part %s asked for repro.txt and was told %q", name, answer)
		}
	}

	// AND EACH PART'S WORK SITS STRAIGHT ON THE CHECKPOINT, with no seal
	// underneath it: the commit the part's own landing wrote has the family's
	// world as its parent and nothing in between. A branch cut the old way
	// carried the ground ladder's machine commit here instead.
	for _, name := range []string{"alpha", "beta"} {
		if got := parentOfCommitSaying(t, family.tree.dir, "task: "+name); got != frozen {
			t.Fatalf("the part %s built on %s, want the family's checkpoint %s", name, got, frozen)
		}
	}

	// THE FAMILY BRANCH LOG SHOWS THE CHECKPOINT BEFORE THE PARTS' MERGES.
	log := gitOut(t, family.tree.dir, "log", "--oneline")
	checkpoint := strings.Index(log, "before its parts were handed out")
	if checkpoint < 0 {
		t.Fatalf("the family branch log has no checkpoint in it:\n%s", log)
	}
	for _, name := range []string{"alpha", "beta"} {
		at := strings.Index(log, "task: "+name)
		if at < 0 {
			t.Fatalf("the part %s never reached the family branch:\n%s", name, log)
		}
		// `git log` is newest first, so "before" is further down the page.
		if at > checkpoint {
			t.Fatalf("the part %s merged before the checkpoint was written:\n%s", name, log)
		}
	}

	// AND THE FAMILY COMES HOME IN ONE MERGE. The parent lands the way its
	// runner lands it: its own ledger committed, its branch merged into the
	// person's repository, once.
	merge, detail := family.tree.comeHome(family.parent.title(), []string{"repro.txt"})
	if merge != mergeMerged {
		t.Fatalf("the family came home as %q (%s)", merge, detail)
	}
	for _, file := range []string{"repro.txt", "alpha.md", "beta.md"} {
		if _, err := os.Stat(filepath.Join(family.ground, file)); err != nil {
			t.Fatalf("%s never reached the person's own branch: %v", file, err)
		}
	}
	if merges := nonEmptyLines(gitOut(t, family.ground, "log", "--merges", "--oneline")); len(merges) != 1 {
		t.Fatalf("the person's branch holds %d merges, want the one landing:\n%s", len(merges), strings.Join(merges, "\n"))
	}
}

// THE SAME THING ON A FOLDER, which is most general work: the mirror #230 opens
// as the family tree takes the checkpoint exactly as a repository does, and
// nothing of any of it reaches the person's folder until the family lands.
func TestAFolderFamilyPutsItsWorkOnTheFamilyBranchToo(t *testing.T) {
	family := newWipFamily(t, TaskModeMirror, nil)
	family.wrote(t, "repro.txt", "the failing case the parent built\n")

	if answer := family.divide(t); !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q", answer)
	}
	frozen := strings.TrimSpace(gitOut(t, family.tree.dir, "rev-parse", "HEAD"))
	if got := gitOut(t, family.tree.dir, "show", frozen+":repro.txt"); !strings.Contains(got, "the failing case") {
		t.Fatalf("the mirror's checkpoint does not hold the parent's file: %q", got)
	}

	parts := partsByTitle(t, family.graph, family.parent)
	for _, name := range []string{"alpha", "beta"} {
		waitDoneNode(t, parts[name])
		if got := parts[name].notice(); got.State != TaskDone || got.Merge != mergeMerged {
			t.Fatalf("the part %s landed %s / %s: %q", name, got.State, got.Merge, got.Report)
		}
		if answer := family.script.answer(name + "/repro"); !strings.Contains(answer, "the failing case") {
			t.Fatalf("the part %s asked for repro.txt and was told %q", name, answer)
		}
	}
	for _, file := range []string{"alpha.md", "beta.md"} {
		if _, err := os.Stat(filepath.Join(family.tree.dir, file)); err != nil {
			t.Fatalf("%s never reached the family tree: %v", file, err)
		}
		// AND THE PERSON'S FOLDER IS UNTOUCHED UNTIL THE FAMILY LANDS.
		if _, err := os.Stat(filepath.Join(family.ground, file)); !os.IsNotExist(err) {
			t.Fatalf("%s reached the person's folder before the family landed", file)
		}
	}
	if _, err := os.Stat(filepath.Join(family.ground, ".git")); err == nil {
		t.Fatalf("the person's folder %q was made into a repository", family.ground)
	}
}

// ── one world, however far apart the parts were cut ─────────────────────────

// THE RACE THIS CLOSES. A part's working copy is prepared when the frontier
// starts it, and the parent goes on working while its parts run — so a sibling
// cut a minute later used to inherit whatever the parent's directory held THEN.
// Here the parent writes `later.txt` after the first part has finished, and the
// second part must still be standing in the world the division named.
func TestPartsCutMinutesApartStandInOneWorld(t *testing.T) {
	held := make(chan struct{})
	family := newWipFamily(t, TaskModeWorktree, func(node *TaskNode) {
		if node.title() == "beta" {
			<-held
		}
	})
	family.wrote(t, "repro.txt", "the failing case the parent built\n")

	if answer := family.divide(t); !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q", answer)
	}
	frozen := strings.TrimSpace(gitOut(t, family.tree.dir, "rev-parse", "HEAD"))

	parts := partsByTitle(t, family.graph, family.parent)
	waitDoneNode(t, parts["alpha"])

	// THE PARENT KEEPS WORKING, which is the parent-stays law and the whole of
	// the window: this file is on its disk before the second part is cut.
	family.wrote(t, "later.txt", "written after the parts were handed out\n")
	close(held)
	waitDoneNode(t, parts["beta"])

	for _, name := range []string{"alpha", "beta"} {
		if got := parts[name].notice(); got.State != TaskDone {
			t.Fatalf("the part %s landed %s: %q", name, got.State, got.Report)
		}
		if answer := family.script.answer(name + "/later"); strings.Contains(answer, "written after") {
			t.Fatalf("the part %s can see work the parent did after the split: %q", name, answer)
		}
		if got := parentOfCommitSaying(t, family.tree.dir, "task: "+name); got != frozen {
			t.Fatalf("the part %s built on %s, want the one world the division froze (%s)", name, got, frozen)
		}
	}
}

// ── nothing written, and nowhere to write ───────────────────────────────────

// A PARENT THAT HAS WRITTEN NOTHING WRITES NO COMMIT — the emptiness law, and
// the whole of the sketch road's answer, since a division drawn before the first
// request has nothing on disk. THE WORLD IS STILL PINNED, because the second
// defect does not need the parent to have written anything first.
func TestADivisionWithNothingWrittenCommitsNothingAndStillPinsTheWorld(t *testing.T) {
	family := newWipFamily(t, TaskModeWorktree, nil)
	before := strings.TrimSpace(gitOut(t, family.tree.dir, "rev-parse", "HEAD"))

	if answer := family.divide(t); !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q", answer)
	}
	if now := strings.TrimSpace(gitOut(t, family.tree.dir, "rev-parse", "HEAD")); now != before {
		t.Fatalf("the family branch moved from %s to %s with nothing written to commit", before, now)
	}
	line := onlyDivision(t, family.journal)
	if line.Checkpoint != "" {
		t.Fatalf("a parent with an empty ledger wrote checkpoint %q", line.Checkpoint)
	}
	if line.Frozen != before {
		t.Fatalf("the division froze %q, want the family tree's own HEAD %q", line.Frozen, before)
	}
	parts := partsByTitle(t, family.graph, family.parent)
	for _, name := range []string{"alpha", "beta"} {
		waitDoneNode(t, parts[name])
	}
}

// A FAMILY STANDING IN THE PERSON'S OWN FOLDER IS NEVER COMMITTED INTO. "Work
// here" means the person's directory, on the person's branch, and a checkpoint
// there would be machinery writing into their history behind their back. It is
// the guard [harnessOwnsThisTree] exists for, and a `.git` is not enough to
// answer it — the person's own repository has one.
func TestAFamilyWorkingInThePersonsOwnRepositoryIsNeverCommittedInto(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "repro.txt"), "the failing case\n")
	head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	dirty := gitOut(t, repo, "status", "--porcelain")

	nest := newDivideNest(t, wideBrief, 0)
	nest.node.config.Workspace = repo
	nest.parent.setTree(taskTree{dir: repo, ground: repo, merge: mergeInPlace,
		mode: TaskModeInPlace, rung: GroundRungHere})
	nest.parent.noteWrote("repro.txt")

	if answer := nest.divide(t, divideArgs(wideEvidence, 2)); !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q", answer)
	}
	if now := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); now != head {
		t.Fatalf("an in-place family committed onto the person's own branch: %s → %s", head, now)
	}
	if now := gitOut(t, repo, "status", "--porcelain"); now != dirty {
		t.Fatalf("the person's working tree changed:\nbefore:\n%s\nafter:\n%s", dirty, now)
	}
	line := onlyDivision(t, nest.journal)
	if line.Frozen != "" || line.Checkpoint != "" {
		t.Fatalf("an in-place family froze %q and checkpointed %q; it has no tree of its own", line.Frozen, line.Checkpoint)
	}
}

// ── the restart ─────────────────────────────────────────────────────────────

// A PART THAT COMES BACK AFTER A RESTART STANDS IN THE WORLD ITS DIVISION FROZE.
// The freeze rides on the checkpoint for exactly this: a resumed part prepares
// its working copy on this road and nowhere else, and one that had forgotten
// would seal the parent's tree AS IT STANDS NOW — the divergence this seam
// closes, arriving through the one door that does not prepare a tree at the
// division.
func TestAPartRestoredFromACheckpointStandsInTheFrozenWorld(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}
	tree, err := prepareTaskTreeOn(context.Background(), place, repo, "aaaa1111bbbb2222", 7,
		"the whole job", taskStand{dir: repo, mode: TaskModeWorktree})
	if err != nil {
		t.Fatalf("prepareTaskTreeOn for the parent: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "repro.txt"), "the failing case\n")
	saved := commitTaskWorkAs(tree.dir, wipCheckpointMessage("the whole job"), []string{"repro.txt"})
	if len(saved) == 0 {
		t.Fatal("the checkpoint staged nothing")
	}
	frozen := strings.TrimSpace(gitOut(t, tree.dir, "rev-parse", "HEAD"))

	graph := newTaskGraph()
	graph.run = func(*TaskNode) {}
	family := graph.reserve()
	graph.admit(family, taskSpec{title: "the whole job", brief: "b", acceptance: "a", depth: 1})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "alpha", brief: "write alpha.md", acceptance: "a", depth: 2,
		parent: family, frozen: frozen})

	// THROUGH THE CHECKPOINT AND BACK, which is the only road that matters here:
	// a field held in memory proves nothing about a process that died.
	encoded, err := json.Marshal(graph.document())
	if err != nil {
		t.Fatalf("encoding the graph: %v", err)
	}
	document, err := decodeTasks(encoded)
	if err != nil {
		t.Fatalf("the checkpoint does not load: %v", err)
	}
	var restored *TaskNode
	for _, record := range document.Nodes {
		if record.ID == id {
			restored = restoreNode(newTaskGraph(), record)
		}
	}
	if restored == nil {
		t.Fatal("the part is not in the checkpoint at all")
	}
	if restored.stand().frozen != frozen {
		t.Fatalf("the resumed part stands on %q, want the world its division froze (%q)", restored.stand().frozen, frozen)
	}

	// AND THE PARENT KEEPS WRITING WHILE THE PROCESS IS AWAY. The resumed part
	// must not pick any of it up.
	writeFile(t, filepath.Join(tree.dir, "later.txt"), "written while nothing was running\n")
	part, err := prepareTaskTreeOn(context.Background(), place, tree.dir, "aaaa1111bbbb2222", id,
		"alpha", restored.stand())
	if err != nil {
		t.Fatalf("prepareTaskTreeOn for the resumed part: %v", err)
	}
	if got := readFile(t, filepath.Join(part.dir, "repro.txt")); !strings.Contains(got, "the failing case") {
		t.Fatalf("the resumed part woke without the family's work: %q", got)
	}
	if _, err := os.Stat(filepath.Join(part.dir, "later.txt")); !os.IsNotExist(err) {
		t.Fatal("the resumed part resealed the parent's tree and picked up work its division never named")
	}
	if got := strings.TrimSpace(gitOut(t, part.dir, "rev-parse", "HEAD")); got != frozen {
		t.Fatalf("the resumed part was cut from %s, want the frozen world %s", got, frozen)
	}
	if part.base != "" {
		t.Fatalf("the resumed part carries base %q; a freeze is history and there is nothing to rebase out", part.base)
	}
}

// ── the seal underneath, which is still everybody else's road ───────────────

// TWO SEALS AT ONCE BOTH CARRY THE PARENT'S FILE. They used to share ONE staging
// file — `.git/aforge-ground-index` — and a parent's siblings are carved
// concurrently, so one seal removed the index another was writing and the loser
// answered "nothing uncommitted" and carved its child from HEAD, silently.
func TestTwoSealsAtOnceBothCarryTheParentsFile(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "wip.txt"), "the parent's unfinished line\n")

	const at = 8
	var (
		wait    sync.WaitGroup
		mu      sync.Mutex
		commits = make([]string, 0, at)
		failed  []error
	)
	for i := 0; i < at; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			commit, err := sealGroundWork(repo, "the whole job")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, err)
				return
			}
			commits = append(commits, commit)
		}()
	}
	wait.Wait()
	if len(failed) > 0 {
		t.Fatalf("%d of %d concurrent seals failed, the first with: %v", len(failed), at, failed[0])
	}
	if len(commits) != at {
		t.Fatalf("%d of %d concurrent seals answered", len(commits), at)
	}
	for _, commit := range commits {
		if commit == "" {
			t.Fatal("a seal answered nothing while the parent held uncommitted work: a child of it would carve from HEAD")
		}
		if got := gitOut(t, repo, "show", commit+":wip.txt"); !strings.Contains(got, "the parent's unfinished line") {
			t.Fatalf("a concurrent seal wrote a world without the parent's file: %q", got)
		}
	}
	// AND NOT ONE OF THEM LEFT ITS STAGING FILE BEHIND.
	gitDir := strings.TrimSpace(gitOut(t, repo, "rev-parse", "--absolute-git-dir"))
	entries, err := os.ReadDir(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), groundIndexPrefix) {
			t.Fatalf("%s was left in the repository", entry.Name())
		}
	}
}

// A CLEAN TREE AND A GIT THAT WOULD NOT RUN ARE TWO DIFFERENT ANSWERS. They used
// to be one — the empty string — which is how a locked index became "the parent
// had nothing uncommitted" and a child was carved from a world that was missing
// its parent's work with nobody told.
func TestASealTellsACleanTreeFromAGitThatWouldNotRun(t *testing.T) {
	repo := newTestRepo(t)
	commit, err := sealGroundWork(repo, "the whole job")
	if err != nil || commit != "" {
		t.Fatalf("a clean tree sealed %q / %v, want nothing and no error", commit, err)
	}

	commit, err = sealGroundWork(t.TempDir(), "the whole job")
	if err == nil {
		t.Fatal("a directory with no repository behind it sealed silently; a child of it would carve from a world nobody made")
	}
	if commit != "" {
		t.Fatalf("a failed seal answered the commit %q", commit)
	}
	if !strings.Contains(err.Error(), "could not be sealed") {
		t.Fatalf("the failure reads %q, want it to say what could not be done", err)
	}
}
