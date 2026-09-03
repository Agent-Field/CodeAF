package session

// THE PROMPT NAMES EXACTLY THE TOOLS THE CALL CARRIES, PROVED BOTH WAYS.
//
// One embedded page is read by every shape of agent this package builds, and
// their belts are not one belt (beltfacts.go states the five families that come
// off, and the turn it cost when a floor node called `tasks` and was answered
// `Unknown tool: tasks`). So this file walks every shape, renders the page the
// way that shape's own door renders it, and asks the two questions that
// together are the law:
//
//   - FORWARD: every tool this page names is on this shape's belt. The failure
//     names the shape and the tool, because a lane that added a sentence has no
//     other way to see which worker it just lied to.
//   - REVERSE: every tool that is on one shape's belt and off another's, and is
//     NAMED anywhere in the page, has a fragment composed from its predicate
//     (beltfacts.go's [beltFacts]) or a line in the debt ledger
//     ([promptNamesBeyondTheBelt]). This is the half that keeps the next
//     conditional tool from being written into the page for everybody.

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	configpkg "github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// beltShape is one agent this package builds: the config its own door builds,
// the belt it actually runs with, and the page it actually reads.
type beltShape struct {
	name string
	// build fills in the config exactly as the shape's door does, and hands
	// back nothing else: everything under test is derived from it.
	build func(t *testing.T, config *Config)
	// narrow is the belt where the shape does not simply run [Agent.belt] — the
	// hand runs a fixed allowlist over it ([forkBelt]).
	narrow func(a *Agent) []bare.Tool
	// callersPage marks the one shape whose page is NOT rendered for it.
	callersPage bool
}

// beltShapes is every shape, and each is built the way its own door builds it —
// task_run.go for a node, fork.go for a hand, standing_run.go for a check — so
// that a door that changes what it hands down changes this test's answer too.
var beltShapes = []beltShape{{
	// The shipping conversation, fully wired: a store behind memory, an
	// accounts hub, the harness machines. This is the fullest belt there is and
	// it is what the universe of tool names is built from.
	name: "a conversation that remembers",
	build: func(t *testing.T, config *Config) {
		config.Memory = openTestBrain(t)
		config.connectHub = &fakeHub{connected: true, account: "you@example.test"}
		config.HarnessStore = subharness.At(t.TempDir())
		config.RunHarness = func(context.Context, string, string, string, func(subharness.Trail)) (string, subharness.Usage, error) {
			return "", subharness.Usage{}, nil
		}
		config.OrchestrateRunner = func(context.Context, string, string, float64) (string, error) { return "", nil }
		config.BashBackgroundAfterSeconds = configpkg.DefaultBashBackgroundAfter
		// AND THE TWO SEAMS THAT ARE THE REST OF THE UNIVERSE. The big machines
		// and `stand` are conditional on somebody being there to answer a card
		// and on there being a store to arm one in (tools.go), and a universe
		// built without them would be a universe that could not tell a tool
		// nobody named from a tool nobody has.
		config.AskConsent = true
		config.Standing = &Standing{}
		config.standingItems = &fakeStanding{}
		config.Subharnesses = registryWith(t, &fakeGeneralist{}, &fakeRunner{manifest: theProgram()})
		config.HarnessCards = true
	},
}, {
	// The same door with memory off, which is what --once and a session opened
	// against no store get: no brain, so no `remember` and no
	// `search_conversations`.
	name:  "a conversation with memory off",
	build: func(t *testing.T, config *Config) {},
}, {
	// A task node one level down that was handed the conversation's graph, so
	// it may hand parts of its own work further out (task_run.go's
	// newTaskAgent, task.go's fan-out law).
	name: "a task node that may fan out",
	build: func(t *testing.T, config *Config) {
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskDepth = 1
	},
}, {
	// THE SHAPE THE DEFECT WAS FOUND ON. A node on the floor of the tree: it
	// has no `tasks`, no `propose_task`, no settings pair, no `watch` and no
	// store, and on 2026-08-23 it did what the page told it and called `tasks`.
	name: "a node on the floor of the tree",
	build: func(t *testing.T, config *Config) {
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskDepth = taskDepthLimit
	},
}, {
	// And the other way to stand on the floor: a worker handed no graph at all,
	// which is what an orchestrate run's workers get (task_run.go).
	name: "a node with no graph",
	build: func(t *testing.T, config *Config) {
		config.InTask = true
	},
}, {
	// A hand (fork.go): this mind copied inside the turn, on a fixed allowlist
	// of a belt and — deliberately — on its CALLER'S page.
	name: "a hand",
	build: func(t *testing.T, config *Config) {
		config.InTask = true
		config.inHand = true
		config.tasker = graphForShape(t)
	},
	narrow: func(a *Agent) []bare.Tool {
		return forkBelt(a.beltTools(), a.config.Workspace)
	},
	callersPage: true,
}, {
	// A standing check's probe (standing_run.go): the parent's config with the
	// conversation taken out of it, InTask, and no store — the throwaway agent
	// it builds has no brain, so the config must not claim one.
	name: "a standing check",
	build: func(t *testing.T, config *Config) {
		config.InTask = true
		config.AskConsent = false
		config.Standing = nil
	},
}}

// openTestBrain is a store in a directory the test owns. It is never the
// person's real one: a test that wrote into ~/.aforge would be a test that
// changes their next conversation.
func openTestBrain(t *testing.T) *store.Store {
	t.Helper()
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	return brain
}

// graphForShape is a conversation's graph, which is what a node is handed down.
func graphForShape(t *testing.T) *TaskGraph {
	t.Helper()
	owner, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	return owner.graph()
}

// shapeAgent builds the shape and hands back the belt it runs with and the page
// it reads. The moment is fixed so the `Now` line cannot move under an
// assertion.
func beltShapeAgent(t *testing.T, shape beltShape) (belt []bare.Tool, page string) {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		// newTestAgent pins a fixed System so transcript assertions do not move
		// with the date; the page under test here is the rendered one, so this
		// shape renders it for itself.
		config.System = ""
		shape.build(t, config)
	})
	belt = agent.beltTools()
	if shape.narrow != nil {
		belt = shape.narrow(agent)
	}
	return belt, renderSystemAt(agent.config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
}

// backtickedName finds every `identifier` the page spells. The page's own
// convention is that a tool is named in backticks, which is why it is the thing
// a lane writing a sentence reaches for and the thing this can read.
var backtickedName = regexp.MustCompile("`([a-z_][a-z_0-9]*)`")

func namesIn(page string) map[string]bool {
	found := map[string]bool{}
	for _, match := range backtickedName.FindAllStringSubmatch(page, -1) {
		found[match[1]] = true
	}
	return found
}

func beltNameSet(belt []bare.Tool) map[string]bool {
	names := make(map[string]bool, len(belt))
	for _, tool := range belt {
		names[tool.Name] = true
	}
	return names
}

// TestEveryToolThePromptNamesIsOnThatShapesBelt is the forward direction.
func TestEveryToolThePromptNamesIsOnThatShapesBelt(t *testing.T) {
	universe := toolUniverse(t)
	for _, shape := range beltShapes {
		if shape.callersPage {
			// A HAND READS ITS CALLER'S PAGE, BYTE FOR BYTE, AND THAT IS THE
			// POINT OF THE VERB. Its transcript is the caller's from message
			// zero, and a page rendered for the hand would break the shared
			// prefix the fork exists to reuse (fork.go's forkSeed). So the page
			// a hand reads names tools its allowlist belt does not carry, this
			// test cannot fix it by rendering, and what is asserted instead is
			// the fact itself — so that the next lane reads why rather than
			// assuming the shape was forgotten.
			continue
		}
		belt, page := beltShapeAgent(t, shape)
		for _, token := range []string{beltFactsToken, handoffFactsToken, programFactsToken} {
			if strings.Contains(page, token) {
				t.Fatalf("%s: the page still carries %s, so its tool-naming facts were never composed", shape.name, token)
			}
		}
		carried := beltNameSet(belt)
		// A SENTENCE THAT NAMES A TOOL IN ORDER TO SAY IT IS NOT HERE IS NOT A
		// PROMISE. The absent-case fragments are exactly that ("There is no
		// `watch` here"), so they come out before the page is read for names.
		residue := page
		for _, fact := range allBeltFacts() {
			if !fact.holds(agentConfigFor(t, shape)) && fact.absent != "" {
				residue = strings.Replace(residue, fact.absent, "", 1)
			}
		}
		for name := range namesIn(residue) {
			if !universe[name] || carried[name] {
				continue
			}
			marker, ledgered := promptNamesBeyondTheBelt[name]
			if !ledgered {
				t.Errorf("%s: the page names `%s` and this shape's belt does not carry it, so the model is being told it has a verb it will be answered `Unknown tool: %s` for",
					shape.name, name, name)
				continue
			}
			if marker != "" && !strings.Contains(page, marker) {
				t.Errorf("%s: the page names `%s` without the sentence that says what to do without it (%q)", shape.name, name, marker)
			}
		}
	}
}

// agentConfigFor rebuilds one shape's config, for the questions that are asked
// of a config rather than of a belt.
func agentConfigFor(t *testing.T, shape beltShape) Config {
	t.Helper()
	config := Config{Workspace: t.TempDir(), Model: "test/model"}
	shape.build(t, &config)
	return config
}

// toolUniverse is every name that is a tool on SOME belt this package builds,
// plus the names the fragments and the ledger account for. A name outside it is
// an ordinary backticked word — an argument, a shell binary — and not this
// test's business.
func toolUniverse(t *testing.T) map[string]bool {
	t.Helper()
	universe := map[string]bool{}
	for _, shape := range beltShapes {
		belt, _ := beltShapeAgent(t, shape)
		for name := range beltNameSet(belt) {
			universe[name] = true
		}
	}
	for _, fact := range allBeltFacts() {
		for _, name := range fact.tools {
			universe[name] = true
		}
	}
	for name := range promptNamesBeyondTheBelt {
		universe[name] = true
	}
	return universe
}

// TestEveryConditionalToolThePageNamesHasAFragment is the reverse direction: a
// tool that varies between belts and is named in the page must be composed from
// its predicate, or written down in the ledger as the debt it is.
func TestEveryConditionalToolThePageNamesHasAFragment(t *testing.T) {
	on, off := map[string]bool{}, map[string]bool{}
	named := map[string]bool{}
	for _, shape := range beltShapes {
		belt, page := beltShapeAgent(t, shape)
		carried := beltNameSet(belt)
		for name := range carried {
			on[name] = true
		}
		for name := range toolUniverse(t) {
			if !carried[name] {
				off[name] = true
			}
		}
		for name := range namesIn(page) {
			named[name] = true
		}
	}

	composed := map[string]bool{}
	for _, fact := range allBeltFacts() {
		for _, name := range fact.tools {
			composed[name] = true
		}
	}
	for name := range named {
		if !on[name] || !off[name] {
			// Either not a tool at all, or one every belt carries: the page may
			// name it once, for everybody, and it will be true.
			continue
		}
		if composed[name] {
			continue
		}
		if _, ledgered := promptNamesBeyondTheBelt[name]; ledgered {
			continue
		}
		t.Errorf("the page names `%s`, which is on some belts and off others, and nothing composes that sentence: give it a fragment in beltFacts or a line in promptNamesBeyondTheBelt",
			name)
	}

	// AND THE LEDGER IS DEBT, NOT FURNITURE. An entry for a tool the page no
	// longer names is an entry that would quietly excuse the next sentence
	// written about it.
	for name := range promptNamesBeyondTheBelt {
		if !named[name] {
			t.Errorf("promptNamesBeyondTheBelt still carries `%s`, which the page no longer names: delete the line", name)
		}
	}
}

// TestAFloorNodeIsToldWhatItCannotReach is the issue's own acceptance, in the
// wording a worker actually reads.
func TestAFloorNodeIsToldWhatItCannotReach(t *testing.T) {
	var floor beltShape
	for _, shape := range beltShapes {
		if shape.name == "a node on the floor of the tree" {
			floor = shape
		}
	}
	belt, page := beltShapeAgent(t, floor)
	carried := beltNameSet(belt)
	for _, gone := range []string{"tasks", "settings", "change_setting", "search_conversations", "watch"} {
		if carried[gone] {
			t.Fatalf("`%s` is on a floor node's belt, so this test is asserting against the wrong shape", gone)
		}
	}
	for _, want := range []string{
		// The record is not reachable, and what to do instead.
		"THE RECORD OF EARLIER WORK IS NOT REACHABLE FROM HERE",
		// A preference cannot be changed from a task, and where it is changed.
		"YOU CANNOT CHANGE A PREFERENCE FROM INSIDE A TASK",
		"/settings",
		// What was said cannot be looked up.
		"cannot be looked up from here",
		// And there is no watch, so waiting is a foreground call.
		"There is no `watch` here",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("a floor node's page does not say %q, so it is told nothing where it used to be told a lie", want)
		}
	}
	// AND NOT ONE WORD OF THE PRESENT CASE.
	for _, gone := range []string{
		"call `tasks` with their words BEFORE answering",
		"goes through `settings` for the row",
		"call `search_conversations` ONCE",
		"Start ONE `watch`",
	} {
		if strings.Contains(page, gone) {
			t.Errorf("a floor node's page still says %q, which names a tool it will be answered `Unknown tool` for", gone)
		}
	}
}

// TestAHandReadsItsCallersPage pins the one shape this law is not enforced on,
// and why: a hand's message[0] is the caller's, byte for byte, because the
// whole economic argument for the verb is the prefix the copies share
// (fork.go's forkSeed). Composing a page for a hand would break it.
func TestAHandReadsItsCallersPage(t *testing.T) {
	caller, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	seed, page := caller.forkSeed()
	if len(seed) == 0 || page == "" {
		t.Fatal("a fork seeds nothing, so a hand has no page at all")
	}
	if page != messageContentText(seed[0]) {
		t.Fatal("a hand's page is not seed[0], so the shared prefix a fork exists for is already broken")
	}
}
