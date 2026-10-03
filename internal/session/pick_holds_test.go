package session

import (
	"testing"
)

// guessingCompleter is a completer whose chain is the catalog's guess: it
// answers every model with two others, as the adapter does when the person wrote
// no fallback list.
type guessingCompleter struct {
	routedCompleter
	chain []string
}

func (c *guessingCompleter) FallbackModels(string) []string { return c.chain }

func guessed() *guessingCompleter {
	return &guessingCompleter{chain: []string{"guessed/one", "guessed/two"}}
}

// A PICK HOLDS ON A TASK. The model the person named for the work failed and
// the chain offers only the catalog's guess, so the node does not move; the same
// work with no pick still walks the chain, and a model the person WROTE as a
// fallback is still taken.
func TestAFailedTaskModelThePersonNamedIsNotReplacedByAGuess(t *testing.T) {
	agent, _ := newTestAgent(t, guessed(), nil)
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	named := &TaskNode{graph: graph, id: 1, state: TaskRunning,
		spec: taskSpec{title: "pinned", model: "vendor/chosen", modelWord: "vendor/chosen"}}
	unnamed := &TaskNode{graph: graph, id: 2, state: TaskRunning,
		spec: taskSpec{title: "open", model: "vendor/chosen"}}

	if next, moved := agent.nextNodeModel(named, "vendor/chosen"); moved {
		t.Fatalf("a task on the model its person named moved to the guess %q", next)
	}
	if next, moved := agent.nextNodeModel(unnamed, "vendor/chosen"); !moved || next != "guessed/one" {
		t.Fatalf("a task nobody named a model for moved to %q, %v; want the chain's first", next, moved)
	}

	written := &guessingCompleter{chain: []string{"written/two", "guessed/one"}}
	written2, _ := newTestAgent(t, written, func(config *Config) { config.ModelFallbacks = []string{"written/two"} })
	agent = written2
	if next, moved := agent.nextNodeModel(named, "vendor/chosen"); !moved || next != "written/two" {
		t.Fatalf("a model the person wrote as a fallback was not taken: %q, %v", next, moved)
	}
}

// A PIN ON THE ROLE HOLDS TOO. The auditor pinned in models.roles fails to give
// a verdict, and the second ask does not go to a guessed model; unpinned, it does.
func TestAPinnedAuditorIsNotReplacedByAGuess(t *testing.T) {
	agent, _ := newTestAgent(t, guessed(), func(config *Config) {
		config.RolesSource = tierSettings(map[string]string{"roles.auditor": "vendor/auditor"})
	})
	if next, moved := agent.failoverCheckerModel("vendor/auditor"); moved {
		t.Fatalf("a pinned auditor moved to the guess %q", next)
	}
	if next, moved := agent.failoverCheckerModel("vendor/other"); !moved || next != "guessed/one" {
		t.Fatalf("an unpinned checker moved to %q, %v; want the chain's first", next, moved)
	}
}
