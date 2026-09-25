package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// WHAT A FAILED CALL SAYS ABOUT THE ROUTE IT WAS SENT ON.
//
// The crew router picks a model AND a route to it — a provider, a send id, a
// price — and when a seat's call fails, what happens next depends on what the
// failure says about that route: whether the account behind it is out of
// credit, whether the key is bad, whether this route will never take this
// model, whether it is only busy. Each provider spells those differently; this
// is where they are read, once, into a handful of kinds nobody downstream has
// to re-read from a status or a sentence. The router and the seat fallback see
// only the kinds (internal/crewroute, internal/session's taskcrew.go).
//
// It reads the facts the refusal door already decided about the error
// ([APIError]'s Payment, Withdrawn, PlanPaused, PlanUnavailable) and the
// status, and nothing else from the prose but the few phrases providers use
// for "this model is not available to you here".

// RouteFailure is one kind of route failure.
type RouteFailure string

const (
	// RoutePayment is the account behind the route out of credit (402, an
	// "insufficient credits" refusal): every PAID route on that account is
	// unaffordable until a paid call on it succeeds again.
	RoutePayment RouteFailure = "payment"
	// RouteAuth is the key refused (401): the provider is disconnected until
	// the person reconnects it.
	RouteAuth RouteFailure = "auth"
	// RouteForbidden is this route refusing this model for this account or
	// plan (403, "only available on…", a data-policy exclusion): the route is
	// quarantined; the model may still be reachable elsewhere.
	RouteForbidden RouteFailure = "forbidden"
	// RouteQuota is a limit reached (429, a paused plan window, a daily cap):
	// the route cools down until the reset, when one was said.
	RouteQuota RouteFailure = "quota"
	// RouteUnavailable is the model not served here any more (404, withdrawn):
	// the route cools down and the model is demoted.
	RouteUnavailable RouteFailure = "unavailable"
	// RouteTransient is a single timeout or 5xx: asked again once, no verdict
	// about the route.
	RouteTransient RouteFailure = "transient"
)

// forbiddenPhrases are how providers word "not for you on this route" in a
// refusal whose status alone does not say it. Each is a provider's own words.
var forbiddenPhrases = []string{
	"only available on",
	"not available for your",
	"not allowed to use",
	"does not have access",
	"data policy",
	"guardrail restrictions",
}

// paymentPhrases are how providers word an empty balance under a status that
// is not 402.
var paymentPhrases = []string{
	"insufficient credit",
	"insufficient balance",
	"insufficient_quota",
	"credit balance is too low",
	"requires more credits",
	"exceeded your current quota",
}

// RouteFailureOf reads one call's error as a route failure: the kind, and when
// the route may be asked again if the provider said (zero when it did not). An
// error that says nothing about the route — a request nothing could serve, a
// cancelled context — answers "".
func RouteFailureOf(err error) (RouteFailure, time.Time) {
	if err == nil || errors.Is(err, context.Canceled) {
		return "", time.Time{}
	}
	var paused *PlanPauseError
	if errors.As(err, &paused) {
		return RouteQuota, parseReset(paused.Reset)
	}
	refusal, ok := RefusalFrom(err)
	if !ok {
		var unavailable *ConnectionUnavailableError
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &unavailable) || terminalTransportFailureFrom(err) {
			return RouteTransient, time.Time{}
		}
		return "", time.Time{}
	}
	said := strings.ToLower(refusal.Message + " " + refusal.Raw + " " + refusal.Code)
	switch {
	case refusal.Payment || refusal.Status == http.StatusPaymentRequired || holdsAny(said, paymentPhrases):
		return RoutePayment, time.Time{}
	case refusal.Status == http.StatusUnauthorized:
		return RouteAuth, time.Time{}
	case refusal.PlanPaused || refusal.Status == http.StatusTooManyRequests:
		return RouteQuota, time.Time{}
	case refusal.Withdrawn:
		return RouteUnavailable, time.Time{}
	case refusal.Status == http.StatusForbidden || refusal.PlanUnavailable || holdsAny(said, forbiddenPhrases):
		return RouteForbidden, time.Time{}
	case refusal.Status == http.StatusNotFound:
		return RouteUnavailable, time.Time{}
	case refusal.Status >= http.StatusInternalServerError:
		return RouteTransient, time.Time{}
	}
	return "", time.Time{}
}

// holdsAny is whether text holds any of the phrases.
func holdsAny(text string, phrases []string) bool {
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// parseReset reads a reset a provider said, in either of the spellings seen:
// an RFC 3339 instant, or a clock time today.
func parseReset(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if at, err := time.Parse(time.RFC3339, raw); err == nil {
		return at
	}
	for _, layout := range []string{"15:04", "3:04pm", "3pm"} {
		if clock, err := time.Parse(layout, strings.ToLower(raw)); err == nil {
			now := time.Now()
			at := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
			if at.Before(now) {
				at = at.Add(24 * time.Hour)
			}
			return at
		}
	}
	return time.Time{}
}
