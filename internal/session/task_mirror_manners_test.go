package session

// A FOLDER LANDING DOES NOT WRITE OVER THE PERSON'S OWN EDIT (issue #258).
//
// The claim under test is one sentence: what a mirror family lays back over
// somebody's folder is measured against what that folder held when the copy was
// made, and a file they changed themselves in between is named rather than
// overwritten. So these drive the real doors — the same [prepareTaskTreeOn] a
// running node calls, the one [landHome] every landing road goes through, and a
// person's own `accept` hours later — and assert both halves of every case: what
// the folder holds afterwards, and what the person is told.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// aMirrorOn is the shape most of these start from: a person's folder with one
// note in it, and a family standing on it in a copy of its own.
func aMirrorOn(t *testing.T, ground string) taskTree {
	t.Helper()
	tree, err := prepareTaskTreeOn(context.Background(), Place{}, t.TempDir(), "dddd4444dddd4444", 11,
		"tidy the notes", taskStand{dir: ground, mode: TaskModeMirror})
	if err != nil {
		t.Fatalf("prepareTaskTreeOn: %v", err)
	}
	return tree
}

// THE DEFECT, PINNED — the issue's own run. The family rewrites `notes.md` in
// its copy, the person rewrites their own while it works, and the landing lays
// nothing at all.
func TestAFolderLandingRefusesToWriteOverThePersonsOwnEdit(t *testing.T) {
	// THE GROUND IS NAMED THE WAY THE DOOR SPELLS IT. prepareTaskTreeOn
	// canonicalizes the folder it stands on (place.go's [canonicalPath]) and
	// every sentence about it names that spelling — which on macOS is
	// /private/var/… for the /var/… that t.TempDir hands out, so a test that
	// compared the raw one was green on Linux and red on every Mac.
	ground := canonicalPath(t.TempDir())
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")

	tree := aMirrorOn(t, ground)
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the line the task wrote\n")
	// The person edits their own copy while the work runs.
	writeFile(t, filepath.Join(ground, "notes.md"), "the line the person typed\n")

	node := loneTestNode(t, "tidy the notes")
	ledger, merge, detail, _ := landHome(node, tree, []string{"notes.md"})
	node.finish("tidied the notes", ledger, "", merge)

	if merge != mergeConflicted {
		t.Fatalf("merge = %q, want the landing to refuse", merge)
	}
	for _, want := range []string{"its work is in " + tree.dir, "was not laid over " + ground,
		"notes.md changed there while this ran"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("the sentence does not say %q: %q", want, detail)
		}
	}
	// AND THE TWO VERSIONS ARE BOTH STILL THERE, which is the whole of the offer:
	// theirs where they left it, the family's where the sentence says it is.
	if got := readFile(t, filepath.Join(ground, "notes.md")); got != "the line the person typed\n" {
		t.Fatalf("the person's folder holds %q — their own edit was written over", got)
	}
	if got := readFile(t, filepath.Join(tree.dir, "notes.md")); got != "the line the task wrote\n" {
		t.Fatalf("the family's copy holds %q, want its own work still in it", got)
	}
}

// AND THE CONTROL, in the same shape: an untouched folder takes every file, the
// mark is the ordinary one and the report gains no sentence (the emptiness law).
func TestAFolderLandingOverAnUntouchedFolderSaysNothingExtra(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")

	tree := aMirrorOn(t, ground)
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the line the task wrote\n")
	writeFile(t, filepath.Join(tree.dir, "under", "deeper.md"), "and the one under it\n")

	node := loneTestNode(t, "tidy the notes")
	_, merge, detail, _ := landHome(node, tree, []string{"notes.md", "under/deeper.md"})
	if merge != mergeInPlace || detail != "" {
		t.Fatalf("merge = %q, detail = %q, want an ordinary folder landing", merge, detail)
	}
	if got := readFile(t, filepath.Join(ground, "notes.md")); got != "the line the task wrote\n" {
		t.Fatalf("notes.md in the person's folder is %q", got)
	}
	if got := readFile(t, filepath.Join(ground, "under", "deeper.md")); got != "and the one under it\n" {
		t.Fatalf("under/deeper.md in the person's folder is %q", got)
	}
}

// ── the four readings of one path ───────────────────────────────────────────

// A FILE THE PERSON CREATED THEMSELVES IS NOT THE FAMILY'S TO OVERWRITE. It was
// absent when the copy was made and it is there now, so somebody wrote it while
// the work ran — the commonest way two people write the same new file.
func TestAFileThePersonCreatedWhileTheWorkRanIsRefused(t *testing.T) {
	ground := t.TempDir()
	tree := aMirrorOn(t, ground)
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the line the task wrote\n")
	writeFile(t, filepath.Join(ground, "notes.md"), "the line the person typed\n")

	changed := groundChanged(tree.dir, ground, []string{"notes.md"})
	if len(changed) != 1 || changed[0] != "notes.md" {
		t.Fatalf("groundChanged = %v, want notes.md refused", changed)
	}
}

// A FILE THE PERSON DELETED IS AN EDIT LIKE ANY OTHER, and it is refused for the
// same reason a rewrite is: throwing a file away is a thing somebody did on
// purpose, and laying the family's version over it would put back the one file
// they had just decided they did not want. They are told its name and where the
// family's copy is, and putting it back is one move they can make themselves.
func TestAFileThePersonDeletedIsRefusedRatherThanPutBack(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")
	tree := aMirrorOn(t, ground)
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the line the task wrote\n")
	if err := os.Remove(filepath.Join(ground, "notes.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	changed := groundChanged(tree.dir, ground, []string{"notes.md"})
	if len(changed) != 1 || changed[0] != "notes.md" {
		t.Fatalf("groundChanged = %v, want the deleted note refused", changed)
	}
}

// A FILE THE FAMILY WROTE AND THEN DELETED IS STILL TAKEN AWAY. The ledger names
// it, the folder still holds exactly what it held when the copy was made, and
// nobody but the family has touched it — so the landing is the one this file
// exists to leave alone.
func TestAFileTheFamilyRemovedIsStillTakenOutOfTheFolder(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "stale.md"), "what was there before\n")
	tree := aMirrorOn(t, ground)
	if err := os.Remove(filepath.Join(tree.dir, "stale.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	node := loneTestNode(t, "tidy the notes")
	_, merge, detail, _ := landHome(node, tree, []string{"stale.md"})
	if merge != mergeInPlace || detail != "" {
		t.Fatalf("merge = %q, detail = %q, want the removal to land", merge, detail)
	}
	if _, err := os.Stat(filepath.Join(ground, "stale.md")); !os.IsNotExist(err) {
		t.Fatal("a file the family deleted is still in the person's folder")
	}
}

// AND A LAY THAT WOULD NOT CHANGE A BYTE IS NOT A COLLISION. This is what makes
// the late road safe to ask twice: a landing that already happened leaves the
// folder holding exactly what the family would lay, and asking again must not
// turn the family's own work into somebody's edit.
func TestAFolderThatAlreadyHoldsTheFamilysWorkLandsAgainQuietly(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")
	tree := aMirrorOn(t, ground)
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the line the task wrote\n")

	node := loneTestNode(t, "tidy the notes")
	if _, merge, _, _ := landHome(node, tree, []string{"notes.md"}); merge != mergeInPlace {
		t.Fatalf("the first landing answered %q", merge)
	}
	if _, merge, detail, _ := landHome(node, tree, []string{"notes.md"}); merge != mergeInPlace || detail != "" {
		t.Fatalf("the second landing answered %q / %q, want it to lay the same bytes again", merge, detail)
	}
}

// A LEDGER PATH THAT NAMES A WHOLE DIRECTORY IS MEASURED AS ONE, because that is
// how it is laid: the target is removed and the tree copied over it, so a file
// the person put inside it is a file the landing would take away.
func TestADirectoryInTheLedgerIsMeasuredAsAWhole(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "under", "kept.md"), "what was there before\n")
	tree := aMirrorOn(t, ground)
	writeFile(t, filepath.Join(tree.dir, "under", "written.md"), "what the task wrote\n")

	// Untouched, the whole directory lands.
	node := loneTestNode(t, "write the section")
	if _, merge, detail, _ := landHome(node, tree, []string{"under"}); merge != mergeInPlace || detail != "" {
		t.Fatalf("merge = %q, detail = %q, want the directory to land", merge, detail)
	}
	if got := readFile(t, filepath.Join(ground, "under", "written.md")); got != "what the task wrote\n" {
		t.Fatalf("under/written.md in the person's folder is %q", got)
	}

	// And a note the person drops into it afterwards is inside what the next
	// landing would remove, so the next landing stands back and names it.
	writeFile(t, filepath.Join(ground, "under", "theirs.md"), "the line the person typed\n")
	if _, merge, detail, _ := landHome(node, tree, []string{"under"}); merge != mergeConflicted ||
		!strings.Contains(detail, "under changed there while this ran") {
		t.Fatalf("merge = %q, detail = %q, want the directory refused", merge, detail)
	}
	if got := readFile(t, filepath.Join(ground, "under", "theirs.md")); got != "the line the person typed\n" {
		t.Fatalf("the person's own note inside the directory is %q", got)
	}
}

// A FOLDER WITH NO RECORD LANDS EXACTLY AS IT LANDED BEFORE THIS EXISTED. It is
// a family carried across an upgrade, or one whose folder could not be read in
// full, and absence is ordinary rather than a reason to need somebody's look.
func TestAFamilyWithNoBaselineLandsTheWayItAlwaysDid(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")
	tree := aMirrorOn(t, ground)
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the line the task wrote\n")
	writeFile(t, filepath.Join(ground, "notes.md"), "the line the person typed\n")
	// The older build's folder: a copy, and nothing written down beside it.
	if err := os.Remove(filepath.Join(tree.dir, aforgeDroppings, groundBaselineRecord)); err != nil {
		t.Fatalf("remove the record: %v", err)
	}

	node := loneTestNode(t, "tidy the notes")
	if _, merge, _, _ := landHome(node, tree, []string{"notes.md"}); merge != mergeInPlace {
		t.Fatalf("merge = %q, want the old road for a folder with no record", merge)
	}
	if got := readFile(t, filepath.Join(ground, "notes.md")); got != "the line the task wrote\n" {
		t.Fatalf("notes.md is %q, want the old road to have laid the family's line", got)
	}
}

// AND THE WALK IS BOUNDED THE WAY EVERY OTHER WALK OVER A GROUND IS
// ([auditRestoreEntries]). A folder too big to read in full writes no record at
// all, because a half-walked one would name files nobody touched; and a path
// this cannot answer for is never called unchanged.
func TestTheBaselineIsBoundedByTheEntryCap(t *testing.T) {
	ground := t.TempDir()
	for _, name := range []string{"one.md", "two.md", "three.md"} {
		writeFile(t, filepath.Join(ground, name), name+"\n")
	}
	if gatherDigests(ground, "", map[string]string{}, &digestWalk{budget: 2}) {
		t.Fatal("a walk past its cap answered as though it had read the whole folder")
	}
	if digest := pathDigest(ground, "one.md", &digestWalk{}); digest != digestUnknown {
		t.Fatalf("a path read past the cap answered %q, want it to be unknown", digest)
	}
	// AND A DIRECTORY WITH A CORNER IT COULD NOT OPEN IS NEVER CALLED UNCHANGED,
	// because laying that path would remove what is inside the corner.
	shut := filepath.Join(ground, "shut")
	writeFile(t, filepath.Join(shut, "inside.md"), "what nobody can see\n")
	if err := os.Chmod(shut, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(shut, 0o700) })
	if digest := pathDigest(ground, "shut", &digestWalk{budget: auditRestoreEntries}); digest != digestUnknown {
		t.Fatalf("a directory that could not be read answered %q, want it to be unknown", digest)
	}
}

// ── the roads a person actually stands on ───────────────────────────────────

// THE RUN ROAD, END TO END. One scripted family on a plain folder, the person
// typing into their own copy of the file while it works, and the landing every
// running node takes.
func TestARunningFolderFamilyLandsNeedingALookWhenTheFolderMoved(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")
	t.Setenv("HOME", t.TempDir())

	node := aRunOnTheFolder(t, ground, true)

	notice := node.notice()
	if notice.State != TaskUnverified {
		t.Fatalf("state = %q, report = %q", notice.State, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, yourCallLead(TaskFacts{Merge: notice.Merge})) {
		t.Fatalf("the report does not lead with the person's look: %q", notice.Report)
	}
	if !strings.Contains(notice.Report, "notes.md changed there while this ran") {
		t.Fatalf("the report does not name the file: %q", notice.Report)
	}
	if got := readFile(t, filepath.Join(ground, "notes.md")); got != "the line the person typed\n" {
		t.Fatalf("the person's folder holds %q — their own edit was written over", got)
	}
	node.graph.mu.Lock()
	dir := node.worktree
	node.graph.mu.Unlock()
	if got := readFile(t, filepath.Join(dir, "notes.md")); got != "the line the task wrote\n" {
		t.Fatalf("the family's copy holds %q, want its own work still in it", got)
	}
}

// aRunOnTheFolder runs one scripted task whose ground is a plain folder, all the
// way through the landing every running node takes. When `person` is set,
// somebody types into their own copy of the same file while the work runs, which
// is the whole of the defect this file is about.
func aRunOnTheFolder(t *testing.T, ground string, person bool) *TaskNode {
	t.Helper()
	said := &scriptedCompleter{steps: []step{
		writeCall("call-notes", "notes.md", "the line the task wrote\n"),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			if person {
				// THE PERSON, IN THEIR OWN EDITOR, WHILE THE WORK IS STILL RUNNING.
				_ = os.WriteFile(filepath.Join(ground, "notes.md"), []byte("the line the person typed\n"), 0o600)
			}
			return textResponse("tidied the notes"), nil
		},
	}}
	agent, _ := newTestAgent(t, said, func(config *Config) {
		config.Workspace = t.TempDir()
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.Divide = false
	})
	graph := agent.graph()
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "tidy the notes", named: true,
		brief: "tidy the notes", deliverable: "the notes, tidied", acceptance: "the notes are tidy",
		ground: ground, mode: TaskModeMirror, depth: 1})
	node := graph.node(id)
	waitDoneNode(t, node)
	return node
}

// AND THE RUN ROAD'S CONTROL, which is the half that says this cost nothing. The
// same scripted task over a folder nobody touched lands its file, settles done,
// wears the ordinary mark and says not one word about any of it.
func TestARunningFolderFamilyOverAnUntouchedFolderStillLands(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")
	t.Setenv("HOME", t.TempDir())

	notice := aRunOnTheFolder(t, ground, false).notice()
	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q", notice.State, notice.Report)
	}
	if notice.Merge != mergeInPlace {
		t.Fatalf("merge = %q, want a folder landing to stay in place", notice.Merge)
	}
	if strings.Contains(notice.Report, "changed there while this ran") ||
		strings.Contains(notice.Report, yourCallLead(TaskFacts{Merge: notice.Merge})) {
		t.Fatalf("the report gained a sentence over an untouched folder: %q", notice.Report)
	}
	if got := readFile(t, filepath.Join(ground, "notes.md")); got != "the line the task wrote\n" {
		t.Fatalf("notes.md in the person's folder is %q, want the work laid", got)
	}
}

// AND THE LATE ROAD, WHICH IS THE WIDER WINDOW. A family that settled needing a
// look is accepted by hand in the morning, over a folder somebody has been
// working in since — and the accept refuses exactly as the run's own landing
// would have, rather than laying a ledger recorded the day before.
func TestAnAcceptedFolderFamilyRefusesAFolderThatMovedUnderIt(t *testing.T) {
	ground := t.TempDir()
	writeFile(t, filepath.Join(ground, "notes.md"), "the original line\n")
	t.Setenv("HOME", t.TempDir())

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = t.TempDir()
		config.AskConsent = false
	})
	graph := stubbedGraph(agent, func(*TaskNode) {})

	tree := aMirrorOn(t, ground)
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the line the task wrote\n")

	id := graph.reserve()
	graph.admit(id, taskSpec{title: "tidy the notes", named: true, brief: "b", acceptance: "a",
		ground: ground, mode: TaskModeMirror, depth: 1})
	family := graph.node(id)
	family.setTree(tree)

	// IT LANDS NEEDING A LOOK, WHICH LAYS NOTHING.
	kept, ledger := keepHome(family, tree, []string{"notes.md"})
	family.finish(yourCallLead(TaskFacts{Merge: kept})+"nobody could judge this", ledger, "", kept)
	graph.complete(family, TaskUnverified)

	// AND THE PERSON SPENDS THE MORNING IN THE SAME FILE.
	writeFile(t, filepath.Join(ground, "notes.md"), "the line the person typed\n")

	if err := agent.ResolveUnverified(id, TaskAccept, "I read it myself"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	notice := family.notice()
	if notice.State != TaskUnverified {
		t.Fatalf("the accepted family is %q: %s", notice.State, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, yourCallLead(TaskFacts{Merge: notice.Merge})) ||
		!strings.Contains(notice.Report, "notes.md changed there while this ran") {
		t.Fatalf("the accept did not say which file moved: %q", notice.Report)
	}
	if got := readFile(t, filepath.Join(ground, "notes.md")); got != "the line the person typed\n" {
		t.Fatalf("the person's folder holds %q — an accept wrote over their own edit", got)
	}
}
