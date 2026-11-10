package session

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestPostSealChargesStampTheHomeTotal(t *testing.T) {
	for _, kind := range []string{"receipt", "hedge", "remote fold"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "project", "aaaa000000000001")
			agent, _ := newTestAgent(t, quietCompleter{}, func(c *Config) { c.SessionFile = filepath.Join(dir, "transcript.jsonl"); c.Place = Place{Dir: dir} })
			collect(t, mustSubmit(t, agent, "seal the turn"))
			agent.SettleWrites()
			if kind == "remote fold" {
				if err := agent.foldRemoteCharge("attempt:1", RunCharge{Model: "m", TokensIn: 10, TokensOut: 5, USD: .00024304}); err != nil {
					t.Fatal(err)
				}
			} else {
				agent.reconciled(provider.Reconciled{Found: true, Hedged: kind == "hedge", Billed: provider.Billed{Model: "m", PromptTokens: 10, CompletionTokens: 5, Cost: .00024304}})
			}
			agent.SettleWrites()
			world := ReadWorld(root)
			if len(world.Projects) != 1 || len(world.Projects[0].Sessions) != 1 {
				t.Fatalf("Home lost the conversation: %+v", world)
			}
			row := world.Projects[0].Sessions[0]
			used := agent.Usage()
			if row.Spend != used.CostUSD || row.Tokens != used.Input+used.Output {
				t.Fatalf("Home=%g/%d cost=%g/%d", row.Spend, row.Tokens, used.CostUSD, used.Input+used.Output)
			}
		})
	}
}
