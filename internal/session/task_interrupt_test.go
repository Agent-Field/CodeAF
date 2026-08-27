package session

// A CANCEL IS AN INTERRUPTION, NEVER A FINDING.
//
// The two tests here are one boundary read from both sides: what happens to a
// node whose CHECK was cut off by something outside the work, and what happens to
// a node the check actually looked at and turned down. The first must never wear
// the second's ending — a kill is not evidence — and the second must be untouched
// by the arm that answers the first.
//
// They drive the real executor over a real repository, for task_repair_test.go's
// reason: "the branch was kept and the claim survived" is not a claim a stub can
// make.

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A NODE CUT OFF MID-CHECK IS UNFINISHED WORK, NOT FAILED WORK.
//
// Measured on SWE-Marathon s10: the runner's own settle-kill produced the
// ctx.Err() that [Agent.workTaskNode] was reading between the check and the
// landing, and the kill wrote the verdict — the node landed FAILED with "stopped
// while its work was being checked" as its whole account. Nothing had looked at
// the deliverable. Nobody had made a finding. The work was on a branch and the
// node's own words about it were thrown away.
//
// SO IT LANDS UNVERIFIED, WITH BOTH HALVES KEPT: the plain sentence saying the
// check never finished, and under it the node's last stated intent, verbatim.
func TestANodeCutOffWhileItsWorkWasBeingCheckedIsUnfinishedAndNotFailed(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	const intent = "Wrote greet.go with the greeting; go build passes."

	// The graph is reached from inside the audit lane, which is the only moment
	// this test can cut the node: before it the run is still going, after it the
	// node has already landed.
	var agent *Agent
	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go with a greeting"),
			finalText("handed off"),
		},
		child: nodeLane(6, func(_, wrote bool) *ai.Response {
			if !wrote {
				return writeResponse("call-src", "greet.go",
					"package greet\n\nfunc Greet() string { return \"hi\" }\n")
			}
			return pricedResponse(intent, 0.01)
		}),
		// THE SETTLE-KILL, LANDED WHERE IT LANDED IN THE MEASURED RUN: the check
		// has been asked its question and the node's context dies underneath it.
		// The call then fails the way a cancelled call fails, so no verdict is
		// ever reached about this work — which is the whole point.
		audit: []step{
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				cutTheNode(agent, 1)
				return nil, context.Canceled
			},
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				return nil, context.Canceled
			},
		},
	}
	built, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	agent = built
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State == TaskFailed {
		t.Fatalf("a node whose check was cut off landed FAILED: the kill wrote the verdict (report %q)", notice.Report)
	}
	if notice.State != TaskUnverified {
		t.Fatalf("state = %q, want unverified — interrupted work is handed up, not judged (report %q)",
			notice.State, notice.Report)
	}
	// THE SENTENCE SAYS WHAT IS KNOWABLE AND NOTHING ELSE.
	if !strings.HasPrefix(notice.Report, incompleteLead) {
		t.Fatalf("report = %q, want it to open with the lead a landing already has for unfinished work", notice.Report)
	}
	if !strings.Contains(notice.Report, "nothing finished checking it") {
		t.Fatalf("report = %q, want it to say the check never finished", notice.Report)
	}
	// AND THE NODE'S OWN LAST WORDS SURVIVE THE KILL.
	if !strings.Contains(notice.Report, intent) {
		t.Fatalf("report = %q, want the node's last stated intent quoted under the sentence", notice.Report)
	}
	// THE WORK IS WHERE THE REPORT SAYS IT IS: on the node's own branch, kept.
	if notice.Branch == "" {
		t.Fatalf("notice = %+v, want the branch the interrupted work is on", notice)
	}
	if notice.Merge == mergeMerged {
		t.Fatal("work nobody finished checking was merged onto the person's branch")
	}
	// AND NOTHING A PERSON READS CARRIES THE MACHINERY'S WORDS.
	assertPlainLanding(t, notice, "a node cut off mid-check")
}

// AND A NODE THE CHECK ACTUALLY TURNED DOWN STILL LANDS AS THE CHECK SAYS.
//
// This is the arm above's boundary. A refutation is a FINDING — somebody looked
// at the work and said what is missing — and the node fails on it with the gaps
// in front, exactly as it did before a cancel had an answer of its own.
func TestANodeThatFailsItsCheckStillLandsOnTheFinding(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go with a greeting"),
			finalText("handed off"),
		},
		child: nodeLane(6, func(_, wrote bool) *ai.Response {
			if !wrote {
				return writeResponse("call-notes", "notes.md", "# what I was thinking about\n")
			}
			return pricedResponse("I wrote up my notes. Done, I think.", 0.01)
		}),
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("greet.go",
				"VERIFIED — git status --porcelain shows greet.go staged",
				"REFUTED — git status --porcelain shows notes.md and no greet.go"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskFailed {
		t.Fatalf("state = %q, want failed — the check made a finding about this work (report %q)",
			notice.State, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, incompleteLead) {
		t.Fatalf("report = %q, want the gaps the check named", notice.Report)
	}
	if !strings.Contains(notice.Report, "no greet.go") {
		t.Fatalf("report = %q, want the checker's own evidence", notice.Report)
	}
	// AND IT IS NOT WEARING THE INTERRUPTED SENTENCE.
	if strings.Contains(notice.Report, "nothing finished checking it") {
		t.Fatalf("report = %q: a finding was dressed as an interruption", notice.Report)
	}
}

// cutTheNode ends one node's run the way something OUTSIDE the work ends it — a
// settle-kill, a quit, a deadline on the session — which is a cancelled context
// and NOT a person's stop ([TaskNode.markStopped]). The two are different
// endings and this test is about the one nobody chose.
func cutTheNode(agent *Agent, id uint64) {
	node := agent.graph().node(id)
	if node == nil {
		return
	}
	node.graph.mu.Lock()
	cancel := node.cancel
	node.graph.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
