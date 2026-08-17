package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// THE FOUR BIG HANDS, and whether the conversation actually has them.
//
// build_harness, list_harnesses, run_adaptive and propose_task are how work
// leaves a turn: a recipe worth repeating, the list that says whether one
// already exists, a many-part goal, and one self-contained job. Each is
// ABSENT-NOT-BROKEN by design (tools.go) — a missing seam takes the verb off the
// belt rather than leaving it there to fail — which is exactly why a wiring
// mistake in a shipping door is silent: the model simply never has the verb, and
// nothing anywhere says so.
//
// So this is the test that fails when one of them is not there. The config below
// is the SHAPE the v3 door assembles (cmd/aforge's chatv3.go): a registry to
// design into, a runner to run what is designed, an adaptive runner, and a
// surface that answers questions. cmd/aforge's own chatv3_belt_test.go pins that
// the door fills those four seams; this pins what filling them buys.

// v3ShapedAgent is a conversation configured the way the interactive door
// configures one.
func v3ShapedAgent(t *testing.T) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.AskConsent = true
		config.HarnessStore = subharness.At(t.TempDir())
		config.RunHarness = func(context.Context, string, string, string) (string, error) {
			return "", nil
		}
		config.OrchestrateRunner = func(context.Context, string, string, float64) (string, error) {
			return "", nil
		}
	})
	return agent
}

func TestTheChatBeltCarriesTheFourBigHands(t *testing.T) {
	agent := v3ShapedAgent(t)
	for _, want := range []string{"build_harness", "list_harnesses", "run_adaptive", "propose_task"} {
		if !agent.hasTool(want) {
			t.Fatalf("%s is not on the belt, so the model does not have the verb", want)
		}
	}
}

// AND EACH ONE NAMES THE SEAM IT NEEDS. A build with a seam missing is a build
// with that verb absent — never one where the model reaches for it and is told
// no — so the gate is asserted per hand rather than as one lump.
func TestEachBigHandIsAbsentWhenItsSeamIs(t *testing.T) {
	for _, want := range []struct {
		name string
		gone []string
		drop func(*Config)
	}{
		{
			name: "no registry to design into",
			gone: []string{"build_harness", "list_harnesses"},
			drop: func(config *Config) { config.HarnessStore = nil },
		},
		{
			name: "no runner to run what is designed",
			gone: []string{"build_harness", "list_harnesses"},
			drop: func(config *Config) { config.RunHarness = nil },
		},
		{
			name: "no adaptive runner",
			gone: []string{"run_adaptive"},
			drop: func(config *Config) { config.OrchestrateRunner = nil },
		},
		{
			name: "nobody watching to answer the card or the gate",
			gone: []string{"build_harness", "list_harnesses", "run_adaptive"},
			drop: func(config *Config) { config.AskConsent = false },
		},
		{
			name: "inside a task node",
			gone: []string{"propose_task"},
			drop: func(config *Config) { config.InTask = true },
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
				config.AskConsent = true
				config.HarnessStore = subharness.At(t.TempDir())
				config.RunHarness = func(context.Context, string, string, string) (string, error) {
					return "", nil
				}
				config.OrchestrateRunner = func(context.Context, string, string, float64) (string, error) {
					return "", nil
				}
				want.drop(config)
			})
			for _, name := range want.gone {
				if agent.hasTool(name) {
					t.Fatalf("%s is on the belt with %s", name, want.name)
				}
			}
		})
	}
}
