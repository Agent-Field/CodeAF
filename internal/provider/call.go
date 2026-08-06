package provider

import (
	"context"
	"sync"
)

// CallClass names the kind of work a call is doing.
//
// It is carried the same way the effort and cache-key knobs are, and for the
// same reason: only the call site knows what the call is for, and only the
// adapter can act on it. The classes are the harness's own passes rather than
// anything generic, because that is the grain ability actually varies at — the
// router lab found the panel's ordering on structured planning different from
// its ordering on reasoning, and averaging them would have hidden both.
type CallClass string

const (
	ClassPlanSpine       CallClass = "plan.spine"
	ClassPlanGround      CallClass = "plan.ground"
	ClassPlanExpand      CallClass = "plan.expand"
	ClassPlanFanOut      CallClass = "plan.fanout"
	ClassPlanBind        CallClass = "plan.bind"
	ClassPlanSize        CallClass = "plan.size"
	ClassPlanAudit       CallClass = "plan.audit"
	ClassPlanContract    CallClass = "plan.contract"
	ClassPlanBrief       CallClass = "plan.brief"
	ClassPlanRevise      CallClass = "plan.revise"
	ClassPlanEnsemble    CallClass = "plan.ensemble"
	ClassPlanRecalibrate CallClass = "plan.recalibrate"
	ClassExecLeaf        CallClass = "exec.leaf"
)

// Call is one unit of routable work: a single planning call, or a whole exec
// leaf together with every turn of its tool loop.
//
// It is a mutable slot rather than a plain context value because the two things
// a router needs from a call site are both writes, and both have to outlive the
// function that opened them. The router writes down which model it picked, so
// that the second turn of a leaf goes where the first one went — a leaf that
// wandered between models would rewrite its prefix cache every turn and mix two
// lineages into one transcript. And the call site writes down how the work
// turned out, which it can only know after it has parsed and checked the answer,
// long after the call itself returned.
//
// A call made without a slot is not an error and not a special case: the slot is
// absent on every path that has no router, and every method here is a no-op then.
type Call struct {
	class   CallClass
	shape   string
	attempt int

	mutex    sync.Mutex
	model    string
	observer func(Verdict)
	reported bool
}

type callContextKey struct{}
type callClassContextKey struct{}

// WithCall opens a slot for one unit of work and stamps what kind of call it is.
// The class named here loses to an override set further out — see WithCallClass.
func WithCall(ctx context.Context, class CallClass) context.Context {
	return WithCallAttempt(ctx, class, 0)
}

// WithCallAttempt opens a slot for a retry. The attempt number is how a caller
// asks for escalation without knowing anything about the panel: attempt 1 means
// "whatever you chose last time was not good enough", and it is the router's job
// to decide what that costs.
func WithCallAttempt(ctx context.Context, class CallClass, attempt int) context.Context {
	return WithCallShape(ctx, class, attempt, "")
}

// WithCallShape opens a slot and names the sub-population this call belongs to
// within its class.
//
// A class is the grain ability varies at across *kinds of work*; a shape is the
// grain it varies at within one kind. It exists for exactly one class today.
// `exec.leaf` covers every leaf the executor runs, and arm B measured what that
// costs: five leaves that exhausted their budget on one oversized task moved the
// single global leaf rating far enough to reroute the leaves of every other
// task, including two where the demoted model had never once failed. A rating is
// only transferable between calls drawn from the same population, and leaves are
// not one population.
//
// The shape is the call site's to name because only it knows: the router sees a
// conversation, the scheduler sees the node the conversation is for. Empty means
// the class is not divided, which is every planning call — those are already one
// request against one schema, which is as narrow as a population gets.
func WithCallShape(ctx context.Context, class CallClass, attempt int, shape string) context.Context {
	if override, ok := ctx.Value(callClassContextKey{}).(CallClass); ok {
		class = override
	}
	if attempt < 0 {
		attempt = 0
	}
	return context.WithValue(ctx, callContextKey{}, &Call{class: class, shape: shape, attempt: attempt})
}

// WithCallClass overrides the class every call opened under ctx belongs to.
//
// It exists for the one case where the same code means two different things: a
// fan-out at the top of a plan is drawing the whole graph from the goal, while
// the same function inside an expansion is splitting one oversized node against
// a far narrower premise. Those are different populations, and a ledger that
// pooled them would learn the average of two things it could have known
// separately.
func WithCallClass(ctx context.Context, class CallClass) context.Context {
	return context.WithValue(ctx, callClassContextKey{}, class)
}

// CallFrom returns the slot for this unit of work, nil when nothing opened one.
func CallFrom(ctx context.Context) *Call {
	call, _ := ctx.Value(callContextKey{}).(*Call)
	return call
}

// CallClassFrom returns the class of the call in flight, empty when unstamped.
func CallClassFrom(ctx context.Context) CallClass {
	if call := CallFrom(ctx); call != nil {
		return call.class
	}
	return ""
}

// Report records how a unit of work turned out. It is the call site's half of
// the contract, and it is idempotent: the first verdict wins, so an error path
// that reports and then falls through to a shared return cannot overwrite what
// it already said. A call nobody reports on stays unverified, which is the
// honest answer rather than a missing one.
func Report(ctx context.Context, verdict Verdict) {
	call := CallFrom(ctx)
	if call == nil {
		return
	}
	call.mutex.Lock()
	observer, fire := call.observer, !call.reported
	call.reported = true
	call.mutex.Unlock()
	if fire && observer != nil {
		observer(verdict)
	}
}

// Class reports what kind of call this is.
func (c *Call) Class() CallClass {
	if c == nil {
		return ""
	}
	return c.class
}

// Shape reports the sub-population within the class, empty when the class is
// not divided.
func (c *Call) Shape() string {
	if c == nil {
		return ""
	}
	return c.shape
}

// Attempt reports how many times this unit of work has already been given up on.
func (c *Call) Attempt() int {
	if c == nil {
		return 0
	}
	return c.attempt
}

// Pin fixes the model this unit of work runs on and returns whatever is now
// fixed. The first caller wins, so every later turn of a leaf's loop is handed
// back the model the first turn chose rather than choosing again.
func (c *Call) Pin(model string) string {
	if c == nil {
		return model
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.model == "" {
		c.model = model
	}
	return c.model
}

// Model reports the model pinned to this unit of work. It is empty when no
// router served the call, which lets callers degrade to their configured model
// without making the single-adapter path participate in routing state.
func (c *Call) Model() string {
	if c == nil {
		return ""
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.model
}

// Observe registers the router's side of the verdict contract. It is set after
// the call has been placed, because until then there is nothing to attribute a
// verdict to.
func (c *Call) Observe(observer func(Verdict)) {
	if c == nil {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if !c.reported {
		c.observer = observer
	}
}
