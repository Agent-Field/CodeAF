package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the four endings, end to end ────────────────────────────────────────────
//
// Each of these runs a REAL task through the real runner — a proposal from the
// conversation, a worker in a git worktree, the check — with only the model
// scripted, and reads the landing the way a surface does: the notice's state,
// ending and report, and the note the conversation is handed. They are the
// measured evening's four rows, replayed.

const wireReset = "decode stream: read tcp 192.168.2.13:51346->104.18.3.115:443: read: connection reset by peer"

func wireError() step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New(wireReset)
	}
}

// endingAgent is [newTestAgent] with the retry ladder shortened to one retry
// and a two-second wait, so a run that spends the ladder twice — once per
// worker — still lands inside the node wait.
func endingAgent(t *testing.T, completer Completer) (*Agent, *TaskGraph) {
	t.Helper()
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AFORGE_RESPONSE_ATTEMPTS", "2")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 0
	})
	return agent, agent.graph()
}

func landedNode(t *testing.T, agent *Agent, graph *TaskGraph) TaskNotice {
	t.Helper()
	collect(t, mustSubmit(t, agent, "add a greeting"))
	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	return node.notice()
}

// (1) ONE RESET IS RETRIED, AND THE NODE LANDS DONE. The measured failure: a
// stream that reset once ended a task on attempt 1.
func TestATaskWhoseStreamResetOnceIsRetriedAndLandsDone(t *testing.T) {
	completer := &routedCompleter{
		parent: []step{proposeCall("Add the greeting", "write greet.go with a greeting"), finalText("handed off")},
		child: append([]step{wireError()}, nodeLane(4, func(_, wrote bool) *ai.Response {
			if !wrote {
				return writeResponse("call-greet", "greet.go", "package main\n\nfunc Greet() string { return \"hi\" }\n")
			}
			return pricedResponse("Done: greet.go written.", 0.01)
		})...),
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("greet.go", "VERIFIED — greet.go is staged", "REFUTED — no greet.go"),
		},
	}
	agent, graph := endingAgent(t, completer)
	started := time.Now()
	notice := landedNode(t, agent, graph)
	if notice.State != TaskDone {
		t.Fatalf("state = %q, ending %q, report %q — one reset ended the task", notice.State, notice.Ending, notice.Report)
	}
	if notice.Ending != "" {
		t.Fatalf("a done node carries an ending %q", notice.Ending)
	}
	if time.Since(started) < 2*time.Second {
		t.Fatal("the reset was not waited out before the retry")
	}
}

// (2) A CONNECTION THAT STAYS DOWN ENDS THE NODE AS "LOST THE CONNECTION" — after
// the ladder was spent twice, once per worker — and not as "failed".
func TestATaskWhoseConnectionStaysDownLandsAsLostTheConnection(t *testing.T) {
	completer := &routedCompleter{
		parent: []step{proposeCall("Add the greeting", "write greet.go with a greeting"), finalText("handed off")},
		child:  []step{wireError(), wireError(), wireError(), wireError(), wireError(), wireError()},
	}
	agent, graph := endingAgent(t, completer)
	notice := landedNode(t, agent, graph)
	if notice.State != TaskFailed || notice.Ending != TaskEndingWire {
		t.Fatalf("state = %q, ending = %q, report %q", notice.State, notice.Ending, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, "lost the connection to the model: ") {
		t.Fatalf("report = %q, want it to lead with the connection", notice.Report)
	}
	// THE LADDER WAS WALKED TWICE: two attempts for the first worker, two for the
	// second built in the same working copy.
	if asked := completer.seen.child; asked != 4 {
		t.Fatalf("the model was asked %d times, want 4 (two attempts × two workers)", asked)
	}
	if note := taskNote(notice, "", TaskSettleAsk, landingAddress{}); !strings.Contains(note, "task 1 incomplete: ") || !strings.Contains(note, "· lost the connection") {
		t.Fatalf("the landing note opens %q", firstLines(note, 1))
	}
	if notice.Branch == "" || notice.Merge == mergeMerged {
		t.Fatalf("branch %q merge %q: the work was not kept", notice.Branch, notice.Merge)
	}
}

// (3) A WORKER THAT KEEPS MAKING THE SAME CALL IS ENDED BY ITS OWN LOOP GUARD, and
// the row says "went in circles" — first cause — even though the check then
// refuses the empty work.
func TestATaskThatGoesInCirclesLandsAsCircling(t *testing.T) {
	var same []step
	for range 16 {
		same = append(same, bashCall("call-same", "git status --porcelain"))
	}
	completer := &routedCompleter{
		parent: []step{proposeCall("Add the greeting", "write greet.go with a greeting"), finalText("handed off")},
		child:  same,
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("greet.go", "VERIFIED — greet.go is staged", "REFUTED — no greet.go"),
		},
	}
	agent, graph := endingAgent(t, completer)
	notice := landedNode(t, agent, graph)
	if notice.State != TaskFailed || notice.Ending != TaskEndingCircling {
		t.Fatalf("state = %q, ending = %q, report %q", notice.State, notice.Ending, notice.Report)
	}
	if note := taskNote(notice, "", TaskSettleAsk, landingAddress{}); !strings.Contains(note, "task 1 incomplete: ") || !strings.Contains(note, "· went in circles") {
		t.Fatalf("the landing note opens %q", firstLines(note, 1))
	}
}

// (4) A WORKER WHOSE WRITES ANOTHER TASK'S COPY REFUSED IS BLOCKED, NOT CIRCLING.
// The refusal itself is treehold.go's; what this pins is that it reaches the
// writer's row and turns the loop guard's ending into the holder's name.
func TestAWorkerRefusedByAnotherTasksTreeLandsAsBlocked(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := inPlaceGraph(t, workspace,
		runningNode{id: 4, title: "repair the parser", ago: 20 * time.Minute},
		runningNode{id: 5, title: "add the tests", ago: 2 * time.Minute})
	agent.config.tasker = graph
	agent.config.taskID = 5
	call := scopedCall("write", filepath.Join(workspace, "src/analysis.rs"))
	if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil, call); ok {
		t.Fatal("the write was not refused")
	}
	blocked := graph.node(5).blockedByNow()
	if !strings.Contains(blocked, "task 4") {
		t.Fatalf("the writer's row names %q as the holder, want task 4", blocked)
	}
	if got := endingOfClaim(loopLeftUndoneNote, blocked, ""); got != TaskEndingBlocked {
		t.Fatalf("ending = %q, want blocked", got)
	}
	if graph.node(4).blockedByNow() != "" {
		t.Fatal("the holder was marked blocked by its own refusal")
	}
}

// (5) A WORKER THAT LANDED ITS WORK QUIETLY IS NOT CIRCLING. The evening's fifth
// row: a worker whose commit-and-push phase was twenty-six distinct, successful
// shell commands with nothing said between them was cut off by the third
// [silent] note and written up as `went in circles` — the one sentence about it
// that was not true. Silence books no nudge now, so the run reaches its landing
// write and the row says nothing about circles.
func TestATaskThatWorkedQuietlyDoesNotGoInCircles(t *testing.T) {
	var quiet []step
	// Past the last silent rung (24 batches) with room to spare, and every
	// command leaves a different file behind, which is what a landing phase does.
	for round := range 3 * silentThreshold(1) {
		quiet = append(quiet, bashCall(fmt.Sprintf("call-land-%d", round),
			fmt.Sprintf("printf 'step %d\\n' > note-%d.txt", round, round)))
	}
	quiet = append(quiet,
		writeCall("call-greet", "greet.go", "package main\n\nfunc Greet() string { return \"hi\" }\n"),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return pricedResponse("Done: greet.go written.", 0.01), nil
		})

	completer := &routedCompleter{
		parent: []step{proposeCall("Add the greeting", "write greet.go with a greeting"), finalText("handed off")},
		child:  quiet,
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("greet.go", "VERIFIED — greet.go is staged", "REFUTED — no greet.go"),
		},
	}
	agent, graph := endingAgent(t, completer)
	notice := landedNode(t, agent, graph)

	if notice.Ending == TaskEndingCircling {
		t.Fatalf("a quiet landing was written up as circling: state %q report %q", notice.State, notice.Report)
	}
	if strings.Contains(notice.Report, loopLeftUndoneNote) {
		t.Fatalf("the loop guard ended a working turn: report %q", notice.Report)
	}
	// AND IT WAS NEVER SCOLDED. A landing phase of distinct, successful shell
	// commands is work, so no note about it belongs in the worker's context at
	// all: the tree moved on every one of them, which resets the silent ladder
	// and answers the "read nothing new" rule in the same reading.
	for _, note := range []string{"[silent]", "[stuck]"} {
		if completer.childSaw(note) {
			t.Fatalf("a quiet landing was handed a %s note", note)
		}
	}
	// AND IT REACHED ITS LAST STEP. The whole defect was a turn taken away with
	// the work unlanded, so "not circling" is only half the claim.
	if !completer.childSaw("greet.go") {
		t.Fatal("the run never reached the write that lands the work")
	}
	if notice.State != TaskDone {
		t.Fatalf("state = %q, ending = %q, report %q", notice.State, notice.Ending, notice.Report)
	}
}

// (6) A WORKER THAT WOULD NOT WRITE ITS NOTES LANDS UNDER ITS OWN WORDS. It was
// advised twice, held three times and then stopped by the write-your-notes rule
// (processrule.go) — and until this landing existed, `endingOfClaim` had nothing
// to read but the worker's last words, so the node settled unexplained and the
// rail said "stopped — branch kept", which reads as a person's own stop.
//
// The script is distinct shell commands with fresh output and no visible text
// beside any of them: distinct, so the loop guard's identity rules have nothing
// to hold; fresh, so the no-progress leash out at the task boundary keeps
// resetting; and silent, so the ladder climbs to the rung where advice stops
// being advice.
func TestATaskWhoseWorkerWillNotWriteItsNotesLandsSayingSo(t *testing.T) {
	var quiet []step
	for round := range enforcedRungAt() + processRuleRefusals + 4 {
		quiet = append(quiet, bashCall(fmt.Sprintf("call-quiet-%d", round),
			fmt.Sprintf("printf 'looked at step %d\\n'", round)))
	}
	completer := &routedCompleter{
		parent: []step{proposeCall("Add the greeting", "write greet.go with a greeting"), finalText("handed off")},
		child:  quiet,
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("greet.go", "VERIFIED — greet.go is staged", "REFUTED — no greet.go"),
		},
	}
	agent, graph := endingAgent(t, completer)
	notice := landedNode(t, agent, graph)

	if notice.State != TaskFailed || notice.Ending != TaskEndingNotes {
		t.Fatalf("state = %q, ending = %q, report %q", notice.State, notice.Ending, notice.Report)
	}
	// THE ROW SAYS WHY, and the landing note the conversation is handed says the
	// same thing in a sentence.
	if note := taskNote(notice, "", TaskSettleAsk, landingAddress{}); !strings.Contains(note, "task 1 incomplete: ") || !strings.Contains(note, "· would not write its notes down") {
		t.Fatalf("the landing note opens %q", firstLines(note, 1))
	}
	// AND THE WORK IS KEPT. Nothing was found wrong with it: the turn was ended
	// from outside, and whatever the worker had done is on its branch.
	if notice.Branch == "" || notice.Merge == mergeMerged {
		t.Fatalf("branch %q merge %q: the work was not kept", notice.Branch, notice.Merge)
	}
	// AND THE RECORD THAT GRADES THE MODEL SAYS `stopped` rather than `did not
	// finish`: the run was ended from outside, and that is not a reading of what
	// the work was worth (taskgrade.go).
	if got := taskGradeOutcome(notice.State, notice.Ending, notice.Stopped); got != "stopped" {
		t.Fatalf("the graded record for this node says %q, want stopped", got)
	}
	// AND THE STOP REALLY CAME FROM THE RULE rather than from a threshold that
	// happened to fire first: the worker read the demand before it was stopped.
	if !completer.childSaw("[held]") {
		t.Fatalf("the worker was never held; report %q", notice.Report)
	}
}
