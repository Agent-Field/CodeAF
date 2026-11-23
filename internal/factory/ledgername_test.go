package factory_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// EVERY JOB HAS ITS OWN LEDGER NAME. A CI item's reference is the word "ci",
// so two CI jobs in one repository must still be named apart on the ledger.
func TestEveryJobHasItsOwnLedgerName(t *testing.T) {
	items := []factory.Item{
		{ID: 1, Repo: "Acme/Web", Kind: factory.KindIssue, Num: 7},
		{ID: 2, Repo: "acme/web", Kind: factory.KindCI},
		{ID: 3, Repo: "acme/web", Kind: factory.KindCI},
	}
	if got := factory.LedgerName(items[0]); got != "acme/web#7" {
		t.Errorf("issue ledger name = %q", got)
	}
	if a, b := factory.LedgerName(items[1]), factory.LedgerName(items[2]); a == b {
		t.Errorf("two CI jobs share the ledger name %q", a)
	}
}
