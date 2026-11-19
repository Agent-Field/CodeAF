package session

import (
	"os"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// EVERY OTHER TEST IN THIS PACKAGE RUNS UNDER A PATIENCE OF ONE, in the
// environment as well as in the process-wide figure. Their agents talk to
// scripted completers and loopback servers, which are "local endpoints" to the
// classifier, and a local endpoint is given ten times the hosted wait under
// `auto`; the ladders in modelhop_test.go size their clock from the hosted
// give-up and assert how many requests it buys, so the figure they describe is
// the hosted one. The variable and not only the number, because every
// config.Load a test runs installs the profile's row over the process figure,
// and the environment pin is what that row yields to.
//
// Tests about the stretch itself set their own with [usePatience].
func init() {
	os.Setenv("CODEAF_RESPONSE_PATIENCE", "1")
	lanes.UseResponsePatience(1)
}

func usePatience(t *testing.T, factor float64) {
	t.Helper()
	lanes.UseResponsePatience(factor)
	t.Cleanup(func() { lanes.UseResponsePatience(1) })
}

// CONTRACT: the turn gives a model as long as the transport gives its calls, so
// a conversation does not hop to another model while the first is still, by the
// transport's own reckoning, working — and a hosted model keeps the ninety
// seconds it always had.
func TestTheTurnsGiveUpIsInThePatienceOfTheModelItIsAsking(t *testing.T) {
	asking := func(base string) *Agent {
		t.Helper()
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.BaseURL = base })
		return agent
	}
	hosted := asking("https://openrouter.ai/api/v1")
	local := asking("http://localhost:11434/v1")
	const model = "test/model"

	usePatience(t, 1)
	base := hosted.giveUp()
	if base != 90*time.Second {
		t.Fatalf("the watched turn's give-up = %v, want the measured 90s", base)
	}
	for name, agent := range map[string]*Agent{"hosted": hosted, "local": local} {
		if got := agent.giveUpOn(model); got != base {
			t.Errorf("under 1, %s: give-up %v, want %v (a hosted turn must not move)", name, got, base)
		}
	}

	usePatience(t, 0)
	if got := hosted.giveUpOn(model); got != base {
		t.Errorf("auto, hosted: give-up %v, want exactly %v", got, base)
	}
	if got := local.giveUpOn(model); got != 15*time.Minute {
		t.Errorf("auto, local: give-up %v, want ten times the hosted ninety seconds", got)
	}

	usePatience(t, 3)
	for name, agent := range map[string]*Agent{"hosted": hosted, "local": local} {
		if got := agent.giveUpOn(model); got != 270*time.Second {
			t.Errorf("under 3, %s: give-up %v, want 4m30s on both", name, got)
		}
	}
}
