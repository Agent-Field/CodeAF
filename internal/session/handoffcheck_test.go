package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// ── the run16 vector-db t1 fixture ──────────────────────────────────────────
//
// These are the words of connected-canvas qloop run 16, vector-db t1 (scored
// 3.6), as the session's transcript holds them: the person's ask, the two
// hand-offs' titles, the refusal task 2 was answered with, what `tasks` said
// after it, and the reply the turn ended on.

const (
	vdbAsk          = "deep research brief on vector dbs w/ a comparison table and charts"
	vdbTableTitle   = "Comparison table of vector DBs"
	vdbBriefTitle   = "The research brief doc"
	vdbTableStarted = "task 1 started: " + vdbTableTitle + "\nIt works from the brief alone, in a copy of its own. " + taskHandoffWakeSentence
	vdbBriefRefused = `task 2 did not start: task "2" has hard dependency "1" across its containment lineage. Nothing is running for it and nothing was started in its place.`
	vdbTasksSaid    = "#1 · " + vdbTableTitle + " · running\n"
	vdbReply        = "Two charts built and a comparison table underway.\n\nOn the canvas now:\n" +
		"- **Latency by store** and **Monthly cost by store** — bar charts drawn from your benchmark numbers.\n" +
		"- **Comparison** — the researched table is being filled for the five stores.\n" +
		"- **The brief** — the write-up, which follows once the table lands so its charts and figures agree with the rest.\n\n" +
		"I'll report when the table and the brief are in.\n\nworkaf://canvas/cv_5tmj32kl7jfn"
	vdbHonest = "Two charts are built and the comparison table is being filled.\n\n" +
		"The brief did not start: it was handed out to wait for the table, and work cannot wait on the run it would join. " +
		"I will start it when the table's report arrives."
)

// vdbHandoffTool stands in for `propose_task` and answers the way run 16's
// engine did: the table started, the brief did not — unless the brief has
// already been refused once, when it starts (the retry case).
func vdbHandoffTool(retried bool) bare.Tool {
	var briefs atomic.Int64
	return bare.Tool{
		Name: "propose_task", Description: "hands work out",
		Schema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"depends_on":{"type":"array"}}}`),
		Execute: func(_ context.Context, raw json.RawMessage) (string, bool, error) {
			if callTitle(string(raw)) == vdbTableTitle {
				return vdbTableStarted, false, nil
			}
			if retried && briefs.Add(1) > 1 {
				return "task 3 started: " + vdbBriefTitle + "\n" + taskHandoffWakeSentence, false, nil
			}
			return vdbBriefRefused, true, nil
		},
	}
}

// vdbTasksTool stands in for `tasks` and lists what run 16's listing did.
func vdbTasksTool() bare.Tool {
	return bare.Tool{
		Name: "tasks", Description: "lists the work",
		Schema: json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return vdbTasksSaid, false, nil
		},
	}
}

// vdbAgent is [replyCheckAgent] with run 16's `propose_task` and `tasks`
// standing in for the real ones, which are taken off the belt first so the
// stubs are the ones the turn calls.
func vdbAgent(t *testing.T, steps []step, retried bool, more ...bare.Tool) (*Agent, *scriptedCompleter) {
	t.Helper()
	agent, completer, _ := replyCheckAgent(t, steps, "NONE")
	stubs := append([]bare.Tool{vdbHandoffTool(retried), vdbTasksTool()}, more...)
	kept := agent.tools[:0]
	for _, tool := range agent.tools {
		if !slices.ContainsFunc(stubs, func(stub bare.Tool) bool { return stub.Name == tool.Name }) {
			kept = append(kept, tool)
		}
	}
	agent.tools = append(kept, stubs...)
	return agent, completer
}

func vdbHandoffSteps() []step {
	return []step{
		callStep("v1", "look", `{"question":"does the latency chart read right"}`),
		callStep("h1", "propose_task", `{"title":"`+vdbTableTitle+`"}`),
		callStep("h2", "propose_task", `{"title":"`+vdbBriefTitle+`","depends_on":[1]}`),
		callStep("t1", "tasks", `{}`),
		replyStep(vdbReply),
	}
}

// THE MEASURED TURN. The brief did not start, the reply promised it, and the
// turn sealed finished. Now the reply is sent back once, FIRST — ahead of the
// stale-look arm that fired in run 16 and was answered [no change] — with the
// unstarted task named and what stopped it, and the reply the person gets says
// it did not start.
func TestTheRun16HandoffTurnIsSentBackForTheBriefThatDidNotStart(t *testing.T) {
	var looked atomic.Int64
	steps := append(vdbHandoffSteps(), replyStep(vdbHonest), replyStep(NoChangeReply), unreachable)
	agent, completer := vdbAgent(t, steps, false, lookTool(&looked))
	collected := runTurnOn(t, agent, vdbAsk)
	if got, want := completer.requests(), 7; got != want {
		t.Fatalf("the loop made %d requests; want %d (the handoff send-back, then the stale look)", got, want)
	}
	lead := messageText(lastOf(completer.request(5)))
	if !isHandoffCheckLead(lead) {
		t.Fatalf("the first send-back was %q; want the handoff check", lead)
	}
	if want := `- task 2 (` + vdbBriefTitle + `): task "2" has hard dependency "1" across its containment lineage`; !strings.Contains(lead, want) {
		t.Fatalf("the send-back %q does not name the brief and what stopped it", lead)
	}
	if strings.Contains(lead, "task 1 (") {
		t.Errorf("the send-back named the task that started: %q", lead)
	}
	if !saidSomething(noticeTexts(collected), handoffCheckNote) || !IsCarryOnNotice(handoffCheckNote) {
		t.Errorf("the line said while it works is not a carry-on line: %q", noticeTexts(collected))
	}
	if got := lastAssistantText(agent); got != vdbHonest {
		t.Errorf("the turn ended on %q; want the reply that says the brief did not start", got)
	}
	if n := strings.Count(transcriptText(agent), handoffCheckLeadOpening); n != 1 {
		t.Errorf("the handoff send-back reached the transcript %d times; want once a turn", n)
	}
}

// [NoChangeReply] KEEPS A REPLY THAT ALREADY SAID IT, and the turn ends there.
func TestNoChangeAfterTheHandoffCheckKeepsTheFirstReply(t *testing.T) {
	steps := append(vdbHandoffSteps()[1:], replyStep(NoChangeReply), unreachable)
	agent, completer := vdbAgent(t, steps, false)
	runTurnOn(t, agent, vdbAsk)
	if got, want := completer.requests(), 5; got != want {
		t.Fatalf("the loop made %d requests; want %d", got, want)
	}
	if got := lastAssistantText(agent); got != vdbReply {
		t.Errorf("the standing reply is %q; want the first", got)
	}
}

// A HAND-OFF STARTED AGAIN UNDER THE SAME TITLE IN THE SAME TURN IS A RETRY,
// and a reply that promises nothing is left alone.
func TestAHandoffStartedAgainInTheSameTurnIsNotRaised(t *testing.T) {
	steps := []step{
		callStep("h1", "propose_task", `{"title":"`+vdbTableTitle+`"}`),
		callStep("h2", "propose_task", `{"title":"`+vdbBriefTitle+`","depends_on":[1]}`),
		callStep("h3", "propose_task", `{"title":"`+vdbBriefTitle+`"}`),
		replyStep("The table and the brief are both running now."),
		unreachable,
	}
	agent, completer := vdbAgent(t, steps, true)
	runTurnOn(t, agent, vdbAsk)
	if got, want := completer.requests(), 4; got != want {
		t.Fatalf("the loop made %d requests; want %d — nothing to raise", got, want)
	}
}

// A PROMISE OF A LATER REPORT NEEDS SOMETHING STILL OUT. With nothing running
// it is sent back once; with a task of this conversation still running it is
// the truth, and goes out as written.
func TestAPromiseOfALaterReportNeedsSomethingStillOut(t *testing.T) {
	promise := "The chart is updated. I'll report when the brief is in."
	agent, completer, _ := replyCheckAgent(t, []step{replyStep(promise), replyStep("The chart is updated; the brief was not started."), unreachable}, "NONE")
	collected := runTurnOn(t, agent, "update the chart")
	if got, want := completer.requests(), 2; got != want {
		t.Fatalf("the loop made %d requests; want %d", got, want)
	}
	if lead := messageText(lastOf(completer.request(1))); lead != handoffPromiseLead {
		t.Fatalf("the send-back was %q; want the promise arm", lead)
	}
	if !saidSomething(noticeTexts(collected), handoffPromiseNote) || !IsCarryOnNotice(handoffPromiseNote) {
		t.Errorf("the line said while it works is not a carry-on line: %q", noticeTexts(collected))
	}

	agent, completer, _ = replyCheckAgent(t, []step{replyStep(promise), unreachable}, "NONE")
	g := agent.graph()
	g.mu.Lock()
	g.nodes[7] = &TaskNode{id: 7, state: TaskRunning, admitBy: agent}
	g.order = append(g.order, 7)
	g.mu.Unlock()
	runTurnOn(t, agent, "update the chart")
	if got, want := completer.requests(), 1; got != want {
		t.Fatalf("a promise with a task still running was sent back: %d requests", got)
	}
}

func TestPromisesALaterReportReadsOnlyPromises(t *testing.T) {
	for _, said := range []string{vdbReply, "I'll let you know when it's done.", "The brief follows once it lands.", "I will report back."} {
		if !promisesALaterReport(said) {
			t.Errorf("%q is not read as a promise", said)
		}
	}
	for _, said := range []string{"The chart is updated.", "Both tasks reported: the table and the brief are done.", "Want the subline larger?"} {
		if promisesALaterReport(said) {
			t.Errorf("%q is read as a promise", said)
		}
	}
}

func TestTheHandoffLeadsAreContinuationContext(t *testing.T) {
	for _, lead := range []string{handoffCheckLead([]string{"- task 2: it stopped"}), handoffPromiseLead} {
		if !isContinuationContext(lead) || !isHandoffCheckLead(lead) {
			t.Errorf("%q is not read as the engine's own continuation", lead)
		}
	}
}

// ── the root cause: a joined task that waits on its run's root ──────────────

// THE STORE REFUSES IT, AND IS RIGHT TO. Run 16's task 2 joined the run task 1
// opened; a joined task is a child of the run's root, and the root was task 1.
// An edge from a child to its own parent is the lineage rule's one refusal.
func TestAJoinedTaskThatWaitsOnItsRunsRootIsRefusedByTheStore(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", "1", vdbTableTitle, "fill the table")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = addJoinedTasks(context.Background(), store, []plandb.TaskSpec{{
		ID: "2", ParentID: store.RootID(), Title: vdbBriefTitle, Description: "write the brief",
		Dependencies: []plandb.Dependency{{TaskID: "1"}},
	}})
	if err == nil || !strings.Contains(err.Error(), `task "2" has hard dependency "1" across its containment lineage`) {
		t.Fatalf("joining a task that waits on the root answered %v; want run 16's lineage refusal", err)
	}
}

// liveRunConversation is a conversation whose run is turning with task 1 at
// its root and task 4 joined beside it.
func liveRunConversation(t *testing.T) *Agent {
	t.Helper()
	agent := programConversation(t, nil)
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", "1", vdbTableTitle, "fill the table")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := addJoinedTasks(context.Background(), store, []plandb.TaskSpec{{ID: "4", ParentID: store.RootID(), Title: "charts", Description: "draw them"}}); err != nil {
		t.Fatal(err)
	}
	agent.beltMu.Lock()
	agent.beltRun = &beltRun{store: store, root: store.RootID(), row: 1, title: vdbTableTitle}
	agent.beltMu.Unlock()
	t.Cleanup(func() {
		agent.beltMu.Lock()
		agent.beltRun = nil
		agent.beltMu.Unlock()
	})
	return agent
}

// SO THE PROPOSAL IS REFUSED BEFORE A CARD OR AN ID, in words that say what
// will happen: the run's report starts a turn, and that is when to propose it.
// A task joined beside the root may still be waited on.
func TestAProposalThatWaitsOnTheRunningRunIsRefusedBeforeACard(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	agent := liveRunConversation(t)
	spec := taskSpec{title: vdbBriefTitle, dependsOn: []uint64{1}}
	if got, want := agent.proposalDependencyRefusal(spec), liveRunWaitRefusal(1); got != want {
		t.Fatalf("a proposal waiting on the running run answered %q, want %q", got, want)
	}
	if !strings.Contains(liveRunWaitRefusal(1), "report arrives") {
		t.Errorf("the refusal does not say when to propose it again: %q", liveRunWaitRefusal(1))
	}
	if refusal := agent.refuseProposedTask(context.Background(), spec); refusal == nil {
		t.Fatal("the door let a proposal waiting on the running run through")
	}
	if got := agent.proposalDependencyRefusal(taskSpec{dependsOn: []uint64{4}}); got != "" {
		t.Fatalf("a proposal waiting on a task joined beside the root was refused: %q", got)
	}
}

// AND ONCE THE RUN HAS ENDED ITS ROW SAYS HOW. Run 16's retry of the brief
// named task 1 after it landed and was told "no task in this session has that
// id". Ended done is a dependency met and is taken out of what is handed on;
// ended any other way is refused as ended without finishing.
func TestAProposalMayNameARunThatHasEnded(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	agent := programConversation(t, nil)
	g := agent.graph()
	agent.publishRunRow(g, TaskNotice{ID: 1, Title: vdbTableTitle, State: TaskDone, StartedAt: agent.taskClockNow()})
	spec := taskSpec{dependsOn: []uint64{1}}
	if got := agent.proposalDependencyRefusal(spec); got != "" {
		t.Fatalf("a proposal naming a run that ended done was refused: %q", got)
	}
	if kept := agent.withoutEndedProgramRuns(spec.dependsOn); !slices.Equal(kept, []uint64{}) {
		t.Fatalf("the dependencies handed on are %v; want the ended run taken out", kept)
	}
	for _, state := range []TaskState{TaskUnverified, TaskFailed, TaskInterrupted} {
		agent := programConversation(t, nil)
		g := agent.graph()
		agent.publishRunRow(g, TaskNotice{ID: 1, Title: vdbTableTitle, State: state, StartedAt: agent.taskClockNow()})
		if got, want := agent.proposalDependencyRefusal(spec), endedUnfinishedRefusal([]uint64{1}); got != want {
			t.Errorf("a proposal naming a run that ended %s answered %q, want %q", state, got, want)
		}
	}
	if got := agent.proposalDependencyRefusal(taskSpec{dependsOn: []uint64{9}}); !strings.Contains(got, "no task in this session has that id") {
		t.Fatalf("an id nothing holds answered %q", got)
	}
}

// ── the checks on a finished reply are not chat rows ────────────────────────

// THE APP LEAVES EVERY CARRY-ON LINE OUT (cmd/codeaf/app.go's pump), and the
// checks on a finished reply were not among them: run 16 showed "it changed
// the work after its last look · asking it to look again before it replies"
// as an Update row under the final reply.
func TestTheChecksOnAFinishedReplyAreCarryOnLines(t *testing.T) {
	for _, line := range []string{replyStaleLookNote, replyCheckNote, failureCheckNote, handoffCheckNote, handoffPromiseNote} {
		if !IsCarryOnNotice(line) {
			t.Errorf("%q would be drawn as a row in the app", line)
		}
	}
}

func lastOf(messages []ai.Message) ai.Message { return messages[len(messages)-1] }
