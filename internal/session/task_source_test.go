package session

// THE SOURCE OF A TASK IS CAPTURED WHEN IT IS ADMITTED, SURVIVES A REOPEN, AND IS
// NEVER INFERRED. Every test below drives a real producer (a typed message and
// propose_task, `/task`, a run row's publish) and then reads the checkpoint or
// reopens it, because a fact that is only in memory is not a fact a restart keeps.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// sourcedAgent is a conversation with a checkpoint behind it, an auto-approving
// proposal road and a runner that lands every node as asked.
func sourcedAgent(t *testing.T, steps []step, land TaskState) (*Agent, string, string) {
	t.Helper()
	journal, checkpoint := journalIn(t)
	completer := &routedCompleter{parent: steps}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.SessionFile = journal
	})
	stubbedGraph(agent, func(node *TaskNode) { node.graph.complete(node, land) })
	return agent, journal, checkpoint
}

func sendAndWait(t *testing.T, agent *Agent, words string) {
	t.Helper()
	events, err := agent.Submit(context.Background(), words)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
}

// heardAs puts a message where the session records the person's words and marks
// the turn as one the person typed into, the way [Agent.Submit] does.
func heardAs(agent *Agent, words string) {
	agent.mu.Lock()
	agent.running = true
	agent.rememberAskLocked(userText(words))
	agent.mu.Unlock()
}

func sourceOfNode(t *testing.T, agent *Agent, id uint64) *TaskSource {
	t.Helper()
	node := agent.taskNode(id)
	if node == nil {
		t.Fatalf("no node %d", id)
	}
	return node.source()
}

// A TASK PROPOSED WHILE ANSWERING A MESSAGE CARRIES THAT MESSAGE, TWO MESSAGES
// ARE TWO BLOCKS, AND THE CHECKPOINT AND A REOPEN KEEP BOTH.
func TestAProposedTaskCarriesTheMessageItWasMadeFromAndKeepsItAcrossAReopen(t *testing.T) {
	agent, _, checkpoint := sourcedAgent(t, []step{
		proposeCall("Sweep the calls", "replace every call to Frobnicate"), finalText("handed off"),
		proposeCall("Audit the pricing", "read the pricing module"), finalText("handed off"),
		proposeCall("Audit the tax", "read the tax module"), finalText("handed off"),
	}, TaskFailed)
	first, second := "sweep the Frobnicate calls", "now audit the pricing code"
	sendAndWait(t, agent, first)
	sendAndWait(t, agent, second)
	sendAndWait(t, agent, second) // the same sentence typed again is another message

	a, b, c := sourceOfNode(t, agent, 1), sourceOfNode(t, agent, 2), sourceOfNode(t, agent, 3)
	if a == nil || b == nil || c == nil {
		t.Fatalf("a task made from a typed message has no source: %v %v %v", a, b, c)
	}
	if a.Kind != TaskSourceMessage || a.Text != first || a.At.IsZero() {
		t.Fatalf("first source = %+v, want the first message, its kind and the time it was heard", a)
	}
	if b.Text != second || b.ID == a.ID {
		t.Fatalf("second source = %+v, want a block of its own carrying the second message", b)
	}
	if c.Text != second || c.ID == b.ID {
		t.Fatalf("the same sentence typed twice shared an identity: %+v %+v", b, c)
	}

	// THE CHECKPOINT HAS IT, and a fresh conversation reading that file has it.
	document := readCheckpoint(t, checkpoint)
	for i, want := range []*TaskSource{a, b, c} {
		got := document.Nodes[i].Source
		if got == nil || got.ID != want.ID || got.Text != want.Text || got.Kind != want.Kind {
			t.Fatalf("node %d on disk = %+v, want %+v", i+1, got, want)
		}
	}
	reopened, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.InTask = true })
	reopened.graph().rehydrate(document, t.TempDir(), TaskSettleAsk)
	views, err := reopened.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 3 || views[0].Source == nil || views[0].Source.ID != a.ID || views[1].Source.ID != b.ID || views[2].Source.Text != second {
		t.Fatalf("a reopened conversation lists %+v", views)
	}
}

// THE MESSAGE IS TAKEN WHEN THE CARD IS RAISED, NOT WHEN IT IS ANSWERED: typing
// again while a proposal is open must not move the proposal to the new message.
func TestAProposalKeepsItsMessageWhenThePersonTypesAgainBeforeAnswering(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	heardAs(agent, "first ask")
	spec := taskSpec{title: "t", brief: "b", acceptance: "a"}
	spec.source = agent.bornSource()
	heardAs(agent, "second ask")
	ran := make(chan *TaskNode, 1)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node; node.graph.complete(node, TaskDone) })
	graph.admit(graph.reserve(), spec)
	if got := (<-ran).source(); got == nil || got.Text != "first ask" {
		t.Fatalf("an answered proposal took the newer message: %+v", got)
	}
}

// A CHILD BELONGS TO ITS PARENT'S BLOCK, NOT TO WHATEVER THE PERSON TYPED SINCE,
// on both children roads: a node's own proposal and a part admitted under it.
func TestAChildInheritsItsParentsSourceNotTheConversationsNewestMessage(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := stubbedGraph(agent, func(node *TaskNode) {})
	heardAs(agent, "port the client to v2")
	parent := graph.reserve()
	graph.admit(parent, taskSpec{title: "p", brief: "b", acceptance: "a"})
	heardAs(agent, "unrelated: rename the logo")

	part := graph.reserve()
	graph.admit(part, taskSpec{title: "part", brief: "b", acceptance: "a", parent: parent, depth: 1})
	if got := sourceOfNode(t, agent, part); got == nil || got.Text != "port the client to v2" {
		t.Fatalf("a part admitted under a parent took %+v", got)
	}

	node, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask, config.taskID, config.taskDepth, config.tasker = true, parent, 1, graph
	})
	if got := node.bornSource(); got == nil || got.Text != "port the client to v2" {
		t.Fatalf("a node proposing a sub-task took %+v", got)
	}
	// A grandchild inherits through the child.
	grand := graph.reserve()
	graph.admit(grand, taskSpec{title: "g", brief: "b", acceptance: "a", parent: part, depth: 2})
	if got := sourceOfNode(t, agent, grand); got == nil || got.ID != sourceOfNode(t, agent, parent).ID {
		t.Fatalf("a grandchild took %+v", got)
	}
}

// A TURN NOBODY TYPED INTO HAS NO SOURCE, AND A RECORD WRITTEN BEFORE SOURCES
// EXISTED STAYS WITHOUT ONE EVEN WHEN THE PERSON TYPES AFTER IT IS REOPENED.
func TestLegacyAndUntypedWorkHasNoSourceAndNothingFillsItIn(t *testing.T) {
	journal, checkpoint := journalIn(t)
	writeCheckpoint(t, checkpoint, taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 2,
		Nodes: []taskRecord{{ID: 1, Title: "old failure", Brief: "b", Acceptance: "a", State: TaskFailed, Request: "an old request, kept as it was"}},
		Runs:  []runRecord{{ID: 2, Title: "old run", State: TaskFailed}},
	})
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask, config.SessionFile = true, journal
	})
	document := readCheckpoint(t, checkpoint)
	agent.graph().rehydrate(document, t.TempDir(), TaskSettleAsk)
	heardAs(agent, "a message typed after the reopen")

	views, _ := agent.AppTasks()
	for _, view := range views {
		if view.Source != nil {
			t.Fatalf("task %s was given a source it never recorded: %+v", view.ID, view.Source)
		}
	}
	// Republishing the legacy run row does not stamp it with today's message.
	agent.publishRunRow(agent.graph(), TaskNotice{ID: 2, Title: "old run", State: TaskFailed})
	if row, ok := agent.appRunRow(2); !ok || row.Source != nil {
		t.Fatalf("a legacy run row was stamped by a republish: %+v", row.Source)
	}
	// And a woken turn that nobody typed into has none to give.
	woken, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if got := woken.bornSource(); got != nil {
		t.Fatalf("a session nobody typed into invented %+v", got)
	}
	// The wire projection says nothing for a record with none.
	if wgSource(document.Nodes[0].Source) != nil {
		t.Fatal("the graph projection invented a source")
	}
}

// RUN ROWS ARE BORN WITH THE MESSAGE THEY WERE MADE FROM, KEEP IT THROUGH EVERY
// REPUBLISH, AND SURVIVE A REOPEN.
func TestARunRowIsBornWithItsMessageAndKeepsItThroughRepublishAndReopen(t *testing.T) {
	journal, checkpoint := journalIn(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.SessionFile = journal })
	graph := agent.graph()
	heardAs(agent, "check the three places-v* watchers")
	one, two, three := graph.reserve(), graph.reserve(), graph.reserve()
	agent.publishRunRow(graph, TaskNotice{ID: one, Title: "watch places-v1", State: TaskQueued})
	first := agent.appRunViewSource(t, one)
	heardAs(agent, "something else entirely")
	agent.publishRunRow(graph, TaskNotice{ID: one, Title: "watch places-v1", State: TaskFailed, Report: "exit 1"})
	agent.publishRunRow(graph, TaskNotice{ID: two, Title: "other", State: TaskQueued})
	if got := agent.appRunViewSource(t, one); got == nil || got.ID != first.ID || got.Text != "check the three places-v* watchers" {
		t.Fatalf("a republish moved the row's block: %+v", got)
	}
	if got := agent.appRunViewSource(t, two); got == nil || got.Text != "something else entirely" || got.ID == first.ID {
		t.Fatalf("the second row did not take the second message: %+v", got)
	}
	// An explicit expectation (a hand-off approved after the person typed again) wins.
	agent.expectRunSource(three, first)
	agent.publishRunRow(graph, TaskNotice{ID: three, Title: "approved late", State: TaskQueued})
	if got := agent.appRunViewSource(t, three); got == nil || got.ID != first.ID {
		t.Fatalf("a captured source lost to the newer message: %+v", got)
	}

	reopened, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.InTask = true })
	reopened.graph().rehydrate(readCheckpoint(t, checkpoint), t.TempDir(), TaskSettleAsk)
	if got := reopened.appRunViewSource(t, one); got == nil || got.ID != first.ID {
		t.Fatalf("a reopened run row lost its source: %+v", got)
	}
}

func (a *Agent) appRunViewSource(t *testing.T, id uint64) *TaskSource {
	t.Helper()
	row, ok := a.appRunRow(id)
	if !ok {
		t.Fatalf("no run row %d", id)
	}
	return a.appRunView(row, 0).Source
}

// `/task` IS ITS OWN SOURCE: the person's command words, as a directive, one per command.
func TestATypedTaskIsItsOwnDirectiveSource(t *testing.T) {
	agent, _, checkpoint := sourcedAgent(t, nil, TaskFailed)
	heardAs(agent, "an unrelated earlier message")
	one, _, _, err := agent.StartTask(context.Background(), "fix the nil-map crash in internal/store", true)
	if err != nil {
		t.Fatal(err)
	}
	two, _, _, err := agent.StartTask(context.Background(), "fix the nil-map crash in internal/store", true)
	if err != nil {
		t.Fatal(err)
	}
	a, b := sourceOfNode(t, agent, one), sourceOfNode(t, agent, two)
	if a == nil || b == nil || a.Kind != TaskSourceDirective || a.Text != "fix the nil-map crash in internal/store" || a.ID == b.ID {
		t.Fatalf("typed tasks carry %+v and %+v", a, b)
	}
	if document := readCheckpoint(t, checkpoint); document.Nodes[0].Source == nil || document.Nodes[0].Source.Kind != TaskSourceDirective {
		t.Fatalf("the directive is not on disk: %+v", document.Nodes[0].Source)
	}
}

// THE SOURCE IS BOUNDED ON A RUNE BOUNDARY AND SAYS IT WAS CUT.
func TestASourceKeepsOnlyTheFirstFourKiBAndSaysSo(t *testing.T) {
	long := strings.Repeat("é", MaxTaskSourceBytes) // two bytes a rune
	text, cut := boundSourceText(long)
	if !cut || len(text) > MaxTaskSourceBytes || !strings.HasPrefix(long, text) || strings.ContainsRune(text, '�') {
		t.Fatalf("bound = %d bytes, cut %v", len(text), cut)
	}
	if short, was := boundSourceText("  hello  "); was || short != "hello" {
		t.Fatalf("a short message was altered: %q %v", short, was)
	}
}

// TWO CONVERSATIONS NEVER SHARE A BLOCK: the same words typed to two agents are two sources.
func TestTwoConversationsNeverShareASourceIdentity(t *testing.T) {
	one, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	two, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	heardAs(one, "same words")
	heardAs(two, "same words")
	a, b := one.bornSource(), two.bornSource()
	if a == nil || b == nil || a.ID == b.ID {
		t.Fatalf("sources %+v / %+v collide", a, b)
	}
	if ghost := (*Agent)(nil).bornSource(); ghost != nil {
		t.Fatal("a missing agent produced a source")
	}
	_ = time.Now
}

// A PLAN ROW BELONGS TO THE BLOCK OF THE RECORD THAT OWNS IT — the run row whose
// PlanTask names it, or the node whose PlanID does — and never to a record that merely
// has the same number. A part a worker filed under a number that equals an unrelated
// node's is not joined to that node; it belongs to its nearest owned ancestor. A legacy
// owner (no source) is a final answer: it is not handed its run root's block.
func TestPlanRowsTakeTheirSourceFromTheRecordThatOwnsThemNeverFromANumberCollision(t *testing.T) {
	folder := filepath.Join(t.TempDir(), sourceOwner)
	place := Place{Dir: folder}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile, config.Place = place.Transcript(), place
	})
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(*TaskNode) {}
	graph.mu.Unlock()
	for graph.reserve() < 12 { // the checkpoint's counter must cover the ids it names
	}
	unrelated := &TaskSource{ID: "s#other", Kind: TaskSourceMessage, Text: "an unrelated message"}
	stranger := graph.reserve() // a node whose number a worker's part will reuse
	graph.admit(stranger, taskSpec{title: "unrelated node", brief: "b", acceptance: "a", source: unrelated})
	strangerID := strconv.FormatUint(stranger, 10)
	owned := graph.reserve() // a node that really owns plan row "ownedpart"
	graph.admit(owned, taskSpec{title: "owner node", brief: "b", acceptance: "a", planID: "ownedpart", source: &TaskSource{ID: "s#3", Kind: TaskSourceMessage, Text: "three"}})

	path := filepath.Join(folder, planStoreFilename)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(path, "the run", "9", "The run", "drive", sourceOwner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "11", Title: "joined from message two", ParentID: "9"},
		{ID: "partx", Title: "a part a worker filed", ParentID: "11"},
		{ID: strconv.FormatUint(stranger, 10), Title: "a worker reused an unrelated node's number", ParentID: "11"},
		{ID: "12", Title: "joined, recorded before sources existed", ParentID: "9"},
		{ID: "direct", Title: "filed straight under the run", ParentID: "9"},
		{ID: "ownedpart", Title: "owned by a node", ParentID: "9"},
	}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	armPlanStore(t, agent, path, sourceOwner)
	one, two := &TaskSource{ID: "s#1", Kind: TaskSourceMessage, Text: "one"}, &TaskSource{ID: "s#2", Kind: TaskSourceMessage, Text: "two"}
	graph.keepRunRows(9, []TaskNotice{{ID: 9, Title: "run", State: TaskRunning, Source: one, PlanTask: "t-9"}})
	graph.keepRunRows(11, []TaskNotice{{ID: 11, Title: "joined", State: TaskRunning, Source: two, PlanTask: "t-11"}})
	graph.keepRunRows(12, []TaskNotice{{ID: 12, Title: "legacy joined", State: TaskRunning, PlanTask: "t-12"}})

	want := map[string]string{
		"t-9": "s#1", "t-11": "s#2", "t-partx": "s#2",
		"t-" + strangerID: "s#2", // its parent's block, NOT the node that shares its number
		"t-12":            "",    // an owner recorded before sources: final, not the root's
		"t-direct":        "s#1", "t-ownedpart": "s#3",
	}
	got := map[string]string{}
	for _, row := range agent.PlanTasks() {
		got[row.ID] = ""
		if row.Source != nil {
			got[row.ID] = row.Source.ID
		}
	}
	owner, err := NewWorkSource().Owner(context.Background(), sourceOwner, folder)
	if err != nil {
		t.Fatal(err)
	}
	for id, source := range want {
		if got[id] != source {
			t.Errorf("PlanTasks: %s source = %q, want %q", id, got[id], source)
		}
		fact, ok := owner.Find(id)
		if !ok {
			t.Fatalf("the graph holds no %s", id)
		}
		var seen string
		if fact.Node.Source != nil {
			seen = fact.Node.Source.ID
		}
		if seen != source {
			t.Errorf("work graph: %s source = %q, want %q", id, seen, source)
		}
	}
}

// DISMISS WORKS ON A ROW OF A RUN'S PLAN STORE TOO, persisted with the same set,
// and a plan row still running or waiting is refused.
func TestPlanDismissIsRefusedWithoutMutationAndLegacyRestoreStillWorks(t *testing.T) {
	folder := filepath.Join(t.TempDir(), sourceOwner)
	place := Place{Dir: folder}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile, config.Place = place.Transcript(), place
	})
	path := filepath.Join(folder, planStoreFilename)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(path, "the run", planRootID, "The run", "drive", sourceOwner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "bad", Title: "fails"}, {ID: "live", Title: "waits on bad", Dependencies: []plandb.Dependency{{TaskID: "bad", Kind: plandb.DepBlocks}}}, {ID: "alone", Title: "fails alone"}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"bad", "alone"} {
		if _, err := store.Claim(id, "w"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Fail(id, "w", "exit 1"); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	armPlanStore(t, agent, path, sourceOwner)
	if v := viewOf(t, agent, "t-alone"); v.State != "failed" || v.Dismissed {
		t.Fatalf("t-alone = %+v", v)
	}
	for _, id := range []string{"t-alone", "t-bad", "t-live"} {
		before := viewOf(t, agent, id)
		if err := act(agent, id, "dismiss"); !errors.Is(err, ErrPlanDismissUnsupported) {
			t.Fatalf("dismiss %s = %v", id, err)
		}
		after := viewOf(t, agent, id)
		if after.Dismissed || before.State != after.State || before.Result != after.Result || before.Spent != after.Spent {
			t.Fatalf("unsupported dismissal changed %s", id)
		}
	}
	// An older checkpoint may already contain a hidden plan row. Restoring is safe
	// and remains available: it only makes the existing failure visible again.
	if err := agent.graph().setDismissed("t-alone", true, false); err != nil {
		t.Fatal(err)
	}
	owner, err := NewWorkSource().Owner(context.Background(), sourceOwner, folder)
	if err != nil {
		t.Fatal(err)
	}
	if fact, _ := owner.Find("t-alone"); !fact.Node.Dismissed {
		t.Fatal("legacy hidden record was lost")
	}
	if err := act(agent, "t-alone", "restore"); err != nil || viewOf(t, agent, "t-alone").Dismissed {
		t.Fatalf("restore: %v", err)
	}

}

// AN ADAPTIVE RUN IS ONE BLOCK: its root row is born with the message it was started
// from, and every row published after it — a worker, a republish, a settle — carries it.
func TestAnAdaptiveRunAndItsWorkersShareTheMessageThatStartedIt(t *testing.T) {
	journal, checkpoint := journalIn(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.SessionFile = journal })
	heardAs(agent, "audit the pricing code")
	family := agent.newOrchestrateFamily("audit the pricing code", "", "run-1")
	heardAs(agent, "a later, unrelated message")
	worker := agent.graph().reserve()
	family.publish(TaskNotice{ID: worker, Run: "run-1", Parent: family.root, Title: "worker", State: TaskRunning})
	family.publish(TaskNotice{ID: family.root, Run: "run-1", Title: "audit the pricing code", State: TaskDone})

	document := readCheckpoint(t, checkpoint)
	if len(document.Runs) != 2 {
		t.Fatalf("runs on disk = %d", len(document.Runs))
	}
	for _, row := range document.Runs {
		if row.Source == nil || row.Source.Text != "audit the pricing code" || row.Source.Kind != TaskSourceMessage {
			t.Fatalf("row %d on disk carries %+v", row.ID, row.Source)
		}
	}
	if document.Runs[0].Source.ID != document.Runs[1].Source.ID {
		t.Fatalf("a run's rows are two blocks: %+v / %+v", document.Runs[0].Source, document.Runs[1].Source)
	}
}

// THE WORDS ARE SCRUBBED BEFORE THEY ARE CUT AND CUT ON A BYTE BOUND THAT KEEPS THE TEXT
// VALID: a secret straddling the limit is not left half-spelled, and a multi-byte rune is
// never split, on either wire.
func TestProjectedSourceWordsAreScrubbedThenCutOnAValidByteBound(t *testing.T) {
	secret := "sk-ant-api03-" + strings.Repeat("x", 60)
	body := strings.Repeat("a ", (MaxTaskSourceBytes-20)/2) + " " + secret + strings.Repeat("é", 100)
	got := wgSource(&TaskSource{ID: "s#1", Kind: TaskSourceMessage, Text: body})
	if got == nil || len(got.Text) > MaxTaskSourceBytes || strings.Contains(got.Text, "sk-ant") || strings.ContainsRune(got.Text, '\uFFFD') {
		t.Fatalf("projected %d bytes, scrubbed=%v", len(got.Text), !strings.Contains(got.Text, "sk-ant"))
	}
	if !got.Cut {
		t.Fatal("a cut source does not say so")
	}
	multi := wgSource(&TaskSource{ID: "s#2", Kind: TaskSourceMessage, Text: strings.Repeat("é", MaxTaskSourceBytes)})
	if len(multi.Text) > MaxTaskSourceBytes || !strings.HasPrefix(strings.Repeat("é", MaxTaskSourceBytes), multi.Text) {
		t.Fatalf("a multi-byte source was cut to %d bytes", len(multi.Text))
	}
}

func TestQuickPromotionPreservesRecordedSourceOrUnknownDuringALaterMessage(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(strconv.FormatBool(legacy), func(t *testing.T) {
			agent, node, _ := appNodeFixture(t, true)
			heardAs(agent, "first message that made the quick task")
			original := agent.bornSource()
			node.graph.mu.Lock()
			node.state = TaskDone
			if !legacy {
				node.spec.source = cloneSource(original)
			}
			node.graph.mu.Unlock()
			graph := stubbedGraph(agent, func(n *TaskNode) { n.graph.complete(n, TaskDone) })
			heardAs(agent, "later unrelated message")
			if err := agent.promoteQuickDirect(node); err != nil {
				t.Fatal(err)
			}
			promoted := graph.node(2)
			if promoted == nil {
				t.Fatal("promotion admitted no full task")
			}
			got := promoted.source()
			if legacy && got != nil {
				t.Fatalf("legacy source was fabricated from current turn: %+v", got)
			}
			if !legacy && (got == nil || got.ID != original.ID) {
				t.Fatalf("promotion source=%+v want original %+v", got, original)
			}
		})
	}
}

func TestQuickAdmissionRecordsTheActualMessageBeforeALaterTurn(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := stubbedGraph(agent, func(n *TaskNode) { n.graph.complete(n, TaskDone) })
	heardAs(agent, "original quick request")
	graph.admit(graph.reserve(), taskSpec{title: "quick", brief: "q", acceptance: "a", quick: newQuickTaskSpec("read a file", nil, nil)})
	got := graph.node(1).source()
	if got == nil || got.Text != "original quick request" {
		t.Fatalf("source=%+v", got)
	}
	heardAs(agent, "a later message")
	if graph.node(1).source().ID != got.ID {
		t.Fatal("later message changed recorded quick source")
	}
}

func TestExplicitUnknownSourceNeverFallsBackToANewerMessage(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	heardAs(agent, "unrelated current message")
	ctx := withTaskSource(context.Background(), nil)
	if !taskSourceCaptured(ctx) || taskSourceFrom(ctx) != nil {
		t.Fatal("explicit unknown was not retained")
	}
	agent.expectRunSource(99, taskSourceFrom(ctx))
	if got := agent.runBorn(99); got != nil {
		t.Fatalf("a delayed run took unrelated current source %+v", got)
	}
	graph := stubbedGraph(agent, func(n *TaskNode) { n.graph.complete(n, TaskDone) })
	graph.admit(graph.reserve(), taskSpec{title: "legacy promotion", brief: "b", acceptance: "a", sourceCaptured: true})
	if got := graph.node(1).source(); got != nil {
		t.Fatalf("explicit captured unknown took current source %+v", got)
	}
}

// A moved chat may retain only its run receipt. The read-only page and listing
// must keep the desktop's message identity without a local plan database.
func TestHistoricalPlanRowsAndPagesKeepTheirRecordedSource(t *testing.T) {
	place := Place{Dir: filepath.Join(t.TempDir(), sourceOwner)}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile, config.Place, config.ProfileDir = place.Transcript(), place, t.TempDir()
	})
	source := &TaskSource{ID: "s#retained", Kind: TaskSourceMessage, Text: "finish the migration"}
	agent.graph().keepRunRows(7, []TaskNotice{{ID: 7, Title: "retained task", State: TaskDone,
		PlanTask: "t-history-7", Source: source}})
	rows := agent.PlanTasks()
	if len(rows) != 1 || rows[0].Source == nil || rows[0].Source.ID != source.ID || rows[0].Source.Text != source.Text {
		t.Fatalf("historical task lost its message identity: %+v", rows)
	}
	page, ok := agent.PlanTaskPage("t-history-7")
	if !ok || !page.Historical || page.Row.Source == nil || page.Row.Source.ID != source.ID {
		t.Fatalf("historical page lost its message identity: %+v, %v", page, ok)
	}
}
