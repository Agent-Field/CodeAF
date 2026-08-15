package revision

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// scriptedJudge is one judge with its answers written down, and a record of the
// completion cap each call actually went out with — which is the whole of what
// these tests are about.
type scriptedJudge struct {
	replies []*ai.Response
	caps    []int
}

func (s *scriptedJudge) CompleteWithMessages(_ context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	request := &ai.Request{}
	for _, option := range options {
		if err := option(request); err != nil {
			return nil, err
		}
	}
	limit := 0
	if request.MaxTokens != nil {
		limit = *request.MaxTokens
	}
	s.caps = append(s.caps, limit)
	index := len(s.caps) - 1
	if index >= len(s.replies) {
		index = len(s.replies) - 1
	}
	return s.replies[index], nil
}

func (s *scriptedJudge) Model() string { return "judge/model" }

// spentThinking is the reply a reasoning model gives when the completion cap is
// sized for the object rather than for the deliberation in front of it: nothing
// at all, finish_reason=length, and a bill for every token of the cap.
func spentThinking(limit int) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant"}, FinishReason: "length"}},
		Usage:   &ai.Usage{CompletionTokens: limit},
	}
}

func said(text string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
	}}, Usage: &ai.Usage{CompletionTokens: 40}}
}

func gateNodeFixture() store.Node {
	return store.Node{ID: "job", Brief: "produce it",
		Provenance: store.Provenance{Intent: "compare the two parsers and include the benchmark numbers"}}
}

// The failure this wave exists for, in one test: a judge that spends its whole
// cap thinking and hands back nothing is asked once more with room to finish,
// and the verdict it then gives is the verdict — not the silent pass an empty
// first reply used to be.
func TestAnEmptyFirstVerdictIsAskedAgainWithRoomAndItsAnswerStands(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	judge := &scriptedJudge{replies: []*ai.Response{
		spentThinking(verdictTokens()),
		said(`{"pass":false,"gaps":"no numbers appear anywhere","quote":"include the benchmark numbers"}`),
	}}
	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")

	if len(judge.caps) != 2 {
		t.Fatalf("the gate made %d calls, want the first and one retry", len(judge.caps))
	}
	if !judgment.Checked || judgment.Pass || judgment.Gaps != "no numbers appear anywhere" {
		t.Fatalf("the retry's verdict was not honoured: %+v", judgment)
	}
	if judgment.Unjudged != "" {
		t.Fatalf("a judged verdict was recorded as unjudged: %q", judgment.Unjudged)
	}
	if judgment.Quote != "include the benchmark numbers" {
		t.Fatalf("the retry's citation was lost: %+v", judgment)
	}
	if judge.caps[1] <= judge.caps[0] {
		t.Fatalf("the retry did not get more room: %v", judge.caps)
	}
	if ceiling := ctxbudget.CompletionReserve(); judge.caps[1] > ceiling {
		t.Fatalf("the retry demanded %d tokens, past the reserve of %d", judge.caps[1], ceiling)
	}
}

// A cap sized for the object and not for the reasoning is what made an empty
// reply possible in the first place. 200 and 400 were the two literals; neither
// is reachable now, and the number moves with the reserve rather than against
// it.
func TestAVerdictsCapIsAFractionOfTheReserveAndNeverTheOldLiterals(t *testing.T) {
	if got := verdictTokens(); got <= 400 {
		t.Fatalf("verdict cap = %d, which is still a cap a reasoning pass spends thinking", got)
	}
	if got, want := verdictTokens(), ctxbudget.CompletionReserve()/verdictShare; got != want {
		t.Fatalf("verdict cap = %d, want the reserve's share %d", got, want)
	}
	// A reserve set small enough that the share falls under the floor gets the
	// floor, and a reserve smaller than the floor is still the operator's word.
	t.Setenv("AFORGE_COMPLETION_RESERVE", "8000")
	if got := verdictTokens(); got != verdictFloorTokens {
		t.Fatalf("verdict cap = %d under a small reserve, want the floor %d", got, verdictFloorTokens)
	}
	t.Setenv("AFORGE_COMPLETION_RESERVE", "1000")
	if got := verdictTokens(); got != 1000 {
		t.Fatalf("verdict cap = %d, want the stated reserve of 1000", got)
	}
	if got := retryVerdictTokens(spentThinking(1000)); got != 1000 {
		t.Fatalf("the retry cap = %d, past a reserve somebody named on purpose", got)
	}
}

// Both attempts empty is the model's problem and not the budget's. Today's
// behaviour is kept — the deliverable ships rather than being held hostage by a
// gate that cannot answer — and the whole change is that the pass now says of
// itself that nothing judged it.
func TestTwoEmptyVerdictsStillShipButNoLongerPassSilently(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	judge := &scriptedJudge{replies: []*ai.Response{spentThinking(verdictTokens())}}
	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")

	if len(judge.caps) != 2 {
		t.Fatalf("the gate made %d calls, want one retry and no more", len(judge.caps))
	}
	if !judgment.Pass || judgment.Checked {
		t.Fatalf("the fail-open pass changed shape: %+v", judgment)
	}
	if judgment.Unjudged == "" {
		t.Fatal("a gate that never judged passed the work silently")
	}
	// A judge that fails the work and cannot name the gap is the same silence
	// wearing a verdict, and it is recorded the same way.
	nameless := &scriptedJudge{replies: []*ai.Response{said(`{"pass":false}`)}}
	got := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, nameless.Model(), nameless), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")
	if !got.Pass || got.Checked || got.Unjudged == "" {
		t.Fatalf("a fail that named nothing passed silently: %+v", got)
	}
	// And a reply that cost nothing is not retried: there is no evidence more
	// room would change it, so the second call is never bought.
	silent := &scriptedJudge{replies: []*ai.Response{{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant"}, FinishReason: "stop"}}}}}
	JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, silent.Model(), silent), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")
	if len(silent.caps) != 1 {
		t.Fatalf("a costless empty reply bought %d calls, want one", len(silent.caps))
	}
}

// The three bounds the gate clips with are shares of the window now, and every
// one of them answers with its old literal when there is no window to share.
func TestTheGatesBoundsSpendTheWindowAndFallBackToTheirLiterals(t *testing.T) {
	unknown := ctxbudget.Budget{}
	known := ctxbudget.For(200_000)

	if got := unknown.Share(gateNotebookShare, gateShareTotal, GateNotebookBytes); got != GateNotebookBytes {
		t.Fatalf("the notebook bound without a window = %d, want the old %d", got, GateNotebookBytes)
	}
	// Eight distilled lessons of up to 512 bytes is what the digest is asked
	// for, and a kilobyte is where items six through eight went missing.
	if got := known.Share(gateNotebookShare, gateShareTotal, GateNotebookBytes); got < 8*512 {
		t.Fatalf("the notebook bound with a window = %d, still short of the 8 lessons above it", got)
	}

	if got := gateEvidenceLines(unknown); got != gateEvidenceRan {
		t.Fatalf("the run tail without a window = %d lines, want the old %d", got, gateEvidenceRan)
	}
	if got := gateEvidenceLines(known); got <= gateEvidenceRan {
		t.Fatalf("the run tail did not grow with the window: %d lines", got)
	}

	long := strings.Repeat("x", 1<<20)
	if got := len(boundedDelivery(long, unknown)); got != deliveryPartialBytes {
		t.Fatalf("the partial without a window = %d bytes, want the old %d", got, deliveryPartialBytes)
	}
	if got := len(boundedDelivery(long, known)); got <= deliveryPartialBytes {
		t.Fatalf("the partial did not grow with the window: %d bytes", got)
	}
}

// And the bound reaches the prompt: what the run did travels as far as the
// window pays for, rather than stopping at a number written before the window
// was known.
func TestMoreOfTheRunTailTravelsWhenTheWindowPaysForIt(t *testing.T) {
	ran := make([]string, 32)
	for index := range ran {
		ran[index] = "sh {\"command\":\"step-" + string(rune('a'+index%26)) + "\"}"
	}
	evidence := Evidence{Ran: ran, Observed: true}
	bare := evidence.block(ctxbudget.Budget{})
	if got := strings.Count(bare, "sh {"); got != gateEvidenceRan {
		t.Fatalf("without a window %d lines travelled, want %d", got, gateEvidenceRan)
	}
	wide := evidence.block(ctxbudget.For(200_000))
	if strings.Count(wide, "sh {") <= gateEvidenceRan {
		t.Fatalf("the window bought no more of the tail:\n%s", wide)
	}
	if !strings.Contains(wide, "The last 32 things the work ran, oldest first:") {
		t.Fatalf("the whole tail did not travel under a 200k window:\n%s", wide)
	}
}
