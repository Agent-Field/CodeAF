package session

// WHAT EVERY WORKER IS TOLD IT IS, AND WHAT ONLY SOME OF THEM CAN DO.
//
// prompts/task.md used to be one page rendered on ONE predicate: may this
// worker hand parts of its work out. A node standing on the floor of the tree
// answers no, so it was given none of it — not the fan-out half it genuinely
// cannot use, and not the half that says there is nobody here to ask, that a
// direction arriving mid-work is the person, what a report is for, or that it
// writes in its own copy and nowhere else. What it read instead was
// prompts/system.md alone, which opens by telling it there is a person here to
// talk to.
//
// So the page is split by the question it answers: prompts/worker.md is the
// ROLE and renders for every task node ([Config.isWorker]); prompts/revise.md
// and prompts/fanout.md are ABILITIES and render on the predicates that put
// their verbs on the belt. prompt_belt_test.go is the other half of this —
// it proves no page names a tool its shape's belt does not carry.

import (
	"strings"
	"testing"
)

// pageFor renders the system prompt one shape actually reads.
func pageFor(t *testing.T, build func(*Config)) string {
	t.Helper()
	config := Config{Workspace: t.TempDir(), Model: "test/model"}
	build(&config)
	return renderSystem(config)
}

// roleSentences are the worker's own sentences: who it is, that nobody is here
// to answer it, that directions arrive, what its report carries, and where it
// may write. Every task node reads all of them.
var roleSentences = []string{
	"# You are a task: one job, worked out loud",
	"YOU OWN THIS OUTCOME",
	"DIRECTIONS STILL REACH YOU",
	"## Your report",
	"IT CARRIES THE SUBSTANCE, NOT THE EVIDENCE TRAIL",
	"YOU WRITE IN YOUR OWN COPY AND NOWHERE ELSE ON THE MACHINE",
	"you never run `git add`",
}

// fanOutSentences are the ones that are only true of a worker that can hand
// parts of its own work out.
var fanOutSentences = []string{
	"## Breaking the work up",
	"NEVER SHARD WORK THAT FITS IN YOUR OWN HANDS",
	"You may hand out at most",
	"is your window onto them",
}

// TestAFloorNodeIsToldWhatItIsAndNotWhatItCannotDo is the defect's own
// acceptance, read off the page a floor worker actually gets.
func TestAFloorNodeIsToldWhatItIsAndNotWhatItCannotDo(t *testing.T) {
	graph := graphForShape(t)
	floor := pageFor(t, func(config *Config) {
		config.InTask = true
		config.tasker = graph
		config.taskID = 7
		config.taskDepth = taskDepthLimit
	})

	for _, want := range roleSentences {
		if !strings.Contains(floor, want) {
			t.Errorf("a floor node's page never says %q, so the narrowest worker in the tree is told nothing about its own job", want)
		}
	}
	// AND THE VERB IT DOES HAVE. A floor node was handed the graph, so its
	// assignment can be moved by the person and the page that says how belongs
	// to it (assignment_tool.go).
	if !strings.Contains(floor, "revise_assignment") {
		t.Error("a floor node is never told how a person's change of mind reaches its done-condition")
	}
	// AND NOT ONE SENTENCE ABOUT A VERB IT WILL BE ANSWERED `Unknown tool` FOR.
	for _, absent := range fanOutSentences {
		if strings.Contains(floor, absent) {
			t.Errorf("a floor node's page says %q, which is about handing work out with a tool it does not have", absent)
		}
	}
}

// AND A NODE THAT MAY FAN OUT READS BOTH, in that order: what it is, then what
// it can do about it.
func TestANodeThatMayFanOutKeepsTheWholePage(t *testing.T) {
	graph := graphForShape(t)
	page := pageFor(t, func(config *Config) {
		config.InTask = true
		config.tasker = graph
		config.taskID = 3
		config.taskDepth = 1
	})
	for _, want := range append(append([]string{}, roleSentences...), fanOutSentences...) {
		if !strings.Contains(page, want) {
			t.Errorf("the page a fan-out worker reads lost %q in the split", want)
		}
	}
	role, fan := strings.Index(page, roleSentences[0]), strings.Index(page, fanOutSentences[0])
	if role < 0 || fan < 0 || fan < role {
		t.Errorf("the abilities are printed before the role (role at %d, fan-out at %d)", role, fan)
	}
}

// AND A WORKER HANDED NO GRAPH IS TOLD ITS ROLE AND NOT A VERB IT LACKS. An
// orchestrate run's node has an id and no tasker, so it has no assignment road:
// `revise_assignment` is off its belt (assignment_tool.go) and the page that
// teaches it comes off with the tool.
func TestAWorkerWithNoGraphIsToldItsRoleWithoutTheAssignmentVerb(t *testing.T) {
	page := pageFor(t, func(config *Config) {
		config.InTask = true
		config.taskID = 4
	})
	for _, want := range roleSentences {
		if !strings.Contains(page, want) {
			t.Errorf("a worker with no graph is not told %q", want)
		}
	}
	if strings.Contains(page, "revise_assignment") {
		t.Error("a worker with no graph is told to call `revise_assignment`, which is not on its belt")
	}
}

// AND WHAT IS NOT A WORKER IS NOT HANDED A WORKER'S PAGE. InTask is the posture
// — nobody is there to ask — and a standing check's probe runs in it without
// having a brief, an acceptance, a report anybody reads or a branch that comes
// home (standing_run.go). Telling it to own an outcome would be the same shape
// of lie the floor node was being told.
func TestSomethingThatIsNotATaskNodeIsNotToldItIsOne(t *testing.T) {
	probe := pageFor(t, func(config *Config) { config.InTask = true })
	for _, absent := range []string{"# You are a task: one job, worked out loud", "YOU OWN THIS OUTCOME"} {
		if strings.Contains(probe, absent) {
			t.Errorf("a probe that is not a task node reads %q", absent)
		}
	}
}
