package resident

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// The incident, at the composition: a leaf that ran a long benchmark campaign,
// shared where it had got to the whole way, wrote its document, and then died on
// its time ceiling with nothing in hand. What it reached must arrive at whoever
// takes over — under the headers the overrun path has always used, because a
// second wording of the same contract is a second contract.
func TestABankIsComposedUnderTheContinuationHeaders(t *testing.T) {
	bank := Bank{
		Shared: []string{
			"implemented all four classification algorithms",
			"benchmarked against RBF-SVM on three datasets",
			"The comparison writeup for all four algorithms is now pulled together into one document",
		},
		Artifacts: []string{"/jobs/craft-4958/04-comparison.md"},
	}

	body := bank.Continuation()
	if !strings.Contains(body, ContinuationPartialHeader) {
		t.Fatalf("the bank did not arrive under the partial header:\n%s", body)
	}
	if !strings.Contains(body, ContinuationFilesHeader) {
		t.Fatalf("the bank did not name its files under the files header:\n%s", body)
	}
	if !strings.Contains(body, BankSharedLead) {
		t.Fatalf("the shared lines arrived without the lead that says they are done:\n%s", body)
	}
	for _, line := range bank.Shared {
		if !strings.Contains(body, line) {
			t.Fatalf("the bank lost a shared line %q:\n%s", line, body)
		}
	}
	if !strings.Contains(body, "/jobs/craft-4958/04-comparison.md") {
		t.Fatalf("the bank lost the file it wrote:\n%s", body)
	}
	// The exact words of the contract, pinned. OverrunGoal writes the same two,
	// and a change to either must be a change to both.
	if ContinuationPartialHeader != "What the previous agent produced before stopping (its partial result arrives as a dependency input; build on it):" {
		t.Fatalf("the partial header moved: %q", ContinuationPartialHeader)
	}
	if ContinuationFilesHeader != "Files already produced, to reuse rather than recreate:" {
		t.Fatalf("the files header moved: %q", ContinuationFilesHeader)
	}
	// And the replan brief still writes them, from here.
	goal := OverrunGoal(store.Node{Brief: "finish the comparison"}, "half of it", []string{"/jobs/x/a.md"}, "")
	if !strings.Contains(goal, ContinuationPartialHeader) || !strings.Contains(goal, ContinuationFilesHeader) {
		t.Fatalf("the replan brief stopped sharing the continuation headers:\n%s", goal)
	}
}

// A bank with nothing in it composes nothing. An input announcing an earlier
// attempt that produced no text, said nothing and wrote no file is a sentence
// that costs tokens to tell a worker its own assignment has already failed once.
func TestAnEmptyBankHandsNothingOn(t *testing.T) {
	if !(Bank{}).Empty() {
		t.Fatal("an empty bank did not say so")
	}
	if !(Bank{Shared: []string{"", "   "}}).Empty() {
		t.Fatal("a bank of blank lines is still an empty bank")
	}
	if (Bank{Shared: []string{"wrote the parser"}}).Empty() {
		t.Fatal("a bank holding one shared line called itself empty")
	}
}

// The bank is not a new record. Every line of it is already in the journal, and
// this is the read that finds them: the replaceable progress rows a long worker
// posts, and the notes it shares in its own words.
func TestBankedProgressReadsALeafsOwnRecordBack(t *testing.T) {
	graph := openRunnerStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "craft-4958", Brief: "Deliver the result of ideate novel classification algorithms", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "ideate novel classification algorithms"}); err != nil {
		t.Fatalf("splice: %v", err)
	}

	record(t, graph, "craft-4958", store.Message{
		Role: store.RoleSystem, Body: "benchmarking",
		Progress: &store.MessageProgress{Phase: "benchmarking", Done: 2, Total: 3,
			Latest: "AdaptiveKernel finished on wine and digits"},
	})
	record(t, graph, "craft-4958", store.Message{
		Role: store.RoleAgent,
		Body: NoteMark + "The comparison writeup for all four algorithms is now pulled together into one document",
	})
	// Two rows that are NOT the work reporting itself: a person steering, and
	// the machinery narrating. Neither belongs in a bank.
	record(t, graph, "craft-4958", store.Message{Role: store.RoleUser, Body: "use the same seed everywhere"})
	record(t, graph, "craft-4958", store.Message{Role: store.RoleAgent, Body: "here is your comparison"})

	lines := BankedProgress(graph, "craft-4958")
	if len(lines) != 2 {
		t.Fatalf("banked lines = %q, want the progress row and the shared note only", lines)
	}
	if lines[0] != "AdaptiveKernel finished on wine and digits" {
		t.Fatalf("the progress row lost its latest line: %q", lines[0])
	}
	if lines[1] != "The comparison writeup for all four algorithms is now pulled together into one document" {
		t.Fatalf("the shared note did not come back whole: %q", lines[1])
	}
	if strings.Contains(lines[1], NoteMark) {
		t.Fatalf("the protocol byte leaked into the bank: %q", lines[1])
	}
}

// A craft-rooted leaf is a leaf. The incident WAS a craft root, and the whole
// point of reading the node's own record is that the mechanism cannot tell —
// and must not be able to tell — which namespace an id came from.
func TestACraftRootedLeafBanksExactlyAsATaskLeafDoes(t *testing.T) {
	graph := openRunnerStore(t)
	for _, id := range []string{"craft-4958", "task-4958"} {
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
			{ID: id, Brief: "compare four algorithms", Stage: 1},
		}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "compare four algorithms"}); err != nil {
			t.Fatalf("splice %s: %v", id, err)
		}
	}
	for _, id := range []string{"craft-4958", "task-4958"} {
		record(t, graph, id, store.Message{
			Role: store.RoleSystem, Body: "writing",
			Progress: &store.MessageProgress{Phase: "writing", Latest: "the writeup is assembled"},
		})
		record(t, graph, id, store.Message{Role: store.RoleAgent, Body: NoteMark + "three of four datasets finished"})
	}

	crafted := Bank{}.WithShared(BankedProgress(graph, "craft-4958")...).Continuation()
	tasked := Bank{}.WithShared(BankedProgress(graph, "task-4958")...).Continuation()
	if crafted != tasked {
		t.Fatalf("a craft root banked differently from a task leaf:\n--- craft ---\n%s\n--- task ---\n%s", crafted, tasked)
	}
	if !strings.Contains(crafted, "the writeup is assembled") {
		t.Fatalf("the craft root's bank lost its progress:\n%s", crafted)
	}
}

// The class-stability rule. Four spellings of one ending — the executor's own
// stop reason, the node watchdog's abandonment, an expired context, a provider
// answering with a timeout status — and one answer: the worker was working, the
// hour ran out, and an hour is not evidence about which KIND of worker this
// assignment needs.
func TestADeadlineDeathIsNotEvidenceAboutTheWorkersClass(t *testing.T) {
	deadlines := []struct {
		name    string
		outcome *executor.Outcome
		err     error
	}{
		{"the executor's own loop ran out of wall clock",
			&executor.Outcome{Stop: executor.StopDeadline, Verdict: provider.VerdictProviderFailure}, nil},
		{"the loop was ordered to land because the clock was close",
			&executor.Outcome{Stop: executor.StopDone, Exhausted: executor.StopDeadline,
				Text: "the benchmark finished on three of the four datasets before hitting a timeout"}, nil},
		{"the node watchdog gave up waiting", nil, &executor.Abandoned{After: 17 * time.Minute}},
		{"the context expired", nil, fmt.Errorf("post completions: %w", context.DeadlineExceeded)},
		{"the provider answered with a gateway timeout", nil, &provider.APIError{Status: 504, Message: "upstream timed out"}},
	}
	for _, deadline := range deadlines {
		if MayReclassify(deadline.outcome, deadline.err) {
			t.Fatalf("%s moved the worker's class", deadline.name)
		}
		if !ClockDeath(deadline.outcome, deadline.err) {
			t.Fatalf("%s was not read as a death on the clock", deadline.name)
		}
	}
	// The watchdog's sentence is unchanged by becoming a type: the room reads it.
	if got := (&executor.Abandoned{After: 17 * time.Minute}).Error(); got != "executor did not return within 17m0s; abandoned" {
		t.Fatalf("the watchdog's sentence changed: %q", got)
	}
}

// The other half of the same rule, and the reason it is a taxonomy rather than a
// blanket refusal: an ending that IS evidence about capability still moves the
// class. An empty response is the named one — full price, nothing delivered.
func TestACapabilityFailureStillMovesTheWorkersClass(t *testing.T) {
	capability := []struct {
		name    string
		outcome *executor.Outcome
		err     error
	}{
		{"the whole budget spent on private deliberation",
			&executor.Outcome{Stop: executor.StopEmpty, Verdict: provider.VerdictEmptyResponse}, nil},
		{"the reply did not parse",
			&executor.Outcome{Verdict: provider.VerdictFormatFailure}, nil},
		{"it parsed and was wrong",
			&executor.Outcome{Verdict: provider.VerdictSemanticFailure}, nil},
		{"it could not converge inside its grant",
			&executor.Outcome{Stop: executor.StopBudget, Verdict: provider.VerdictBudgetStop}, nil},
		{"a straggler handed back on measured evidence",
			&executor.Outcome{Stop: executor.StopOverrun, Verdict: provider.VerdictBudgetStop}, nil},
		{"the tools it was given were refused, and nothing ran",
			nil, &provider.APIError{Status: 404, Message: "No endpoints found that support tool use"}},
	}
	for _, failure := range capability {
		if ClockDeath(failure.outcome, failure.err) {
			t.Fatalf("%s was misread as a death on the clock", failure.name)
		}
		if !MayReclassify(failure.outcome, failure.err) {
			t.Fatalf("%s was refused as evidence about the worker's class", failure.name)
		}
	}
	// Weather is not capability either, and the taxonomy already said so: a
	// provider failure grades nothing, so it moves nothing.
	if MayReclassify(&executor.Outcome{Verdict: provider.VerdictProviderFailure}, nil) {
		t.Fatal("a provider failure moved the worker's class")
	}
	// And an ending nobody reported is not an ending.
	if MayReclassify(nil, nil) {
		t.Fatal("a leaf that neither failed nor produced anything moved the worker's class")
	}
}

// A leaf claimed a second time is a leaf whose first run was interrupted, and
// the store's own attempt counter is how the runner knows. It is the whole of
// the pickup signal.
func TestALeafRequeuedAtLaunchCarriesAnAttemptCountAndItsBank(t *testing.T) {
	graph := openRunnerStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "run the campaign", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "run the campaign"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	claim, ok, err := graph.Claim("task-1", "first-process")
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start: %v", err)
	}
	record(t, graph, "task-1", store.Message{
		Role: store.RoleSystem, Body: "benchmarking",
		Progress: &store.MessageProgress{Phase: "benchmarking", Latest: "two of three datasets are through"},
	})

	// The process goes away; the next launch sweeps the claim it left behind.
	released, err := graph.ReleaseOrphans()
	if err != nil {
		t.Fatalf("release orphans: %v", err)
	}
	if len(released) != 1 || released[0] != "task-1" {
		t.Fatalf("released = %q, want the interrupted leaf", released)
	}
	node, ok, err := graph.Node("task-1")
	if err != nil || !ok {
		t.Fatalf("read node: ok=%v err=%v", ok, err)
	}
	if node.Status != store.Pending {
		t.Fatalf("status = %v, want pending so the next resident claims it", node.Status)
	}
	// The snapshot the runner hands to execution is read before its own claim,
	// so a leaf that has been here before says so: attempt above zero.
	if node.Attempt == 0 {
		t.Fatalf("attempt = %d, want the first attempt counted so the pickup can be seen", node.Attempt)
	}
	if lines := BankedProgress(graph, "task-1"); len(lines) != 1 ||
		lines[0] != "two of three datasets are through" {
		t.Fatalf("the interrupted leaf's bank did not survive the sweep: %q", lines)
	}
	// And the second claim counts again, so nothing about the sweep resets it.
	second, ok, err := graph.Claim("task-1", "second-process")
	if err != nil || !ok {
		t.Fatalf("second claim: ok=%v err=%v", ok, err)
	}
	_ = second
	again, _, err := graph.Node("task-1")
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	if again.Attempt <= node.Attempt {
		t.Fatalf("attempt did not increment on the retry: %d then %d", node.Attempt, again.Attempt)
	}
}

func record(t *testing.T, graph *store.Store, nodeID string, message store.Message) {
	t.Helper()
	message.NodeID = nodeID
	if _, err := thread.Record(graph, message); err != nil {
		t.Fatalf("record on %s: %v", nodeID, err)
	}
}
