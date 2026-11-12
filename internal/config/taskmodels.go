package config

import (
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// TASK MODELS: AUTO, OR ONE MODEL FOR EVERY TASK.
//
// Every task is admitted with a decision about its three roles, and that
// decision is frozen with the task — a running task keeps its picks whatever a
// person changes after. The profile says how the decision is made:
//
//   - AUTO, the default, is the router's pick per role inside the allowed
//     models, with any pin the allowed models still admit kept as written
//     (crew.go). A pin they no longer admit is KEPT and the role is routed,
//     and the decision carries a warning saying so.
//   - ONE MODEL is one explicit model on one explicit route for every role.
//     Nothing is picked, the allowed models and the pins are left exactly as
//     they are for when Auto comes back, and a one model that cannot be
//     reached is a refusal at admission — never another model quietly.
//
// The mode is a row, read when a task is admitted. It is NOT the `--one-model`
// launch flag, which takes the router away from a whole process and is
// untouched by this: a process started with it never asks.

// The task-models rows, spelled once. PROFILE-ONLY, like every crew row: a
// repository that could set them could send a visitor's work to a model nobody
// on that machine chose.
const (
	// KeyTaskModelsMode is `auto` or `one`; absent is auto.
	KeyTaskModelsMode = "models.tasks.mode"
	// KeyTaskModelsOne is the one model, `model` or `model@provider`. It is
	// kept while the mode is auto, so the choice comes back.
	KeyTaskModelsOne = "models.tasks.one"
)

// TaskModelsModeAt is the profile's mode. Anything but `one` is auto.
func TaskModelsModeAt(profileDir string) crewroute.Mode {
	if value, _ := persistedString(profileDir, KeyTaskModelsMode); strings.EqualFold(strings.TrimSpace(value), string(crewroute.ModeOne)) {
		return crewroute.ModeOne
	}
	return crewroute.ModeAuto
}

// TaskOneModelAt is the saved one model, false when none was chosen or the
// row does not read.
func TaskOneModelAt(profileDir string) (CrewPin, bool) {
	value, held := persistedString(profileDir, KeyTaskModelsOne)
	if !held {
		return CrewPin{}, false
	}
	pin, auto, err := ParseCrewPin(value)
	if auto || err != nil {
		return CrewPin{}, false
	}
	return pin, true
}

// routeOneModel is one task's decision in one-model mode: the one model on its
// route in every role. Per-task pins the person gave this task still stand.
func routeOneModel(profileDir string, ask CrewAsk) (crewroute.Decision, error) {
	one, ok := TaskOneModelAt(profileDir)
	if !ok {
		return crewroute.Decision{}, &crewroute.NoEligibleError{Seat: crewroute.Worker, Mode: crewroute.ModeOne,
			Action: "choose the one model for every task, or switch Task models back to Auto"}
	}
	providers := CrewProvidersAt(profileDir)
	route := resolveCrewPin(one, providers)
	// THE ONE MODEL IS NOT PUT ON TRIAL. Auto may send a task's first call to
	// an account it last saw refuse, because another model is there if it
	// refuses again; one-model mode has no other model, so a route known to be
	// signed out, out of credit or resting refuses the task with the one fix.
	health := crewHealthAt(profileDir)
	if why, ok := crewPinReachable(providers, route, health); !ok {
		refusal := &crewroute.NoEligibleError{Seat: crewroute.Worker, Mode: crewroute.ModeOne, Action: health.actionFor(route, "choose another one model")}
		refusal.Reject(route.Model, route.Provider, why)
		return crewroute.Decision{}, refusal
	}
	pins := map[crewroute.Seat]crewroute.Pin{}
	for _, seat := range crewroute.Seats {
		pins[seat] = route
	}
	for seat, pin := range ask.Pins {
		pins[seat] = resolveCrewPin(pin, providers)
	}
	for seat, send := range ask.Sends {
		if send = strings.TrimSpace(send); send != "" {
			pins[seat] = resolveCrewPin(CrewPin{Model: send}, providers)
		}
	}
	reading := crewroute.Classify(ask.Task)
	// The figures of the one model — its quality and estimate for the line —
	// are read from every reachable model, allowed or not: one-model mode does
	// not ask the allowed models.
	facts := crewRouteFacts{free: CrewFreeRoutesAt(profileDir), health: crewHealthAt(profileDir).probing()}
	figures := crewCandidatesWith(crewroute.Allowed{Base: crewroute.BaseAll}, providers, facts)
	d, err := crewroute.Decide(crewroute.Request{
		Task: ask.Task, Class: reading.Class, Reading: &reading, Candidates: figures, Pins: pins,
		Stronger: ask.Stronger, Again: ask.Again,
	})
	if err != nil {
		return crewroute.Decision{}, err
	}
	d.Why, d.Sure = reading.Why, reading.Sure
	d.Redo = ask.Stronger != nil || ask.Again != nil
	d.Mode = crewroute.ModeOne
	if d.OneOff {
		// A stronger redo the person asked for stepped over the one model and
		// was picked by the router: that one task is Auto's.
		d.Mode = crewroute.ModeAuto
	}
	day := CrewDay{}
	if CrewHistory != nil {
		day = CrewHistory(profileDir)
	}
	if _, atCap := crewroute.Pace(day.SpentUSD, CrewCapAt(profileDir)); atCap {
		return d, ErrCrewAtCap
	}
	return d, nil
}

// crewPinReachable is whether a resolved pin's route is one route health
// expects to answer — a provider connected and on, whose key was not refused,
// whose account is not out of credit for a paid route, on a send that is not
// resting — and why not.
func crewPinReachable(providers []CrewProvider, route crewroute.Pin, health crewHealth) (crewroute.RejectReason, bool) {
	p, ok := crewProviderByID(providers, route.Provider)
	switch {
	case !ok || !p.On || health.disconnected[p.ID] || (route.Kind == crewroute.Metered && health.unaffordable[p.ID]):
		return crewroute.RejectProviderUnavailable, false
	}
	if _, resting := health.blocked[route.Send]; resting {
		return crewroute.RejectRouteUnavailable, false
	}
	return "", true
}

// actionFor is the one thing a person can do about one route health holds
// back, followed by the other way out (or), or the other way alone when health
// has nothing to say about the route — a provider not connected, or off.
func (h crewHealth) actionFor(route crewroute.Pin, or string) string {
	switch {
	case h.disconnected[route.Provider]:
		return "reconnect " + route.Provider + " with /connect, or " + or
	case route.Kind == crewroute.Metered && h.unaffordable[route.Provider]:
		return "add credit on " + route.Provider + ", or " + or
	}
	if until, resting := h.blocked[route.Send]; resting {
		return crewroute.ShortModel(route.Model) + " on " + route.Provider + " is resting until " + until.Local().Format("15:04") + " · try then, or " + or
	}
	provider := route.Provider
	if provider == "" {
		provider = "its provider"
	}
	return "connect or turn on " + provider + ", or " + or
}

// crewSavedPins is the profile's pins resolved for routing, under two laws:
//
//   - A PIN THE ALLOWED MODELS LEAVE OUT IS SET ASIDE: the role is routed by
//     Auto for this task and the decision carries a pin_not_allowed warning.
//     That is the one exception, because the person said both things — this
//     model, and not this model — and the later word is theirs.
//   - AN ALLOWED PIN IS NEVER SUBSTITUTED. A pin whose route is known not to
//     answer — its provider gone, off, signed out, out of credit, or the send
//     resting — is the role's refusal ([crewroute.NoEligibleError]), with the
//     one fix, and never a quiet move to another model or another provider.
//
// The refusals are per seat, so a seat this task's own pins fill is not
// refused for a saved pin it does not use.
func crewSavedPins(profileDir string, providers []CrewProvider, health crewHealth) (map[crewroute.Seat]crewroute.Pin, []crewroute.SeatWarning, map[crewroute.Seat]*crewroute.NoEligibleError) {
	rule := CrewAllowedAt(profileDir)
	pins := map[crewroute.Seat]crewroute.Pin{}
	var warnings []crewroute.SeatWarning
	refused := map[crewroute.Seat]*crewroute.NoEligibleError{}
	saved := CrewPinsAt(profileDir)
	for _, seat := range crewroute.Seats {
		pin, ok := saved[seat]
		if !ok {
			continue
		}
		route := resolveCrewPin(pin, providers)
		if !crewPinAdmitted(rule, pin.Model, route.Provider) {
			warnings = append(warnings, crewroute.SeatWarning{Seat: seat, Kind: crewroute.PinNotAllowed, Model: pin.Model, Provider: pin.Provider})
			continue
		}
		if why, ok := crewPinReachable(providers, route, health); !ok {
			refusal := &crewroute.NoEligibleError{Seat: seat, Mode: crewroute.ModeAuto,
				Action: health.actionFor(route, "unpin the "+string(seat)+" from "+crewroute.ShortModel(pin.Model))}
			refusal.Reject(route.Model, route.Provider, why)
			refused[seat] = refusal
			continue
		}
		pins[seat] = route
	}
	return pins, warnings, refused
}

// withoutWarningsFor drops the warnings about seats a task's own pins filled.
func withoutWarningsFor(warnings []crewroute.SeatWarning, filled map[crewroute.Seat]bool) []crewroute.SeatWarning {
	var out []crewroute.SeatWarning
	for _, w := range warnings {
		if !filled[w.Seat] {
			out = append(out, w)
		}
	}
	return out
}

// crewNoEligible is the typed refusal for a seat nothing allowed can take,
// with why each model on each connected route was turned away — allowed rows
// first, because those are the ones a person did not expect to lose.
func crewNoEligible(profileDir string, seat crewroute.Seat, cause error) *crewroute.NoEligibleError {
	refusal := &crewroute.NoEligibleError{Seat: seat, Mode: crewroute.ModeAuto}
	var unreachable ErrCrewUnreachable
	if errors.As(cause, &unreachable) {
		refusal.Cause = unreachable
		refusal.Action = unreachable.Action
	}
	if refusal.Action == "" || strings.HasPrefix(refusal.Action, "no allowed model") {
		// The ordinary case says the role and the one door that fixes it.
		refusal.Action = "no allowed model can do the " + string(seat) + " role · widen " + CrewCommand + " models or pin one with " + CrewCommand
	}
	rows, err := taskModelRows(nil, profileDir)
	if err != nil {
		return refusal
	}
	for _, allowedFirst := range []bool{true, false} {
		for _, row := range rows {
			if row.Allowed != allowedFirst {
				continue
			}
			reason := row.Reason
			if reason == "" && !row.Seats[seat] {
				reason = crewroute.RejectCannotDoRole
			}
			if reason == "" {
				// Eligible at a glance and still not seated: the route was
				// tried and rests, or its model was kept off for failing.
				reason = crewroute.RejectRouteUnavailable
			}
			refusal.Reject(row.Model.ID, row.Provider, reason)
		}
	}
	return refusal
}

// pinBase is a pin's model without its thinking level.
func pinBase(id string) string {
	base, _ := roles.SplitEffort(strings.TrimSpace(id))
	return base
}
