package revision

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// THE GATE JUDGES THE CHANGE, NOT A SENTENCE ABOUT IT.
//
// A worker that drives a whole pipeline behind a process boundary used to hand
// the gate one line, and on the ending that carried no line at all it handed
// "the coding run ended without saying how it went". The gate then judged a
// void, which it did differently every time it was asked. The account arrives
// as rows — the diff and the verification story, in the same grammar as the run
// tail beside them — so the judge is reading what happened rather than a claim
// about it.
func TestTheEvidenceBlockCarriesTheWorkersOwnAccount(t *testing.T) {
	account := &exec.Account{
		Files: []exec.FileChange{
			{Path: "internal/parser/commas.go", Change: exec.ChangeChanged, Added: 24, Removed: 3},
			{Path: "internal/parser/commas_test.go", Change: exec.ChangeAdded, Added: 41},
		},
		Checks: []exec.Check{
			{Command: "go build ./...", Kind: "build", Passed: true},
			{Command: "go test ./...", Kind: "test", Passed: true, Tail: "ok  aforge 1.2s"},
		},
	}
	block := Evidence{Account: account, Observed: true}.block(ctxbudget.Budget{})
	for _, want := range []string{
		"2 files changed, +65 -3 lines",
		"changed internal/parser/commas.go (+24 -3)",
		"added internal/parser/commas_test.go (+41 +0)",
		"passed: go test ./...",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the record omits %q:\n%s", want, block)
		}
	}
	// An account is a record like the others: holding one, the block is never
	// the sentence that says nothing was exercised.
	if strings.Contains(block, UnexercisedRecord) {
		t.Fatalf("a run with a diff and a green suite read as unexercised:\n%s", block)
	}
	// A red suite is stated as red. The account is evidence and never an
	// argument, so nothing here softens it.
	red := Evidence{Observed: true, Account: &exec.Account{Checks: []exec.Check{
		{Command: "go test ./...", Tail: "--- FAIL: TestTrailing\nmore"},
	}}}.block(ctxbudget.Budget{})
	if !strings.Contains(red, "failed: go test ./... — --- FAIL: TestTrailing") {
		t.Fatalf("a red check did not reach the judge as red:\n%s", red)
	}
	// And a worker with no account of itself manufactures none.
	if got := (Evidence{Account: &exec.Account{}}).block(ctxbudget.Budget{}); got != "" {
		t.Fatalf("an empty account rendered %q", got)
	}
}
