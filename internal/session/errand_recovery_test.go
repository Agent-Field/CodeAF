package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── ONE RECOVERY MACHINE FOR ERRANDS ────────────────────────────────────────
//
// THE MEASURED FAILURE. Task 1 of conversation 57d51779f63ac603, 2026-09-10.
// A division review was asked of `z-ai/glm-5.3`, which wrote its first token in
// one second and was still writing at ninety, when it was cut. The fall-through
// rung, `deepseek/deepseek-v4-flash-0731`, wrote its first token in 4.8 seconds
// and was cut at ninety too. The parts were then admitted with nobody having
// read them, on a journal line indistinguishable from a reviewed admission,
// after three minutes and twenty seconds in which the person watching saw the
// word "sizing the work" and nothing else.
//
// The ninety seconds was arithmetic: the review's three minutes divided by the
// two rungs of the ladder and armed as a hard deadline on each call. A wall
// clock cannot tell a rung that is silent from a rung that is answering, so it
// killed both — the shape #786 fixed for a person's turn and left errands out of.

// errandScript is what one model does when an errand asks it.
type errandScript struct {
	// answer is what comes back, after `after` has passed. An empty answer with
	// no error is a model that said nothing.
	answer string
	// err is what comes back instead, and it is how a test spells the guard
	// having cut a rung: [context.DeadlineExceeded] is exactly what the call
	// returns when internal/provider's stream guard ends a silent stream.
	err   error
	after time.Duration
}

// errandLadder is a completer that answers per model and records what each call
// was handed — including, and this is the assertion the whole wave turns on, HOW
// MUCH TIME WAS LEFT ON ITS CLOCK when it arrived.
type errandLadder struct {
	mu     sync.Mutex
	script map[string][]errandScript
	calls  []errandCall
}

// errandCall is one request as the model saw it.
type errandCall struct {
	model string
	// left is what [context.Context.Deadline] said was still to come. A ladder
	// that hands its first rung an even share of the errand's patience gives it
	// half; a ladder that bounds the ERRAND gives it nearly all of it, and no
	// clock on the test machine can turn one of those readings into the other.
	left time.Duration
}

func (l *errandLadder) CompleteWithMessages(ctx context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	l.mu.Lock()
	var left time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		left = time.Until(deadline)
	}
	l.calls = append(l.calls, errandCall{model: request.Model, left: left})
	steps := l.script[request.Model]
	step := errandScript{err: errors.New("no script for " + request.Model)}
	if len(steps) > 0 {
		step = steps[0]
		if len(steps) > 1 {
			l.script[request.Model] = steps[1:]
		}
	}
	l.mu.Unlock()

	if step.after > 0 {
		select {
		case <-time.After(step.after):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if step.err != nil {
		return nil, step.err
	}
	return textResponse(step.answer), nil
}

func (l *errandLadder) seen() []errandCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]errandCall{}, l.calls...)
}

// twoRungAgent is an agent whose errands walk a two-rung ladder: a model on the
// role's tier, then the conversation's own.
func twoRungAgent(t *testing.T, client Completer) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, client, func(config *Config) {
		config.RolesSource = tierSettings(map[string]string{
			string(roles.TierKey(roles.TierLow)): "tier/one",
		})
	})
	return agent
}

// A RUNG THAT IS STILL ANSWERING IS NOT CUT BY THE LADDER'S ARITHMETIC.
//
// The first rung is handed what is left of the ERRAND, not a share of it, so an
// answer that takes longer than the old even split allowed still lands and is
// still used. Both halves are asserted: the clock the call was handed, which is
// the same on any machine, and the answer arriving after the moment the split
// would have killed it.
func TestAnErrandRungStillAnsweringIsNotCutByTheLaddersOwnShare(t *testing.T) {
	const budget = 2 * time.Second
	ladder := &errandLadder{script: map[string][]errandScript{
		"tier/one": {{answer: "the answer", after: 1200 * time.Millisecond}},
	}}
	agent := twoRungAgent(t, ladder)

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	response, model, err := agent.callRole(ctx, roles.RoleTitle, "session/model", nil)
	if err != nil {
		t.Fatalf("the errand failed: %v", err)
	}
	if model != "tier/one" || strings.TrimSpace(response.Text()) != "the answer" {
		t.Fatalf("the errand answered %q on %q, want the first rung's own answer", response.Text(), model)
	}

	calls := ladder.seen()
	if len(calls) != 1 {
		t.Fatalf("the ladder made %d calls, want the one rung that answered: %+v", len(calls), calls)
	}
	// THE FIRST RUNG WAS HANDED THE ERRAND'S CLOCK LESS THE FLOOR'S RESERVE — four
	// fifths of it ([errandReserve]) — and not the even split's half. The answer
	// above takes 1.2s, which the half would have cut and the reserve does not.
	// Anything at or under half is that arithmetic still in place.
	if calls[0].left <= budget/2 {
		t.Fatalf("the first rung was handed %s of a %s errand, want everything but the floor's reserve",
			calls[0].left, budget)
	}
}

// A RUNG THE GUARD CUT LEAVES THE NEXT ONE WHAT IS LEFT, WHICH IS NEARLY ALL OF IT.
//
// This is the half the even split was written for on 2026-08-28 — a wedged
// endpoint must not eat the budget the fall-through rung needs — and it is
// better served by the errand's own clock than by arithmetic, because the guard
// ends a silent rung in a fraction of the patience rather than at its share of
// it. The cut is spelled the way the guard spells it: the call returns the
// context's own deadline error.
func TestASilentErrandRungLeavesTheNextOneTheRestOfThePatience(t *testing.T) {
	const budget = 2 * time.Second
	ladder := &errandLadder{script: map[string][]errandScript{
		"tier/one":      {{err: context.DeadlineExceeded, after: 200 * time.Millisecond}},
		"session/model": {{answer: "the floor answered"}},
	}}
	agent := twoRungAgent(t, ladder)

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	response, model, err := agent.callRole(ctx, roles.RoleTitle, "session/model", nil)
	if err != nil {
		t.Fatalf("the errand failed: %v", err)
	}
	if model != "session/model" || strings.TrimSpace(response.Text()) != "the floor answered" {
		t.Fatalf("the errand answered %q on %q, want the fall-through rung", response.Text(), model)
	}

	calls := ladder.seen()
	if len(calls) != 2 {
		t.Fatalf("the ladder made %d calls, want one per rung: %+v", len(calls), calls)
	}
	// THE SECOND RUNG GOT THE REMAINDER AND NOT A SHARE. Under the even split it
	// was handed what was left divided by one — which looks the same here — but
	// the FIRST rung was handed half, and the fact this asserts is that neither
	// of them was.
	if calls[1].left < budget-700*time.Millisecond {
		t.Fatalf("the fall-through rung was handed %s of a %s errand, want what the first rung did not spend",
			calls[1].left, budget)
	}
}

// AND WHAT THE BOUNDARY SAYS ABOUT A FAILED RUNG IS WHAT THE LADDER DOES.
//
// The verdict used to be read and dropped. It decides one thing now, and it is
// the thing an errand can act on: a 4xx that named no upstream is the router
// reading our own bytes and saying no, which no endpoint and no model will fix —
// so the rung below is a second charge for the same refusal and is not made.
func TestAnErrandStopsWhereTheNextRungCouldOnlyBeRefusedTheSameWay(t *testing.T) {
	ladder := &errandLadder{script: map[string][]errandScript{
		"tier/one": {{err: refusalOf(400, "no endpoints found that support tool use", "", "")}},
	}}
	agent := twoRungAgent(t, ladder)

	if _, _, err := agent.callRole(context.Background(), roles.RoleTitle, "session/model", nil); err == nil {
		t.Fatal("a request no endpoint will serve came back as a success")
	}
	if calls := ladder.seen(); len(calls) != 1 {
		t.Fatalf("the ladder made %d calls, want only the rung that was refused: %+v", len(calls), calls)
	}
}

// AND EVERY OTHER FAILURE STILL FALLS THROUGH, which is the law this file's
// header states and the one the stop above must not quietly widen into. "That
// model is down" arrives as a sentence nothing can classify, and it is the exact
// case the ladder exists for.
func TestAnErrandWhoseRungIsMerelyDownStillFallsThrough(t *testing.T) {
	ladder := &errandLadder{script: map[string][]errandScript{
		"tier/one":      {{err: errors.New("that model is down")}},
		"session/model": {{answer: "the floor answered"}},
	}}
	agent := twoRungAgent(t, ladder)

	response, model, err := agent.callRole(context.Background(), roles.RoleTitle, "session/model", nil)
	if err != nil {
		t.Fatalf("the errand failed: %v", err)
	}
	if model != "session/model" || strings.TrimSpace(response.Text()) != "the floor answered" {
		t.Fatalf("the errand answered %q on %q, want the fall-through rung", response.Text(), model)
	}
}

// AND THE ERRAND'S PATIENCE IS NEVER EXCEEDED, whatever the ladder does inside
// it. Every rung fails, the errand ends, and it ends on the clock the caller set
// rather than on any arithmetic of its own.
func TestAnErrandWhoseEveryRungFailsEndsInsideTheCallersPatience(t *testing.T) {
	const budget = time.Second
	ladder := &errandLadder{script: map[string][]errandScript{
		"tier/one":      {{err: errors.New("that model is down")}},
		"session/model": {{err: errors.New("that model is down")}},
	}}
	agent := twoRungAgent(t, ladder)

	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	began := time.Now()
	if _, _, err := agent.callRole(ctx, roles.RoleTitle, "session/model", nil); err == nil {
		t.Fatal("an errand whose every rung failed answered without an error")
	}
	if took := time.Since(began); took > budget {
		t.Fatalf("the errand took %s of a %s patience", took, budget)
	}
	if len(ladder.seen()) != 2 {
		t.Fatalf("the ladder made %d calls, want one per rung: %+v", len(ladder.seen()), ladder.seen())
	}
}
