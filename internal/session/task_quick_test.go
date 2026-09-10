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
func quickAgent(t *testing.T, completer Completer) (*Agent, *TaskGraph, string) {
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
