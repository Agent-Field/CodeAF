package session

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// oneNodesWorld admits one node onto a graph belonging to the given conversation
// and answers the world it was actually handed — the brief as [TaskGraph.briefLocked]
// assembled it at the moment the frontier started it, which is the seam this file
// is about. No provider and no git: the runner is scripted, exactly as the rest of
// the frontier's tests script it (task_test.go).
func oneNodesWorld(t *testing.T, agent *Agent, brief string) string {
	t.Helper()
	graph := newTaskGraph()
	graph.home = agent
	assembled := make(chan string, 1)
	graph.run = func(node *TaskNode) {
		assembled <- node.assembledBrief()
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "do the work", brief: brief, acceptance: "it is done"})
	select {
	case world := <-assembled:
		return world
	case <-time.After(10 * time.Second):
		t.Fatal("the node never started")
		return ""
	}
}

// theSystemPrompt is message[0] as the model will read it, re-rendered the way a
// turn re-renders it ([Agent.startTurnLocked]).
func theSystemPrompt(t *testing.T, agent *Agent) string {
	t.Helper()
	agent.refreshStandingLocked()
	agent.refreshSystemLocked()
	if len(agent.messages) == 0 || len(agent.messages[0].Content) == 0 {
		t.Fatal("the agent has no system message")
	}
	return agent.messages[0].Content[0].Text
}

// ── the birth seam, on a task ───────────────────────────────────────────────

// THE PLACE A TASK IS BORN IN IS THE WORKSPACE AND THE CONVERSATION THAT ADMITTED
// IT. This is the whole of the birth seam's resolution law, and every one of the
// five orders below is here to pin one half of it: what reaches (this project's,
// the machine's), what does not (another project's, another chat's), and what the
// person kept out of here.
func TestATaskCarriesTheOrdersStandingOverTheConversationThatAdmittedIt(t *testing.T) {
	agent, store := ordersAgent(t)
	workspace, session := agent.standingPlace()
	house, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory to file a machine-wide order under: %v", err)
	}

	anOrder(t, store, "never touch the public API", workspace, standing.AltitudeProject, session)
	anOrder(t, store, "run the tests before you say you are done", house, standing.AltitudeMachine, session)
	anOrder(t, store, "in another project", filepath.Join(workspace, "..", "elsewhere"), standing.AltitudeProject, session)
	anOrder(t, store, "in another chat", workspace, standing.AltitudeConversation, "another")
	kept := anOrder(t, store, "not in this chat", workspace, standing.AltitudeProject, session)
	kept.Exceptions = []standing.Exception{{SessionID: session, At: time.Now()}}
	if err := store.Save(kept); err != nil {
		t.Fatalf("except: %v", err)
	}

	world := oneNodesWorld(t, agent, "fix the crash in the parser")

	if !strings.Contains(world, "fix the crash in the parser") {
		t.Fatalf("the node lost its own brief:\n%s", world)
	}
	if !strings.Contains(world, standingWorldHeading) {
		t.Fatalf("the node was told nothing about the orders over it:\n%s", world)
	}
	for _, want := range []string{"never touch the public API", "run the tests before you say you are done"} {
		if !strings.Contains(world, want) {
			t.Fatalf("%q never reached the node:\n%s", want, world)
		}
	}
	for _, unwanted := range []string{"in another project", "in another chat", "not in this chat"} {
		if strings.Contains(world, unwanted) {
			t.Fatalf("%q reached a node it does not govern:\n%s", unwanted, world)
		}
	}
	// AND THE NODE IS TOLD WHOSE THESE ARE AND WHAT TO DO WITH ONE IT CANNOT KEEP.
	// A list of sentences with no sentence around it is a list a worker may read
	// as suggestions, which is the one thing an order is not.
	if !strings.Contains(world, standingWorldIntro) {
		t.Fatalf("the node was handed the orders with nothing saying whose they are:\n%s", world)
	}
	if !strings.Contains(world, standingWorldReport) {
		t.Fatalf("the node was not told where to say it cannot honour one:\n%s", world)
	}
	// THE ORDERS COME AFTER THE WORK, never in front of it.
	if strings.Index(world, standingWorldHeading) < strings.Index(world, "fix the crash in the parser") {
		t.Fatalf("the orders were put in front of the job:\n%s", world)
	}
}

// A node whose prerequisites taught it something meets THAT first and the orders
// after it: the reports are the job, the orders are the conditions the job runs
// under.
func TestTheOrdersComeAfterWhatTheWorkBeforeTaughtTheNode(t *testing.T) {
	agent, store := ordersAgent(t)
	workspace, session := agent.standingPlace()
	anOrder(t, store, "never touch the public API", workspace, standing.AltitudeProject, session)

	graph := newTaskGraph()
	graph.home = agent
	assembled := make(chan string, 2)
	release := make(chan struct{})
	graph.run = func(node *TaskNode) {
		assembled <- node.assembledBrief()
		if node.id == 1 {
			<-release
		}
		node.finish("the bug is in the tokenizer", nil, "", "")
		node.graph.complete(node, TaskDone)
	}
	first, second := graph.reserve(), graph.reserve()
	graph.admit(first, taskSpec{title: "find it", brief: "find the bug", acceptance: "named"})
	graph.admit(second, taskSpec{title: "fix it", brief: "fix the bug", acceptance: "tests pass", dependsOn: []uint64{first}})
	<-assembled
	close(release)

	var world string
	select {
	case world = <-assembled:
	case <-time.After(10 * time.Second):
		t.Fatal("the dependent never started")
	}
	learned := strings.Index(world, "What the work before you learned")
	orders := strings.Index(world, standingWorldHeading)
	if learned < 0 || orders < 0 || orders < learned {
		t.Fatalf("the world reads in the wrong order (learned at %d, orders at %d):\n%s", learned, orders, world)
	}
	if !strings.Contains(world, "the bug is in the tokenizer") {
		t.Fatalf("the prerequisite's report never reached the dependent:\n%s", world)
	}
}

// THE EMPTINESS LAW, byte for byte. Nothing standing here is no section, no
// heading, and not one character more than the brief the node was given.
func TestANodeWithNothingStandingOverItIsHandedItsBriefAndNothingElse(t *testing.T) {
	agent, _ := ordersAgent(t)
	if world := oneNodesWorld(t, agent, "fix the crash in the parser"); world != "fix the crash in the parser" {
		t.Fatalf("a node with no orders over it was handed %q", world)
	}

	// And so is a conversation with no ambient side at all: a door that wired
	// nothing answers the way it answers when the ambient side is off.
	off, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if world := oneNodesWorld(t, off, "fix the crash in the parser"); world != "fix the crash in the parser" {
		t.Fatalf("a node in a session with no ambient side was handed %q", world)
	}
}

// A PAUSED ORDER GOVERNS NOTHING WHILE IT IS PAUSED. The resolver already
// answers active only; this is the seam agreeing with it, because a worker held
// to a rule the person switched off is the failure the pause key exists to
// prevent.
func TestAPausedOrderNeverReachesANodesWorld(t *testing.T) {
	agent, store := ordersAgent(t)
	workspace, session := agent.standingPlace()
	paused := anOrder(t, store, "never touch the public API", workspace, standing.AltitudeProject, session)
	paused.Status = standing.StatusPaused
	if err := store.Save(paused); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if world := oneNodesWorld(t, agent, "fix it"); world != "fix it" {
		t.Fatalf("a paused order reached a node's world: %q", world)
	}
}

// A HOLD IS WHAT THE BIRTH SEAM IS FOR.
//
// A rule never wakes: no pass will ever check it, fire it or spend a cent on it,
// so riding into the world of the work that starts is not one of the ways it is
// kept — it is THE way. This is the same section every other kind rides in, and
// the point of the test is that nothing about a rule's emptiness keeps it out of
// one: no rails, no action, no cadence, and it still reaches the node.
func TestAHoldRidesIntoTheWorldOfTheWorkThatStarts(t *testing.T) {
	agent, store := ordersAgent(t)
	workspace, session := agent.standingPlace()
	rule, err := store.Create(standing.Item{
		Words:     "always run the tests before you say you are done",
		Workspace: workspace,
		Origin:    standing.Origin{SessionID: session},
		When:      standing.When{Kind: standing.WhenHold},
		Altitude:  standing.AltitudeProject,
	})
	if err != nil {
		t.Fatalf("cannot stand a rule up: %v", err)
	}
	if rule.Spends() || !rule.NextDue.IsZero() {
		t.Fatalf("a rule was stood up as something that wakes: %+v", rule.When)
	}

	world := oneNodesWorld(t, agent, "fix the crash in the parser")
	if !strings.Contains(world, "always run the tests before you say you are done") {
		t.Fatalf("the rule never reached the node:\n%s", world)
	}
	if !strings.Contains(world, standingWorldIntro) {
		t.Fatalf("the rule arrived with nothing saying whose it is:\n%s", world)
	}

	// AND INTO A CONVERSATION'S OWN INSTRUCTIONS, which is the other half of the
	// same seam: the model reasoning in this chat is under the rule from the top
	// of the very next turn.
	if prompt := theSystemPrompt(t, agent); !strings.Contains(prompt, "always run the tests before you say you are done") {
		t.Fatalf("the rule never reached the conversation's own world:\n%s", prompt)
	}
}

// ── the birth seam, in a conversation ───────────────────────────────────────

// The conversation's own world gains the section when something stands over it
// and LOSES IT AGAIN when the last one is stopped — the prompt says what is true
// now, never what was true when the window opened.
func TestTheConversationsWorldGainsAndLosesTheStandingSectionWithTheSet(t *testing.T) {
	agent, store := ordersAgent(t)
	workspace, session := agent.standingPlace()

	bare := theSystemPrompt(t, agent)
	if strings.Contains(bare, standingWorldHeading) {
		t.Fatalf("a conversation with nothing standing over it carries a section:\n%s", bare)
	}

	made := anOrder(t, store, "never touch the public API", workspace, standing.AltitudeProject, session)
	withOrder := theSystemPrompt(t, agent)
	for _, want := range []string{"<standing>", standingWorldHeading, standingWorldIntro, "never touch the public API"} {
		if !strings.Contains(withOrder, want) {
			t.Fatalf("the conversation was not told %q:\n%s", want, withOrder)
		}
	}
	// A CONVERSATION HAS NOBODY TO REPORT TO — it is talking to the person right
	// now — so the node's closing line has no business here.
	if strings.Contains(withOrder, standingWorldReport) {
		t.Fatal("the conversation was told to put it in a report it does not write")
	}

	if err := agent.StandingStandDown(made.ID); err != nil {
		t.Fatalf("stand down: %v", err)
	}
	if after := theSystemPrompt(t, agent); after != bare {
		t.Fatalf("stopping the last order left the section behind:\n%s", after)
	}
}

// ── the bound ───────────────────────────────────────────────────────────────

// A world section is context somebody pays for on every request of every turn,
// so it is BOUNDED — the longest-standing orders lead and survive the clip, and
// what was cut is counted rather than quietly dropped.
func TestTheStandingSectionIsBoundedLongestStandingFirst(t *testing.T) {
	made := make([]standing.Item, 0, standingWorldMost+3)
	// Newest first going in, so an unsorted section would come out backwards.
	for at := standingWorldMost + 2; at >= 0; at-- {
		made = append(made, standing.Item{
			Words:   "order " + strconv.Itoa(at),
			Created: time.Date(2026, 1, 1+at, 9, 0, 0, 0, time.UTC),
		})
	}
	section := renderStandingWorld(made, standingWorldReport)

	lines := make([]string, 0, standingWorldMost)
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "- ") {
			lines = append(lines, strings.TrimPrefix(line, "- "))
		}
	}
	if len(lines) != standingWorldMost {
		t.Fatalf("the section lists %d orders, want at most %d:\n%s", len(lines), standingWorldMost, section)
	}
	if lines[0] != "order 0" || lines[standingWorldMost-1] != "order "+strconv.Itoa(standingWorldMost-1) {
		t.Fatalf("the section is not longest-standing first:\n%s", section)
	}
	if !strings.Contains(section, "…3 more") {
		t.Fatalf("the section does not say what it cut:\n%s", section)
	}

	// THE COMPILED BRIEF IS WHAT THE MACHINERY FOLLOWS, and one order is one
	// line however the brief was written.
	compiled := renderStandingWorld([]standing.Item{{
		Words: "keep main green",
		Brief: standing.Brief{Prompt: "before you say a change is done,\n  run the tests"},
	}}, "")
	if !strings.Contains(compiled, "- before you say a change is done, run the tests\n") {
		t.Fatalf("the compiled brief did not ride as one line:\n%s", compiled)
	}
	if strings.Contains(compiled, "keep main green") {
		t.Fatalf("the words were used where a compiled brief exists:\n%s", compiled)
	}
	if renderStandingWorld(nil, standingWorldReport) != "" {
		t.Fatal("an empty set rendered a section")
	}
}
