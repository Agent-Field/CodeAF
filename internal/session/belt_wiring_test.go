package session

import (
	"context"
	"testing"

	configpkg "github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// THE BIG HANDS, and whether the conversation actually has them.
//
// build_harness, list_harnesses and propose_task are how work leaves a turn: a
// recipe worth repeating, the list that says whether one already exists, and one
// self-contained job — wide or not, because `wide` starts one worker that hands
// the parts out itself. Each is ABSENT-NOT-BROKEN by design (tools.go) — a
// missing seam takes the verb off the belt rather than leaving it there to fail
// — which is exactly why a wiring mistake in a shipping door is silent: the
// model simply never has the verb, and nothing anywhere says so.
//
// THERE WERE FOUR, and the fourth was `run_adaptive`. It is off the belt for
// good now (tools_harness.go): a chat turn takes ONE ROAD for ordinary work, and
// the planner engine is reached only by somebody naming it. The adaptive runner
// is still wired into the config below, exactly as the shipping door wires it,
// so that this file keeps proving the absence is a decision rather than a seam
// somebody forgot to fill.
//
// So this is the test that fails when one of the surviving hands is not there.
// The config below is the SHAPE the v3 door assembles (cmd/aforge's chatv3.go):
// a registry to design into, a runner to run what is designed, an adaptive
// runner, and a surface that answers questions.

// v3ShapedAgent is a conversation configured the way the interactive door
// configures one.
func v3ShapedAgent(t *testing.T) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.AskConsent = true
		config.BashBackgroundAfterSeconds = configpkg.DefaultBashBackgroundAfter
		config.HarnessStore = subharness.At(t.TempDir())
		config.RunHarness = func(context.Context, string, string, string, func(subharness.Trail)) (string, subharness.Usage, error) {
			return "", subharness.Usage{}, nil
		}
		config.OrchestrateRunner = func(context.Context, string, string, float64) (string, error) {
			return "", nil
		}
	})
	return agent
}

func TestTheChatBeltCarriesTheBigHands(t *testing.T) {
	agent := v3ShapedAgent(t)
	for _, want := range []string{"build_harness", "list_harnesses", "propose_task"} {
		if !agent.hasTool(want) {
			t.Fatalf("%s is not on the belt, so the model does not have the verb", want)
		}
	}
	// AND NOT THE ONE THAT IS GONE, on the fully-wired shape where it would
	// otherwise appear. This is the assertion that would have caught the verb
	// coming back.
	if agent.hasTool("run_adaptive") {
		t.Fatal("run_adaptive is on the belt: a chat turn can open a planned run again")
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
			name: "nobody watching to answer the card",
			gone: []string{"build_harness", "list_harnesses"},
			drop: func(config *Config) { config.AskConsent = false },
		},
		{
			// A NODE WITH NO GRAPH TO ADMIT INTO. A task may hand pieces of its
			// own work out (task.go's fan-out law) and does it by admitting into
			// the CONVERSATION's graph, so an agent inside a task that was handed
			// none — an adaptive run's worker, an auditor — has no verb at all.
			// The floor of the tree is the other half, and task_nest_test.go pins
			// it beside the prompt that has to agree with it.
			name: "inside a task node with no graph to admit into",
			gone: []string{"propose_task", "tasks"},
			drop: func(config *Config) { config.InTask = true },
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
				config.AskConsent = true
				config.HarnessStore = subharness.At(t.TempDir())
				config.RunHarness = func(context.Context, string, string, string, func(subharness.Trail)) (string, subharness.Usage, error) {
					return "", subharness.Usage{}, nil
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
