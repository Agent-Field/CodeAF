package session

// WHAT A TASK PRODUCED, ALL THE WAY DOWN THE DELIVERY PATH.
//
// Each test here runs a REAL task through the real runner — a proposal from the
// conversation, a worker, the check — with only the model scripted, and then
// follows the answer out of the node: onto the card, into the landing note the
// conversation folds, into the brief of the task that waits on it, into the
// finding a continuation is handed, and through a checkpoint into a session
// that was not running when the work happened.
//
// The answer is deliberately on LINE FOUR of what the worker says. Three lines
// of preamble and then the thing that was asked for is the ordinary shape of a
// model's answer, and it is exactly the shape the card's three-line preview
// cuts off — so a test whose answer is on line one would pass against a build
// that had never kept a result at all.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// resultCommandLine is the answer under test: one line a person would paste
// into a terminal, unreachable from the card's preview because three lines of
// preamble stand in front of it, and carrying Unicode because a result that is
// cut has to be cut between runes.
const resultCommandLine = `aforge --host beta-07 --at /srv/state run --seed 4711 --note "réplica ✅ 上海"`

// answerOnLineFour is the whole of what the scripted worker says.
const answerOnLineFour = "I read the deployment notes and the runbook first.\n" +
	"The staging box and the production box disagree about the state directory.\n" +
	"So the command has to name the state directory explicitly.\n" +
	resultCommandLine + "\n" +
	"Run it from the repository root; it takes about a minute."

// resultAgent is [endingAgent] with a journal behind it, so the graph has a
// checkpoint file to be restored from. Everything else — the shortened retry
// ladder, no repair rounds, no consent — is the ending suite's own harness.
func resultAgent(t *testing.T, completer Completer) (*Agent, *TaskGraph, string) {
	t.Helper()
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())
	// No door in this file may reach a real profile or a real provider: the
	// completer is scripted, and these two are what a leaked key or a leaked home
	// would come in through.
	t.Setenv("AFORGE_HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("AFORGE_RESPONSE_ATTEMPTS", "2")
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.SessionFile = journal
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 0
	})
	return agent, agent.graph(), journal
}

// resultCompleter scripts a conversation that hands one question off and a
// worker that answers it in five lines. The auditor is asked and says the
// answer is there — an answer-only task writes nothing, so there is no file for
// it to read.
func resultCompleter(answer string) *routedCompleter {
	return &routedCompleter{
		parent: []step{
			proposeCall("Name the command", "work out the exact command to run on beta-07"),
			finalText("handed off"),
		},
		child: nodeLane(3, func(bool, bool) *ai.Response { return textResponse(answer) }),
		audit: []step{verdict("VERIFIED — the answer names the command and the directory")},
	}
}

// childRequestsSince is what the node lane was asked after a mark taken
// earlier. It is how a SECOND run of the same node — a continuation — is told
// from the first from outside the harness.
func childRequestsSince(completer *routedCompleter, mark int) []([]ai.Message) {
	completer.mu.Lock()
	defer completer.mu.Unlock()
	if mark >= len(completer.childRequests) {
		return nil
	}
	out := make([]([]ai.Message), 0, len(completer.childRequests)-mark)
	return append(out, completer.childRequests[mark:]...)
}

// childSawSince reports whether any request the node lane took after mark
// carried the needle.
func childSawSince(completer *routedCompleter, mark int, needle string) bool {
	for _, request := range childRequestsSince(completer, mark) {
		for _, message := range request {
			if strings.Contains(messageText(message), needle) {
				return true
			}
		}
	}
	return false
}

// runAnsweringTask lands the answer-only task and hands back its node.
func runAnsweringTask(t *testing.T, completer *routedCompleter) (*Agent, *TaskGraph, *TaskNode, string) {
	t.Helper()
	agent, graph, journal := resultAgent(t, completer)
	collect(t, mustSubmit(t, agent, "what exactly do I run on beta-07?"))
	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	return agent, graph, node, journal
}

// (1) THE CARD IS THE PREVIEW AND THE RECORD IS THE ANSWER. The landing note
// the conversation reads — the one that becomes the person's reply — carries
// the command; the row on the roster still carries the three lines it always
// did.
func TestTheAnswerBelowTheCardsPreviewReachesTheLandingNote(t *testing.T) {
	completer := resultCompleter(answerOnLineFour)
	_, _, node, _ := runAnsweringTask(t, completer)

	notice := node.notice()
	if notice.State != TaskDone {
		t.Fatalf("state = %q, ending %q, report %q", notice.State, notice.Ending, notice.Report)
	}
	// The card is unchanged: three lines, and the command is not among them.
	if !strings.Contains(notice.Report, "I read the deployment notes") {
		t.Fatalf("report = %q, want the worker's own opening line", notice.Report)
	}
	if strings.Contains(notice.Report, resultCommandLine) {
		t.Fatalf("report = %q — the card grew past its three lines", notice.Report)
	}
	// The record is the whole answer.
	kept := node.result()
	if !strings.Contains(kept.text, resultCommandLine) {
		t.Fatalf("the node kept %q, want the command in it", kept.text)
	}
	if kept.bytes != len(answerOnLineFour) {
		t.Fatalf("the node kept %d bytes of an answer of %d", kept.bytes, len(answerOnLineFour))
	}
	if kept.source == "" {
		t.Fatal("the kept result names no source to read the whole of it from")
	}
	// And the note the conversation folds carries it, under the report.
	note := taskNote(notice, taskURI(node.journalPath()), TaskSettleAsk, landingAddress{})
	if !strings.Contains(note, resultCommandLine) {
		t.Fatalf("the landing note does not carry the answer:\n%s", note)
	}
	if !strings.Contains(note, resultWholeLead) {
		t.Fatalf("the landing note carries the answer with nothing to say what it is:\n%s", note)
	}
	if notice.ResultCut {
		t.Fatalf("an answer of %d bytes was called an excerpt", len(answerOnLineFour))
	}
	if strings.Index(note, resultWholeLead) < strings.Index(note, "I read the deployment notes") {
		t.Fatalf("the answer stands above the report it belongs under:\n%s", note)
	}
}

// (2) A SHORT ANSWER IS SAID ONCE. The preview already carries the whole of a
// two-line answer, so nothing repeats it underneath — which is what keeps every
// ordinary landing reading exactly as it did before results were kept.
func TestAnAnswerThePreviewAlreadyCarriesIsNotSaidTwice(t *testing.T) {
	const short = "The command is `make build`.\nIt takes about forty seconds."
	completer := resultCompleter(short)
	_, _, node, _ := runAnsweringTask(t, completer)

	notice := node.notice()
	if notice.State != TaskDone {
		t.Fatalf("state = %q, report %q", notice.State, notice.Report)
	}
	if notice.Result != "" || notice.ResultWhole != "" {
		t.Fatalf("a covered answer was carried anyway: %q / %q", notice.Result, notice.ResultWhole)
	}
	note := taskNote(notice, "", TaskSettleAsk, landingAddress{})
	if carriesAnAnswerBlock(note) {
		t.Fatalf("a two-line answer was repeated under its own preview:\n%s", note)
	}
	if strings.Count(note, "make build") != 1 {
		t.Fatalf("the note says the same answer more than once:\n%s", note)
	}
}

// (3) THE NEXT TASK IS TOLD WHAT THE LAST ONE PRODUCED, and so is a second
// attempt at the same node. Both are the same seam — a node being briefed — and
// both used to be handed the card.
func TestTheNextTaskAndTheContinuationAreToldTheAnswerAndNotTheCard(t *testing.T) {
	completer := resultCompleter(answerOnLineFour)
	agent, graph, node, _ := runAnsweringTask(t, completer)

	// THE DEPENDENT. It is admitted after the work it waits on has landed, so
	// the frontier starts it at once and its brief is assembled from the record.
	completer.mu.Lock()
	mark := len(completer.childRequests)
	completer.mu.Unlock()
	dependent := graph.reserve()
	graph.admit(dependent, taskSpec{
		title:      "Run it",
		brief:      "run what the first task worked out\n" + taskBriefMark,
		acceptance: "it ran",
		dependsOn:  []uint64{node.id},
	})
	waitDoneNode(t, graph.node(dependent))
	if !childSawSince(completer, mark, resultCommandLine) {
		t.Fatal("the task that waited on the answer was briefed without it")
	}

	// THE CONTINUATION. Same node, re-armed: the finding it is handed is the
	// last attempt's answer, not the last attempt's card.
	completer.mu.Lock()
	mark = len(completer.childRequests)
	completer.mu.Unlock()
	if err := agent.ContinueTask(node.id, "now say which directory to run it from"); err != nil {
		t.Fatalf("ContinueTask: %v", err)
	}
	finding := findingOf(node)
	if !strings.Contains(finding, resultCommandLine) {
		t.Fatalf("the continuation's finding is %q, want the answer in it", finding)
	}
	if !strings.Contains(finding, "now say which directory") {
		t.Fatalf("the continuation's finding lost the person's words: %q", finding)
	}
	waitDoneNode(t, node)
	if !childSawSince(completer, mark, resultCommandLine) {
		t.Fatal("the second attempt was not told what the first one produced")
	}
}

// (4) AND IT SURVIVES THE PROCESS. The graph is written down, read back into a
// session that never saw the work, and the answer is still there — on the
// restored node, in the note that session would fold, and in the brief of a
// task admitted after the restore.
func TestTheAnswerSurvivesTheCheckpointAndBriefsATaskAfterTheRestore(t *testing.T) {
	completer := resultCompleter(answerOnLineFour)
	_, graph, node, journal := runAnsweringTask(t, completer)
	graph.checkpoint()

	document, found := loadTaskCheckpoint(taskCheckpointPath(journal))
	if !found {
		t.Fatal("the graph was not written down")
	}
	restored := newTaskGraph()
	restored.rehydrate(document, "", TaskSettleAsk)
	back := restored.node(node.id)
	if back == nil {
		t.Fatalf("task %d did not come back", node.id)
	}
	if state := back.stateNow(); state != TaskDone {
		t.Fatalf("the restored node is %q, want done", state)
	}
	if kept := back.result(); !strings.Contains(kept.text, resultCommandLine) {
		t.Fatalf("the restored node kept %q", kept.text)
	}
	note := taskNote(back.notice(), "", TaskSettleAsk, landingAddress{})
	if !strings.Contains(note, resultCommandLine) {
		t.Fatalf("a landing note built after the restore lost the answer:\n%s", note)
	}
	// AND A TASK BRIEFED AFTER THE RESTORE INHERITS IT. This is the same
	// assembly the frontier runs, asked of the graph the file rebuilt.
	sink := &TaskNode{
		graph:     restored,
		id:        restored.reserve(),
		spec:      taskSpec{title: "Run it", brief: "run what the first task worked out"},
		dependsOn: []uint64{node.id},
	}
	restored.mu.Lock()
	restored.nodes[sink.id] = sink
	inherited := restored.inheritedLocked(sink)
	restored.mu.Unlock()
	if !strings.Contains(inherited, resultCommandLine) {
		t.Fatalf("the brief assembled after the restore lost the answer:\n%s", inherited)
	}
}

// (5) A CHECKPOINT FROM BEFORE RESULTS WERE KEPT STILL LANDS ITS REPORT. The
// field is absent, the node comes back with the account it always had, and
// nothing downstream invents a second copy of it.
func TestACheckpointWithNoResultFallsBackToItsReport(t *testing.T) {
	const report = "the reconciler builds its map in reconcile()"
	raw := taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 1,
		Nodes: []taskRecord{{
			ID: 1, Title: "Find the crash", Brief: "look", Acceptance: "found",
			State: TaskDone, Report: report, Merge: mergeMerged, Noted: true,
		}},
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"result"`) {
		t.Fatalf("a record with no result wrote one anyway: %s", encoded)
	}
	var document taskDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	graph := newTaskGraph()
	graph.rehydrate(document, "", TaskSettleAsk)
	node := graph.node(1)
	if node == nil {
		t.Fatal("the legacy node did not come back")
	}
	notice := node.notice()
	if notice.Report != report {
		t.Fatalf("report = %q, want %q", notice.Report, report)
	}
	if notice.Result != "" || notice.ResultWhole != "" {
		t.Fatalf("a legacy record grew a result: %q / %q", notice.Result, notice.ResultWhole)
	}
	note := taskNote(notice, "", TaskSettleAsk, landingAddress{})
	if strings.Count(note, report) != 1 {
		t.Fatalf("the legacy report is said %d times:\n%s", strings.Count(note, report), note)
	}
}

// (6) A WRITING TASK'S RESULT IS THE DOCUMENT IT WROTE ABOUT, and one long
// enough to outrun the cap is kept whole on disk with the record pointing at
// it. The cut is at a rune boundary, and the pointer rides the lead so that a
// second fit downstream cannot cut it off.
func TestAResultTooLongToKeepIsWrittenWholeAndPointedAt(t *testing.T) {
	// Every paragraph is a different sentence, so a fragment can be located in
	// the whole, and each carries Unicode so a cut in the wrong place is
	// visible as an invalid rune rather than as a byte nobody looks at.
	var long strings.Builder
	long.WriteString("Chapter one — the ferry to Ōshima.\n")
	for index := range 400 {
		long.WriteString("Paragraph ")
		long.WriteString(strings.Repeat("…", 1))
		long.WriteString(" number ")
		long.WriteString(strings.Repeat("é", 20))
		long.WriteString(" of the crossing, and the gulls behind it, part ")
		long.WriteString(strings.Repeat("漢", 10))
		long.WriteString(".\n")
		_ = index
	}
	answer := long.String()
	if len(answer) <= taskResultLimit {
		t.Fatalf("the fixture is %d bytes, which does not reach the cap", len(answer))
	}

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Write the chapter", "write chapter.md and say what is in it"),
			finalText("handed off"),
		},
		child: nodeLane(4, func(_, wrote bool) *ai.Response {
			if !wrote {
				return writeResponse("call-chapter", "chapter.md", "# Chapter one\n")
			}
			return textResponse(answer)
		}),
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("chapter.md", "VERIFIED — chapter.md is staged", "REFUTED — no chapter.md"),
		},
	}
	agent, graph, _ := resultAgent(t, completer)
	collect(t, mustSubmit(t, agent, "write me the first chapter"))
	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)

	kept := node.result()
	// The record counts the answer as it was kept — trimmed of the trailing
	// newline the model wrote, which is what every other reading of a last
	// message here is trimmed of too.
	if kept.bytes != len(strings.TrimSpace(answer)) {
		t.Fatalf("the record says %d bytes of an answer of %d", kept.bytes, len(strings.TrimSpace(answer)))
	}
	if len(kept.text) > taskResultLimit {
		t.Fatalf("the kept body is %d bytes, past the %d cap", len(kept.text), taskResultLimit)
	}
	if !utf8.ValidString(kept.text) {
		t.Fatal("the kept body was cut through a rune")
	}
	if !strings.HasSuffix(kept.text, "…") {
		t.Fatalf("a cut body is not marked: it ends %q", lastRunes(kept.text, 8))
	}
	if kept.overflow == "" {
		t.Fatal("an answer past the cap left no file behind")
	}
	whole, err := os.ReadFile(kept.overflow)
	if err != nil {
		t.Fatalf("reading the whole answer back: %v", err)
	}
	if string(whole) != strings.TrimSpace(answer) {
		t.Fatalf("the file holds %d bytes of an answer of %d", len(whole), len(answer))
	}

	notice := node.notice()
	if notice.State != TaskDone {
		t.Fatalf("state = %q, report %q", notice.State, notice.Report)
	}
	if len(notice.Result) > taskResultCarry {
		t.Fatalf("one reader was handed %d bytes, past the %d carry", len(notice.Result), taskResultCarry)
	}
	if !utf8.ValidString(notice.Result) {
		t.Fatal("the carried body was cut through a rune")
	}
	note := taskNote(notice, "", TaskSettleAsk, landingAddress{})
	if !strings.Contains(note, taskURI(kept.overflow)) {
		t.Fatalf("the landing note does not say where the whole answer is:\n%s", firstLines(note, 6))
	}
	if !strings.Contains(note, "Chapter one — the ferry") {
		t.Fatalf("the landing note carries none of the answer:\n%s", firstLines(note, 6))
	}
	if !notice.ResultCut {
		t.Fatal("an answer past the cap was carried as though it were whole")
	}
	// The pointer is on the lead, so a fit that cuts the tail cannot take it away.
	block := resultBlock(resultDelivery{
		body: notice.Result, where: notice.ResultWhole, cut: notice.ResultCut, held: notice.ResultHeld,
	})
	head, _, _ := strings.Cut(block, "\n")
	if !strings.Contains(head, taskURI(kept.overflow)) {
		t.Fatalf("the block's first line does not name the whole: %q", head)
	}
	if !strings.HasPrefix(head, resultPartLead) {
		t.Fatalf("an excerpt was labelled %q", head)
	}
	// The rename left nothing half-written beside it.
	entries, err := os.ReadDir(filepath.Dir(kept.overflow))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), taskResultSuffix) && filepath.Join(filepath.Dir(kept.overflow), entry.Name()) != kept.overflow {
			t.Fatalf("a temporary answer file was left behind: %s", entry.Name())
		}
	}
	if strings.Index(block, taskURI(kept.overflow)) > strings.Index(block, "Chapter one") {
		t.Fatalf("the pointer stands after the answer, where a fit would cut it: %q", head)
	}
}

// (7) A LANDING THAT TURNED THE WORK BACK NAMES ITS OUTPUT RATHER THAN
// RECYCLING IT. The check found the work missing, that answers the claim, and
// the news the model reads is what is missing — not the work's own account of a
// job it was just refused, which a reader would take as standing. It is not
// silence either: the output is still there, and the landing says where.
func TestARefusedLandingNamesItsOutputWithoutHandingItOn(t *testing.T) {
	completer := resultCompleter(answerOnLineFour)
	completer.audit = []step{verdict("REFUTED — nothing names the state directory")}
	_, _, node, _ := runAnsweringTask(t, completer)

	notice := node.notice()
	if notice.State != TaskFailed || notice.Ending != TaskEndingRefused {
		t.Fatalf("state = %q, ending = %q, report %q", notice.State, notice.Ending, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, incompleteLead) {
		t.Fatalf("report = %q, want it to lead with what is missing", notice.Report)
	}
	if !notice.ResultHeld {
		t.Fatal("a refused landing handed its answer on as an ordinary result")
	}
	if notice.Result != "" {
		t.Fatalf("a refused landing carried the work's own answer: %q", notice.Result)
	}
	note := taskNote(notice, "", TaskSettleAsk, landingAddress{})
	if strings.Contains(note, resultCommandLine) {
		t.Fatalf("the refused landing recycled the claim anyway:\n%s", note)
	}
	if strings.Contains(note, resultWholeLead) || strings.Contains(note, resultPartLead) {
		t.Fatalf("refused output was labelled as though it stood:\n%s", note)
	}
	// Named, not suppressed — and the name is somewhere a reader can go.
	if !strings.Contains(note, resultHeldLead) {
		t.Fatalf("the refused landing said nothing about what the work produced:\n%s", note)
	}
	kept := node.result()
	if notice.ResultWhole != kept.whereWhole() || notice.ResultWhole == "" {
		t.Fatalf("the landing names %q as where the output is", notice.ResultWhole)
	}
	if _, err := os.Stat(strings.TrimPrefix(notice.ResultWhole, "file://")); err != nil {
		t.Fatalf("the landing names a source that cannot be read: %v", err)
	}
	// And the record still has it, whole: nothing was thrown away.
	if !strings.Contains(kept.text, resultCommandLine) {
		t.Fatalf("the record lost the answer as well: %q", kept.text)
	}
}

// (7b) A DONE LANDING WHOSE REPORT WAS WHOLLY REWRITTEN STILL DELIVERS THE
// ANSWER. What travels is decided by the node's state and ending, not by
// whether the answer can still be found in the card's wording — a late verdict,
// an accept or a re-check may replace that wording entirely, and the answer the
// work produced is unchanged by any of them.
func TestADoneLandingWithARewrittenReportStillDeliversItsAnswer(t *testing.T) {
	completer := resultCompleter(answerOnLineFour)
	_, graph, node, _ := runAnsweringTask(t, completer)

	const rewritten = "accepted on a person's own reading; its branch merged into yours"
	graph.mu.Lock()
	node.report = rewritten
	graph.mu.Unlock()

	notice := node.notice()
	if notice.Report != rewritten {
		t.Fatalf("report = %q, want the rewritten landing", notice.Report)
	}
	if notice.State != TaskDone {
		t.Fatalf("state = %q, want done", notice.State)
	}
	if !strings.Contains(notice.Result, resultCommandLine) {
		t.Fatalf("a rewritten report lost the answer: %q", notice.Result)
	}
	note := taskNote(notice, "", TaskSettleAsk, landingAddress{})
	if !strings.Contains(note, resultCommandLine) || !strings.Contains(note, rewritten) {
		t.Fatalf("the landing note lost one of its two halves:\n%s", note)
	}
	graph.mu.Lock()
	inherited := node.deliveredLocked()
	graph.mu.Unlock()
	if !strings.Contains(inherited, resultCommandLine) {
		t.Fatalf("what a dependent would inherit lost the answer:\n%s", inherited)
	}
}

// (7c) A SHORT ANSWER SURVIVES A REWRITE TOO. The one report test left is
// whether the answer is already there word for word; a rewritten report is not,
// so the answer that used to be omitted as a duplicate is handed over instead.
func TestAShortAnswerIsHandedOverOnceItsReportNoLongerCarriesIt(t *testing.T) {
	const short = "The command is `make build`.\nIt takes about forty seconds."
	completer := resultCompleter(short)
	_, graph, node, _ := runAnsweringTask(t, completer)

	if notice := node.notice(); notice.Result != "" {
		t.Fatalf("a report carrying its answer verbatim carried it twice: %q", notice.Result)
	}
	graph.mu.Lock()
	node.report = "accepted on a person's own reading"
	graph.mu.Unlock()

	notice := node.notice()
	if !strings.Contains(notice.Result, "make build") {
		t.Fatalf("the short answer was lost with the wording that carried it: %q", notice.Result)
	}
	if notice.ResultCut {
		t.Fatal("a two-line answer was called an excerpt")
	}
}

// (7d) A LANDING NOBODY COULD CHECK, AND THE RE-CHECK AFTER IT, BOTH DELIVER.
// Needing a look is a statement about who decides, not a finding against the
// work, so the answer travels — and it travels again through the landing a
// fresh check writes over the top of it.
func TestAnUncheckedLandingAndTheReauditAfterItBothCarryTheAnswer(t *testing.T) {
	// The checker answers with prose — no verdict, whichever rung of the ladder
	// asks it — until the person asks for a fresh look, and then it answers. A
	// flag rather than a fixed list of turns, because how many times the ladder
	// asks before it gives up is the audit's own law and not this test's.
	var lookedAgain atomic.Bool
	completer := resultCompleter(answerOnLineFour)
	completer.audit = make([]step, 12)
	for index := range completer.audit {
		completer.audit[index] = func(context.Context, []ai.Message) (*ai.Response, error) {
			if lookedAgain.Load() {
				return textResponse("VERIFIED — the command names the state directory"), nil
			}
			return textResponse("I had a look and it seems reasonable enough."), nil
		}
	}
	agent, _, node, _ := runAnsweringTask(t, completer)

	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("state = %q, want the node waiting on a person", state)
	}
	if note := taskNote(node.notice(), "", TaskSettleAsk, landingAddress{}); !strings.Contains(note, resultCommandLine) {
		t.Fatalf("the unchecked landing did not carry the answer:\n%s", note)
	}
	lookedAgain.Store(true)
	if err := agent.ResolveUnverified(node.id, TaskReaudit, "look again"); err != nil {
		t.Fatalf("ResolveUnverified: %v", err)
	}
	settled := waitForState(t, node, TaskDone)
	if !strings.Contains(taskNote(settled, "", TaskSettleAsk, landingAddress{}), resultCommandLine) {
		t.Fatalf("the landing the re-check wrote lost the answer; report %q", settled.Report)
	}
}

// (8) A NODE WITH NO TRANSCRIPT SAYS SO RATHER THAN POINTING AT NOTHING. There
// is nowhere to write the whole answer and nowhere to read it from, so the
// block that carries the excerpt says the rest was not kept — the one thing a
// reader must never be told is that a fragment is the whole.
func TestAnAnswerWithNowhereToKeepItIsNotCalledComplete(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "Write it"}}
	graph.nodes[1] = node

	answer := strings.Repeat("a paragraph of the crossing. ", 900)
	node.keepResult(answer)
	kept := node.result()
	if kept.overflow != "" {
		t.Fatalf("a node with no transcript wrote a file at %q", kept.overflow)
	}
	if kept.source != "" {
		t.Fatalf("a node with no transcript named a source: %q", kept.source)
	}
	if len(kept.text) > taskResultLimit {
		t.Fatalf("the kept body is %d bytes, past the cap", len(kept.text))
	}

	node.report = firstLines(answer, taskReportLines)
	node.state = TaskDone
	delivery := node.carriedResultLocked()
	if !delivery.cut || delivery.body == "" {
		t.Fatalf("an answer past the cap was carried whole: cut=%v body=%d bytes", delivery.cut, len(delivery.body))
	}
	if delivery.where != "" {
		t.Fatalf("there is nowhere to read the whole of it, but the block names %q", delivery.where)
	}
	block := resultBlock(delivery)
	if !strings.HasPrefix(block, resultPartLead+resultWholeGone) {
		t.Fatalf("the block claims more than it has: %q", firstLines(block, 1))
	}
	if strings.Contains(block, resultWholeLead) {
		t.Fatalf("an excerpt was labelled as the whole: %q", firstLines(block, 1))
	}
}

// (9) A SIDECAR THAT COULD NOT BE WRITTEN LEAVES THE TRANSCRIPT AS THE SOURCE,
// and never a path to a file that is not there. The journal here sits under a
// name that is a FILE, so the directory cannot be made.
func TestASidecarThatCouldNotBeWrittenPointsAtTheTranscriptInstead(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(blocked, "20260101-000000.000000_1.jsonl")

	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "Write it"}, journal: journal}
	graph.nodes[1] = node

	answer := strings.Repeat("a paragraph of the crossing. ", 900)
	node.keepResult(answer)
	kept := node.result()
	if kept.overflow != "" {
		t.Fatalf("a failed write was reported as a file at %q", kept.overflow)
	}
	if kept.whereWhole() != taskURI(journal) {
		t.Fatalf("the whole is said to be at %q, want the transcript %q", kept.whereWhole(), taskURI(journal))
	}
	if _, err := os.Stat(blocked + string(filepath.Separator)); err == nil {
		t.Fatal("the failed write made a directory out of the file in its way")
	}
}

// (10) THE SIDECAR IS WRITTEN WHENEVER A READER WOULD ONLY SEE PART OF THE
// ANSWER, and not only past the record's own cap: an answer between the carry
// and the cap is kept whole on the record and still needs somewhere for a
// reader holding four thousand bytes of it to go.
func TestAnAnswerLongerThanOneReaderGetsIsWrittenBesideTheTranscript(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "tasks", "20260101-000000.000000_1.jsonl")
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "Write it"}, journal: journal}
	graph.nodes[1] = node

	answer := strings.TrimSpace(strings.Repeat("a paragraph of the crossing. ", 300))
	if len(answer) <= taskResultCarry || len(answer) > taskResultLimit {
		t.Fatalf("the fixture is %d bytes; it must sit between the carry and the cap", len(answer))
	}
	node.keepResult(answer)
	kept := node.result()
	if kept.bytes != len(answer) || kept.text != answer {
		t.Fatal("an answer inside the cap was cut on the way into the record")
	}
	if kept.overflow == "" {
		t.Fatal("an answer longer than one reader gets left nothing to point at")
	}
	whole, err := os.ReadFile(kept.overflow)
	if err != nil {
		t.Fatalf("reading the whole answer back: %v", err)
	}
	if string(whole) != answer {
		t.Fatalf("the file holds %d bytes of an answer of %d", len(whole), len(answer))
	}
	node.report = firstLines(answer, taskReportLines)
	node.state = TaskDone
	delivery := node.carriedResultLocked()
	if !delivery.cut || len(delivery.body) > taskResultCarry {
		t.Fatalf("the carried body is %d bytes and cut=%v", len(delivery.body), delivery.cut)
	}
	if delivery.where != taskURI(kept.overflow) {
		t.Fatalf("the pointer is %q, want the file beside the transcript", delivery.where)
	}
}

// (11) THE POINTER SURVIVES THE SHARED POT. A dependent's brief fits every
// prerequisite into one bounded pot, so a long report can leave the answer under
// it clipped — but the address of the whole rides the header, which is not in
// the pot, and it names a file the worker can open (a task's reads are not
// bounded to its copy; its writes are).
func TestAnInheritedAnswerKeepsItsPointerWhenTheBodyIsClipped(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "tasks", "20260101-000000.000000_1.jsonl")
	graph := newTaskGraph()
	producer := &TaskNode{
		graph: graph, id: 1, state: TaskDone,
		spec:    taskSpec{title: "Work out the command"},
		journal: journal,
	}
	graph.nodes[1] = producer

	answer := answerOnLineFour + "\n" + strings.Repeat("and then a further paragraph of detail. ", 400)
	producer.keepResult(answer)
	// A landing with a great deal to say about the work: long enough that the
	// pot has little room left for the answer under it.
	producer.report = firstLines(answer, taskReportLines) + "\n" +
		strings.Repeat("the check ran the suite and read the diff. ", 120)

	sink := &TaskNode{
		graph: graph, id: 2, dependsOn: []uint64{1},
		spec: taskSpec{title: "Run it", brief: "run what the first task worked out"},
	}
	graph.nodes[2] = sink

	graph.mu.Lock()
	inherited := graph.inheritedLocked(sink)
	pointer := producer.resultPointerLocked()
	graph.mu.Unlock()

	kept := producer.result()
	if kept.overflow == "" {
		t.Fatal("the fixture produced no file for the pointer to name")
	}
	if !strings.Contains(pointer, taskURI(kept.overflow)) {
		t.Fatalf("the pointer is %q, want the file beside the transcript", pointer)
	}
	if !strings.Contains(inherited, taskURI(kept.overflow)) {
		t.Fatalf("the inherited brief lost the pointer:\n%s", inherited)
	}
	// It is on the header line, above the body the pot clips.
	header, _, _ := strings.Cut(inherited[strings.Index(inherited, "Work out the command"):], "\n")
	if !strings.Contains(header, taskURI(kept.overflow)) {
		t.Fatalf("the pointer is not on the header: %q", header)
	}
	if !strings.Contains(inherited, "…") {
		t.Fatal("the fixture did not clip anything, so the pointer was never at risk")
	}
	// And it names something that is there to be read.
	if _, err := os.Stat(kept.overflow); err != nil {
		t.Fatalf("the pointer names a file that is not there: %v", err)
	}
}

// (12) A LANDING THAT IS REWRITTEN LATER STILL HANDS THE ANSWER ON. The check
// gave no verdict, the node landed needing a look, and somebody then accepted it
// — which recomposes the report hours after the answer was kept. The rule about
// what to carry is asked of the report as it stands, so the answer travels with
// the new landing as it did with the old one.
func TestAnAcceptedLandingStillCarriesTheAnswerItWasKeptWith(t *testing.T) {
	completer := resultCompleter(answerOnLineFour)
	completer.audit = []step{
		finalText("I had a look and it seems reasonable enough."),
		finalText("Still reasonable; I would not like to say either way."),
	}
	agent, _, node, _ := runAnsweringTask(t, completer)

	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("state = %q, want the node waiting on a person; report %q", state, node.notice().Report)
	}
	before := taskNote(node.notice(), "", TaskSettleAsk, landingAddress{})
	if !strings.Contains(before, resultCommandLine) {
		t.Fatalf("the unverified landing did not carry the answer:\n%s", before)
	}
	if err := agent.ResolveUnverified(node.id, TaskAccept, "I read it myself"); err != nil {
		t.Fatalf("ResolveUnverified: %v", err)
	}
	notice := node.notice()
	if notice.State != TaskDone {
		t.Fatalf("state after the accept = %q, report %q", notice.State, notice.Report)
	}
	after := taskNote(notice, "", TaskSettleAsk, landingAddress{})
	if !strings.Contains(after, resultCommandLine) {
		t.Fatalf("the accepted landing lost the answer:\n%s", after)
	}
	if !carriesAnAnswerBlock(after) {
		t.Fatalf("the accepted landing carries the answer with no label:\n%s", after)
	}
}

// waitForState waits for a node to reach one state and answers its notice
// there. The re-check road settles the node from a goroutine of its own, so the
// only honest way to read the landing it writes is to wait for it.
func waitForState(t *testing.T, node *TaskNode, want TaskState) TaskNotice {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if node.stateNow() == want {
			return node.notice()
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %d is %q, want %q", node.id, node.stateNow(), want)
	return TaskNotice{}
}

// carriesAnAnswerBlock reports whether a landing note put the work's own answer
// under its report, under either of the two labels it can wear.
func carriesAnAnswerBlock(note string) bool {
	return strings.Contains(note, resultWholeLead) || strings.Contains(note, resultPartLead)
}

// findingOf is the block a continuation added to this node, read under the
// graph's lock like every other field beside it.
func findingOf(node *TaskNode) string {
	node.graph.mu.Lock()
	defer node.graph.mu.Unlock()
	return node.finding
}
