package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// A command in the original prose is evidence and never execution authority.
// The deterministic opening freezes the words without asking a model to turn
// part of them into a command declaration.
func TestTheSessionAcceptanceDoesNotMintChecksFromProse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	const ask = "port the parser and run go test ./..."
	const check = "go test ./..."
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
		c.SessionFile = path
	})
	steward := agent.steward()
	steward.hear(ask)
	agent.openAcceptance(context.Background(), nil)
	if got := agent.sessionChecks(); len(got) != 0 {
		t.Fatalf("request prose granted command authority: %v", got)
	}
	if completer.requests() != 0 {
		t.Fatalf("opening the request made %d model calls", completer.requests())
	}
	if agent.tasker() != nil {
		t.Fatal("declaring a session verifier invented a task graph")
	}
	if steward.setAcceptanceContract(ask, "a replacement", []string{"false"}) {
		t.Fatal("a later contract replaced the frozen verifier")
	}
	if got := steward.declaredChecks(); len(got) != 0 {
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
	if len(recorded) != 0 {
		t.Fatalf("acceptance receipt invented a declared verifier: %v", recorded)
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
