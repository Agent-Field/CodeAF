package session

// THE ATTRIBUTION LAW HAS TWO READERS AND THIS FILE HOLDS BOTH TO THE SAME BYTES.
//
// One reader is the model, which is told the law in words on the belt and then
// types the trailer itself (beltfacts.go). The other is the harness, which
// commits a node's work without asking anybody and appends the trailer with no
// model in the loop (task_run.go's [signed]). A build where one signs and the
// other does not is a build whose git history cannot be counted — half the
// commits aforge made in somebody's name would carry no provenance at all.
//
// The bytes are pinned in internal/exec (attribution_test.go there), so what is
// asked here is that the constant is what reaches each reader — never a second
// spelling typed into this package.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// TestTheAttributionRowIsOnTheBeltOnlyWhenTheRowIsOn is the belt half. The row
// is the person's own answer, so the page must be silent for somebody who said
// no — a page that told the model about signing anyway would be spending the
// prefix, on every request of every turn, teaching a model to think about a
// thing it must not do.
func TestTheAttributionRowIsOnTheBeltOnlyWhenTheRowIsOn(t *testing.T) {
	on := promptWithBeltFacts(Config{Workspace: t.TempDir(), Model: "test/model", Attribution: true})
	if !strings.Contains(on, exec.AttributionTrailer) {
		t.Fatalf("attribution is on and the page never spells the trailer %q", exec.AttributionTrailer)
	}
	if !strings.Contains(on, exec.AttributionPullFooter) {
		t.Fatalf("attribution is on and the page never spells the pull-request footer")
	}
	// AND THE PLACES IT MUST NOT GO ARE ON THE PAGE, because that half is the
	// half a model gets wrong: a footer in the reply, a trailer in a README.
	for _, want := range []string{"commit subject", "README", "CONTRIBUTING"} {
		if !strings.Contains(on, want) {
			t.Fatalf("the page does not say attribution stays out of %q", want)
		}
	}

	off := promptWithBeltFacts(Config{Workspace: t.TempDir(), Model: "test/model"})
	for _, unwanted := range []string{exec.AttributionTrailer, exec.AttributionPullFooter, "agentfield-bot", "Co-Authored-By"} {
		if strings.Contains(off, unwanted) {
			t.Fatalf("attribution is off and the page still says %q", unwanted)
		}
	}
}

// A HAND IS TOLD NOTHING EITHER, and it is the one shape where the row is off
// with the setting ON. Its `bash` is rebuilt read-only (fork.go's [forkBelt]),
// so a hand that signed a commit would first have to be allowed to make one.
func TestAHandIsNotToldHowToSignACommitItCannotMake(t *testing.T) {
	hand := Config{Workspace: t.TempDir(), Model: "test/model", Attribution: true, inHand: true}
	if hand.signsGitWork() {
		t.Fatal("a hand carries the attribution law, and its bash cannot commit")
	}
	if page := promptWithBeltFacts(hand); strings.Contains(page, exec.AttributionTrailer) {
		t.Fatal("a hand's page spells the trailer")
	}
}

// TestALandedCommitCarriesTheTrailer is the harness half: the commit nobody was
// asked about. It is the one attribution nothing else can catch — no model saw
// this message, so a missing trailer here is silent forever.
func TestALandedCommitCarriesTheTrailer(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "d1d1d1d1d1d1d1d1", 1, "write the report")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "report.md"), "# what happened\n")

	if _, problem, _ := commitTaskWork(tree.dir, "write the report", []string{"report.md"}, true); problem != "" {
		t.Fatalf("the landing could not commit: %s", problem)
	}
	body := gitOut(t, tree.dir, "log", "-1", "--format=%B")
	if !strings.Contains(body, exec.AttributionTrailer) {
		t.Fatalf("the commit carries no trailer:\n%s", body)
	}
	// A TRAILER IS A TRAILER BLOCK, which is a blank line and then the line —
	// git reads nothing else as one, and a subject with aforge in it is exactly
	// what the law forbids.
	if !strings.HasSuffix(strings.TrimRight(body, "\n"), "\n\n"+exec.AttributionTrailer) {
		t.Fatalf("the trailer is not a trailer block:\n%q", body)
	}
	if subject := strings.SplitN(body, "\n", 2)[0]; strings.Contains(subject, "aforge <") {
		t.Fatalf("the subject carries the signature: %q", subject)
	}
}

// AND A COMMIT MADE FOR SOMEBODY WHO SAID NO CARRIES NOTHING. The row off is not
// a smaller signature; it is none.
func TestALandedCommitIsUnsignedWhenTheRowIsOff(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "d2d2d2d2d2d2d2d2", 1, "write the report")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "report.md"), "# what happened\n")

	if _, problem, _ := commitTaskWork(tree.dir, "write the report", []string{"report.md"}, false); problem != "" {
		t.Fatalf("the landing could not commit: %s", problem)
	}
	if body := gitOut(t, tree.dir, "log", "-1", "--format=%B"); strings.Contains(body, "agentfield-bot") {
		t.Fatalf("attribution is off and the commit is signed anyway:\n%s", body)
	}
}

// THE SETTING TRAVELS WITH THE WORK. A node is handed no ProfileDir and could
// not re-read the row if it wanted to (session.go's [Config.Attribution]), so a
// child that did not inherit this would sign for somebody who turned signing
// off — in a worktree, with nobody watching.
func TestATaskNodeInheritsWhetherItSigns(t *testing.T) {
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Attribution = true
	})
	// THROUGH THE PRODUCTION CONSTRUCTOR AND NEVER AROUND IT (task_divide_test.go's
	// [workerFor] says why): the defect this guards against is a field the real
	// constructor forgot to copy, and a Config literal written here would copy it
	// by hand and prove nothing.
	worker, _ := workerFor(t, session, taskSpec{
		title: "land the change", request: "land the change", brief: "land the change",
		acceptance: "it lands", depth: 1,
	})
	if !worker.config.Attribution {
		t.Fatal("the person's answer did not travel from the conversation to the worker it built")
	}
	if !worker.signsGitWork() {
		t.Fatal("a worker that inherited the row still would not sign what it lands")
	}
}
