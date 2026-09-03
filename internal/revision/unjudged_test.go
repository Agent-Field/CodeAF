package revision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// unreachableJudge is the gate's model on the day the route has no endpoint
// left: every call comes back as the same error and nothing is ever decoded.
//
// It counts its calls because the count IS the law under test. A judge that
// answers can be asserted on its answer; a judge that never answers can only be
// asserted on how many times it was asked.
type unreachableJudge struct {
	err   error
	calls int
}

func (u *unreachableJudge) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	u.calls++
	return nil, u.err
}

func (u *unreachableJudge) Model() string { return "judge/model" }

// judgeTheUnreachable puts one deliverable to a gate that cannot be reached and
// hands back the judgement and the judge, so each case below reads as the three
// facts it is about: how many times the gate was asked, what the note says, and
// that the work still ships.
func judgeTheUnreachable(ctx context.Context, err error) (Judgment, *unreachableJudge) {
	settings := config.Config{Model: "worker/model"}
	judge := &unreachableJudge{err: err}
	return JudgeDeliverable(ctx, settings, pool.Adopt(settings, judge.Model(), judge),
		nil, gateNodeFixture(), "parser A wins", "", Evidence{}, "worker/model"), judge
}

// ONE MORE ASK BEFORE A DELIVERY GOES OUT WITH NOTHING HAVING READ IT.
//
// reef-145 delivered unjudged twice on a route that answered 404 in 58 ms — fast
// enough that the run had minutes of wall left and spent none of them asking a
// second time (#514). The three cases below are the whole of the rule: ask again
// when there is time, do not when there is not, and do not when the refusal was
// about the request and every endpoint would say the same. The note says which
// happened, because "asked twice and still nothing" and "there was no time to
// ask" are different facts about a run and only one of them is anybody's to fix.
func TestAGateThatCouldNotBeReachedIsAskedOnceMoreInsideTheWall(t *testing.T) {
	weather := errors.New("dial tcp 10.0.0.1:443: connect: connection refused")

	// A wall with room in it: asked twice, and the note says so.
	judgment, judge := judgeTheUnreachable(context.Background(), weather)
	if judge.calls != 2 {
		t.Fatalf("the gate was asked %d times over a wall with room in it, want the call and one more", judge.calls)
	}
	if !strings.Contains(judgment.Unjudged, GateUnreached) ||
		!strings.Contains(judgment.Unjudged, "asked twice") {
		t.Fatalf("the note does not say the gate was asked twice: %q", judgment.Unjudged)
	}
	if !strings.Contains(judgment.Unjudged, "connection refused") {
		t.Fatalf("the note dropped the provider's own sentence: %q", judgment.Unjudged)
	}
	// FAIL-OPEN STANDS. The work ships and nothing manufactures a verdict for it.
	if !judgment.Pass || judgment.Checked || judgment.Fault != "" {
		t.Fatalf("an unreachable gate stopped being the fail-open pass it is: %+v", judgment)
	}

	// A wall with no room for a call: asked once, and the note says why.
	short, cancel := context.WithTimeout(context.Background(), gateSecondAskFloor/2)
	defer cancel()
	judgment, judge = judgeTheUnreachable(short, weather)
	if judge.calls != 1 {
		t.Fatalf("the gate was asked %d times with no wall left to ask inside, want one", judge.calls)
	}
	if !strings.Contains(judgment.Unjudged, gateNoTimeToAsk) {
		t.Fatalf("the note does not say the wall left no room: %q", judgment.Unjudged)
	}
	if strings.Contains(judgment.Unjudged, gateAskedTwice) {
		t.Fatalf("a gate asked once claimed it was asked twice: %q", judgment.Unjudged)
	}

	// A REFUSAL ABOUT THE REQUEST IS NOT WEATHER. Every endpoint reads the same
	// bytes, so asking again spends the deadline to be told the same thing.
	judgment, judge = judgeTheUnreachable(context.Background(),
		&provider.APIError{Status: 400, Message: "context length exceeded"})
	if judge.calls != 1 {
		t.Fatalf("a refusal about the request bought %d calls, want one", judge.calls)
	}
	if !strings.Contains(judgment.Unjudged, gateAskRefused) {
		t.Fatalf("the note does not say why asking again would not help: %q", judgment.Unjudged)
	}

	// And an upstream's own 400 is weather like any other: the router named
	// somebody else, and another endpoint may well serve it.
	_, judge = judgeTheUnreachable(context.Background(),
		&provider.APIError{Status: 400, Provider: "some-upstream", Raw: "busy"})
	if judge.calls != 2 {
		t.Fatalf("an upstream's refusal bought %d calls, want the call and one more", judge.calls)
	}
}

// The gate that ANSWERED and had nothing to say is untouched by any of this: it
// was reached, so there is nothing to ask again, and its note keeps its own
// words.
func TestAGateThatFailedTheWorkAndNamedNoGapIsStillTheOldFailOpenPass(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	nameless := &scriptedJudge{replies: []*ai.Response{said(`{"pass":false}`)}}
	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, nameless.Model(), nameless), nil, gateNodeFixture(),
		"parser A wins", "", Evidence{}, "worker/model")
	if !judgment.Pass || judgment.Checked {
		t.Fatalf("a fail that named nothing stopped being the fail-open pass: %+v", judgment)
	}
	if judgment.Unjudged != "the gate failed the work and named no gap" {
		t.Fatalf("the note was reworded: %q", judgment.Unjudged)
	}
	if len(nameless.caps) != 1 {
		t.Fatalf("a gate that answered was asked %d times, want one", len(nameless.caps))
	}
}
