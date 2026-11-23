package session

import (
	"path/filepath"
	"testing"
)

func TestFoldedRunJournalRetainsSpendWithoutBillableDigestRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = path
		c.usageLedger = filepath.Join(t.TempDir(), UsageLedgerName)
	})
	fold := &beltFold{agent: a}
	fold.charge(RunCharge{Model: "test/model", TokensIn: 100, TokensOut: 10, USD: .05})
	fold.total(.08)
	d, err := ReadDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Usage) != 0 {
		t.Fatalf("folded work became %d billable digest rows", len(d.Usage))
	}
	usd, tokens := d.Spend()
	if usd != .08 || tokens != 110 {
		t.Fatalf("fold lost conversation totals: usd=%g tokens=%d", usd, tokens)
	}
}
