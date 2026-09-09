package session

// WHAT EACH SHAPE ACTUALLY CARRIES, AND WHERE THE BUDGET WAS NOT LOOKING.
//
// prefixbudget_test.go bounds ONE number: the widest page plus the tool block of
// the conversation door's belt. That is the right thing to ratchet and it is not
// the whole bill. Two gaps were measured on 2026-09-05:
//
//   - IT WEIGHS [widestPage] AND NOT A RENDERED ONE. The widest page has every
//     belt fact in its present case and none of the footer — no `Project` block,
//     no `Now`, no AGENTS.md — so it is neither an upper nor a lower bound on
//     what a real agent reads. A task node's page is a different page again: it
//     carries prompts/worker.md and the fan-out page on top.
//   - IT WEIGHS ONE SHAPE. A task room pays a prefix of its own on every round of
//     every step it takes, and nothing was measuring it.
//
// So this file renders each shape the way its door does and prints what it
// carries. It is a MEASUREMENT and not a second budget: one ratchet is enough,
// and a second one would be two numbers to raise. What it asserts is only the
// ordering the design rests on — a task room's fixed prefix is smaller than the
// conversation's, because the verbs a room has no use for are not on its belt
// and the sections about them are not on its page.

import (
	"context"
	"encoding/json"
	"testing"

	configpkg "github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// prefixShape is one door's config, named as a person would name it.
type prefixShape struct {
	name  string
	build func(t *testing.T, config *Config)
}

// prefixShapes are the four the cost question is actually asked about: the
// conversation somebody types into, the same conversation with approvals off,
// and a task room at both of the depths a room can stand at.
var prefixShapes = []prefixShape{{
	name: "chat",
	build: func(t *testing.T, config *Config) {
		conversationDoor(t, config)
	},
}, {
	// `--yolo` is approvals and nothing else (principal.go), and it is measured
	// because approvals are a PREDICATE on this belt: the harness machines and
	// the saved programs need somebody who can answer a card, so a session with
	// nobody watching carries neither the verbs nor the paragraphs defining them.
	name: "yolo",
	build: func(t *testing.T, config *Config) {
		conversationDoor(t, config)
		config.AskConsent = false
	},
}, {
	// A room that may still hand parts of its own work out (task_run.go).
	name: "task room (may fan out)",
	build: func(t *testing.T, config *Config) {
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskID = 1
		config.taskDepth = 1
	},
}, {
	// And one on the floor of the tree, which is the leanest agent this package
	// builds.
	name: "task room (floor)",
	build: func(t *testing.T, config *Config) {
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskID = 2
		config.taskDepth = taskDepthLimit
	},
}}

// conversationDoor wires what cmd/aforge's interactive door wires, including the
// two seams that decide whole sections of the page: somebody watching who can
// answer a card, and a store for `stand` to leave something in.
func conversationDoor(t *testing.T, config *Config) {
	t.Helper()
	config.AskConsent = true
	config.BashBackgroundAfterSeconds = configpkg.DefaultBashBackgroundAfter
	config.HarnessStore = subharness.At(t.TempDir())
	config.RunHarness = func(context.Context, string, string, string, func(subharness.Trail)) (string, subharness.Usage, error) {
		return "", subharness.Usage{}, nil
	}
	config.OrchestrateRunner = func(context.Context, string, string, float64) (string, error) { return "", nil }
	config.Standing = &Standing{}
	config.standingItems = &fakeStanding{}
}

// TestTheFixedPrefixOfEveryShapeIsMeasured prints the bill each door pays and
// holds the one ordering the design depends on.
func TestTheFixedPrefixOfEveryShapeIsMeasured(t *testing.T) {
	measured := map[string]int{}
	for _, shape := range prefixShapes {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
			// The rendered page and not newTestAgent's fixed stand-in.
			config.System = ""
			shape.build(t, config)
		})
		definitions := agent.beltDefinitions()
		block, err := json.Marshal(definitions)
		if err != nil {
			t.Fatal(err)
		}
		page := systemTextOf(agent)
		measured[shape.name] = len(page) + len(block)
		t.Logf("%-24s %6d bytes (~%d tokens): page %d + %d tools %d",
			shape.name, len(page)+len(block), (len(page)+len(block))/4, len(page), len(definitions), len(block))
	}
	// THE ORDERING IS THE DESIGN. A room is where substantive work runs, so its
	// prefix is paid on every round of every step it takes; it must not be
	// carrying the conversation's own machinery — the standing section, the
	// settings pair, the window onto other windows — to do it.
	if measured["task room (floor)"] >= measured["chat"] {
		t.Fatalf("a task room on the floor carries %d bytes against the conversation's %d: a room is paying for verbs it does not have",
			measured["task room (floor)"], measured["chat"])
	}
}
