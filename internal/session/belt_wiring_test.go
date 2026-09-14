package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/subharness"
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
// The config below is the SHAPE the v3 door assembles (cmd/codeaf's chatv3.go):
// a registry to design into, a runner to run what is designed, an adaptive
// runner, and a surface that answers questions.

// v3ShapedAgent is the conversation the interactive door configures, which is
// [shippedShapeAgent] and is not a second opinion about it.
//
// IT USED TO BE A SECOND OPINION, AND THAT WAS THE MEASURED HOLE. This function
// built its own thin config — no memory store, no accounts hub, no standing
// items, no saved programs, eighteen tools — and four tests weighed the shipping
// door through it, including both prefix budgets. A machine somebody has
// finished setting up carries five more tools and fourteen kilobytes more
// prefix, so every one of those gates reported on a conversation nobody has.
// There is ONE shipped shape now and this is a name for it.
func v3ShapedAgent(t *testing.T) *Agent {
	t.Helper()
	return shippedShapeAgent(t)
}

// shippedBeltShape names the row of [beltShapes] that IS the shipping
// conversation, fully wired.
//
// THE INDEX IS NOT THE IDENTITY. `beltShapes[0]` read the fullest shape only for
// as long as nobody put a row in front of it — and what reads it is a budget
// whose whole job is to be the number for the SHIPPED belt, which would have
// gone on passing, quietly, against whatever shape had moved into slot zero.
// That is the same failure this fixture exists to repair, one layer down.
const shippedBeltShape = "a conversation that remembers"

// beltShapeNamed is one row of [beltShapes] by its own name, and fails loudly
// rather than returning a zero shape that would build an agent out of nothing.
func beltShapeNamed(t *testing.T, name string) beltShape {
	t.Helper()
	names := make([]string, 0, len(beltShapes))
	for _, shape := range beltShapes {
		if shape.name == name {
			return shape
		}
		names = append(names, shape.name)
	}
	t.Fatalf("there is no belt shape called %q; the shapes are %q", name, names)
	return beltShape{}
}

func shippedShapeAgent(t *testing.T) *Agent {
	t.Helper()
	shape := beltShapeNamed(t, shippedBeltShape)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		// The rendered page is what the budget weighs, so this shape renders its
		// own rather than taking newTestAgent's fixed one.
		config.System = ""
		shape.build(t, config)
	})
	return agent
}

func TestTheChatBeltCarriesTheBigHands(t *testing.T) {
	agent := v3ShapedAgent(t)
	for _, want := range []string{"build_harness", "list_harnesses", "propose_task"} {
		if !agent.offers(want) {
			t.Fatalf("%s is not on the belt, so the model does not have the verb", want)
		}
	}
	// AND WHERE EACH ONE LIVES, because "the model has the verb" is now two
	// answers and a wiring mistake could turn either into the other. propose_task
	// is how work leaves a turn and is carried; the two saved-procedure hands wait
	// on the `harnesses` shelf one `load_capability` call away
	// (tools_capabilities.go), which is what a lane reading this needs to know.
	if !agent.hasTool("propose_task") {
		t.Fatal("propose_task is on a shelf: the verb work leaves a turn by must be carried")
	}
	for _, shelved := range []string{"build_harness", "list_harnesses"} {
		if agent.hasTool(shelved) {
			t.Errorf("%s is carried on the belt; it belongs on the harnesses shelf", shelved)
		}
	}
	if !agent.hasTool(loadCapabilityToolName) {
		t.Fatal("something was shelved and there is no load_capability to fetch it back")
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
