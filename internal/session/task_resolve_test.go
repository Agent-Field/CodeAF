package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// RESOLVING A NODE NOBODY COULD JUDGE — the three answers, and what they must
// not do to each other.
//
// Every test here is about a node that has ALREADY LANDED in TaskUnverified.
// The work is over, the branch is where it was kept, and the only thing that
// moves it is somebody deciding: an accept, a refute, or a fresh auditor whose
// verdict arrives minutes later ([Agent.ResolveUnverified]). Three doors onto
// one landing is exactly the shape that goes wrong two ways — two answers at
// once, and an answer that overwrites the account of the work it is answering —
// and those two are what is asserted below.

// unverifiedNode lands one node in TaskUnverified with the report a real one
// carries: the audit's non-answer, and the work's own claim under it
// (task_run.go's workTaskNode). It runs in the person's own tree, so nothing
// here needs a repository — the branch is what a merge would move, and an
// in-place node has none.
func unverifiedNode(t *testing.T, mutate func(*Config)) (*Agent, *TaskNode) {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, mutate)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish(withReport(needsLookLead+"the checker could not be asked: dial tcp: connection refused", workClaimSaid),
			nil, "", "")
		node.keepClaim(workClaimSaid)
		node.graph.complete(node, TaskUnverified)
	})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Add the guard", brief: "b", acceptance: "a"})
	node := graph.node(id)
	waitDoneNode(t, node)
	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("the node landed %q, want unverified", state)
	}
	return agent, node
}

// workClaimSaid is the work's own account of itself: the one sentence in this
// file that no auditor wrote, and the one every assertion looks for.
const workClaimSaid = "added the nil-map guard and a regression test that fails without it"

// inPlaceTree is the working copy of a node that ran where the person is
// standing — what [TaskNode.workingCopy] hands back for the nodes above.
func inPlaceTree(t *testing.T, node *TaskNode, agent *Agent) taskTree {
	t.Helper()
	tree, err := node.workingCopy(agent.config.Workspace)
	if err != nil {
		t.Fatalf("workingCopy: %v", err)
	}
	return tree
}

// A SECOND NON-ANSWER MUST NOT DELETE THE WORK'S OWN ACCOUNT.
//
// The re-audit came back with nothing again. That replaces the last non-answer —
// two auditors failing to answer is one fact — and it says nothing whatever
// about what the work did, so the claim under it is the only description of the
// work that exists. It is what the card shows, what the index row reads, what a
// dependent is handed as "what the work before you learned", and what an accept
// carries into TaskDone.
func TestASecondNonAnswerKeepsTheWorksOwnReport(t *testing.T) {
	agent, node := unverifiedNode(t, nil)

	verdict := noVerdict("the checker could not be asked: dial tcp: connection refused", "").twice()
	agent.landAudit(node, inPlaceTree(t, node, agent), verdict, nil)

	report := node.notice().Report
	if !strings.Contains(report, workClaimSaid) {
		t.Fatalf("the re-audit deleted the work's own account:\n%s", report)
	}
	if !strings.HasPrefix(report, needsLookLead) {
		t.Fatalf("the fresh non-answer is not what the card leads with:\n%s", report)
	}
	if !strings.Contains(report, "asked twice") {
		t.Fatalf("the report does not say this is the second nothing:\n%s", report)
	}
	// The stale non-answer is REPLACED, not stacked: one lead line, not two.
	if n := strings.Count(report, needsLookLead); n != 1 {
		t.Fatalf("the report carries %d non-answers, want 1:\n%s", n, report)
	}
	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("state = %q, want unverified: nobody said anything about this work", state)
	}
}

// AND A VERDICT MUST NOT LAND ON TOP OF THE NON-ANSWER IT ANSWERS.
//
// The fresh auditor said VERIFIED. The card now leads with the evidence, and the
// work's claim stands under it — but the line saying nobody could judge this
// work is GONE, because it has just been judged. A card reading "VERIFIED …"
// over "finished, but needs your look — …" contradicts itself in two lines.
func TestAReauditThatVerifiesDropsTheStaleNonAnswer(t *testing.T) {
	agent, node := unverifiedNode(t, nil)

	verdict := auditVerdict{
		verified: true, answered: true, word: auditVerified,
		evidence: []string{"go test ./internal/parse ok", "3 files changed"},
	}
	agent.landAudit(node, inPlaceTree(t, node, agent), verdict, nil)

	report := node.notice().Report
	if !strings.Contains(report, "go test ./internal/parse ok") {
		t.Fatalf("the verdict's evidence is not on the card:\n%s", report)
	}
	if !strings.Contains(report, workClaimSaid) {
		t.Fatalf("the accepted node lost the work's own account:\n%s", report)
	}
	if strings.Contains(report, needsLookLead) || strings.Contains(report, auditUnverified) {
		t.Fatalf("a verified card still says nobody could judge it:\n%s", report)
	}
	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("state = %q, want done", state)
	}
}

// A NODE FROM AN OLDER CHECKPOINT STILL SAYS SOMETHING. Nothing kept its claim
// apart, so the report is carried whole rather than guessed apart: it may lead
// with a stale line, and that is strictly better than a card with the work
// deleted off it.
func TestALandingWithNoKeptClaimCarriesTheReportWhole(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	node.graph.mu.Lock()
	node.claim = "" // a checkpoint written before the claim was kept
	node.graph.mu.Unlock()

	agent.landAudit(node, inPlaceTree(t, node, agent), noVerdict("the checker could not be asked", "").twice(), nil)

	if report := node.notice().Report; !strings.Contains(report, workClaimSaid) {
		t.Fatalf("an older node's only account of itself was dropped:\n%s", report)
	}
}

// TWO ANSWERS AT ONCE: ONE LANDS, THE OTHER IS TOLD WHY IT DID NOT.
//
// The model can put two `tasks … resolve accept` calls in one batch, and a tool
// batch runs concurrently (loop.go). Both would read TaskUnverified and both
// would bring the branch home — a second merge over a branch the first already
// merged and deleted, two index rows, two cards. The state check and the settle
// are one claim, so exactly one of them is a resolution.
func TestTwoResolutionsAtOnceLandExactlyOnce(t *testing.T) {
	agent, node := unverifiedNode(t, nil)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	// Released together, so the two are as close to simultaneous as a test can
	// put them — which is how they arrive out of one tool batch.
	start := make(chan struct{})
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = agent.ResolveUnverified(node.id, TaskAccept, "I read the diff")
		}()
	}
	close(start)
	wg.Wait()

	landed := 0
	for _, err := range errs {
		if err == nil {
			landed++
		}
	}
	if landed != 1 {
		t.Fatalf("%d of 2 concurrent accepts landed, want exactly 1 (errors: %v)", landed, errs)
	}
	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("state = %q, want done", state)
	}
	if report := node.notice().Report; strings.Count(report, acceptedLine("")) != 1 {
		t.Fatalf("the node was accepted twice into one report:\n%s", report)
	}
}

// AND THE REFUSAL NAMES WHAT IS IN FLIGHT. A person told "no" by a surface owes
// nothing to the machinery; what they need is which answer is already on its way
// and therefore what they are waiting for.
func TestAResolutionRefusesWhileAnotherIsInFlight(t *testing.T) {
	agent, node := unverifiedNode(t, nil)

	if err := node.claimSettle(claimReaudit); err != nil {
		t.Fatalf("claiming the node: %v", err)
	}
	for _, answer := range []TaskResolution{TaskAccept, TaskRefute, TaskReaudit} {
		err := agent.ResolveUnverified(node.id, answer, "")
		if err == nil {
			t.Fatalf("%s landed on top of a re-audit that had the node", answer)
		}
		if !strings.Contains(err.Error(), claimReaudit) {
			t.Fatalf("the refusal does not say what is in flight: %v", err)
		}
	}
	// And once it is handed back, the same answer goes through.
	node.releaseSettle()
	if err := agent.ResolveUnverified(node.id, TaskRefute, "not finished"); err != nil {
		t.Fatalf("refuting a node nobody holds failed: %v", err)
	}
	if state := node.stateNow(); state != TaskFailed {
		t.Fatalf("state = %q, want failed", state)
	}
}

// A RE-AUDIT WITH NOWHERE TO WRITE ITS LOG IS REFUSED, NOT STARTED ANYWAY.
//
// The job row is where the cancel is registered. Without one the audit would run
// on a bare context: no `jobs kill`, no death at [Agent.Close], and a landing
// that finishes a node into a session that has gone. The person keeps their
// other two answers, and the node is left exactly as it was — unclaimed, so the
// next attempt is refused for its own reason rather than for this one.
func TestAReauditThatCannotBeListedIsRefused(t *testing.T) {
	agent, node := unverifiedNode(t, nil)

	// A workspace whose jobs directory cannot be created: the path walks through
	// a regular file.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing the blocker: %v", err)
	}
	agent.jobs.workspace = filepath.Join(blocked, "workspace")

	err := agent.ResolveUnverified(node.id, TaskReaudit, "")
	if err == nil {
		t.Fatal("a re-audit with no job row was started anyway")
	}
	if !strings.Contains(err.Error(), "could not be started") {
		t.Fatalf("the refusal does not say the re-audit never began: %v", err)
	}
	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("state = %q, want unverified: a refused re-audit changes nothing", state)
	}
	// THE CLAIM WAS HANDED BACK. A node left claimed by a re-audit that never
	// started is a node nobody can ever resolve.
	node.graph.mu.Lock()
	held := node.settling
	node.graph.mu.Unlock()
	if held != "" {
		t.Fatalf("the node is still claimed by %q after a refused re-audit", held)
	}
	if err := agent.ResolveUnverified(node.id, TaskAccept, "I read it myself"); err != nil {
		t.Fatalf("accepting after a refused re-audit failed: %v", err)
	}
}

// The claim is only ever a claim on a node that NEEDS A LOOK. Every other state
// is answered with the same sentence the tool has always answered with, and the
// check is part of the claim rather than a question asked before it.
func TestOnlyANodeThatNeedsALookCanBeClaimed(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	if err := agent.ResolveUnverified(node.id, TaskAccept, ""); err != nil {
		t.Fatalf("accepting: %v", err)
	}
	err := node.claimSettle(claimAccept)
	if err == nil {
		t.Fatal("a done node was claimed for a resolution")
	}
	if want := fmt.Sprintf("task %d is %s", node.id, TaskDone); !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal does not name the state: %v", err)
	}
}
