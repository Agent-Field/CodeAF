package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// The actual acceptance writer's structured field reaches the inline verifier
// without creating a task or mining a command from the acceptance sentence.
func TestTheSessionAcceptanceFreezesItsExplicitChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	const ask = "port the parser"
	const check = "go test ./..."
	payload, err := json.Marshal(map[string]any{
		"work": true, "goal": ask, "acceptance": "the parser accepts all fixtures",
		"checks": []string{check}, "why": "verify the parser",
	})
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText(string(payload))}}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
		c.SessionFile = path
	})
	steward := agent.steward()
	steward.hear(ask)
	agent.openAcceptance(context.Background(), nil)
	if got := agent.sessionChecks(); !slices.Equal(got, []string{check}) {
		t.Fatalf("structured checks did not reach inline verification: %v", got)
	}
	if agent.tasker() != nil {
		t.Fatal("declaring a session verifier invented a task graph")
	}
	copy := steward.declaredChecks()
	copy[0] = "false"
	if steward.setAcceptanceContract(ask, "a replacement", []string{"false"}) {
		t.Fatal("a later contract replaced the frozen verifier")
	}
	if got := steward.declaredChecks(); !slices.Equal(got, []string{check}) {
		t.Fatalf("the frozen declaration was mutated: %v", got)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	var recorded []string
	for _, entry := range journaledEntries(t, path, "principal") {
		if entry.Principal != nil && entry.Principal.Event == "acceptance" {
			recorded = entry.Principal.Checks
		}
	}
	if !slices.Equal(recorded, []string{check}) {
		t.Fatalf("acceptance receipt lost its declared verifier: %v", recorded)
	}

	// Principal receipts are historical evidence. Reopening creates a fresh
	// principal, as it already did for acceptance; no old command is restored
	// as permission for whatever the new unattended ask turns out to be.
	resumed, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
		c.SessionFile = path
	})
	resumed.steward().hear("explain the parser without running commands")
	if got := resumed.sessionChecks(); len(got) != 0 {
		t.Fatalf("a historical acceptance granted a new ask execution rights: %v", got)
	}
}

func TestSessionChecksRequireTheSameAskAndAValidDeclaration(t *testing.T) {
	steward := NewSteward("port the parser", Budget{Wall: time.Hour}, nil)
	if steward.setAcceptanceContract("deploy the service", "deployed", []string{"touch deployed"}) {
		t.Fatal("a contract for a different ask was accepted")
	}
	if steward.Acceptance() != "" || len(steward.declaredChecks()) != 0 {
		t.Fatal("the stale declaration changed the goal's contract")
	}
	unnamed := NewSteward("", Budget{Wall: time.Hour}, nil)
	unnamed.setAcceptanceContract("", "a missing ask", []string{"touch unknown"})
	if len(unnamed.declaredChecks()) != 0 {
		t.Fatal("a declaration without an ask granted execution rights")
	}
	for _, checks := range [][]string{
		{"true", "touch one; touch two"},
		{"rm -rf /"},
	} {
		owner := NewSteward("port the parser", Budget{Wall: time.Hour}, nil)
		if !owner.setAcceptanceContract(owner.Ask(), "the parser works", checks) {
			t.Fatal("an invalid verifier prevented the acceptance being recorded")
		}
		if got := owner.declaredChecks(); len(got) != 0 {
			t.Fatalf("invalid declaration granted commands: %v", got)
		}
	}
}
