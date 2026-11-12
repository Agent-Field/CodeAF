package crewroute

import (
	"fmt"
	"strings"
)

// WHAT A DECISION SAYS ABOUT THE POLICY IT WAS ADMITTED UNDER.
//
// A decision is read at a task's admission and frozen with it, so whatever a
// person changes afterwards reaches the next task and never the running one.
// Three facts travel with it for whoever draws the task: which mode admitted it
// (the router's pick per role, or one model for every role), what was wrong
// with the person's saved pins at that moment, and — when no decision could be
// formed — which role had nothing to run on and why each model was turned away.
// None of the three is a reading of the settings later: a surface that wants
// to know what a task ran on reads its decision, never today's settings.

// Mode is how a decision's roles were chosen.
type Mode string

const (
	// ModeAuto is the router's pick per role, inside the allowed models, with
	// any eligible pin kept as written. An empty Mode on a decision written
	// before modes existed reads as auto.
	ModeAuto Mode = "auto"
	// ModeOne is one model for every role, the person's explicit choice: no
	// role is picked, and no role moves to another model on its own.
	ModeOne Mode = "one"
)

// WarningKind is what was wrong with a role's saved pin at admission.
type WarningKind string

const (
	// PinNotAllowed is a pin the allowed models no longer admit. The pin stays
	// saved; the role was routed by Auto for this task.
	PinNotAllowed WarningKind = "pin_not_allowed"
	// PinProviderUnavailable is a pin (or the one model) whose provider is not
	// connected, turned off, or refused at the moment of admission.
	PinProviderUnavailable WarningKind = "provider_unavailable"
)

// SeatWarning is one role's warning, with the pin it is about.
type SeatWarning struct {
	Seat     Seat
	Kind     WarningKind
	Model    string
	Provider string `json:",omitempty"`
}

// RejectReason is why one model could not take a role.
type RejectReason string

const (
	// RejectNotAllowed is a model the allowed models leave out on that route.
	RejectNotAllowed RejectReason = "not_allowed"
	// RejectNoPrice is a model whose provider published no price: a pin may
	// name it, Auto never picks it.
	RejectNoPrice RejectReason = "no_price"
	// RejectProviderUnavailable is a route whose provider is not connected,
	// turned off, signed out, refused its key or out of credit.
	RejectProviderUnavailable RejectReason = "provider_unavailable"
	// RejectRouteUnavailable is a route resting after failures — cooling down,
	// kept out, or not served any more.
	RejectRouteUnavailable RejectReason = "route_unavailable"
	// RejectCannotDoRole is a model that cannot do the role: no tool calls,
	// not text, or too little context.
	RejectCannotDoRole RejectReason = "cannot_do_role"
)

// Rejection is one model on one route turned away from a role.
type Rejection struct {
	Model    string
	Provider string `json:",omitempty"`
	Reason   RejectReason
}

// MaxRejections bounds how many rejections one error carries; the rest are
// counted in [NoEligibleError.Omitted].
const MaxRejections = 20

// NoEligibleError is a role with NOTHING ALLOWED TO RUN ON: no eligible pin,
// no allowed model on a reachable route that can do it. The task is not
// started and is not moved onto any other model — not the conversation's own,
// not the last crew's — so whoever admitted it can stop and ask.
type NoEligibleError struct {
	Seat Seat
	Mode Mode
	// Rejected is why each considered model was turned away, at most
	// [MaxRejections] of them, allowed models first.
	Rejected []Rejection
	Omitted  int
	// Action is the one plain thing a person can do, when the routes say one
	// (add credit, reconnect); empty otherwise.
	Action string
	// Cause is an older error this one stands for, so a caller matching that
	// error still finds it ([errors.As]).
	Cause error
}

func (e *NoEligibleError) Error() string {
	if e.Action != "" {
		return e.Action
	}
	if e.Mode == ModeOne {
		return fmt.Sprintf("the one model for every task cannot run the %s role", e.Seat)
	}
	return fmt.Sprintf("no allowed model can do the %s role · allow one, or pin one", e.Seat)
}

// Unwrap is the error this one stands for.
func (e *NoEligibleError) Unwrap() error { return e.Cause }

// Reject adds one rejection, keeping the bound.
func (e *NoEligibleError) Reject(model, provider string, reason RejectReason) {
	if len(e.Rejected) >= MaxRejections {
		e.Omitted++
		return
	}
	e.Rejected = append(e.Rejected, Rejection{Model: model, Provider: strings.ToLower(provider), Reason: reason})
}
