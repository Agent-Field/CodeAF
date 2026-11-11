package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestRealTurnsPersistDistinctRunIdentityWithEveryBilledStep(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, UsageLedgerName)
	journal := filepath.Join(dir, "session.jsonl")
	ids := []string{}
	answer := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		id := runIdentity(ctx)
		if id == "" {
			t.Error("provider received no immutable run identity")
		}
		ids = append(ids, id)
		return textResponse("done"), nil
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{answer, answer}}, func(c *Config) { c.usageLedger = ledger; c.SessionFile = journal })
	collect(t, mustSubmit(t, agent, "first"))
	collect(t, mustSubmit(t, agent, "second"))
	FlushUsage()
	rows, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("rows=%+v ids=%v", rows, ids)
	}
	for i, row := range rows {
		if row.Run != ids[i] {
			t.Fatalf("row %d attributed to %q instead of %q", i, row.Run, ids[i])
		}
	}
	raw, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	runs := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		var v struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		_ = json.Unmarshal([]byte(line), &v)
		if v.Type == "usage_run" {
			runs = append(runs, v.ID)
		}
	}
	if len(runs) != 2 || runs[0] != ids[0] || runs[1] != ids[1] {
		t.Fatalf("journal identities %v do not match actual turns %v", runs, ids)
	}
}

func TestNewExecutionDoesNotInheritItsParentsRunIdentity(t *testing.T) {
	parent := withRunIdentity(context.Background())
	child := withRunIdentity(parent)
	if runIdentity(parent) == "" || runIdentity(child) == "" || runIdentity(parent) == runIdentity(child) {
		t.Fatal("new execution inherited old identity")
	}
	if runIdentity(context.Background()) != "" {
		t.Fatal("legacy execution guessed an identity")
	}
}

func TestLateUnattributedMoneyIsNotAssignedToTheCurrentRun(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	a, _ := ledgerAgent(t, ledger)
	old := withRunIdentity(context.Background())
	newer := withRunIdentity(old)
	var turn Usage
	a.addUsage(&turn, billedCall("test/model", 3, 1, .1), "test/model", "", laneFacts{}, provider.BillingFact{}, runIdentity(old))
	a.bank(bankedCall{used: Usage{Input: 2, Output: 1, CostUSD: .2, Calls: 1}, model: "test/model", ledger: true, late: true})
	a.addUsage(&turn, billedCall("test/model", 4, 2, .3), "test/model", "", laneFacts{}, provider.BillingFact{}, runIdentity(newer))
	FlushUsage()
	rows, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Run != runIdentity(old) || rows[1].Run != "" || rows[2].Run != runIdentity(newer) {
		t.Fatalf("incorrect run attribution %+v", rows)
	}
}
