package exec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// offeredTools is a scripted completer that also remembers what schema each turn
// was offered. The belt a leaf can see is the whole of "is this capability
// present", so a test about absence has to read the request rather than the
// outcome.
type offeredTools struct {
	turns [][]ai.ToolCall
	seen  [][]string
}

func (o *offeredTools) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	var names []string
	for _, definition := range request.Tools {
		names = append(names, definition.Function.Name)
	}
	o.seen = append(o.seen, names)

	index := len(o.seen) - 1
	message := ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "half of it, written down"}}}
	if index < len(o.turns) {
		message.ToolCalls = o.turns[index]
	} else {
		message.Content = []ai.ContentPart{{Type: "text", Text: "done"}}
	}
	return &ai.Response{
		Choices: []ai.Choice{{Message: message, FinishReason: "stop"}},
		Usage:   &ai.Usage{PromptTokens: 10, CompletionTokens: 5},
	}, nil
}

func (o *offeredTools) offered(turn int, name string) bool {
	if turn >= len(o.seen) {
		return false
	}
	for _, offered := range o.seen[turn] {
		if offered == name {
			return true
		}
	}
	return false
}

const splitCall = `{"parts":[` +
	`{"title":"The Python cases","summary":"the eight python fixtures","brief":"Work through tests/py and report each failure."},` +
	`{"title":"The Go cases","summary":"the four go fixtures","brief":"Work through tests/go and report each failure."}` +
	`],"evidence":"tests/ holds two independent suites with no shared fixtures"}`

// The whole of the cooperative ending: the leaf says the assignment is several
// jobs, and the run stops there.
//
// Terminal is the contract and not a nicety. A leaf that asked to divide and
// then carried on would be producing exactly the work its own children have
// just been commissioned to produce, and the job would pay for both — so the
// second scripted turn in this test is one the loop must never reach.
func TestRequestSplitEndsTheRunAndCarriesTheDivision(t *testing.T) {
	client := &offeredTools{turns: [][]ai.ToolCall{
		{call("c0", "request_split", splitCall)},
		{call("c1", "write", `{"path":"never.md","text":"the leaf kept working"}`)},
	}}
	linear := NewLinear(client, workspace(t), nil, 50, 150_000, time.Minute).WithSwarm(true)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 7, Brief: "run every test case in tests/"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopSplit {
		t.Fatalf("stop = %s, want split — nothing else records that the leaf handed its budget back", outcome.Stop)
	}
	if outcome.Turns != 1 {
		t.Fatalf("turns = %d, want 1 — the request is terminal and the leaf kept working", outcome.Turns)
	}
	if !outcome.SplitRequest.Valid() {
		t.Fatalf("split request = %+v, want two whole parts", outcome.SplitRequest)
	}
	if got := outcome.SplitRequest.Parts[1].Brief; !strings.Contains(got, "tests/go") {
		t.Fatalf("second part's brief = %q, want the assignment the model wrote", got)
	}
	if !strings.Contains(outcome.SplitRequest.Evidence, "two independent suites") {
		t.Fatalf("evidence = %q, want what the leaf found", outcome.SplitRequest.Evidence)
	}
	// The partial is what the parts consume, so it has to survive the ending.
	if !strings.Contains(outcome.Text, "half of it") {
		t.Fatalf("text = %q, want the partial the leaf was holding", outcome.Text)
	}
	// Nothing ran out. Reading this as an overrun would spend a second round on
	// a decision the split has already made, and grading it as a budget stop
	// would teach the ruler that this worker could not do the work — from the
	// one run where it read the work correctly.
	if outcome.Overran() {
		t.Fatal("a cooperative split reads as an overrun; both growth paths would fire on one settlement")
	}
	if outcome.Verdict != provider.VerdictUnverifiedSuccess {
		t.Fatalf("verdict = %s, want an unverified success — the leaf declined to converge on the wrong thing", outcome.Verdict)
	}
	if outcome.Verdict.Escalates() {
		t.Fatal("a leaf that divided cooperatively grades as a failure; it will be retried on a stronger model for having been right")
	}
}

// Off, the verb is absent rather than present and refused — this codebase's
// rule for a capability with nothing behind it, and the whole of the swarm
// gate's inertness at the leaf.
//
// A model that cannot see the tool cannot call it, so absence is the guarantee;
// checking that the outcome is unchanged would only prove that this particular
// script did not try.
func TestTheDivisionVerbIsOffTheBeltWithoutSwarm(t *testing.T) {
	for _, swarm := range []bool{false, true} {
		client := &offeredTools{}
		linear := NewLinear(client, workspace(t), nil, 50, 150_000, time.Minute).WithSwarm(swarm)
		if _, err := linear.Run(context.Background(), Task{NodeID: 8, Brief: "work"}); err != nil {
			t.Fatal(err)
		}
		if got := client.offered(0, "request_split"); got != swarm {
			t.Fatalf("swarm=%t: request_split on the belt = %t", swarm, got)
		}
	}
}

// A reflex already has the verb for "this is bigger than it looked": promote.
// Two answers to one question is one answer too many, and the first thing to go
// wrong with the second is that it disagrees.
func TestAReflexIsNotOfferedTheDivisionVerb(t *testing.T) {
	client := &offeredTools{}
	linear := NewLinear(client, workspace(t), nil, 50, 150_000, time.Minute).WithSwarm(true)
	if _, err := linear.Run(context.Background(), Task{NodeID: 9, Brief: "rename the file", Reflex: true}); err != nil {
		t.Fatal(err)
	}
	if client.offered(0, "request_split") {
		t.Fatal("a reflex was handed both promote and request_split")
	}
	if !client.offered(0, "promote") {
		t.Fatal("a reflex lost promote")
	}
}

// A one-part division is the split that only restates. plan.WorthKeeping would
// refuse it a round later at the price of a planning call, so it is refused
// here for free — and a part with no brief is a title with nothing behind it,
// which is how a node comes to be executed against its own name.
func TestOnlyAWholeDivisionIsWorthActingOn(t *testing.T) {
	whole := &SplitRequest{Parts: []SplitPart{
		{Title: "One", Summary: "a", Brief: "do a"},
		{Title: "Two", Summary: "b", Brief: "do b"},
	}}
	cases := []struct {
		name    string
		request *SplitRequest
		valid   bool
	}{
		{"nil — every leaf that never asked", nil, false},
		{"no parts", &SplitRequest{}, false},
		{"one part", &SplitRequest{Parts: whole.Parts[:1]}, false},
		{"a part with no brief", &SplitRequest{Parts: []SplitPart{
			whole.Parts[0], {Title: "Two", Summary: "b"},
		}}, false},
		{"a part with no title", &SplitRequest{Parts: []SplitPart{
			whole.Parts[0], {Summary: "b", Brief: "do b"},
		}}, false},
		{"two whole parts", whole, true},
	}
	for _, test := range cases {
		if got := test.request.Valid(); got != test.valid {
			t.Errorf("%s: Valid() = %t, want %t", test.name, got, test.valid)
		}
	}
}

// The ownable-subject test lives in the tool description because that is the
// one place a model reads before deciding, and every looser phrasing of it was
// agreed with and then ignored: a model asked to divide will divide, and the
// cheapest division is the procedure it was about to follow, relabelled.
//
// So the two load-bearing clauses are pinned. The knowing-nothing clause is
// what a relabelled procedure cannot survive; the terminal sentence is what
// stops the leaf paying for its children's work as well as its own.
func TestTheDivisionVerbStatesItsTestAndItsFinality(t *testing.T) {
	description := requestSplitDefinition().Function.Description
	for _, clause := range []string{
		"knowing nothing of what the other parts produced",
		"stage of one procedure",
		"Two parts minimum",
		"ENDS your run immediately",
	} {
		if !strings.Contains(description, clause) {
			t.Errorf("the request_split description lost %q:\n%s", clause, description)
		}
	}
}

// Arguments that will not parse still end the run. The model has said the
// assignment is several jobs and that finding is the expensive half; what
// arrives malformed is refused by Valid one layer up, which delivers the
// partial — a far better ending than handing the loop back to a worker that has
// just announced it is stopping.
func TestAMalformedDivisionStillEndsTheRun(t *testing.T) {
	client := &offeredTools{turns: [][]ai.ToolCall{
		{call("c0", "request_split", `{"parts": not json`)},
		{call("c1", "write", `{"path":"never.md","text":"the leaf kept working"}`)},
	}}
	linear := NewLinear(client, workspace(t), nil, 50, 150_000, time.Minute).WithSwarm(true)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 10, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopSplit || outcome.Turns != 1 {
		t.Fatalf("stop = %s in %d turns, want split on turn 1", outcome.Stop, outcome.Turns)
	}
	if outcome.SplitRequest.Valid() {
		t.Fatal("a request the loop could not read was passed to the growth path as though it were whole")
	}
}
