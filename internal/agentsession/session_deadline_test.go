package agentsession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestSessionDurationIncludesTheCompletedAnswer(t *testing.T) {
	api := (&fakeAPI{}).then(answer("done", 0))
	result, err := RunSession(t.Context(), api.serve(t), SessionOrder{
		Root: repo(t), Prompt: "answer", MaxTurns: 6, Policy: testPolicy,
	})
	if err != nil || result.Text != "done" {
		t.Fatalf("answer = %+v, error = %v", result, err)
	}
	if result.Duration <= 0 {
		t.Fatal("the completed session has no elapsed time")
	}
}

// A manual clock expires contexts at their recorded deadlines. Advancing an
// in-flight read reproduces the provider cancellation without real sleeps.
type sessionTestClock struct {
	now      time.Time
	contexts []*sessionTestContext
}

type sessionTestContext struct {
	context.Context
	parent   context.Context
	deadline time.Time
	clock    *sessionTestClock
	cancel   context.CancelFunc
}

func (c *sessionTestContext) Deadline() (time.Time, bool) { return c.deadline, true }
func (c *sessionTestContext) Err() error {
	if err := c.parent.Err(); err != nil {
		return err
	}
	if !c.clock.now.Before(c.deadline) {
		return context.DeadlineExceeded
	}
	return c.Context.Err()
}

func (c *sessionTestClock) runtime() sessionClock {
	return sessionClock{now: func() time.Time { return c.now }, withDeadline: func(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
		if bound, ok := parent.Deadline(); ok && bound.Before(deadline) {
			deadline = bound
		}
		ctx, cancel := context.WithCancel(parent)
		out := &sessionTestContext{Context: ctx, parent: parent, deadline: deadline, clock: c, cancel: cancel}
		c.contexts = append(c.contexts, out)
		return out, cancel
	}}
}

func (c *sessionTestClock) advance(to time.Time) {
	c.now = to
	for _, ctx := range c.contexts {
		if !to.Before(ctx.deadline) {
			ctx.cancel()
		}
	}
}

func TestSessionReadingCutoffLeavesTheAnswerInsideTheOriginalWall(t *testing.T) {
	for _, reserve := range []time.Duration{0, 22500 * time.Millisecond} {
		t.Run(reserve.String(), func(t *testing.T) {
			start := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
			clock := &sessionTestClock{now: start}
			calls := 0
			complete := func(ctx context.Context, request Request) (*ai.Response, error) {
				calls++
				deadline, _ := ctx.Deadline()
				if calls == 1 {
					clock.advance(start.Add(10 * time.Second))
					_, response := callTool("read_file", `{"path":"README.md"}`)(wireRequest{})
					out := response.(ai.Response)
					return &out, nil
				}
				if calls == 2 {
					end := start.Add(28 * time.Second)
					if deadline.Before(end) {
						clock.advance(deadline)
						return nil, ctx.Err()
					}
					clock.advance(end)
					_, response := callTool("list_dir", `{"path":"."}`)(wireRequest{})
					out := response.(ai.Response)
					return &out, nil
				}
				if reserve > 0 && (len(request.Tools) != 0 || request.Messages[len(request.Messages)-1].Content[0].Text != testPolicy.AnswerNow) {
					t.Fatal("the reading cutoff did not force an answer without tools")
				}
				if deadline != start.Add(45*time.Second) {
					t.Fatalf("the answer's hard deadline changed: %v", deadline.Sub(start))
				}
				end := clock.now.Add(18 * time.Second)
				if !end.Before(deadline) {
					clock.advance(deadline)
					return nil, ctx.Err()
				}
				clock.advance(end)
				_, response := answer(`{"file":"README.md","line":1}`, 0)(wireRequest{})
				out := response.(ai.Response)
				return &out, nil
			}
			result, err := runSession(t.Context(), complete, SessionOrder{Root: repo(t), Prompt: "read then answer",
				MaxTurns: 6, Wall: 45 * time.Second, AnswerReserve: reserve, Policy: testPolicy, Schema: findingSchema}, clock.runtime())
			if reserve == 0 {
				if !errors.Is(err, context.DeadlineExceeded) || result.Failed == "" || result.Duration != 45*time.Second {
					t.Fatalf("the unreserved session should hit its wall: %+v, %v", result, err)
				}
			} else if err != nil || result.Failed != "" || len(result.JSON) == 0 || result.Tools != 1 || result.Duration != 40500*time.Millisecond {
				t.Fatalf("the reserved session should answer from its completed read: %+v, %v", result, err)
			}
		})
	}
}

func TestSessionAnswerReserveNeverHidesAStopOrExtendsADeadline(t *testing.T) {
	for _, kind := range []string{"caller stop", "provider refusal", "provider timeout", "answer timeout"} {
		t.Run(kind, func(t *testing.T) {
			start := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
			clock := &sessionTestClock{now: start}
			parent, stop := context.WithCancel(t.Context())
			defer stop()
			parent, cancel := clock.runtime().withDeadline(parent, start.Add(20*time.Second))
			defer cancel()
			calls := 0
			failure := errors.New("provider timeout")
			if kind == "provider refusal" {
				failure = &RefusedError{Code: 403, Message: "refused"}
			}
			complete := func(ctx context.Context, request Request) (*ai.Response, error) {
				calls++
				deadline, _ := ctx.Deadline()
				if calls == 1 {
					if deadline != start.Add(10*time.Second) {
						t.Fatal("reserve did not honor half the shorter caller deadline")
					}
					if kind == "caller stop" {
						stop()
						return nil, ctx.Err()
					}
					if kind != "answer timeout" {
						return nil, failure
					}
					clock.advance(deadline)
					return nil, ctx.Err()
				}
				if deadline != start.Add(20*time.Second) || len(request.Tools) != 0 {
					t.Fatal("answer extended the caller deadline or kept reading")
				}
				clock.advance(deadline)
				return nil, ctx.Err()
			}
			result, err := runSession(parent, complete, SessionOrder{Root: repo(t), MaxTurns: 6,
				Wall: 45 * time.Second, AnswerReserve: 30 * time.Second, Policy: testPolicy}, clock.runtime())
			wantCalls := 1
			if kind == "answer timeout" {
				wantCalls = 2
			}
			if err == nil || result.Failed == "" || calls != wantCalls {
				t.Fatalf("failure was hidden: %+v, %v, %d calls", result, err, calls)
			}
		})
	}
}

func TestSessionAtTheTimeCutoffStillChecksItsSchemaAndBoundsCorrections(t *testing.T) {
	start := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &sessionTestClock{now: start}
	calls := 0
	complete := func(ctx context.Context, request Request) (*ai.Response, error) {
		calls++
		if calls == 1 {
			deadline, _ := ctx.Deadline()
			clock.advance(deadline)
			return nil, ctx.Err()
		}
		if len(request.Tools) != 0 {
			t.Fatal("an answer correction resumed reading")
		}
		clock.advance(clock.now.Add(time.Second))
		_, response := answer(`{"file":"README.md"}`, 0)(wireRequest{})
		out := response.(ai.Response)
		return &out, nil
	}
	result, err := runSession(t.Context(), complete, SessionOrder{Root: repo(t), MaxTurns: 6, Wall: 45 * time.Second,
		AnswerReserve: 22500 * time.Millisecond, Policy: testPolicy, Schema: findingSchema}, clock.runtime())
	if err != nil || result.Failed == "" || len(result.JSON) != 0 || calls != 2+testPolicy.FollowUps {
		t.Fatalf("invalid answers were accepted or corrections were unbounded: %+v, %v, %d calls", result, err, calls)
	}
}
