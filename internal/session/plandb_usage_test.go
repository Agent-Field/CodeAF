package session

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// A TASK'S PAGE CARRIES THE MODEL AND THE TOKENS ITS LEDGER RECORDS: the model
// it spent most through, and every token read and written. A task the ledger
// names nothing for carries neither, so a room draws no blank and no zero.
func TestPlanTaskPageCarriesTheLedgersModelAndTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a",
		plandb.TaskSpec{ID: "alpha", Title: "Alpha", Description: "the work order"},
		plandb.TaskSpec{ID: "beta", Title: "Beta", Description: "no spend yet"},
	)
	store, err := plandb.Open(path, "", planRootID, "", "")
	if err != nil {
		t.Fatalf("reopen the store to write spend: %v", err)
	}
	for _, charge := range []struct {
		model   string
		usd     float64
		in, out int
	}{
		{"deepseek/deepseek-v4-flash", 0.03, 9000, 1000},
		{"qwen/qwen3-coder", 0.01, 2000, 400},
	} {
		if err := store.AddSpend("alpha", charge.model, "worker", charge.usd, charge.in, charge.out); err != nil {
			t.Fatalf("write spend: %v", err)
		}
	}
	_ = store.Close()

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	armPlanStore(t, agent, path, "chat-a")
	page, ok := agent.PlanTaskPage("t-alpha")
	if !ok {
		t.Fatal("the chat's own task answered no page")
	}
	if page.Row.Model != "deepseek/deepseek-v4-flash" || page.Row.Tokens != 12400 {
		t.Fatalf("page row model = %q tokens = %d, want the ledger's deepseek/deepseek-v4-flash and 12400", page.Row.Model, page.Row.Tokens)
	}
	bare, ok := agent.PlanTaskPage("t-beta")
	if !ok {
		t.Fatal("the chat's second task answered no page")
	}
	if bare.Row.Model != "" || bare.Row.Tokens != 0 {
		t.Fatalf("a task the ledger names nothing for carries model %q tokens %d", bare.Row.Model, bare.Row.Tokens)
	}
}
