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
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	configpkg "github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/subharness"
)

// beltShape is one agent this package builds: the config its own door builds,
// the belt it actually runs with, and the page it actually reads.
type beltShape struct {
	name string
	// build fills in the config exactly as the shape's door does, and hands
	// back nothing else: everything under test is derived from it.
	build func(t *testing.T, config *Config)
	// mint replaces the whole construction where a shape is not built by
	// rendering its own config — a hand is minted by fork.go and opens on its
	// caller's page, so it is the one shape whose text this test must take
	// from the code that makes it rather than from [renderSystemAt].
	mint func(t *testing.T) mintedShape
}

// mintedShape is one shape as it actually runs: the belt it carries, the system
// text it reads, and the part of that text composed for somebody ELSE.
type mintedShape struct {
	belt []bare.Tool
	// shelved is what this shape HAS and is not carrying: the tools held back
	// for the tool block's sake, one `load_capability` call away
	// (tools_capabilities.go). The page may name one — it is a verb the model can
	// have — but only where the page also says how to fetch it, which is the
	// extra half of the forward law below.
	shelved []bare.Tool
	page    string
}

// buildShippedConversation is the shipping conversation, fully wired, and it is
// A NAMED FUNCTION so that more than one row of [beltShapes] can be it. The lean
// row is this shape with the window changed and nothing else; it used to build a
// bare config of its own, which meant two rows of one table disagreed about what
// "the shipping door" means and only one of them was a conversation anybody has.
func buildShippedConversation(t *testing.T, config *Config) {
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
	// AND THE PROGRAMS THE BUILD CARRIES, as the chat door hands them over
	// (cmd/codeaf's chatv3.go: `Delegates: v3Delegates()`). Their paragraph —
	// each program's own guide, and codeaf's rule about the folder one is handed
	// — rides every request of the shipping conversation, so a shape that left
	// them off would weigh, and lint, a page nobody is sent.
	config.Delegates = builtin.All()
}

// beltShapes is every shape, and each is built the way its own door builds it —
// task_run.go for a node, standing_run.go for a check — so that a door that
// changes what it hands down changes this test's answer too.
var beltShapes = []beltShape{{
	// The shipping conversation, fully wired: a store behind memory, an
	// accounts hub, the harness machines. This is the fullest belt there is and
	// it is what the universe of tool names is built from.
	name:  "a conversation that remembers",
	build: buildShippedConversation}, {
	// The same door with memory off, which is what --once and a session opened
	// against no store get: no brain, so no `remember` and no
	// `search_conversations`.
	name:  "a conversation with memory off",
	build: func(t *testing.T, config *Config) {},
}, {
	name: "a worker with inherited conversation reads",
	build: func(t *testing.T, config *Config) {
		config.InTask = true
		config.ConversationHistory = openTestBrain(t)
	},
}, {
	// A task node one level down that was handed the conversation's graph, so
	// it may hand parts of its own work further out (task_run.go's
	// newTaskAgent, task.go's fan-out law).
	name: "a task node that may fan out",
	build: func(t *testing.T, config *Config) {
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskID = 1
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
		config.taskID = 2
		config.taskDepth = taskDepthLimit
	},
}, {
	// And the other way to stand on the floor: a worker handed no graph at all,
	// which is what an orchestrate run's workers get (task_run.go).
	name: "a node with no graph",
	build: func(t *testing.T, config *Config) {
		config.InTask = true
		config.taskID = 3
	},
}, {
	// THE LEAN CONVERSATION (promptprofile.go): the same shipping door on a
	// sixteen-thousand-token window, which shelves four more groups than a full
	// belt and is HANDED the `questions` group rather than being told to fetch
	// it. It is a shape here because the two halves of this law — the page names
	// only what the belt has, and it says how a shelved verb arrives — are
	// exactly what a second partition can break.
	name: "a lean conversation on a small window",
	build: func(t *testing.T, config *Config) {
		// IT IS THE SHIPPED SHAPE WITH THE WINDOW CHANGED, and one row of this
		// table is not allowed to disagree with another about what "the shipping
		// door" means. This built a bare config until #996 — no store, no hub, no
		// standing items, no saved programs — while the gate that bounds the lean
		// prefix built the full one, so the two were measuring different
		// conversations under the same name and only one of them was a
		// conversation anybody has.
		buildShippedConversation(t, config)
		config.ContextWindow = leanWindow
	},
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

// systemTextOf is what an agent's message[0] is rebuilt from
// ([Agent.refreshSystemLocked]), read under the lock that guards it.
func systemTextOf(a *Agent) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.system
}

// openTestBrain is a store in a directory the test owns. It is never the
// person's real one: a test that wrote into ~/.codeaf would be a test that
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
func beltShapeAgent(t *testing.T, shape beltShape) mintedShape {
	t.Helper()
	if shape.mint != nil {
		return shape.mint(t)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		// newTestAgent pins a fixed System so transcript assertions do not move
		// with the date; the page under test here is the rendered one, so this
		// shape renders it for itself.
		config.System = ""
		shape.build(t, config)
	})
	return mintedShape{
		belt:    agent.beltTools(),
		shelved: agent.shelvedTools(),
		page:    renderSystemAt(agent.config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)),
	}
}

// C8: The account cue exists only beside the account tools, fits in one short
// sentence, and tells the model to look before denying access and to carry on
// inside the same turn.
func TestTheAccountBeltFactTeachesConnectOnDemandOnlyWithAHub(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	withHub := renderSystemAt(Config{connectHub: &fakeHub{}}, now)
	var fact string
	for _, line := range strings.Split(withHub, "\n") {
		if strings.Contains(line, "The person has accounts you can act in") {
			fact = line
			break
		}
	}
	if fact == "" {
		t.Fatal("a session with a hub is not told that the person has accounts")
	}
	for _, want := range []string{
		"NEVER answer \"I don't have access to your X\" before calling `services`",
		"not connected yet", "through `use_service`", "next request of this same turn",
	} {
		if !strings.Contains(fact, want) {
			t.Errorf("the account fact does not say %q: %s", want, fact)
		}
	}
	if len(fact) > 420 {
		t.Errorf("the account fact is %d bytes, want at most 420: %s", len(fact), fact)
	}

	withoutHub := renderSystemAt(Config{}, now)
	for _, absent := range []string{"The person has accounts you can act in", "`services`", "`use_service`"} {
		if strings.Contains(withoutHub, absent) {
			t.Errorf("a session without a hub is told about accounts through %q", absent)
		}
	}
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
		minted := beltShapeAgent(t, shape)
		belt, page := minted.belt, minted.page
		for _, token := range []string{beltFactsToken, handoffFactsToken, programFactsToken} {
			if strings.Contains(page, token) {
				t.Fatalf("%s: the page still carries %s, so its tool-naming facts were never composed", shape.name, token)
			}
		}
		carried := beltNameSet(belt)
		// AND A SHELVED TOOL IS A VERB THIS SHAPE HAS. It is not in the tool block
		// yet, so naming it is a promise only when the page also says how it
		// arrives — which is `load_capability` and the group's own word
		// (tools_capabilities.go). Naming it without those is the same defect as
		// naming a tool nobody has, one round trip later.
		shelved := beltNameSet(minted.shelved)
		for name := range shelved {
			carried[name] = true
		}
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
		for name := range namesIn(residue) {
			if !shelved[name] {
				continue
			}
			if !strings.Contains(page, "`"+loadCapabilityToolName+"`") {
				t.Errorf("%s: the page names `%s`, which waits on a shelf, and never names `%s` — the model will reach for it and be answered `Unknown tool: %s`",
					shape.name, name, loadCapabilityToolName, name)
				continue
			}
			group := capabilityGroupOf(name)
			if group == "" || !strings.Contains(page, "`"+group+"`") {
				t.Errorf("%s: the page names `%s` and tells the model to load, without ever naming the `%s` group it is in",
					shape.name, name, group)
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
		for name := range beltNameSet(beltShapeAgent(t, shape).belt) {
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
		minted := beltShapeAgent(t, shape)
		belt, page := minted.belt, minted.page
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
	// AND THE LEAN PROFILE COMPOSES ITS OWN LINE THE SAME WAY. The one sentence
	// a lean page owes — the verbs waiting on the shelf and the group each is in
	// — is built from [leanCapabilityGroups] and each row's own predicate
	// (promptprofile.go's [Config.leanShelfPointer]), which is exactly what a
	// beltFact is. So the names it spells are composed, not written into the
	// page for everybody, and a row that stopped holding takes its names with it.
	for _, group := range leanCapabilityGroups {
		for _, name := range group.members {
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
	minted := beltShapeAgent(t, floor)
	belt, page := minted.belt, minted.page
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

// ── the hand, driven ────────────────────────────────────────────────────────

// handModel is a model that answers a hand and REMEMBERS WHAT IT WAS OFFERED:
// the system text it was given and the tool block in front of it. The scripted
// completer records messages and the model name; what this test is about is the
// tools, which nothing else captures.
type handModel struct {
	mu       sync.Mutex
	systems  []string
	offered  [][]string
	requests [][]ai.Message
	// reach is the tool the model asks for on its first round: the point of the
	// test is that a hand cannot make this call, whatever its page once said.
	reach string
}
