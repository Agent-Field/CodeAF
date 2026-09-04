package session

// Steering that CHANGES THE WORK, end to end: the room's door, the worker's
// loop, the tool it revises with, the packet the checker is judged from, and the
// landing that has to decide whether what it is about to publish is still what
// the person asked for.
//
// Everything here runs a real child agent in a real worktree against a scripted
// provider. The pure laws of the module are assignment_test.go's; what these
// assert is that the laws are actually reached from the doors a person and a
// worker use.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// jsonReportTask is the work every fixture here starts from: a report in JSON,
// with a done-condition that names JSON, so that a person asking for CSV is
// asking for something the original acceptance forbids.
func jsonReportTask() step {
	arguments, _ := json.Marshal(taskArguments{
		Title:       "Write the report",
		Summary:     "two lines the person reads",
		Brief:       "write report.json in the task folder\n" + taskBriefMark,
		Deliverable: "report.json in the task folder",
		Acceptance:  "report.json exists and parses as JSON",
	})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("call-task", "propose_task", string(arguments)), nil
	}
}

// reviseCall is the worker folding one direction into the assignment.
func reviseCall(id string, direction, at uint64, acceptance, work string) step {
	arguments, _ := json.Marshal(reviseArguments{
		Direction:  direction,
		AtRevision: at,
		Acceptance: acceptance,
		Work:       work,
	})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "revise_assignment", string(arguments)), nil
	}
}

// fileCall is the worker writing one file in its own copy.
func fileCall(id, path, content string) step {
	arguments, _ := json.Marshal(struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{Path: path, Content: content})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "write", string(arguments)), nil
	}
}

// gate is one place a scripted step stops so the test can act while the work is
// genuinely mid-flight, rather than hoping a sleep lands in the right window.
type gate struct {
	reached chan struct{}
	go_     chan struct{}
	once    sync.Once
}

func newGate() *gate {
	return &gate{reached: make(chan struct{}), go_: make(chan struct{})}
}

func (g *gate) stop() {
	g.once.Do(func() { close(g.reached) })
	<-g.go_
}

func (g *gate) waitFor(t *testing.T, what string) {
	t.Helper()
	select {
	case <-g.reached:
	case <-time.After(20 * time.Second):
		t.Fatalf("never reached %s", what)
	}
}

func (g *gate) release() { close(g.go_) }

// steeringAgent is the fixture the tests below share: a real repository, a real
// graph, and a scripted parent, worker and auditor.
func steeringAgent(t *testing.T, completer *routedCompleter) (*Agent, *TaskGraph, string) {
	t.Helper()
	repo := newTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: t.TempDir(), Workspace: repo}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	return agent, agent.graph(), repo
}

// handOffTheReport runs the conversation's turn that admits the node.
func handOffTheReport(t *testing.T, agent *Agent) {
	t.Helper()
	events, err := agent.Submit(context.Background(), "write me the report")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
}

// THE WHOLE POINT OF THE SLICE. A person walks into a running task, says "CSV
// instead of JSON", and the finished work is judged against CSV — by the same
// checker, from the same packet, with their own words in it.
func TestAPersonsDirectionMidWorkChangesWhatTheFinishedWorkIsJudgedBy(t *testing.T) {
	mid := newGate()
	completer := &routedCompleter{
		parent: []step{jsonReportTask(), finalText("handed off")},
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				mid.stop()
				return toolResponse("call-json", "write", `{"path":"report.json","content":"{\"rows\":1}\n"}`), nil
			},
			reviseCall("call-revise", 1, 0,
				"report.csv exists in the task folder, one comma-separated row per entry",
				"the report is written as CSV rather than JSON"),
			fileCall("call-csv", "report.csv", "rows,1\n"),
			finalText("Wrote report.csv with one row per entry."),
		},
		audit: []step{
			bashCall("call-diff", "git diff --cached"),
			verdictFromEvidence("report.csv", "VERIFIED — report.csv added", "REFUTED — no csv in the diff"),
		},
	}
	agent, graph, repo := steeringAgent(t, completer)
	handOffTheReport(t, agent)
	mid.waitFor(t, "the worker's first step")

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	receipt, err := agent.SteerTask(1, "CSV instead of JSON, please")
	if err != nil {
		t.Fatalf("SteerTask: %v", err)
	}
	if receipt.Held || receipt.Direction == 0 {
		t.Fatalf("receipt = %+v, want the line delivered to a live worker with a receipt id", receipt)
	}
	mid.release()
	waitDoneNode(t, node)

	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("state = %q, report = %q", state, node.notice().Report)
	}
	if version := node.assignmentVersion(); version != 1 {
		t.Fatalf("assignment version = %d, want the person's direction to have moved it once", version)
	}
	if now := node.acceptance(); !strings.Contains(now, "report.csv") {
		t.Fatalf("the effective done-condition is %q, want the one the person asked for", now)
	}
	// AND THE ADMITTED CONTRACT IS STILL ON THE RECORD, WORD FOR WORD. What was
	// first agreed is history and history is not editable.
	if first := node.admittedAcceptance(); !strings.Contains(first, "report.json") {
		t.Fatalf("the admitted acceptance reads %q, want it untouched", first)
	}

	// THE CHECKER JUDGED THE WORK THE PERSON ASKED FOR. The packet carries the
	// revised condition, their own words, and the sentence that stops it holding
	// the work to the withdrawn one as well.
	packet := messagesText(completer.auditAsked())
	for _, want := range []string{
		"report.csv exists in the task folder",
		"CSV instead of JSON",
		"no longer governs",
	} {
		if !strings.Contains(packet, want) {
			t.Fatalf("the auditor's packet does not carry %q:\n%s", want, packet)
		}
	}

	// AND THE WORK CAME HOME. The person asked for CSV and there is a CSV file on
	// their branch.
	if _, err := os.Stat(filepath.Join(repo, "report.csv")); err != nil {
		t.Fatalf("the merged work has no report.csv: %v", err)
	}
}

// AND AN ORDINARY QUESTION MOVES NOTHING. This is the other half of the same
// law: a person asking why gets an answer, not a new contract, and no model call
// is spent deciding which of the two it was.
func TestAnOrdinaryQuestionLeavesTheAssignmentExactlyWhereItWas(t *testing.T) {
	mid := newGate()
	completer := &routedCompleter{
		parent: []step{jsonReportTask(), finalText("handed off")},
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				mid.stop()
				return toolResponse("call-json", "write", `{"path":"report.json","content":"{\"rows\":1}\n"}`), nil
			},
			finalText("I used a map because the rows are keyed by id. report.json is written."),
		},
		audit: []step{
			bashCall("call-diff", "git diff --cached"),
			verdictFromEvidence("report.json", "VERIFIED — report.json added", "REFUTED — nothing added"),
		},
	}
	agent, graph, _ := steeringAgent(t, completer)
	handOffTheReport(t, agent)
	mid.waitFor(t, "the worker's first step")

	node := graph.node(1)
	if _, err := agent.SteerTask(1, "why did you go with a map there?"); err != nil {
		t.Fatalf("SteerTask: %v", err)
	}
	mid.release()
	waitDoneNode(t, node)

	if version := node.assignmentVersion(); version != 0 {
		t.Fatalf("assignment version = %d, want a question to have changed nothing", version)
	}
	if now, first := node.acceptance(), node.admittedAcceptance(); now != first {
		t.Fatalf("the done-condition moved on a question: %q vs %q", now, first)
	}
	// The question is still on the record — and READ, not pending, so it cannot
	// hold a landing that has already answered it.
	said := node.directionsNow()
	if len(said) != 1 || said[0].state != directionRead || said[0].version != 0 {
		t.Fatalf("directions = %+v, want one read direction that applied nothing", said)
	}
	if packet := messagesText(completer.auditAsked()); strings.Contains(packet, "no longer governs") {
		t.Fatalf("the auditor was told the goal had moved when it had not:\n%s", packet)
	}
}

// A DIRECTION THAT ARRIVES WHILE THE CHECKER IS READING CANNOT BE LANDED OVER.
// The work is finished, the gate is about to pass it, and the person says
// something that changes what finished means: nothing publishes, the node never
// passes through a final state, and the next round is the one that reads them.
func TestADirectionDuringTheCheckCannotBeLandedOverAndTheWorkGoesRoundAgain(t *testing.T) {
	checking := newGate()
	completer := &routedCompleter{
		parent: []step{jsonReportTask(), finalText("handed off")},
		child: []step{
			fileCall("call-json", "report.json", "{\"rows\":1}\n"),
			finalText("Wrote report.json."),
			// The second attempt, which the person's words bought.
			reviseCall("call-revise", 1, 0,
				"report.csv exists in the task folder, one comma-separated row per entry",
				"the report is written as CSV rather than JSON"),
			fileCall("call-csv", "report.csv", "rows,1\n"),
			finalText("Wrote report.csv instead, as asked."),
		},
		audit: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				checking.stop()
				return textResponse("VERIFIED — report.json added"), nil
			},
			bashCall("call-diff", "git diff --cached"),
			verdictFromEvidence("report.csv", "VERIFIED — report.csv added", "REFUTED — no csv in the diff"),
		},
	}
	agent, graph, repo := steeringAgent(t, completer)
	handOffTheReport(t, agent)
	checking.waitFor(t, "the check")

	node := graph.node(1)
	receipt, err := agent.SteerTask(1, "CSV instead of JSON, please")
	if err != nil {
		t.Fatalf("SteerTask while the work was being checked: %v", err)
	}
	if !receipt.Held || receipt.Direction == 0 {
		t.Fatalf("receipt = %+v, want the words held on the task's record", receipt)
	}
	checking.release()
	waitDoneNode(t, node)

	// THE VERDICT THAT PASSED THE JSON DID NOT PUBLISH IT. What landed is the work
	// the person actually asked for, judged against the condition they set.
	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("state = %q, report = %q", state, node.notice().Report)
	}
	if version := node.assignmentVersion(); version != 1 {
		t.Fatalf("assignment version = %d, want the held direction to have been taken up", version)
	}
	if _, err := os.Stat(filepath.Join(repo, "report.csv")); err != nil {
		t.Fatalf("the person's correction never reached the work that came home: %v", err)
	}
	// AND THE SECOND ATTEMPT WAS HANDED THEIR UNREAD WORDS. Without that it would
	// have started again from the original brief and done the same thing twice.
	second := messagesText(completer.childAskedAt(2))
	if !strings.Contains(second, "CSV instead of JSON") {
		t.Fatalf("the round the person's words bought never saw them:\n%s", second)
	}
	if !strings.Contains(second, directionCarriedLead) {
		t.Fatalf("the carried words were not labelled as unapplied:\n%s", second)
	}
}

// AND A DIRECTION THAT ARRIVES AFTER THE BOUNDARY IS TAKEN BELONGS TO THE NEXT
// ROUND. This is the ordering no timing can produce: the landing has claimed
// publication and the merge is going out, so the honest answer is that the work
// lands and the words wait — never that they were applied to it.
func TestADirectionAfterThePublicationBoundaryBelongsToTheNextRound(t *testing.T) {
	completer := &routedCompleter{
		parent: []step{jsonReportTask(), finalText("handed off")},
		child: []step{
			fileCall("call-json", "report.json", "{\"rows\":1}\n"),
			finalText("Wrote report.json."),
		},
		audit: []step{
			bashCall("call-diff", "git diff --cached"),
			verdictFromEvidence("report.json", "VERIFIED — report.json added", "REFUTED — nothing added"),
		},
	}
	agent, graph, repo := steeringAgent(t, completer)

	var (
		once sync.Once
		late = make(chan SteerReceipt, 1)
	)
	graph.mu.Lock()
	graph.publishBarrier = func(*TaskNode) {
		once.Do(func() {
			receipt, err := agent.SteerTask(1, "actually make it TSV")
			if err != nil {
				t.Errorf("SteerTask inside the publication boundary: %v", err)
			}
			late <- receipt
		})
	}
	graph.mu.Unlock()

	handOffTheReport(t, agent)
	node := graph.node(1)
	waitDoneNode(t, node)

	receipt := <-late
	if !receipt.Held || receipt.Direction == 0 {
		t.Fatalf("receipt = %+v, want the words kept on the record", receipt)
	}
	if !strings.Contains(receipt.Landing, "already landing") {
		t.Fatalf("the receipt says %q, want it to say the work was already going out", receipt.Landing)
	}
	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("state = %q, want the landing that had already claimed the boundary to finish", state)
	}
	if version := node.assignmentVersion(); version != 0 {
		t.Fatalf("assignment version = %d, want words that came after publication to have changed nothing", version)
	}
	if _, err := os.Stat(filepath.Join(repo, "report.json")); err != nil {
		t.Fatalf("the work that was already merging did not land: %v", err)
	}
	// AND THEY ARE NOT LOST: still on the record, unread, for a continue to take
	// up. The one thing that must never happen is a claim that they were applied.
	said := node.directionsNow()
	if len(said) != 1 || said[0].state != directionPending {
		t.Fatalf("directions = %+v, want the late words kept and unapplied", said)
	}
}

// A RUN THE PERSON KEEPS CORRECTING STOPS RATHER THAN MERGING WORK THEY HAVE
// MOVED ON FROM. The bound on rounds is there so a task cannot spend forever; it
// is not permission to publish the version nobody asked for. Nothing merges, the
// branch keeps the work, and the card says what is still waiting.
func TestARunCorrectedPastItsRoundLimitStopsWithoutMergingOrSayingDone(t *testing.T) {
	// Each check hands the test its own release, so a correction lands inside
	// every checking window rather than near one.
	checks := make(chan chan struct{})
	checking := func(context.Context, []ai.Message) (*ai.Response, error) {
		release := make(chan struct{})
		checks <- release
		<-release
		return textResponse("VERIFIED — report.json added"), nil
	}
	worker := []step{fileCall("call-json", "report.json", "{\"rows\":1}\n"), finalText("Wrote report.json.")}
	completer := &routedCompleter{parent: []step{jsonReportTask(), finalText("handed off")}}
	for round := 0; round <= directedRoundLimit+1; round++ {
		completer.child = append(completer.child, worker...)
		completer.audit = append(completer.audit, checking)
	}
	agent, graph, repo := steeringAgent(t, completer)

	// The worker never folds any of them in — which is the point: the person's
	// last word is still outstanding when the rounds run out.
	said := 0
	go func() {
		for round := 0; round <= directedRoundLimit; round++ {
			release, open := <-checks
			if !open {
				return
			}
			if _, err := agent.SteerTask(1, fmt.Sprintf("correction %d: not like that", round+1)); err != nil {
				close(release)
				return
			}
			said++
			close(release)
		}
	}()

	handOffTheReport(t, agent)
	node := graph.node(1)
	waitDoneNode(t, node)

	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("state = %q, want work the person has moved on from settled as unfinished rather than done", state)
	}
	if _, err := os.Stat(filepath.Join(repo, "report.json")); err == nil {
		t.Fatal("stale work was merged onto the person's checkout after the round limit")
	}
	if node.directedRounds != directedRoundLimit {
		t.Fatalf("rounds = %d, want the bound to have stopped the run", node.directedRounds)
	}
	report := node.notice().Report
	if !strings.Contains(report, "did not land") || !strings.Contains(report, "continue this task") {
		t.Fatalf("the card reads %q, want it to say the work did not land and what to do", report)
	}
	// AND THE WORK IS STILL THERE TO LOOK AT: a branch, kept.
	if _, changed, branch, _ := node.leavings(); branch == "" || len(changed) == 0 {
		t.Fatalf("branch = %q changed = %v, want the run's work kept for inspection", branch, changed)
	}
	if said == 0 {
		t.Fatal("no correction ever reached the node, so this proves nothing")
	}
}

// TWO CORRECTIONS APPLY IN THE ORDER THEY WERE SAID, and a call that names a
// version the assignment has already moved past is refused rather than merged.
func TestTwoDirectionsApplyInSequenceAndAStaleOneIsRefused(t *testing.T) {
	first, second := newGate(), newGate()
	var stale string
	completer := &routedCompleter{
		parent: []step{jsonReportTask(), finalText("handed off")},
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				first.stop()
				return toolResponse("call-json", "write", `{"path":"report.json","content":"{\"rows\":1}\n"}`), nil
			},
			reviseCall("call-revise-1", 1, 0, "report.csv exists in the task folder", "CSV rather than JSON"),
			func(context.Context, []ai.Message) (*ai.Response, error) {
				second.stop()
				return toolResponse("call-csv", "write", `{"path":"report.csv","content":"rows,1\n"}`), nil
			},
			// The version this names is the one BEFORE the first revision: a
			// correction applied on top of a stale reading would put the person's
			// earlier condition back over their later one.
			reviseCall("call-stale", 2, 0, "report.csv exists, unsorted", ""),
			func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
				stale = lastToolText(messages)
				return toolResponse("call-revise-2", "revise_assignment",
					`{"direction":2,"at_revision":1,"acceptance":"report.csv exists in the task folder, sorted by date"}`), nil
			},
			finalText("Wrote report.csv, sorted by date."),
		},
		audit: []step{
			bashCall("call-diff", "git diff --cached"),
			verdictFromEvidence("report.csv", "VERIFIED — report.csv added", "REFUTED — no csv"),
		},
	}
	agent, graph, _ := steeringAgent(t, completer)
	handOffTheReport(t, agent)

	first.waitFor(t, "the worker's first step")
	if _, err := agent.SteerTask(1, "CSV instead of JSON, please"); err != nil {
		t.Fatalf("first steer: %v", err)
	}
	first.release()
	second.waitFor(t, "the step after the first correction")
	if _, err := agent.SteerTask(1, "and sort it by date"); err != nil {
		t.Fatalf("second steer: %v", err)
	}
	second.release()

	node := graph.node(1)
	waitDoneNode(t, node)

	if version := node.assignmentVersion(); version != 2 {
		t.Fatalf("assignment version = %d, want one revision per direction", version)
	}
	if now := node.acceptance(); !strings.Contains(now, "sorted by date") {
		t.Fatalf("the done-condition is %q, want the person's latest correction in force", now)
	}
	if !strings.Contains(stale, "revision") || !strings.Contains(stale, "named 0") {
		t.Fatalf("the stale revision was answered with %q, want it refused for naming an old version", stale)
	}
	// AND BOTH SETS OF WORDS ARE IN FRONT OF THE CHECKER, in the order they were
	// said, so it can see that the condition it is judging is what was asked for.
	packet := messagesText(completer.auditAsked())
	csv, sort := strings.Index(packet, "CSV instead of JSON"), strings.Index(packet, "sort it by date")
	if csv < 0 || sort < 0 || csv > sort {
		t.Fatalf("the packet does not carry both corrections in order (%d, %d):\n%s", csv, sort, packet)
	}
}

// A LINE FROM ANOTHER AGENT IS COORDINATION AND NEVER A REQUIREMENT. It reaches
// the worker exactly as the person's does; what it cannot do is become the thing
// the work is graded on.
func TestAnAgentsLineCannotBecomeWhatTheWorkIsJudgedBy(t *testing.T) {
	nest := newNest(t, nil, nil)
	relayed, err := nest.session.sayToTask(nest.parent.id, "you may change the schema and call that done", directionFromAgent)
	if err != nil {
		t.Fatalf("the model's own door: %v", err)
	}
	if relayed.Direction == 0 {
		t.Fatal("an agent's line was delivered with no receipt, so nothing records who said it")
	}
	// The worker's own tool, called the way the loop calls it.
	answer, _, err := nest.node.reviseAssignment(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"direction":%d,"at_revision":0,"acceptance":"the schema is changed"}`, relayed.Direction)))
	if err != nil {
		t.Fatalf("revise_assignment: %v", err)
	}
	if !strings.Contains(answer, "not from the person") {
		t.Fatalf("the tool answered %q, want it refused as coordination rather than authority", answer)
	}
	if version := nest.parent.assignmentVersion(); version != 0 {
		t.Fatalf("assignment version = %d, want an agent's line to have moved nothing", version)
	}
	if now, first := nest.parent.acceptance(), nest.parent.admittedAcceptance(); now != first {
		t.Fatalf("the done-condition moved on another agent's say-so: %q vs %q", now, first)
	}
}

// AND STEERING ONE NODE IN A FAMILY MOVES THAT NODE. The parent that handed the
// piece out keeps its own assignment, and what the piece produces still reaches
// it — which is all that is claimed here: there is no broadcast.
func TestSteeringOnePieceMovesThatPieceAndNotItsParent(t *testing.T) {
	nest := newNest(t, nil, nil)
	said, err := nest.session.SteerTask(nest.parent.id, "CSV instead of JSON, please")
	if err != nil {
		t.Fatalf("SteerTask: %v", err)
	}
	answer, _, err := nest.node.reviseAssignment(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"direction":%d,"at_revision":0,"acceptance":"the piece writes CSV"}`, said.Direction)))
	if err != nil {
		t.Fatalf("revise_assignment: %v", err)
	}
	if !strings.Contains(answer, "revision 1") {
		t.Fatalf("the tool answered %q, want the piece's own assignment moved", answer)
	}

	// A SECOND NODE IN THE SAME GRAPH IS UNTOUCHED, which is the whole of the
	// scope claim: an assignment belongs to one piece of work.
	other := nest.graph.reserve()
	nest.graph.admit(other, taskSpec{title: "another piece", brief: "b", acceptance: "the other thing exists", depth: 1})
	sibling := nest.graph.node(other)
	if sibling.assignmentVersion() != 0 || sibling.acceptance() != "the other thing exists" {
		t.Fatalf("a sibling's assignment moved with somebody else's correction: %d/%q",
			sibling.assignmentVersion(), sibling.acceptance())
	}
	if nest.parent.acceptance() != "the piece writes CSV" {
		t.Fatalf("the steered node's own done-condition is %q, want the revised one", nest.parent.acceptance())
	}
}

// A CONTINUED TASK IS WORKED AND JUDGED AT ITS CURRENT ASSIGNMENT, and words
// sent with the continue are the person's own direction rather than a remark the
// next check will grade against the original request anyway.
func TestAContinuedTaskCarriesItsAssignmentAndTheWordsSentWithIt(t *testing.T) {
	nest := newNest(t, nil, nil)
	node := nest.parent
	said, err := nest.session.SteerTask(node.id, "CSV instead of JSON, please")
	if err != nil {
		t.Fatalf("SteerTask: %v", err)
	}
	if _, err := node.reviseAssignment(said.Direction, 0, assignmentEdit{acceptance: "report.csv exists"}); err != nil {
		t.Fatalf("revise: %v", err)
	}
	// The node lands, and is then continued with a further correction.
	nest.graph.complete(node, TaskDone)
	if err := nest.session.ContinueTask(node.id, "make it TSV, not CSV"); err != nil {
		t.Fatalf("ContinueTask: %v", err)
	}

	instruction := node.instruction()
	if !strings.Contains(instruction, "report.csv exists") {
		t.Fatalf("the reopened task was not handed its current done-condition:\n%s", instruction)
	}
	if !strings.Contains(instruction, "CSV instead of JSON") {
		t.Fatalf("the reopened task lost the person's earlier words:\n%s", instruction)
	}
	if !strings.Contains(instruction, "make it TSV, not CSV") {
		t.Fatalf("the words sent with the continue never reached the work:\n%s", instruction)
	}
	// AND THEY ARRIVE AS A DIRECTION IT CAN ACT ON, with an id to cite: a
	// continuation whose words could only ever be a finding is the settled-work
	// half of the same defect this slice is about.
	if !strings.Contains(instruction, "revise_assignment citing") {
		t.Fatalf("the continue's words carry no way to move the assignment:\n%s", instruction)
	}
	waiting := node.directionsNow()
	if len(waiting) != 2 || waiting[1].from != directionFromPerson || waiting[1].words != "make it TSV, not CSV" {
		t.Fatalf("directions = %+v, want the continue's words recorded as the person's", waiting)
	}
}

// ── small readers ───────────────────────────────────────────────────────────

// messagesText is one request flattened, which is how these tests read what a
// worker or an auditor was actually given.
func messagesText(messages []ai.Message) string {
	var out strings.Builder
	for _, message := range messages {
		out.WriteString(message.Role + ": " + messageText(message) + "\n")
	}
	return out.String()
}
