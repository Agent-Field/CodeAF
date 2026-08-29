package revision

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/shaped"
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

// cutVerdict is a judgement that ran out of room mid-object: the expensive half
// of the answer is in hand and the closing brace is not.
func cutVerdict(text string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
		FinishReason: "length"}}, Usage: &ai.Usage{CompletionTokens: 8192}}
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
		spentThinking(shaped.Room(shaped.Ask{Lane: "gate"}, "")),
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
// it. The figure itself is the shared seam's — one place for every structured
// call in the harness — and this is the gate asserting that it still gets what
// it measured here.
func TestAVerdictsCapIsAFractionOfTheReserveAndNeverTheOldLiterals(t *testing.T) {
	verdictRoom := func() int { return shaped.Room(shaped.Ask{Lane: "gate"}, "") }
	if got := verdictRoom(); got <= 400 {
		t.Fatalf("verdict cap = %d, which is still a cap a reasoning pass spends thinking", got)
	}
	if got, want := verdictRoom(), ctxbudget.CompletionReserve()/8; got != want {
		t.Fatalf("verdict cap = %d, want the reserve's share %d", got, want)
	}
	// A reserve set small enough that the share falls under the floor gets the
	// floor, and a reserve smaller than the floor is still the operator's word.
	t.Setenv("AFORGE_COMPLETION_RESERVE", "8000")
	if got := verdictRoom(); got != 4096 {
		t.Fatalf("verdict cap = %d under a small reserve, want the floor", got)
	}
	t.Setenv("AFORGE_COMPLETION_RESERVE", "1000")
	if got := verdictRoom(); got != 1000 {
		t.Fatalf("verdict cap = %d, want the stated reserve of 1000", got)
	}
}

// BOTH ATTEMPTS UNREADABLE IS A FAULT ON THE GATE, AND THE RUN SAYS SO.
//
// This is the behaviour this wave changed, and it is the s4 sweep's second fatal
// shape. The old path shipped the work and passed it — the gate could not
// answer, so the caller took that for "no opinion" and the run exited 0 as
// though the check had been made and had held. A check that did not happen is
// not a check that passed. The judgement now carries a Fault, which nothing
// downstream can mistake for a verdict, and the delivery path turns it into a
// partial run with the reason on the stream.
func TestTwoUnreadableVerdictsFaultTheGateRatherThanPassingTheWork(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	judge := &scriptedJudge{replies: []*ai.Response{spentThinking(shaped.Room(shaped.Ask{Lane: "gate"}, ""))}}
	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")

	if len(judge.caps) != 2 {
		t.Fatalf("the gate made %d calls, want the attempt and one re-ask", len(judge.caps))
	}
	if judgment.Pass || judgment.Checked {
		t.Fatalf("a gate that never answered still passed the work: %+v", judgment)
	}
	if judgment.Fault == "" {
		t.Fatalf("a gate that never answered left no fault behind: %+v", judgment)
	}
	if judgment.Unjudged != "" {
		t.Fatalf("a fault was recorded as the old fail-open pass: %+v", judgment)
	}
	// A judge that fails the work and cannot name the gap is a different thing —
	// it answered, and what it said was empty — and that one is unchanged.
	nameless := &scriptedJudge{replies: []*ai.Response{said(`{"pass":false}`)}}
	got := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, nameless.Model(), nameless), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")
	if !got.Pass || got.Checked || got.Unjudged == "" {
		t.Fatalf("a fail that named nothing passed silently: %+v", got)
	}
	// A reply that cost nothing is asked again too. It used to be exempt on the
	// argument that more room would not change it — which was true of the
	// BUDGET and is not the question the re-ask asks: it restates the format
	// contract, and FAILSAFE's floor says a run is never failed before any work
	// started while a retry is still possible.
	silent := &scriptedJudge{replies: []*ai.Response{{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant"}, FinishReason: "stop"}}}}}
	JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, silent.Model(), silent), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")
	if len(silent.caps) != 2 {
		t.Fatalf("a silent reply bought %d calls, want the attempt and one re-ask", len(silent.caps))
	}
}

// A verdict cut off at the ceiling is CONTINUED, and the verdict it completes to
// is the verdict. Nothing about this used to work: the object never closed, the
// decode failed, and the delivery was passed unjudged.
func TestACutVerdictIsContinuedAndItsAnswerStands(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	judge := &scriptedJudge{replies: []*ai.Response{
		cutVerdict(`{"pass":false,"gaps":"no numbers appear anywhere","quote":"include the benchm`),
		said(`ark numbers"}`),
	}}
	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")
	if !judgment.Checked || judgment.Pass {
		t.Fatalf("the continued verdict was not honoured: %+v", judgment)
	}
	if judgment.Quote != "include the benchmark numbers" {
		t.Fatalf("the two halves were not joined: %+v", judgment)
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
