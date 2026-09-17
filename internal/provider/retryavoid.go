package provider

import (
	"context"
	"errors"
	"strings"
)

// ── THE LANE A RETRY MUST NOT RE-ASK ────────────────────────────────────────
//
// A call that failed on one upstream lane, retried by the layer above, is a
// NEW call here: fresh knobs, fresh encode, and — under the shipped simple
// routing row — no `provider` object on the wire at all. OpenRouter's own
// routing then answers every attempt the same way it answered the first, and
// the layer above burns its whole retry budget on one machine.
//
// The fix rides the context, the way every other per-call fact does
// ([WithPatientRateLimits], [WithRoutingIntent], [WithServedEndpoint]): the
// caller that owns the retry names the lanes that already failed THIS call,
// and the encoder writes them into `provider.ignore` on every body the call's
// remaining attempts send. The list is the caller's to scope — it is carried
// on the context of one call, so it is gone the moment that call is over and
// never reaches a later one or a setting.
//
// A LANE'S NAME ARRIVES FROM THE WIRE, not from a guess: `APIError.Provider`
// spells it for an upstream fault, [StreamCut.Provider] for a cut stream. An
// error that named nobody leaves the list alone, and a base that names no
// lanes hands the caller nothing to put here — so on every endpoint that does
// not speak the router's dialect, requests are byte-for-byte what they always
// were.

type retryAvoidKey struct{}

// WithRetryAvoid asks every request made under ctx to carry the named lanes in
// its routing preferences' ignore list. Names are trimmed, deduplicated and
// emptied of blanks; a list that holds nothing changes nothing.
func WithRetryAvoid(ctx context.Context, lanes []string) context.Context {
	if len(lanes) == 0 {
		return ctx
	}
	kept := make([]string, 0, len(lanes))
	for _, lane := range lanes {
		lane = strings.TrimSpace(lane)
		if lane == "" {
			continue
		}
		duplicate := false
		for _, held := range kept {
			if equalLane(held, lane) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			kept = append(kept, lane)
		}
	}
	if len(kept) == 0 {
		return ctx
	}
	return context.WithValue(ctx, retryAvoidKey{}, kept)
}

// RetryAvoidFrom is the list of lanes the caller has asked this call's bodies
// to ignore, nil when the call is not a retry after a named failure. It is the
// read side of [WithRetryAvoid], exported for the layer that composes the
// retry and asserts what it stamped.
func RetryAvoidFrom(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	lanes, _ := ctx.Value(retryAvoidKey{}).([]string)
	return lanes
}

// FailedLane is the upstream lane a failed call names, "" when the failure
// implicates nobody — the wire fact a retry turns into a lane to avoid. A
// 5xx relayed from a named upstream names that provider, a cut stream names
// the provider the stream named; a router's own refusal carries no provider
// name, a transport fault names no machine, and an empty name is a fact the
// retry keeps: the retry goes where it always went.
func FailedLane(err error) string {
	var relayed *APIError
	if errors.As(err, &relayed) && relayed.Status >= 500 {
		return strings.TrimSpace(relayed.Provider)
	}
	var cut *StreamCut
	if errors.As(err, &cut) {
		return strings.TrimSpace(cut.Provider)
	}
	return ""
}
