package session

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── THE ONE ROAD HOME, AND THE ONE REFUSAL SETTLEMENT ──
//
// Both functions under test here exist because the thing they do was written out
// more than once, and a road with several copies is a road where a check added
// to one copy is a check the others do not get. These tests are about the SHARED
// shape rather than about any one caller: what a landing does, in what order, and
// what a refusal decides.

// A LANDING THAT HOLDS COMPOSES ITS REPORT IN ONE FIXED ORDER, whichever road
// asked for it. `head` leads, `tail` stands under it, and what the merge itself
// had to say stands under both — which is the shape every card, every project
// row and every parent's note downstream reads from the top.
//
// THAT A NODE SETTLES ONCE IS NOT THIS TEST'S CLAIM AND COULD NOT BE: this calls
// the landing directly. It is proved where it has always been proved, by the
// suites that drive the real runner end to end — task_progress_test.go,
// task_landing_test.go and task_test.go — none of which this change touches.
func TestEveryRoadHomeComposesTheSameReport(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "cccc3333cccc3333", 1, "add the parser")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	node := loneTestNode(t, "add the parser")
	writeFile(t, filepath.Join(tree.dir, "parser.py"), "def parse():\n    return 1\n")

	agent := &Agent{}
	state := agent.landFinished(context.Background(), node, tree, []string{"parser.py"},
		"the parser reads the shared line", "checked against the acceptance", " (unaudited)", io.Discard)

	if state != TaskDone {
		t.Fatalf("a landing that holds is %q, want it done", state)
	}
	if _, err := os.Stat(filepath.Join(repo, "parser.py")); err != nil {
		t.Fatalf("the work did not come home: %v", err)
	}
	report, changed, branch, merge := node.leavings()
	if merge != mergeMerged {
		t.Fatalf("merge = %q, want it merged", merge)
	}
	if branch != tree.branch || !containsString(changed, "parser.py") {
		t.Fatalf("the landing lost the branch or the files: %q %v", branch, changed)
	}
	lead := strings.Index(report, "the parser reads the shared line")
	under := strings.Index(report, "checked against the acceptance")
	if lead != 0 || under < lead {
		t.Fatalf("the report does not lead with the work's own account:\n%s", report)
	}
	// AND THE LOG'S OWN NOTE IS NOT THE PERSON'S. " (unaudited)" belongs to
	// whoever is reading the machinery.
	if strings.Contains(report, "unaudited") {
		t.Fatalf("the log's note reached the person's report:\n%s", report)
	}
}

// AND A LANDING THAT WOULD NOT MERGE NEEDS A LOOK RATHER THAN SAYING DONE — the
// same not-merged test, on the same road, whoever asked for the landing.
func TestTheOneRoadHomeStillRefusesToCallAConflictDone(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "dddd4444dddd4444", 1, "edit the shared file")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	node := loneTestNode(t, "edit the shared file")
	// The person's uncommitted work and the node's copy both move the same line.
	// The branch itself stays at the cut, so this reaches the merge-conflict road.
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's own line\n")
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "the node's line\n")

	agent := &Agent{}
	state := agent.landFinished(context.Background(), node, tree, []string{"shared.txt"},
		"the shared line now carries the flag", "checked against the acceptance", "", io.Discard)

	if state != TaskUnverified {
		t.Fatalf("a conflicted landing is %q, want it to need a look", state)
	}
	report, _, _, merge := node.leavings()
	if merge != mergeConflicted {
		t.Fatalf("merge = %q, want it to say the branch would not go", merge)
	}
	if !strings.HasPrefix(report, yourCallLead(TaskFacts{Merge: mergeConflicted})) {
		t.Fatalf("the report does not lead with the person's own words:\n%s", report)
	}
	if !strings.Contains(report, "the shared line now carries the flag") {
		t.Fatalf("the node's own words were dropped:\n%s", report)
	}
}

// A REAL CONFLICT'S NOTICE NAMES THE KEPT BRANCH AND DOES NOT CLAIM SUCCESS.
// comeHome can strand a commit (F31/F32) and the model then tells the person
// the work "arrived as a merge result". The merge mark is a hard precondition
// of the sentence the model reads: if the branch did not fasten, that sentence
// names the branch and never says the work finished or merged.
func TestAConflictedMergeNoticeNamesTheBranchAndDoesNotClaimSuccess(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "eeee5555eeee5555", 1, "edit the shared file")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	node := loneTestNode(t, "edit the shared file")
	// Keeping the person's clash uncommitted leaves the branch at the recorded
	// cut while still making the merge refuse to overwrite their work.
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's own line\n")
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "the node's line\n")

	agent := &Agent{}
	state := agent.landFinished(context.Background(), node, tree, []string{"shared.txt"},
		"the shared line now carries the flag", "checked against the acceptance", "", io.Discard)
	if state != TaskUnverified {
		t.Fatalf("a conflicted landing is %q, want it to need a look", state)
	}
	// landFinished writes the leavings; complete is what stamps the state the
	// notice reads. The production path does both before the model sees this.
	node.graph.mu.Lock()
	node.state = state
	node.graph.mu.Unlock()

	note := taskNote(node.notice(), "", TaskSettleAsk, landingAddress{person: true})
	if !strings.Contains(note, tree.branch) {
		t.Fatalf("the notice does not name the kept branch %s:\n%s", tree.branch, note)
	}
	for _, success := range []string{
		"task 1 finished",
		"merged into yours",
		"arrived as a merge",
		yourCallLead(TaskFacts{Merge: mergeConflicted}),
	} {
		if strings.Contains(note, success) {
			t.Fatalf("the notice claims success (%q):\n%s", success, note)
		}
	}
}

// ── THE REFUSAL SETTLEMENT ──

// A REFUSAL NOBODY ANSWERED GIVES THE ADJUDICATION BACK; AN ANSWERED ONE DOES
// NOT. That is the whole of what this decides, and it decided it inside a
// two-hundred-line function until #260 gave it a name.
func TestADivisionsRefusalRefundsTheTiebreakOnlyWhereNobodyAnswered(t *testing.T) {
	unanswered := divisionRefusal{said: divisionUnadjudicated(), why: "the reviewer did not answer"}
	answered := divisionRefusal{said: "not split", why: "refused: the parts are one job"}

	// A BELOW-FLOOR ASK NOBODY ANSWERED. The adjudication was bought and never
	// used, so it comes back and the record says the review never happened.
	node := adjudicatingTestNode(t)
	line := journalDivision{}
	said, person := settleDivisionRefusal(node, unanswered, true, &line)
	if said != divisionUnadjudicated() || person != "" {
		t.Fatalf("said %q / person %q, want the worker told nothing was decided", said, person)
	}
	if line.Decision != divisionRefusedUnreviewed {
		t.Fatalf("the record says %q, want it to say the review never answered", line.Decision)
	}
	if !node.takeTiebreak() {
		t.Fatal("the adjudication was not given back")
	}

	// AN ANSWERED REFUSAL ON THE SAME ROAD KEEPS IT SPENT.
	node = adjudicatingTestNode(t)
	line = journalDivision{}
	said, person = settleDivisionRefusal(node, answered, true, &line)
	if said != "not split" || person != "" {
		t.Fatalf("said %q / person %q, want the reviewer's own answer", said, person)
	}
	if line.Decision != divisionRefusedReview {
		t.Fatalf("the record says %q, want it to say the review refused", line.Decision)
	}
	if node.takeTiebreak() {
		t.Fatal("an answered refusal gave the adjudication back")
	}

	// AND A DIVISION THAT WAS NEVER BELOW THE FLOOR HAS NO ADJUDICATION IN IT AT
	// ALL, so the refusal is the ordinary one however the reviewer worded it.
	node = adjudicatingTestNode(t)
	line = journalDivision{}
	settleDivisionRefusal(node, unanswered, false, &line)
	if line.Decision != divisionRefusedReview {
		t.Fatalf("the record says %q, want the ordinary refusal", line.Decision)
	}
}

// AND THE ONE REFUSAL THAT IS NOT ABOUT THE DIVISION comes back with the
// person's own job on it, with the record's prefix taken off.
func TestARefusalAboutTheWorkHandsBackThePersonsOwnJob(t *testing.T) {
	node := adjudicatingTestNode(t)
	line := journalDivision{}
	said, person := settleDivisionRefusal(node, divisionRefusal{
		said:   "not split",
		why:    "refused: only the repository's owner can approve this",
		nobody: true,
	}, false, &line)

	if said != "not split" {
		t.Fatalf("said %q, want the reviewer's sentence to the worker", said)
	}
	if person != "only the repository's owner can approve this" {
		t.Fatalf("person = %q, want the reason with the record's prefix off", person)
	}
	if line.Decision != divisionRefusedNobody {
		t.Fatalf("the record says %q, want it to say no worker can do this", line.Decision)
	}
}

// adjudicatingTestNode is a node that has spent its one tiebreak, which is the
// state every case above starts from.
func adjudicatingTestNode(t *testing.T) *TaskNode {
	t.Helper()
	node := loneTestNode(t, "split the sweep")
	if !node.takeTiebreak() {
		t.Fatal("a fresh node would not give up its one adjudication")
	}
	return node
}

// THE F31 CASE THE CONFLICT TEST CANNOT REACH: a merge that exits zero while the
// branch never fastens (a stale ref, a fetch that did not move the tip). The notice
// reads branchFastened, so the unit of truth is that check itself: a merged branch is
// fastened, an un-merged branch is not, and a missing root or branch is never home.
func TestBranchFastenedIsTrueOnlyWhenTheBranchIsOnHEAD(t *testing.T) {
	repo := newTestRepo(t)

	// A branch cut and committed but NOT merged is not fastened.
	mustGit(t, repo, "checkout", "-b", "task/unmerged")
	writeFile(t, filepath.Join(repo, "b.txt"), "two\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "node work")
	mustGit(t, repo, "checkout", "work")
	if branchFastened(repo, "task/unmerged") {
		t.Fatalf("an un-merged branch reported fastened")
	}

	// Merged into HEAD, it is fastened.
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "merge", "--no-edit", "task/unmerged")
	if !branchFastened(repo, "task/unmerged") {
		t.Fatalf("a merged branch reported unfastened")
	}

	// No root, or no branch, is never a landing.
	if branchFastened("", "task/unmerged") || branchFastened(repo, "") {
		t.Fatalf("an empty root or branch reported fastened")
	}
}
