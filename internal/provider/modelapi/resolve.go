package modelapi

// Which model answers a program's call.
//
// A program asks for the models of its own pool — senior-dev names DeepSeek,
// Qwen, Kimi, GLM and MiniMax ids, sometimes behind an `openrouter/` prefix —
// and it was written for a machine that has an OpenRouter account. The person
// running it may not have one: their profile can reach models only through a
// service of their own (a local proxy, a vendor's key), which knows none of
// those ids. A program is not a person who can be asked to pick again, so the
// answer is decided here, once per call, by one rule.
//
// THE RULE: HONOUR THE ASK, OR ANSWER WITH THE RUN'S OWN SEAT, AND SAY WHICH.
// The model the program asked for is used whenever one of this person's
// services can take a call on it. When none can, the call is answered on the
// run's work seat — the model a task's own worker would sit on in this run —
// and the turn the conversation log keeps says so in its Served field. A call
// is NEVER refused only because this machine does not know the id it named:
// failing a whole task over a spelling the person never chose is the wrong
// trade, and the seat is a model they did choose.
//
// WHETHER A SERVICE CAN TAKE A CALL IS NOT DECIDED HERE. It is the account
// pool's own question (internal/session's ServesModel, the same test the
// conversation's client pool asks before it seats a model), handed in by the
// caller as a function, so the model API and the conversation cannot come to
// two answers about one machine.

import (
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// Resolve is THE ONE PLACE a program's model becomes the model that answers
// it. asked is the id as the program wrote it, serves answers whether one of
// this person's services can take a call on a model (nil answers yes for
// every model), and seats are the models a call falls to, in order — the run's
// work seat first.
//
// model is what the call goes out as; served is set exactly when it is not
// the model that was asked for, and it is what [delegate.Turn.Served] carries.
// A seat that cannot be served either is passed over for the next; when
// nothing can be served the call goes out as asked, so the funnel's own road
// answers it and says why, rather than this function inventing a refusal.
func Resolve(asked string, serves func(model string) bool, seats ...string) (model, served string) {
	asked = strings.TrimSpace(asked)
	can := func(candidate string) bool { return serves == nil || serves(candidate) }
	if asked != "" && can(asked) {
		return asked, ""
	}
	for _, seat := range seats {
		seat = strings.TrimSpace(seat)
		if seat == "" || seat == asked {
			continue
		}
		if can(seat) {
			return seat, seat
		}
	}
	if asked == "" {
		// A call that named no model at all is answered on the first seat
		// there is, whatever can be said about it: there is nothing else to
		// send, and the funnel says why if it cannot.
		for _, seat := range seats {
			if seat = strings.TrimSpace(seat); seat != "" {
				return seat, seat
			}
		}
	}
	return asked, ""
}

// unknownHere reports that a call failed because this machine could not serve
// the model it went out as — no key for the service the id resolves to, or a
// router that carries no such model — which is the one failure the seat can
// cure. It reads the funnel's own typed facts, never its sentence.
func unknownHere(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, provider.ErrNoAPIKey) {
		return true
	}
	refusal, ok := provider.RefusalFrom(err)
	return ok && refusal.Withdrawn
}

// without is serves with one model struck out: the funnel has just said it
// cannot serve it, whatever the account pool believed a moment ago.
func without(serves func(string) bool, gone string) func(string) bool {
	return func(model string) bool {
		if strings.TrimSpace(model) == strings.TrimSpace(gone) {
			return false
		}
		return serves == nil || serves(model)
	}
}
