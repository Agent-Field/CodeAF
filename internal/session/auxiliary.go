package session

import (
	"context"
	"strings"

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
		//
		// And IntentBackground for the other half of the same sentence. Nobody
		// asked for it and nobody is waiting on it, so the fastest endpoint is
		// worth nothing here and its price is worth everything — every errand in
		// this package routes by price rather than by speed
		// (internal/provider's velocity.go). This is the one place that says so,
		// because this is the one place an errand is made.
		callCtx := provider.WithRoutingIntent(provider.WithoutStream(ctx), provider.IntentBackground)
		if effort, ok := provider.ParseEffort(rung.Effort); ok && effort != provider.EffortNone {
			// WithReasoningEffort and not the configured setter: a level carried
			// on a tier value is a HARNESS default, which the adapter drops for a
			// model no catalog can vouch for. A person's own ctrl+t is the other
			// setter and does not reach an errand at all.
			callCtx = provider.WithReasoningEffort(callCtx, effort)
		}
		// AND A SLOT FOR WHOEVER ANSWERS, so the errand's own call line can name
		// the endpoint the way a turn's does. An errand routes by price, which
		// means it is exactly the kind of request whose server cannot be guessed
		// from the model name.
		served := &provider.ServedEndpoint{}
		callCtx = provider.WithServedEndpoint(callCtx, served)
		callCtx, cancel := context.WithTimeout(callCtx, patience)
		response, callErr := client.CompleteWithMessages(callCtx, messages,
			append(append([]ai.Option{}, options...), ai.WithModel(rung.Model))...)
		cancel()
		if callErr == nil && response != nil {
			// AND THE ERRAND WRITES ITS OWN CALL LINE, exactly as a step of the
			// turn does (loop.go's [Agent.addUsage]). Without it the journal's
			// call lines covered only the conversation's own requests, and a
			// measured run's lines summed to $0.123 against a real bill of $0.739
			// — the whole of the difference being three side-calls to a
			// mastermind. See [journalCall] for why that is a record worth
			// nothing and why the role rides the line.
			a.journalRoleCall(response, role, rung.Model, served.Name())
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

// journalRoleCall writes ONE errand's own accounting down, on the same line
// shape a step of the turn writes (see [journalCall]).
//
// IT IS EVIDENCE AND NEVER SPEND, exactly as the turn's line is: the caller
// folds the money into the session's totals through [Agent.addAuxiliaryUsage],
// and the replay drops these lines rather than adding them a second time. What
// this buys is the question the summed lines could not answer — which model was
// asked what, and what that one request cost — for the half of the bill that has
// nobody's turn behind it.
//
// THE MODEL FALLS BACK TO THE RUNG. A provider that names itself in the response
// is the better answer, because it is who actually served the request; a
// provider that names nothing would otherwise leave the line saying only that
// somebody was paid, so the rung the ladder resolved stands in for it.
//
// A response that reported no usage writes nothing, which [sessionFile.appendCall]
// enforces on its own side too — the emptiness law, and a stream cut before its
// final chunk is exactly that case.
func (a *Agent) journalRoleCall(response *ai.Response, role roles.Role, rung, endpoint string) {
	if response == nil || response.Usage == nil {
		return
	}
	model := strings.TrimSpace(response.Model)
	if model == "" {
		model = strings.TrimSpace(rung)
	}
	usage := response.Usage
	a.file.appendCall(journalCall{
		Model:      model,
		Endpoint:   strings.TrimSpace(endpoint),
		Role:       string(role),
		Input:      usage.PromptTokens,
		CacheRead:  usage.CacheReadTokens(),
		CacheWrite: usage.CacheCreationTokens(),
		Output:     usage.CompletionTokens,
		CostUSD:    costOf(usage),
	})
}

// The two failures this file names itself. Both are the shape a caller has
// always handled — an error, and nothing to read — rather than anything new.
var (
	errNoCompleter = errStr("session: no model client")
	errEmptyAnswer = errStr("the model answered with nothing")
)

type errStr string

func (e errStr) Error() string { return string(e) }
