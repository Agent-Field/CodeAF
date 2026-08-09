package exec

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// runawayCompleter is the failure mode the probe lab measured: a model that
// spends its entire token allowance on private reasoning and returns 200 OK with
// no content at all. It is not an error and it is not an answer, and it is the
// most expensive outcome available — full price, nothing delivered.
type runawayCompleter struct {
	mutex      sync.Mutex
	calls      int
	completion int
}

func (r *runawayCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	r.mutex.Lock()
	r.calls++
	r.mutex.Unlock()
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant"}, FinishReason: "length"}},
		Usage:   &ai.Usage{PromptTokens: 100, CompletionTokens: r.completion},
	}, nil
}

// TestRunawayTurnAbandonsTheLeafInsteadOfNudgingIt is the circuit breaker. The
// loop's existing answer to an empty reply is to nudge and continue, which is
// right when the reply was merely truncated and a trap when the model is burning
// its whole budget thinking — nudging buys the same thing again at the same
// price. One turn, then out, so that escalation can happen.
func TestRunawayTurnAbandonsTheLeafInsteadOfNudgingIt(t *testing.T) {
	client := &runawayCompleter{completion: 80_000}
	linear := NewLinear(client, workspace(t), nil, 20, 100_000, time.Minute)

	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopEmpty {
		t.Fatalf("stop = %s, want %s", outcome.Stop, StopEmpty)
	}
	if outcome.Verdict != provider.VerdictEmptyResponse {
		t.Fatalf("verdict = %s, want the runaway-reasoning verdict", outcome.Verdict)
	}
	if client.calls != 1 {
		t.Fatalf("made %d calls, want one — the same model was asked again", client.calls)
	}
}

// TestAnOrdinaryEmptyReplyIsStillNudged is the other half. A short empty turn
// that costs almost nothing is a hiccup, not a mode, and cutting the leaf for it
// would throw away work over a truncated think.
func TestAnOrdinaryEmptyReplyIsStillNudged(t *testing.T) {
	client := &runawayCompleter{completion: 50}
	linear := NewLinear(client, workspace(t), nil, 4, 100_000, time.Minute)

	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop == StopEmpty {
		t.Fatal("a cheap empty reply tripped the circuit breaker")
	}
	if client.calls != 4 {
		t.Fatalf("made %d calls, want the loop to have kept nudging", client.calls)
	}
}

// TestLeafVerdictsSeparateStoppingFromSucceeding is the whole point of the
// taxonomy. All three of these leaves produce text and all three would be
// StateDone; only one of them worked.
func TestLeafVerdictsSeparateStoppingFromSucceeding(t *testing.T) {
	tests := []struct {
		name    string
		outcome Outcome
		want    provider.Verdict
	}{
		{"finished", Outcome{Stop: StopDone, Text: "the deliverable"}, provider.VerdictUnverifiedSuccess},
		{"out of budget", Outcome{Stop: StopBudget, Text: "half the deliverable"}, provider.VerdictBudgetStop},
		{"out of turns", Outcome{Stop: StopTurnCap, Text: "still going"}, provider.VerdictTurnCap},
		{"timed out", Outcome{Stop: StopDeadline, Text: "partial"}, provider.VerdictProviderFailure},
		{"transport gave up", Outcome{Stop: StopError}, provider.VerdictProviderFailure},
		{"said nothing", Outcome{Stop: StopDone}, provider.VerdictEmptyResponse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := verdictFor(&test.outcome); got != test.want {
				t.Fatalf("verdict = %s, want %s", got, test.want)
			}
			// Only the ones that mean "could not finish" may move a rating, and
			// a timeout is never among them.
			positive, graded := test.want.Graded()
			if positive {
				t.Fatal("a general leaf has no verifier and must never claim a verified success")
			}
			if test.want == provider.VerdictProviderFailure && graded {
				t.Fatal("a transport failure is being read as evidence about the model")
			}
		})
	}
}

// verdictExecutor returns a fixed verdict, and a different one once retried.
type verdictExecutor struct {
	mutex    sync.Mutex
	attempts int
	first    Outcome
	then     Outcome
}

func (v *verdictExecutor) Subharness() string { return "linear" }

func (v *verdictExecutor) Run(ctx context.Context, task Task) (*Outcome, error) {
	v.mutex.Lock()
	v.attempts++
	attempt := v.attempts
	v.mutex.Unlock()
	// The attempt number reaches the executor through the routing slot, which is
	// how a router is told to climb without the scheduler knowing what it is
	// climbing to.
	if provider.CallFrom(ctx).Class() != provider.ClassExecLeaf {
		return nil, context.Canceled
	}
	outcome := v.first
	if attempt > 1 {
		outcome = v.then
	}
	return &outcome, nil
}

// TestAFailedLeafIsRetriedOnceWhenThereIsSomewhereToGo covers the escalation
// path, and the guard on it: without a panel the scheduler must behave exactly
// as it always has, because a retry that cannot reach a better model is only a
// second bill.
func TestAFailedLeafIsRetriedOnceWhenThereIsSomewhereToGo(t *testing.T) {
	build := func() (*plan.Graph, *verdictExecutor) {
		graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
		graph.Add(plan.Node{Stage: 1, Title: "Leaf"})
		return graph, &verdictExecutor{
			first: Outcome{Stop: StopBudget, Text: "ran out", Turns: 9, Verdict: provider.VerdictBudgetStop},
			then:  Outcome{Stop: StopDone, Text: "finished", Turns: 3, Verdict: provider.VerdictUnverifiedSuccess},
		}
	}

	graph, fake := build()
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 2).WithGovernor(calmGovernor())
	scheduler.Escalations = 1
	if err := scheduler.Run(context.Background(), graph); err != nil {
		t.Fatal(err)
	}
	if fake.attempts != 2 {
		t.Fatalf("ran the leaf %d times, want a retry after the budget stop", fake.attempts)
	}
	node := graph.Node(1)
	if node.State != plan.StateDone || node.Result != "finished" {
		t.Fatalf("node = %s %q, want the retry's result", node.State, node.Result)
	}
	if node.Verdict != provider.VerdictUnverifiedSuccess {
		t.Fatalf("verdict = %s, want the retry's", node.Verdict)
	}

	// The same graph with no panel behind it: one attempt, and the budget stop
	// is recorded exactly as before.
	plain, plainFake := build()
	plainScheduler := NewScheduler(NewRegistry(plainFake), workspace(t), 2).WithGovernor(calmGovernor())
	if err := plainScheduler.Run(context.Background(), plain); err != nil {
		t.Fatal(err)
	}
	if plainFake.attempts != 1 {
		t.Fatalf("ran the leaf %d times with no panel, want exactly one", plainFake.attempts)
	}
	if state := plain.Node(1).State; state != plan.StateDone {
		t.Fatalf("node = %s, want the unchanged single-model behaviour", state)
	}
}

// TestAProviderFailureIsNotRetriedUpTheLadder keeps escalation pointed at the
// thing it can fix. A timeout is weather; sending the work to a dearer model
// does not make the network better, it only makes the outage cost more.
func TestAProviderFailureIsNotRetriedUpTheLadder(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	graph.Add(plan.Node{Stage: 1, Title: "Leaf"})
	fake := &verdictExecutor{
		first: Outcome{Stop: StopError, Verdict: provider.VerdictProviderFailure},
		then:  Outcome{Stop: StopDone, Text: "finished", Verdict: provider.VerdictUnverifiedSuccess},
	}
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 2).WithGovernor(calmGovernor())
	scheduler.Escalations = 1
	_ = scheduler.Run(context.Background(), graph)

	if fake.attempts != 1 {
		t.Fatalf("ran the leaf %d times, want no escalation for a transport failure", fake.attempts)
	}
	if state := graph.Node(1).State; state != plan.StateFailed {
		t.Fatalf("node = %s, want failed", state)
	}
	if failure := graph.Node(1).Failure; !strings.Contains(failure, "produced no result") {
		t.Fatalf("failure = %q", failure)
	}
}
