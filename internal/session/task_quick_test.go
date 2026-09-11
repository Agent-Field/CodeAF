package session

// THE QUICK TASK, END TO END, THROUGH THE REAL RUNNER.
//
// Every test here drives a real conversation with a scripted model: the chat
// calls `quick_task`, the graph admits a node, the frontier starts it, a real
// worker agent runs in the caller's own workspace, and the node lands. Nothing
// about the road is stubbed — only the model is — because the four things this
// wave has to be true about are all facts about the ROAD: that two of them run
// at once, that two claiming one file do not, that ticking an item moves the
// row a person is watching, and that one started under a task reports to that
// task rather than to the conversation.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the fixture ─────────────────────────────────────────────────────────────

// quickLanes scripts one conversation and any number of workers against one
// provider, told apart by a MARK that rides in each worker's own opening
// message.
//
// It is not [routedCompleter] because the thing under test here is
// CONCURRENCY. That fixture has one child lane with one counter, so two workers
// running at the same time race for the same script entry and the test can
// neither say which answered nor prove they overlapped. Here every lane is
// named and has a script of its own, so two workers running together take two
// scripts and a test that expected them to overlap can say so.
type quickLanes struct {
	mu sync.Mutex
	// lanes is the script for each mark, and "" is the conversation.
	lanes map[string][]step
	seen  map[string]int
	// asked is every request each lane took, which is the only place a worker's
	// opening message can be read from the outside.
	asked map[string][][]ai.Message
}

func newQuickLanes(conversation []step) *quickLanes {
	return &quickLanes{
		lanes: map[string][]step{"": conversation},
		seen:  map[string]int{},
		asked: map[string][][]ai.Message{},
	}
}

// lane hangs a script on a mark. The mark has to be something only that
// worker's messages can contain, which in practice is a word in its own line.
func (c *quickLanes) lane(mark string, steps ...step) *quickLanes {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lanes[mark] = steps
	return c
}

func (c *quickLanes) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	// The errands beside the work are answered before the lanes are touched, for
	// [routedCompleter]'s reason said about this fixture: a namer or a captioner
	// carries the worker's own words and would otherwise take the step the test
	// scripted for the worker.
	if isNameCall(messages) || isCaptionCall(messages) || isTitleCall(messages) {
		return textResponse(""), nil
	}
	c.mu.Lock()
	which := c.laneOf(messages)
	snapshot := make([]ai.Message, len(messages))
	copy(snapshot, messages)
	c.asked[which] = append(c.asked[which], snapshot)
	var next step
	if index := c.seen[which]; index < len(c.lanes[which]) {
		next = c.lanes[which][index]
	}
	c.seen[which]++
	c.mu.Unlock()

	if next == nil {
		return textResponse("(unscripted)"), nil
	}
	return next(ctx, messages)
}

// laneOf decides which script answers this request, and it is two rules
// because a transcript is not a private thing.
//
// THE CONVERSATION IS IDENTIFIED POSITIVELY, by the system prompt the test
// harness pins on it, rather than by having no mark. A chat that has just
// asked for two quick tasks is holding both of their lines in its own
// assistant message, so "no mark" is false of it from its second request on.
//
// AND A WORKER TAKES THE EARLIEST MARK IN ITS OWN CONTEXT. Its brief opens with
// THE WORK, which is its line; what it may ALSO be holding is the calls that
// already ran, quoted under the admission headings below it (admission.go), and
// those carry its sibling's line. First in the document wins, which is the job
// it was given rather than the record it came out of. Called with the lock
// held.
func (c *quickLanes) laneOf(messages []ai.Message) string {
	if len(messages) > 0 && messages[0].Role == "system" && strings.TrimSpace(messageText(messages[0])) == "SYSTEM" {
		return ""
	}
	for _, message := range messages {
		text := messageText(message)
		which, earliest := "", -1
		for mark := range c.lanes {
			if mark == "" {
				continue
			}
			if at := strings.Index(text, mark); at >= 0 && (earliest < 0 || at < earliest) {
				which, earliest = mark, at
			}
		}
		if earliest >= 0 {
			return which
		}
	}
	return ""
}

// asksOf is every request one lane took.
func (c *quickLanes) asksOf(mark string) [][]ai.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]ai.Message(nil), c.asked[mark]...)
}

// sawIn reports whether any request a lane took carried the needle.
func (c *quickLanes) sawIn(mark, needle string) bool {
	for _, request := range c.asksOf(mark) {
		for _, message := range request {
			if strings.Contains(messageText(message), needle) {
				return true
			}
		}
	}
	return false
}

// quickCall is the model asking for one quick task.
//
// IT ALWAYS NAMES A TITLE, and the title never carries the lane's mark. The
// receipt the conversation reads back echoes the title (task_quick.go's
// [quickStartedWord]), so a mark left in it would put the CONVERSATION into the
// worker's lane on its very next request and take the step the worker was
// scripted for — which is a real property of the receipt and a trap for this
// fixture, not a defect in either.
func quickCall(id, title, line string, items, files []string) step {
	arguments, _ := json.Marshal(quickArguments{Title: title, Line: line, Items: items, Files: files})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, quickTaskToolName, string(arguments)), nil
	}
}

// quickBatch is two quick tasks asked for in ONE response, which is what a
// model fanning out actually emits and is the only shape that can prove the
// two overlap.
func quickBatch(first, second step) step {
	return func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		one, _ := first(ctx, messages)
		two, _ := second(ctx, messages)
		return &ai.Response{
			Choices: []ai.Choice{{Message: ai.Message{
				Role: "assistant",
				ToolCalls: append(append([]ai.ToolCall{},
					one.Choices[0].Message.ToolCalls...),
					two.Choices[0].Message.ToolCalls...),
			}}},
			Usage: &ai.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
		}, nil
	}
}

// quickAgent is a conversation wired the way the interactive door wires one,
// minus everything that would reach a real profile, a real provider or a real
// home. The workspace is a repository because a quick task runs in the
// person's own copy and the treehold question is asked of a real tree.
// `more` is folded in after the wiring above it, so a test that wants a
// checkpoint on disk can name a session file without a second copy of this
// function going out of step with this one.
func quickAgent(t *testing.T, completer Completer, more ...func(*Config)) (*Agent, *TaskGraph, string) {
	t.Helper()
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AFORGE_HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
		for _, apply := range more {
			apply(config)
		}
	})
	return agent, agent.graph(), repo
}

// quickNodeSaying finds the node whose line carries the needle. IDS ARE NOT
// STABLE ACROSS A BATCH and that is a fact about the road rather than a wart:
// two calls in one response are run concurrently (loop.go), so which of them
// reserves the lower id is a race, and a test that named them by number would
// be asserting on the scheduler.
func quickNodeSaying(t *testing.T, graph *TaskGraph, needle string) *TaskNode {
	t.Helper()
	graph.mu.Lock()
	defer graph.mu.Unlock()
	for _, id := range graph.order {
		node := graph.nodes[id]
		if node != nil && node.spec.quick != nil && strings.Contains(node.spec.quick.line, needle) {
			return node
		}
	}
	t.Fatalf("no quick node says %q; the graph holds %d", needle, len(graph.order))
	return nil
}

// barrier is a rendezvous for N workers: every arrival blocks until all of them
// have arrived. It is how "they ran at the same time" is asserted rather than
// timed — if the road serialised them the second never arrives, the first never
// leaves, and the test fails on the deadline with a sentence saying so.
type barrier struct {
	once    sync.Once
	arrived chan struct{}
	need    int
	mu      sync.Mutex
	count   int
}

func newBarrier(need int) *barrier {
	return &barrier{arrived: make(chan struct{}), need: need}
}

func (b *barrier) arrive(t *testing.T, who string) {
	t.Helper()
	b.mu.Lock()
	b.count++
	full := b.count >= b.need
	b.mu.Unlock()
	if full {
		b.once.Do(func() { close(b.arrived) })
	}
	select {
	case <-b.arrived:
	case <-time.After(20 * time.Second):
		t.Errorf("%s waited for the others and they never came: the work was run one at a time", who)
	}
}

// ── (1) two at once ─────────────────────────────────────────────────────────

// TWO QUICK TASKS ASKED FOR IN ONE BREATH RUN IN ONE BREATH, and each one's
// last message is the answer that comes back.
//
// The overlap is proved by a rendezvous rather than by a clock: each worker's
// only model call blocks until the other has made its own, so a road that ran
// them one after the other cannot reach the second call at all. That is the
// whole promise of the verb — four files compared at once, not one after
// another — and a wall-clock margin would be a test that passes on a fast
// machine and flakes on a loaded one.
func TestTwoQuickTasksInOneBatchRunAtOnceAndBothLand(t *testing.T) {
	together := newBarrier(2)
	completer := newQuickLanes([]step{
		quickBatch(
			quickCall("q1", "read the first file", "read ALPHA-SIDE and say what it holds", nil, nil),
			quickCall("q2", "read the second file", "read BETA-SIDE and say what it holds", nil, nil),
		),
		finalText("both are out"),
	})
	completer.lane("ALPHA-SIDE", func(context.Context, []ai.Message) (*ai.Response, error) {
		together.arrive(t, "the first quick task")
		return textResponse("ALPHA holds the tariff table."), nil
	})
	completer.lane("BETA-SIDE", func(context.Context, []ai.Message) (*ai.Response, error) {
		together.arrive(t, "the second quick task")
		return textResponse("BETA holds the regional overrides."), nil
	})

	agent, graph, _ := quickAgent(t, completer)
	collect(t, mustSubmit(t, agent, "compare the two files"))

	one := quickNodeSaying(t, graph, "ALPHA-SIDE")
	two := quickNodeSaying(t, graph, "BETA-SIDE")
	waitDoneNode(t, one)
	waitDoneNode(t, two)

	for _, want := range []struct {
		node   *TaskNode
		answer string
	}{{one, "ALPHA holds the tariff table."}, {two, "BETA holds the regional overrides."}} {
		notice := want.node.notice()
		if notice.State != TaskDone {
			t.Fatalf("quick task %d is %q (%q): %s", want.node.id, notice.State, notice.Ending, notice.Report)
		}
		if notice.Kind != TaskKindQuick {
			t.Fatalf("quick task %d says its kind is %q", want.node.id, notice.Kind)
		}
		// ITS LAST MESSAGE IS ITS RESULT, on the row and in the note the
		// conversation folds.
		if !strings.Contains(notice.Report, want.answer) {
			t.Fatalf("quick task %d reports %q, want its own last message", want.node.id, notice.Report)
		}
		note := taskNote(notice, taskURI(want.node.journalPath()), TaskSettleAsk, landingAddress{})
		if !strings.Contains(note, want.answer) {
			t.Fatalf("the note for quick task %d does not carry its answer:\n%s", want.node.id, note)
		}
		// AND NO BRANCH AND NO MERGE, EVER — the emptiness law on a kind that has
		// neither and could never have either.
		if notice.Branch != "" || notice.Merge != "" {
			t.Fatalf("quick task %d landed with branch %q merge %q", want.node.id, notice.Branch, notice.Merge)
		}
	}
	// And the two answers did not cross: each note carries its own.
	if strings.Contains(one.notice().Report, "BETA") || strings.Contains(two.notice().Report, "ALPHA") {
		t.Fatal("the two quick tasks reported each other's answers")
	}
}

// ── (2) one file, one at a time ─────────────────────────────────────────────

// TWO QUICK TASKS CLAIMING ONE PATH RUN ONE AFTER THE OTHER, through the edge
// the graph already has: the second is admitted with the first's id on its
// depends_on, and the frontier does the rest.
//
// The receipt says so in the same breath, because a model that is told only
// "started" plans its next call around two things happening at once.
func TestTwoQuickTasksClaimingOneFileRunOneAfterTheOther(t *testing.T) {
	release := make(chan struct{})
	held := func(context.Context, []ai.Message) (*ai.Response, error) {
		<-release
		return textResponse("the section is rewritten"), nil
	}
	completer := newQuickLanes([]step{
		quickBatch(
			quickCall("q1", "rewrite the first section", "rewrite the FIRST-CLAIM section", nil, []string{"notes.md"}),
			quickCall("q2", "rewrite the second section", "rewrite the SECOND-CLAIM section", nil, []string{"notes.md"}),
		),
		finalText("both are out"),
	})
	completer.lane("FIRST-CLAIM", held)
	completer.lane("SECOND-CLAIM", held)

	agent, graph, _ := quickAgent(t, completer)
	events := mustSubmit(t, agent, "rewrite both sections of notes.md")

	// WHICH OF THE TWO WINS IS THE SCHEDULER'S, and the law is about the pair
	// rather than about either one: exactly one of them is working, and the
	// other is waiting on it by name.
	waitFor(t, "both quick tasks to be admitted", func() bool {
		graph.mu.Lock()
		defer graph.mu.Unlock()
		return len(graph.order) == 2
	})
	first := quickNodeSaying(t, graph, "FIRST-CLAIM")
	second := quickNodeSaying(t, graph, "SECOND-CLAIM")
	waitFor(t, "one of the two to take notes.md", func() bool {
		return first.stateNow() == TaskRunning || second.stateNow() == TaskRunning
	})
	running, waiting := first, second
	if second.stateNow() == TaskRunning {
		running, waiting = second, first
	}
	if state := waiting.stateNow(); state != TaskQueued {
		t.Fatalf("both quick tasks are live on notes.md: %d is %q and %d is %q",
			running.id, running.stateNow(), waiting.id, state)
	}
	// AND IT WAITS ON THE OTHER BY NAME, which is the whole mechanism: a
	// depends_on edge and nothing new in the frontier.
	if len(waiting.dependsOn) != 1 || waiting.dependsOn[0] != running.id {
		t.Fatalf("quick task %d waits on %v, want task %d", waiting.id, waiting.dependsOn, running.id)
	}
	// AND THE MODEL WAS TOLD WHY, in the receipt, at the moment it asked — a
	// model told only "started" plans its next call around two things happening
	// at once.
	want := fmt.Sprintf("waits for task %d (both claim notes.md)", running.id)
	if !transcriptCarries(agent, want) {
		t.Fatalf("no tool result says %q, so the wait was never explained to the model", want)
	}

	close(release)
	collect(t, events)
	waitDoneNode(t, running)
	waitDoneNode(t, waiting)
	if state := waiting.stateNow(); state != TaskDone {
		t.Fatalf("the waiting quick task settled %q once the other let go", state)
	}
}

// ── (3) the list ────────────────────────────────────────────────────────────

// TICKING AN ITEM MOVES THE ROW A PERSON IS WATCHING. The row's state word is
// the one [TaskNode.doingNow] already replaces, so this is the whole of what
// the surface had to learn about lists — and it is asserted on the notice
// because the notice is what a surface draws.
func TestTickingAnItemMovesTheRowToTheNextItem(t *testing.T) {
	items := []string{"read the schema", "read the migration", "say which disagrees"}
	// The graph is handed to the worker's own script through this, and it is set
	// BEFORE the turn that starts the worker: a variable assigned after Submit is
	// a variable the worker's goroutine may read before it is written.
	var (
		mu      sync.Mutex
		running *TaskGraph
		drawn   string
	)

	completer := newQuickLanes([]step{
		quickCall("q1", "compare the pair", "compare the SCHEMA-WALK pair", items, nil),
		finalText("it is out"),
	})
	completer.lane("SCHEMA-WALK",
		func(context.Context, []ai.Message) (*ai.Response, error) {
			ticked, _ := json.Marshal(map[string]int{"done": 1})
			return toolResponse("i1", quickItemsToolName, string(ticked)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			// The row as it stands one tick in, read at the only moment a scripted
			// model can read it: the request that follows the call.
			mu.Lock()
			graph := running
			mu.Unlock()
			drawn = graph.node(1).notice().Doing
			return textResponse("the migration is the one that disagrees"), nil
		},
	)

	agent, graph, _ := quickAgent(t, completer)
	mu.Lock()
	running = graph
	mu.Unlock()

	collect(t, mustSubmit(t, agent, "compare the schema and the migration"))
	node := graph.node(1)
	if node == nil {
		t.Fatal("no quick node was admitted")
	}
	waitDoneNode(t, node)

	if want := "quick · 1/3 · read the migration"; drawn != want {
		t.Fatalf("the row read %q one tick in, want %q", drawn, want)
	}
	// AND THE WORKER WAS TOLD WHERE IT HAD GOT TO, in the words the tool answers
	// with, because the count is the only thing it cannot see for itself.
	if !completer.sawIn("SCHEMA-WALK", "items 1/3 done") {
		t.Fatal("the `items` call did not answer with the count, so the worker cannot tell how far down its list it is")
	}
	// A LANDED ROW HAS NO DOING LINE AT ALL. The word describes what is happening
	// now, and nothing is.
	if doing := node.notice().Doing; doing != "" {
		t.Fatalf("a landed quick task still says %q", doing)
	}
}

// ── (4) a quick task under a task ───────────────────────────────────────────

// A QUICK TASK STARTED BY A TASK HANGS UNDER IT AND REPORTS TO IT. Nesting is
// [TaskNotice.Parent] and nothing else, so this comes for free — and the half
// worth pinning is the other one: the note goes to the WORKER that asked for
// it, never to the conversation, which is the whole reason a worker can hand
// out a piece of reading and carry on.
func TestAQuickTaskUnderATaskReportsToItsTaskAndNotTheConversation(t *testing.T) {
	completer := newQuickLanes([]step{
		proposeCall("Reconcile the tables", "work out which table is authoritative"),
		finalText("handed off"),
	})
	// The task's own worker, told apart by the mark riding in its brief: it hands
	// the reading to a quick task, ends its turn, and answers on the turn the
	// quick task's note starts for it.
	completer.lane(taskBriefMark,
		quickCall("q1", "read the ledger table", "read the LEDGER-SIDE table and say what it holds", nil, nil),
		finalText("the reading is out; I will answer when it lands"),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The ledger table is the authoritative one."), nil
		},
	)
	completer.lane("LEDGER-SIDE", func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("LEDGER holds four thousand settled rows."), nil
	})

	agent, graph, _ := quickAgent(t, completer)
	collect(t, mustSubmit(t, agent, "which of the two tables is authoritative?"))

	task := graph.node(1)
	if task == nil {
		t.Fatal("no task was admitted")
	}
	// The quick task is started by the WORKER and so appears after the
	// conversation's own turn has ended.
	waitFor(t, "the worker to hand its reading to a quick task", func() bool {
		graph.mu.Lock()
		defer graph.mu.Unlock()
		return len(graph.order) == 2
	})
	quick := graph.node(2)
	waitDoneNode(t, quick)
	waitDoneNode(t, task)

	if notice := quick.notice(); notice.Parent != task.id {
		t.Fatalf("the quick task hangs under %d, want the task %d", notice.Parent, task.id)
	}
	if notice := quick.notice(); notice.Kind != TaskKindQuick {
		t.Fatalf("the nested node's kind is %q", notice.Kind)
	}
	// AND ITS ANSWER REACHED THE WORKER THAT ASKED FOR IT.
	if !completer.sawIn(taskBriefMark, "LEDGER holds four thousand settled rows.") {
		t.Fatal("the quick task's answer never reached the worker that started it")
	}
	// AND NOT THE CONVERSATION, whose own lane was never handed the quick task's
	// words: a person watching a chat must not have somebody else's reading
	// scroll past.
	if completer.sawIn("", "LEDGER holds four thousand settled rows.") {
		t.Fatal("the quick task's answer was delivered into the conversation as well as to its parent")
	}
}

// ── (5) the person keeps working ────────────────────────────────────────────

// A QUICK NODE HOLDS ITS FILES, NEVER THE TREE. An in-place node whose worktree
// is set claims the whole directory the moment it runs (treehold.go's
// [TaskGraph.claimOver]) and every write from the conversation into it is
// refused with its name on the refusal. That is the right law for a task the
// person pointed at their own folder and the exact opposite of what a quick
// task is for, so [Agent.runQuickNode] deliberately never sets one — and this
// is the test that says so out loud, because the field is one line away in
// every other body in this package.
func TestAWriteFromTheChatIsNotRefusedWhileAQuickTaskRuns(t *testing.T) {
	release := make(chan struct{})
	completer := newQuickLanes([]step{
		quickCall("q1", "draft the note", "draft the HOLDING-PATTERN note", nil, []string{"drafted.md"}),
		finalText("it is out"),
	})
	completer.lane("HOLDING-PATTERN", func(context.Context, []ai.Message) (*ai.Response, error) {
		<-release
		return textResponse("the note is drafted"), nil
	})

	agent, graph, workspace := quickAgent(t, completer)
	events := mustSubmit(t, agent, "draft the note")
	waitFor(t, "the quick task to start", func() bool {
		return graph.node(1) != nil && graph.node(1).stateNow() == TaskRunning
	})

	// The conversation's own write, into a path the quick task did not claim,
	// asked of the guard the same way a real write is.
	if claim, held := graph.claimOver(workspace+"/unrelated.md", 0, ""); held {
		t.Fatalf("a running quick task holds the whole workspace: task %d (%q) at %s", claim.id, claim.title, claim.dir)
	}
	close(release)
	collect(t, events)
	waitDoneNode(t, graph.node(1))
}

// transcriptCarries reports whether anything the conversation has recorded —
// which includes every tool result it read — contains the needle.
func transcriptCarries(agent *Agent, needle string) bool {
	for _, entry := range agent.Transcript() {
		if strings.Contains(entry.Text, needle) {
			return true
		}
	}
	return false
}

// ── (6) a session that closed under it ──────────────────────────────────────

// A QUICK TASK CAUGHT BY THE CLOSE SETTLES AND NEVER COMES BACK.
//
// What tells the runner to hand a node to the quick body is `taskSpec.quick`,
// and that field is not in the checkpoint — so a quick node put back on the
// frontier is one the next session would run as an ORDINARY WORKER: a copy of
// the folder, a branch and a check, for work whose whole promise was that it
// had none of those. It is the design's and the run's own law
// (task_store.go's [interrupt]) and it is pinned here because the fall-through
// under it is what an unlisted kind silently gets.
func TestAQuickTaskCaughtByTheCloseSettlesRatherThanResuming(t *testing.T) {
	settled, branch := interrupt(taskRecord{Kind: TaskKindQuick, State: TaskRunning}, "")
	if settled.State != TaskFailed {
		t.Fatalf("an interrupted quick task came back %q — the next session would run it as a worker in a worktree", settled.State)
	}
	if settled.Report != quickInterruptedReport {
		t.Fatalf("it settled saying %q, want %q", settled.Report, quickInterruptedReport)
	}
	if settled.EndedAt.IsZero() {
		t.Fatal("a quick task that settles on the close carries no ending time, so its row rebuilds undated")
	}
	if branch != "" {
		t.Fatalf("an interrupted quick task named branch %q, and it has none to name", branch)
	}
}

// ── (7) the list's own invariant ────────────────────────────────────────────

// DONE IS PARALLEL TO ITEMS, AND THE SPEC HOLDS IT ITSELF.
//
// A spec whose ticks were shorter than its list used to be a spec that took the
// session down on the first tick: the ceiling road built its own literal and
// left `done` nil (checkpoint_quick.go), so `items {"done": 1}` indexed off the
// end of a slice of length zero. The invariant is now the spec's, so a road
// that forgets — including one nobody has written yet — starts the list
// untickled instead of faulting.
func TestASpecWhoseTicksAreShortOfItsItemsGrowsRatherThanFaulting(t *testing.T) {
	// A spec assembled the way a road that forgot the field assembles one.
	spec := &quickTaskSpec{line: "walk the three", items: []string{"one", "two", "three"}}
	spec.growDoneLocked()
	if len(spec.done) != len(spec.items) {
		t.Fatalf("done is %d long against %d items, want them parallel", len(spec.done), len(spec.items))
	}
	if spec.doneCountLocked() != 0 {
		t.Fatalf("a list nobody has ticked counts %d done, want none", spec.doneCountLocked())
	}

	// AND IT NEVER THROWS A TICK AWAY. Growing is the only move: a shorter
	// `done` would be the harness deciding the worker had not done work it said
	// it had done.
	spec.done[2] = true
	spec.items = append(spec.items, "four")
	spec.growDoneLocked()
	if len(spec.done) != 4 || !spec.done[2] {
		t.Fatalf("done = %v after the list grew, want the third tick kept and a fourth slot added", spec.done)
	}

	// AND THE CONSTRUCTOR HOLDS IT FROM THE FIRST MOMENT, which is what every
	// road is now made to come through.
	made := newQuickTaskSpec("walk the two", []string{"one", "two"}, nil)
	if len(made.done) != 2 {
		t.Fatalf("a spec straight from the constructor has %d ticks against 2 items", len(made.done))
	}
	if made.nextItemLocked() != "one" {
		t.Fatalf("the constructor's spec is on %q, want the first item", made.nextItemLocked())
	}
}

// THE LAST TICK TELLS THE WORKER TO ANSWER.
//
// A list that is all ticked has one move left, and the reply to the tick that
// finished it says so. Pinned on a measured run: a quick worker ticked 4/4,
// lost its answer twice to a provider fault, hopped to another model and read
// on for eleven minutes without ever saying anything — the sentence about
// ending was in a system prompt the hop did not re-read, and nothing in the
// transcript itself said the work was over. The tool's reply is in the
// transcript, so it is the one place that sentence cannot be missed.
func TestTheLastTickTellsTheWorkerItsNextMessageIsTheAnswer(t *testing.T) {
	graph := &TaskGraph{}
	node := &TaskNode{graph: graph, spec: taskSpec{quick: newQuickTaskSpec("walk the two", []string{"one", "two"}, nil)}}

	reply, _ := node.quickListChange(1, nil)
	if strings.Contains(reply, quickListDoneWord) {
		t.Fatalf("the first tick of two already says to answer: %q", reply)
	}
	reply, _ = node.quickListChange(2, nil)
	if !strings.HasSuffix(reply, quickListDoneWord) {
		t.Fatalf("the tick that finished the list replied %q, want it to end on %q", reply, quickListDoneWord)
	}

	// AND A TICK THAT ADDS IN THE SAME BREATH IS NOT THE LAST: the list grew,
	// so the worker is told the new count and nothing about ending.
	node = &TaskNode{graph: graph, spec: taskSpec{quick: newQuickTaskSpec("walk the one", []string{"one"}, nil)}}
	reply, _ = node.quickListChange(1, []string{"and then two"})
	if strings.Contains(reply, quickListDoneWord) {
		t.Fatalf("a tick that appended a step still says to answer: %q", reply)
	}
}

// A FAULT UNDER THE LIST DOOR DOES NOT KEEP THE GRAPH'S LOCK.
//
// This is the difference between a tool that faults and a session that stops. A
// panicking tool is survivable by design — [Agent.runToolsWarm] seeds every slot
// with a refusal and recovers into it, and the model reads it and carries on —
// but the door's unlock used to be written at the bottom of the body, so a fault
// on the way past kept `graph.mu` for good. The node's heartbeat, its row and
// its landing all take that lock, which is why the measured run showed no
// further model call and a row left `running` until the window was killed.
//
// The fault is induced through a node with no quick spec at all, because after
// the invariant above no ordinary tick can fault any more. What is asserted is
// not the panic — it is that the lock is free afterwards.
func TestAFaultUnderTheListDoorDoesNotKeepTheGraphsLock(t *testing.T) {
	graph := &TaskGraph{}
	node := &TaskNode{graph: graph}

	func() {
		// The fault itself is not the assertion — a door that answered this
		// instead of faulting would be fine too. What is asserted is what the
		// lock looks like on the way out either way.
		defer func() { _ = recover() }()
		node.quickItemChange(1, nil)
	}()

	if !graph.mu.TryLock() {
		t.Fatal("the graph's lock is still held after a fault under the list door: every later reader of the graph — the node's beat, its row, its landing — would block for the life of the session")
	}
	graph.mu.Unlock()
}

// A QUICK TASK WHOSE TURN ENDS ON AN ERROR LANDS, AND SAYS SO IN WORDS.
//
// The row must never be left running: whatever ends the child's turn, the node
// settles and the card carries an account a person can read. Pinned because the
// measured failure was the other thing entirely — a node that neither finished
// nor failed, still `running` a quarter of an hour later.
func TestAQuickTaskWhoseTurnEndsOnAnErrorLandsRatherThanHanging(t *testing.T) {
	completer := newQuickLanes([]step{
		quickCall("q1", "read the ledger", "read the ERRAND-SIDE ledger and say what it holds", []string{"open it", "say what it holds"}, nil),
		finalText("asked for it"),
	})
	// The worker's every call fails. The script is long enough that no retry
	// ladder can walk off the end of it and be answered by the fixture's own
	// unscripted reply.
	failing := func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("provider returned error: 502 bad gateway")
	}
	worker := make([]step, 0, 24)
	for round := 0; round < 24; round++ {
		worker = append(worker, failing)
	}
	completer.lane("ERRAND-SIDE", worker...)

	agent, graph, _ := quickAgent(t, completer)
	// AND THE WORKER'S LADDER IS SPENT ON THE TEST'S CLOCK: what ends it is the
	// node's own give-up, which is minutes of a person's time ([onATestClock]).
	onATestClock(t)
	collect(t, mustSubmit(t, agent, "read the ledger for me"))
	node := quickNodeSaying(t, graph, "ERRAND-SIDE")
	waitDoneNode(t, node)

	notice := node.notice()
	if notice.State == TaskRunning || notice.State == "" {
		t.Fatalf("a quick task whose turn ended is still %q", notice.State)
	}
	if notice.State != TaskFailed {
		t.Fatalf("it landed %q, want %q — nothing about its work is known", notice.State, TaskFailed)
	}
	// A LANDED ROW HAS NO DOING LINE, whatever ended it.
	if notice.Doing != "" {
		t.Errorf("a landed quick task still says %q", notice.Doing)
	}
	// AND THE CARD LEADS WITH WHAT HAPPENED, in the person's words. No
	// machinery: whatever the harness caught, the card is what somebody reads.
	if !strings.HasPrefix(notice.Report, "the quick task ended on an error") {
		t.Fatalf("the card reads %q, want it to open on what happened", notice.Report)
	}
	for _, banned := range []string{"panic", "goroutine", "nil pointer", "index out of range"} {
		if strings.Contains(strings.ToLower(notice.Report), banned) {
			t.Errorf("the card says %q, and %q is machinery a person is never shown", notice.Report, banned)
		}
	}
}

// ── (8) the conversation that reopens ───────────────────────────────────────

// A QUICK TASK COMES BACK OUT OF THE CHECKPOINT, AND SO DOES EVERYTHING BESIDE
// IT.
//
// This is the defect, end to end through the real door. A quick node is
// admitted with NO ACCEPTANCE on purpose — nothing checks one, so a "DONE WHEN"
// would be a contract with no reader (task_quick.go's [Agent.newQuickSpec]) —
// and [decodeTasks] refused any node without one. The refusal is whole-document
// by design, so ONE quick task anywhere in a conversation's history meant that
// conversation reopened with no task graph at all: every finished row, every
// piece of running work, the whole family, gone, and one line in a log file
// saying the checkpoint was corrupt. Measured on a real one, 2026-09-11.
//
// WHAT IT DOES NOT ASSERT IS THE LIST. A quick node's items and ticks are not on
// the record at all, so a restored one comes back with none — which is why one
// that has not finished settles rather than going back on the frontier
// ([nothingIsComingBackForIt]). Carrying the list is the record's own shape and
// is somebody else's change; this one is the rule the decoder asks.
func TestAQuickTaskComesBackFromTheCheckpoint(t *testing.T) {
	items := []string{"read the schema", "read the migration", "say which disagrees"}
	completer := newQuickLanes([]step{
		quickCall("q1", "compare the pair", "compare the LEDGER-WALK pair", items, []string{"notes.md"}),
		finalText("it is out"),
	})
	// The worker ticks an item and answers, so the node reaching the checkpoint
	// is one whose list was being written while it ran rather than a node that
	// went straight from admitted to done.
	completer.lane("LEDGER-WALK",
		func(context.Context, []ai.Message) (*ai.Response, error) {
			ticked, _ := json.Marshal(map[string]int{"done": 2})
			return toolResponse("i1", quickItemsToolName, string(ticked)), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the migration is the one that disagrees"), nil
		},
	)

	journal, checkpoint := journalIn(t)
	agent, graph, _ := quickAgent(t, completer, func(config *Config) {
		config.SessionFile = journal
	})
	collect(t, mustSubmit(t, agent, "compare the schema and the migration"))
	node := quickNodeSaying(t, graph, "LEDGER-WALK")
	waitDoneNode(t, node)
	id := node.id
	written := awaitRecord(t, checkpoint, id, func(record taskRecord) bool {
		return record.State == TaskDone
	}, "done")

	// ON DISK: the kind, and the empty acceptance that is the design rather than
	// a field somebody forgot.
	if written.Kind != TaskKindQuick {
		t.Fatalf("the checkpoint calls node %d a %q, want %q", id, written.Kind, TaskKindQuick)
	}
	if written.Acceptance != "" {
		t.Fatalf("a quick node was written with acceptance %q — nothing checks one, and inventing a clause is the wrong fix", written.Acceptance)
	}

	// AND THE WHOLE FILE IS STILL A FILE THIS STORE WILL READ. This is the line
	// the defect failed on: before the fix `ok` was false and the document that
	// came back was empty.
	document, ok := loadTaskCheckpoint(checkpoint)
	if !ok {
		t.Fatal("a conversation that ran one quick task reopens with NO TASK GRAPH: the checkpoint was refused whole")
	}

	// AND IT REBUILDS AS A QUICK NODE — the kind, the state it landed in, and the
	// answer that was its whole point.
	fresh := newTaskGraph()
	fresh.rehydrate(document, t.TempDir(), TaskSettleAsk)
	back := fresh.node(id)
	if back == nil {
		t.Fatalf("node %d is not in the rebuilt graph", id)
	}
	if back.kind != TaskKindQuick {
		t.Fatalf("it came back a %q, want %q", back.kind, TaskKindQuick)
	}
	if back.spec.acceptance != "" {
		t.Fatalf("it came back carrying acceptance %q, which nothing wrote", back.spec.acceptance)
	}
	if state := back.stateNow(); state != TaskDone {
		t.Fatalf("a finished quick task came back %q", state)
	}
	if report := back.notice().Report; !strings.Contains(report, "the migration") {
		t.Fatalf("its answer did not survive: %q", report)
	}
}

// THE RULE STAYS STRICT FOR ORDINARY WORK, and that is the half of this fix
// that is easy to lose. A node with no acceptance is a node no auditor can
// judge, so a document carrying one was not written by this code and is refused
// whole. What changed is only that the question is now asked OF THE KIND: the
// one kind admitted without an acceptance is let through, and every other node
// answers for itself.
func TestOnlyAQuickNodeMayCarryNoAcceptance(t *testing.T) {
	document := func(kind TaskKind) []byte {
		encoded, err := json.MarshalIndent(taskDocument{
			Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
			Nodes: []taskRecord{{
				ID: 1, Title: "compare the pair", Brief: "compare the pair",
				Acceptance: "", Kind: kind, State: TaskDone, Noted: true,
			}},
		}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}

	if _, err := decodeTasks(document(TaskKindQuick)); err != nil {
		t.Fatalf("a quick node with no acceptance was refused: %v — nothing ever checks one", err)
	}
	for _, kind := range []TaskKind{"", TaskKindHarness, TaskKindSubharness} {
		if _, err := decodeTasks(document(kind)); err == nil {
			t.Fatalf("a %q node with no acceptance was accepted, and the rule that catches a half-written file is gone", kind)
		}
	}
}

// THE REAL FILE'S SHAPE, PINNED.
//
// This is the checkpoint the defect was found in, reduced to the shape that
// matters: four finished quick nodes with `"acceptance": ""` beside ordinary
// work that has an acceptance, a parent and a dependency. Every one of those
// nodes was thrown away together, so the conversation reopened empty.
//
// The queued quick node is the other half of the ruling. A quick node's list is
// not on the record, so nothing here can rebuild the body it would run — and the
// one thing that must never happen to it is being handed to an ORDINARY worker: a
// copy of the folder, a branch and a check for work whose whole promise was that
// it had none of those. It settles, and says so.
func TestACheckpointFromAConversationThatRanQuickTasksDecodes(t *testing.T) {
	const content = `{"type":"tasks","version":1,"seq":7,"nodes":[
		{"id":1,"title":"index.html — Y2K chrome home","summary":"s","brief":"b","acceptance":"","kind":"quick","state":"done","noted":true},
		{"id":2,"title":"contact.html — risograph print","summary":"s","brief":"b","acceptance":"","kind":"quick","state":"done","noted":true},
		{"id":3,"title":"evidence.html — phosphor terminal","summary":"s","brief":"b","acceptance":"","kind":"quick","state":"done","noted":true},
		{"id":4,"title":"product.html — blueprint schematic","summary":"s","brief":"b","acceptance":"","kind":"quick","state":"done","noted":true},
		{"id":5,"title":"quick insights","summary":"s","brief":"b","acceptance":"everything asked for is done","state":"done","noted":true},
		{"id":6,"title":"Fix the hero headline","summary":"s","brief":"b","acceptance":"the screenshot shows separation","parent":5,"depends_on":[5],"state":"queued"},
		{"id":7,"title":"read the other two","summary":"s","brief":"b","acceptance":"","kind":"quick","parent":5,"state":"queued"}]}`

	document, err := decodeTasks([]byte(content))
	if err != nil {
		t.Fatalf("a conversation that ran four quick tasks cannot reopen: %v", err)
	}
	if len(document.Nodes) != 7 {
		t.Fatalf("%d nodes came back, want all 7", len(document.Nodes))
	}

	graph := newTaskGraph()
	recovery := graph.rehydrate(document, t.TempDir(), TaskSettleAsk)
	for id := uint64(1); id <= 4; id++ {
		node := graph.node(id)
		if node == nil {
			t.Fatalf("quick node %d is not in the rebuilt graph", id)
		}
		if node.kind != TaskKindQuick {
			t.Fatalf("node %d came back a %q, want %q", id, node.kind, TaskKindQuick)
		}
		if state := node.stateNow(); state != TaskDone {
			t.Fatalf("finished quick node %d came back %q", id, state)
		}
	}
	// The ordinary work beside them is untouched: its acceptance, its family and
	// its edge are all still there, and it is still waiting its turn.
	ordinary := graph.node(6)
	if ordinary == nil || ordinary.spec.acceptance == "" {
		t.Fatalf("the ordinary node lost its acceptance: %+v", ordinary)
	}
	if ordinary.parent != 5 || len(ordinary.dependsOn) != 1 || ordinary.dependsOn[0] != 5 {
		t.Fatalf("the ordinary node's family and edge did not survive: parent %d, waits on %v", ordinary.parent, ordinary.dependsOn)
	}
	if state := ordinary.stateNow(); state != TaskQueued {
		t.Fatalf("the ordinary node came back %q, want it still waiting", state)
	}

	// AND THE QUICK NODE THAT NEVER STARTED IS OVER RATHER THAN ON THE FRONTIER.
	waiting := graph.node(7)
	if waiting == nil {
		t.Fatal("the queued quick node is not in the rebuilt graph")
	}
	if state := waiting.stateNow(); state != TaskFailed {
		t.Fatalf("a queued quick node came back %q — the next session would run it in a worktree with a branch and a check", state)
	}
	if report := waiting.notice().Report; report != quickLostReport {
		t.Fatalf("it settled saying %q, want %q", report, quickLostReport)
	}
	if waiting.parent != 5 {
		t.Fatalf("it lost its family: parent %d, want 5", waiting.parent)
	}
	if recovery.done != 5 || recovery.waiting != 1 || recovery.interrupted != 1 {
		t.Fatalf("recovery counted %+v, want 5 done, 1 waiting and the one that never started", recovery)
	}
}

// EVERY KIND'S ACCEPTANCE MATCHES WHAT ITS KIND DECLARES.
//
// [kindsWithoutAcceptance] is a property of the KIND, and a property is only
// worth having if it is true of the specs the builders actually make. This is
// the line that keeps the two from drifting — which is exactly what went wrong:
// `quick_task`'s builder left the field empty on purpose, the store's validator
// asked every record for one, and neither side was written against anything the
// other could read.
//
// A kind added later fails here until it either fills an acceptance or declares
// that it carries none.
func TestEveryKindsAcceptanceMatchesWhatItDeclares(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = t.TempDir()
	})
	quick, refusal := agent.newQuickSpec("compare the pair", []string{"one"}, nil, nil)
	if refusal != "" {
		t.Fatalf("the quick door refused its own test call: %s", refusal)
	}

	for _, built := range []struct {
		what string
		spec taskSpec
	}{
		{"a quick task", quick},
		{"a subharness design", taskSpec{
			title: "t", brief: "b",
			acceptance: "a page the person approves, saved into this machine's harness registry",
			design:     &harnessDesignSpec{goal: "g"},
		}},
		{"a subharness run", taskSpec{
			title: "t", brief: "b",
			acceptance: "the answer this program promises, in the shape it declares",
			run:        &subharnessRunSpec{},
		}},
		{"ordinary work", taskSpec{title: "t", brief: "b", acceptance: "the tests pass"}},
	} {
		kind := built.spec.kind()
		if !acceptanceHolds(kind, built.spec.acceptance) {
			t.Errorf("%s is admitted as a %q with acceptance %q, and that kind does not declare that it carries none: the checkpoint would refuse the whole file it lands in",
				built.what, kind, built.spec.acceptance)
		}
		// AND THE DECLARATION IS NOT SLACK EITHER. A kind that declares it
		// carries no acceptance and then fills one is a kind whose records would
		// be let through unchecked for a field it does in fact have.
		if kindWithoutAcceptance(kind) && strings.TrimSpace(built.spec.acceptance) != "" {
			t.Errorf("%s declares it carries no acceptance and was admitted with %q", built.what, built.spec.acceptance)
		}
	}
}
