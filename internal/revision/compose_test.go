package revision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// A GATE THAT FAILS THE WORDS MUST NOT BUY THE WORK AGAIN.
//
// The measured failure: a leaf that changes the workspace landed its change, the
// project's own suite went green, and the delivery gate failed the deliverable's
// PROSE. The one revision round that bought re-ran the whole leaf into a tree
// where the fix was already in — 23 model calls, zero edits, 80% of the leaf's
// bill — and then its empty outcome replaced the pass that had done the work.
//
// The decision that stops it is three structural facts and no reading of the
// critique's words, which is what makes it a mechanism rather than a judgement
// wearing one.

type mutatingWorker struct{ exec.Executor }

func (mutatingWorker) Subharness() string { return "digger" }
func (mutatingWorker) Mutates() bool      { return true }

type prosaicWorker struct{ exec.Executor }

func (prosaicWorker) Subharness() string { return "linear" }

func landedAccount() *exec.Account {
	return &exec.Account{
		Files:  []exec.FileChange{{Path: "internal/pricing/discount.go", Change: exec.ChangeChanged, Added: 6, Removed: 2}},
		Range:  exec.Range{Base: "1111111111111111", Head: "2222222222222222"},
		Checks: []exec.Check{{Command: "go test ./...", Passed: true}},
		Final:  "the discount was applied before the floor rather than after it",
	}
}

func TestOnlyFinishedLandedVerifiedWorkSkipsTheWorker(t *testing.T) {
	landed := &exec.Outcome{Account: landedAccount()}
	if !Composable(mutatingWorker{}, landed) {
		t.Fatal("a landed, green change by a mutating worker would still re-run the whole engine")
	}
	// A worker whose deliverable is prose is always re-run: the thing that was
	// judged wrong is the thing being remade.
	if Composable(prosaicWorker{}, landed) {
		t.Fatal("a prose worker's revision round was replaced by a composition")
	}
	// Narration with nothing derived behind it is not evidence that anything is
	// in the tree, so it must not skip the worker.
	narrated := &exec.Outcome{Account: &exec.Account{
		Files:  landedAccount().Files,
		Checks: landedAccount().Checks,
	}}
	if Composable(mutatingWorker{}, narrated) {
		t.Fatal("a change nobody derived from the repository was treated as landed work")
	}
	// A red suite is unsettled work whatever is on disk.
	red := &exec.Outcome{Account: landedAccount()}
	red.Account.Checks = []exec.Check{{Command: "go test ./...", Passed: false}}
	if Composable(mutatingWorker{}, red) {
		t.Fatal("work whose own checks did not settle skipped the worker")
	}
	// And a leaf that ran no checks at all says nothing either way, which is
	// not the same as saying everything passed.
	unchecked := &exec.Outcome{Account: landedAccount()}
	unchecked.Account.Checks = nil
	if Composable(mutatingWorker{}, unchecked) {
		t.Fatal("a leaf that verified nothing was read as verified")
	}
	if Composable(mutatingWorker{}, nil) {
		t.Fatal("a missing outcome composed")
	}
}

// The gate is handed the change's own text, not a list of names. This is the
// half that could have convicted the false root cause: the deliverable claimed a
// logic error in pricing, and everything the gate held said which files moved.
func TestTheGateIsHandedTheChangeItself(t *testing.T) {
	patch := filepath.Join(t.TempDir(), "node.patch")
	body := "--- a/internal/pricing/discount.go\n+++ b/internal/pricing/discount.go\n" +
		"@@\n-\treturn max(price-discount, floor)\n+\treturn max(price, floor) - discount\n"
	if err := os.WriteFile(patch, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := Evidence{Observed: true, Account: landedAccount(), Patch: patch}
	block := evidence.block(ctxbudget.Budget{})
	if !strings.Contains(block, "return max(price, floor) - discount") {
		t.Fatalf("the record does not carry the change's own lines:\n%s", block)
	}
	if !strings.Contains(block, "git diff 111111111111..222222222222") {
		t.Fatalf("the record does not place the change in the repository's history:\n%s", block)
	}

	// A patch that has been swept away between the run and the review renders
	// nothing at all. An unreadable diff must never be able to read as an empty
	// one, which is the direction that acquits.
	gone := Evidence{Observed: true, Account: landedAccount(), Patch: patch + ".missing"}
	if strings.Contains(gone.block(ctxbudget.Budget{}), "The change itself") {
		t.Fatal("a patch that is not on disk was rendered as though it were")
	}
}
