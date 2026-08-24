package session

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── ONE CALL NOBODY TYPED ───────────────────────────────────────────────────
//
// An auxiliary call is an errand: the name a session gives itself, the two words
// a task is called, the judge that reads one turn and answers one question. They
// are all the same shape — resolve a role to a model, ask once, bill the person
// for it — and until this file each of them spelled that shape itself, which is
// how three of them ended up with three different answers to the two questions
// below.
//
// HOW LONG IT MAY TAKE. The adapter bounds a completion between five and fifteen
// minutes, scaled off the output cap (internal/provider's
// adaptiveCompletionTimeout). That is the right bound for a person's turn and a
// ridiculous one for eight words of title: a session with a wedged errand held a
// provider slot and a goroutine for a quarter of an hour, and the person waiting
// for their own turn behind it was told nothing. So every call here carries the
// bound of its role's TIER ([roles.PatienceFor]) — derived, never a table of
// per-role seconds — and a caller that knows its own call is tighter still says
// so and wins, because the nearer of two deadlines on one context is the one in
// force.
//
// AND WHAT HAPPENS WHEN THE MODEL CANNOT ANSWER AT ALL. The errand used to be
// abandoned: a role pinned to a small model that is down, or a tier naming a
// slug an account has lost access to, and the session simply had no title, no
// task name, no judgement — quietly, with the model the person is talking to
// sitting there able to do the work.
//
// So it FALLS THROUGH ONE RUNG. The rung is the roles ladder's own next entry —
// pin, then tier, then the model the conversation is on — and that is the whole
// reason it is the ladder and not the adapter's fallback chain. The ladder is
// ALREADY the one answer to "which model answers this call"; its floor is the
// session model, which is by construction a model that works, because it is the
// one answering the person's own turns. The adapter's chain answers a different
// question — "where does this CONVERSATION go" — and it already applies to these
// calls underneath, on a refusal and on pacing that will not clear
// (internal/provider's endpoints.go). Spelling "the next model" twice over one
// request is how two answers drift.
//
// ONE rung, and then the failure is real. A ladder walked to the bottom on every
// errand would turn one bad minute at a provider into three charges and three
// waits for an answer nobody asked for.

// roleFallThroughs is how many rungs BELOW the resolved one an errand may try.
// One: see above.
const roleFallThroughs = 1

// callRole makes one auxiliary call and reports which model actually answered.
//
// The returned model is not decoration and callers must bill THAT one: an
// errand that fell through and was billed against the model that refused it
// would put a charge on a row nobody's request ever reached.
//
// `sessionDefault` is the ladder's floor, exactly as [roles.ResolveCall] takes
// it. Options are the caller's own — temperature, output cap, a response format
// — and the model is added here so no caller can name one that disagrees with
// the rung it is on.
func (a *Agent) callRole(
	ctx context.Context,
	role roles.Role,
	sessionDefault string,
	messages []ai.Message,
	options ...ai.Option,
) (*ai.Response, string, error) {
	a.mu.Lock()
	source, client := a.config.RolesSource, a.client
	a.mu.Unlock()
	rungs, err := roles.Ladder(roles.Source(source), role, sessionDefault)
	if err != nil {
		return nil, "", err
	}
	if client == nil {
		return nil, "", errNoCompleter
	}
	if len(rungs) > roleFallThroughs+1 {
		rungs = rungs[:roleFallThroughs+1]
	}
	patience := roles.PatienceFor(role)

	var lastErr error
	for _, rung := range rungs {
		// WithoutStream because nobody asked for this call: left on the turn's
		// stream it would type itself into the room in the model's voice.
		callCtx := provider.WithoutStream(ctx)
		if effort, ok := provider.ParseEffort(rung.Effort); ok && effort != provider.EffortNone {
			// WithReasoningEffort and not the configured setter: a level carried
			// on a tier value is a HARNESS default, which the adapter drops for a
			// model no catalog can vouch for. A person's own ctrl+t is the other
			// setter and does not reach an errand at all.
			callCtx = provider.WithReasoningEffort(callCtx, effort)
		}
		callCtx, cancel := context.WithTimeout(callCtx, patience)
		response, callErr := client.CompleteWithMessages(callCtx, messages,
			append(append([]ai.Option{}, options...), ai.WithModel(rung.Model))...)
		cancel()
		if callErr == nil && response != nil {
			return response, rung.Model, nil
		}
		// The person's own interrupt, or the caller's deadline, ends the errand
		// where it stands. Walking a ladder on a context that is already over is
		// two more requests that cannot land.
		if ctx.Err() != nil {
			return nil, rung.Model, ctx.Err()
		}
		lastErr = callErr
		if lastErr == nil {
			lastErr = errEmptyAnswer
		}
	}
	return nil, "", lastErr
}

// The two failures this file names itself. Both are the shape a caller has
// always handled — an error, and nothing to read — rather than anything new.
var (
	errNoCompleter = errStr("session: no model client")
	errEmptyAnswer = errStr("the model answered with nothing")
)

type errStr string

func (e errStr) Error() string { return string(e) }
