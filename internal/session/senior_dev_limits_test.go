package session

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

func TestSeniorDevRunWithoutConversationLimitsHasFiniteCeilings(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	program := testPrograms("senior-dev")[0]
	run := &beltRun{delegate: &program}
	spec := a.beltRunSpec(run, "repair it")
	if spec.CostUSD != delegate.DefaultSeniorDevCostUSD || spec.Elapsed != time.Duration(delegate.DefaultSeniorDevHours)*time.Hour {
		t.Fatalf("senior-dev got cost %.2f and wall %v", spec.CostUSD, spec.Elapsed)
	}
}

func TestSeniorDevRunUsesRemainingConversationLimitsBelowDefaults(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SpendRailUSD = 5
		config.Budget = Budget{Wall: 2 * time.Hour}
	})
	a.mu.Lock()
	a.usage.CostUSD = 1
	a.mu.Unlock()
	a.startedAt = time.Time{}
	program := testPrograms("senior-dev")[0]
	spec := a.beltRunSpec(&beltRun{delegate: &program}, "repair it")
	if spec.CostUSD != 4 || spec.Elapsed != 2*time.Hour {
		t.Fatalf("senior-dev got cost %.2f and wall %v, want the remaining conversation limits", spec.CostUSD, spec.Elapsed)
	}
}

func TestTypedSeniorDevStartSaysItsEffectiveCeiling(t *testing.T) {
	double := newBeltRunDouble("submitted and verified")
	registerBeltRunEngine(t, double)
	workspace := newTestRepo(t)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "submitted and verified"}, func(config *Config) {
		config.Workspace = workspace
		config.Place = Place{Dir: t.TempDir()}
		config.Delegates = testPrograms("senior-dev")
		config.SpendRailUSD = 2
	})
	id, _, note, err := agent.StartDelegate(context.Background(), "senior-dev", "repair the parser")
	if err != nil {
		t.Fatal(err)
	}
	if note != "up to $2.00 and 3h" {
		t.Fatalf("typed start said %q", note)
	}
	<-double.entered
	endBeltRun(t, agent, double)
	if id == 0 {
		t.Fatal("the typed start returned no task")
	}
}

func TestSeniorDevManualDefaultFiguresFollowTheConstants(t *testing.T) {
	page, err := os.ReadFile("../manual/chat/senior-dev.md")
	if err != nil {
		t.Fatal(err)
	}
	want := (delegate.Ceilings{}).SeniorDev().Summary()
	if !strings.Contains(string(page), want) {
		t.Fatalf("senior-dev manual lacks %q", want)
	}
}
