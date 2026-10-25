package main

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── a task's models (V80): tasks.routing.get and tasks.routing.act ─────────
//
// Both forward to the engine's one task-routing door (internal/session's
// taskrouting.go) and turn its record into the wire's shapes. Nothing here
// decides a model, prices one, or reads today's settings.

// TaskRouting answers one task's models as recorded.
func (b *appBackend) TaskRouting(id string, changes int) (appwire.TaskRouting, error) {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.TaskRouting{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	_, live, endLive, err := b.borrowLive(agent)
	if err != nil {
		return appwire.TaskRouting{}, err
	}
	defer endLive()
	view, err := live.conn.Conv.TaskRouting(local, changes)
	if err != nil {
		return appwire.TaskRouting{}, appRoutingError(err)
	}
	return appRoutingOf(id, view), nil
}

// ActOnTaskRouting is a person's change to one task's models. A refusal the
// person can act on is an answer, not an error.
func (b *appBackend) ActOnTaskRouting(p appwire.TaskRoutingActParams) (appwire.TaskRoutingActResult, error) {
	agent, local, ok := appSplitID(p.ID)
	if !ok {
		return appwire.TaskRoutingActResult{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", p.ID)
	}
	_, live, endLive, err := b.admitLive(agent)
	if err != nil {
		return appwire.TaskRoutingActResult{}, err
	}
	defer endLive()
	act := session.TaskRoutingAction{Kind: p.Action, Rerun: p.Rerun, RaiseCapUSD: p.RaiseCapUSD, ChangeCount: p.ChangeCount}
	if len(p.Picks) > 0 {
		act.Picks = map[crewroute.Seat]string{}
		for role, ref := range p.Picks {
			act.Picks[crewroute.Seat(role)] = appSendOf(ref)
		}
	}
	got, err := live.conn.Conv.TaskRoutingAct(context.Background(), local, act)
	var refusal *session.TaskRoutingRefusal
	if errors.As(err, &refusal) && appRoutingRefusalAnswered(refusal.Code) {
		view, readErr := live.conn.Conv.TaskRouting(local, 0)
		if readErr != nil {
			return appwire.TaskRoutingActResult{}, appRoutingError(readErr)
		}
		return appwire.TaskRoutingActResult{Routing: appRoutingOf(p.ID, view), Refused: appRefusedOf(refusal)}, nil
	}
	if err != nil {
		return appwire.TaskRoutingActResult{}, appRoutingError(err)
	}
	out := appwire.TaskRoutingActResult{Routing: appRoutingOf(p.ID, got.View)}
	for _, seat := range got.Applied {
		out.Applied = append(out.Applied, string(seat))
	}
	if got.Rerun != 0 {
		out.RerunTaskID = appTaskID(agent, got.Rerun)
	}
	return out, nil
}

// appSendOf is the id the engine routes a model reference by: the model on
// the provider's route when one is named.
func appSendOf(ref *appwire.ModelRef) string {
	if ref == nil {
		return ""
	}
	return ref.ID
}

// appRoutingRefusalAnswered is whether a refusal is the person's to act on.
func appRoutingRefusalAnswered(code string) bool {
	switch code {
	case session.TaskRoutingNotFound, session.TaskRoutingBadAction, session.TaskRoutingUnavailable:
		return false
	}
	return true
}

// appRoutingError sorts an engine refusal into the wire's codes.
func appRoutingError(err error) error {
	var refusal *session.TaskRoutingRefusal
	if errors.As(err, &refusal) {
		switch refusal.Code {
		case session.TaskRoutingNotFound:
			return appwire.Errorf(appwire.CodeNotFound, "%s", refusal.Message)
		case session.TaskRoutingBadAction:
			return appwire.Errorf(appwire.CodeBadParams, "%s", refusal.Message)
		}
		return appwire.Errorf(appwire.CodeEngine, "%s", refusal.Message)
	}
	return appEngineError(err)
}

func appRefusedOf(r *session.TaskRoutingRefusal) *appwire.TaskRoutingRefused {
	out := &appwire.TaskRoutingRefused{Code: r.Code, Message: r.Message, Role: string(r.Seat)}
	if r.Code == session.TaskRoutingOverCap {
		required, capUSD := r.RequiredUSD, r.CapUSD
		out.RequiredCapUSD, out.CapUSD = &required, &capUSD
	}
	if r.Blocked != nil {
		out.Blocked = appBlockedOf(*r.Blocked)
	}
	return out
}

func appRef(model, provider string) appwire.ModelRef {
	return appwire.ModelRef{ID: model, Provider: strings.ToLower(provider)}
}

func appRefPtr(model, provider string) *appwire.ModelRef {
	if model == "" {
		return nil
	}
	ref := appRef(model, provider)
	return &ref
}

func appBlockedOf(b session.TaskRoutingBlock) *appwire.TaskRoutingWait {
	mode := string(b.Mode)
	if mode == "" {
		mode = appwire.TaskModelsAuto
	}
	out := &appwire.TaskRoutingWait{Role: string(b.Seat), Mode: mode, Omitted: b.Omitted, Action: b.Action, At: b.At, Rejected: []appwire.TaskModelRejected{}}
	for _, r := range b.Rejected {
		out.Rejected = append(out.Rejected, appwire.TaskModelRejected{Model: appRef(r.Model, r.Provider), Reason: string(r.Reason)})
	}
	return out
}

// appRoutingOf is the engine's record as the wire's shape.
func appRoutingOf(id string, v session.TaskRoutingView) appwire.TaskRouting {
	out := appwire.TaskRouting{
		TaskID: id, State: v.State, Mode: string(v.Mode), Running: v.Running, Settled: v.Settled,
		Restored: v.Restored, EstimateUSD: v.EstimateUSD, Calls: v.Calls, UnknownCalls: v.UnknownCalls,
		HelperUSD: v.HelperUSD, Class: v.Class, StoppedAction: v.Stopped, ChangeCount: v.ChangeCount,
		RestoreCount: v.RestoreCount, RoleFacts: v.RoleFacts,
	}
	if v.Effort == string(crewroute.EffortBest) || v.Effort == string(crewroute.EffortCheap) {
		out.Effort = v.Effort
	}
	if v.SpentKnown {
		spent := v.SpentUSD
		out.SpentUSD = &spent
	}
	if v.TaskCapUSD > 0 {
		capUSD := v.TaskCapUSD
		out.TaskCapUSD = &capUSD
	}
	if v.Model != "" {
		out.Model = appRefPtr(v.Model, "")
	}
	for _, r := range v.Roles {
		role := appwire.TaskRoutingRole{Role: string(r.Seat), Model: appRef(r.Model, r.Provider), Billing: string(r.Kind), Source: r.Source, Status: r.Status,
			Quality: r.Quality, EstimateUSD: r.EstimateUSD, Price: appTariff(r.Price), Calls: r.Calls, UnknownCalls: r.UnknownCalls}
		if r.SpentKnown {
			spent := r.SpentUSD
			role.SpentUSD = &spent
		}
		for _, alt := range r.Alternatives {
			role.Alternatives = append(role.Alternatives, appwire.TaskRoutingOption{Model: appRef(alt.Model, alt.Provider), Billing: string(alt.Kind), Quality: alt.Quality, EstimateUSD: alt.EstimateUSD, Price: appTariff(alt.Price)})
		}
		for _, rej := range r.Rejected {
			out := appwire.TaskRoutingRejection{Model: appRef(rej.Model, rej.Provider), Reason: rej.Reason}
			if !rej.Until.IsZero() {
				until := rej.Until
				out.Until = &until
			}
			role.Rejected = append(role.Rejected, out)
		}
		out.Roles = append(out.Roles, role)
	}
	for _, w := range v.Warnings {
		out.Warnings = append(out.Warnings, appwire.TaskRoutingWarning{Role: string(w.Seat), Kind: string(w.Kind), Model: appRef(w.Model, w.Provider)})
	}
	for _, c := range v.Changes {
		change := appwire.TaskRoutingChange{At: c.At, Role: string(c.Seat), From: appRefPtr(c.From, c.FromProvider), To: appRefPtr(c.To, c.ToProvider), By: c.By, Kind: c.Kind, Reason: c.Why}
		if c.CapUSD > 0 {
			capUSD := c.CapUSD
			change.CapUSD = &capUSD
		}
		out.Changes = append(out.Changes, change)
	}
	if v.Blocked != nil {
		out.Blocked = appBlockedOf(*v.Blocked)
	}
	if len(v.Requested) > 0 {
		picks := &appwire.TaskRoutingPicks{}
		for seat, model := range v.Requested {
			ref := appRef(model, "")
			switch seat {
			case crewroute.Worker:
				picks.Worker = &ref
			case crewroute.Planner:
				picks.Planner = &ref
			case crewroute.Checker:
				picks.Checker = &ref
			}
		}
		out.Requested = picks
	}
	return out
}

// appModelsChip is a task row's models in brief, from what the engine
// published for it.
func appModelsChip(state string, d *crewroute.Decision, pins map[crewroute.Seat]bool, model string) *appwire.TaskModelsChip {
	switch state {
	case session.TaskRoutingPicking, session.TaskRoutingBlocked:
		return &appwire.TaskModelsChip{State: state}
	case session.TaskRoutingRouted:
		if d == nil {
			return nil
		}
	default:
		if model == "" {
			return nil
		}
		return &appwire.TaskModelsChip{State: session.TaskRoutingSingle, Roles: appwire.TaskRoutingPicks{Worker: appRefPtr(model, "")}, Distinct: 1}
	}
	mode := string(d.Mode)
	if mode == "" {
		mode = appwire.TaskModelsAuto
	}
	chip := &appwire.TaskModelsChip{State: session.TaskRoutingRouted, Mode: mode}
	distinct := map[string]bool{}
	for _, seat := range crewroute.Seats {
		pick := d.Seat(seat)
		if pick.Model == "" {
			continue
		}
		distinct[pick.Model] = true
		if pick.Pinned || pins[seat] {
			chip.Pinned++
		}
		ref := appRefPtr(pick.Model, pick.Provider)
		switch seat {
		case crewroute.Worker:
			chip.Roles.Worker = ref
		case crewroute.Planner:
			chip.Roles.Planner = ref
		case crewroute.Checker:
			chip.Roles.Checker = ref
		}
	}
	chip.Distinct = len(distinct)
	return chip
}

// appNoticeModels is the chip a live task update carries.
func appNoticeModels(notice *session.TaskNotice, proposed bool) *appwire.TaskModelsChip {
	switch {
	case proposed:
		return appModelsChip(session.TaskRoutingPicking, nil, nil, "")
	case notice.Waiting == session.TaskWaitingNoModel:
		return appModelsChip(session.TaskRoutingBlocked, nil, nil, "")
	case notice.Crew != nil:
		var pins map[crewroute.Seat]bool
		if notice.CrewState != nil {
			pins = notice.CrewState.TaskPins
		}
		return appModelsChip(session.TaskRoutingRouted, notice.Crew, pins, "")
	}
	return nil
}

// appTariff is a route's per-token tariff as dollars per million tokens; nil
// stays unknown.
func appTariff(p *crewroute.RoutePrice) *appwire.TaskModelPrice {
	if p == nil {
		return nil
	}
	return &appwire.TaskModelPrice{InPerM: p.Prompt * 1e6, OutPerM: p.Completion * 1e6}
}

// TaskRoutingOptions answers what Change models… offers one task, each change
// previewed by the engine.
func (b *appBackend) TaskRoutingOptions(id string) (appwire.TaskRoutingOptionsView, error) {
	agent, local, ok := appSplitID(id)
	if !ok {
		return appwire.TaskRoutingOptionsView{}, appwire.Errorf(appwire.CodeBadParams, "%s is not a task id", id)
	}
	_, live, endLive, err := b.borrowLive(agent)
	if err != nil {
		return appwire.TaskRoutingOptionsView{}, err
	}
	defer endLive()
	got, err := live.conn.Conv.TaskRoutingOptions(local)
	if err != nil {
		return appwire.TaskRoutingOptionsView{}, appRoutingError(err)
	}
	return appwire.TaskRoutingOptionsView{TaskID: id, Scope: got.Scope, Stronger: appPreview(got.Stronger), Best: appPreview(got.Best),
		Pick: got.Pick, Auto: got.Auto, Retry: got.Retry}, nil
}

func appPreview(p *session.TaskRoutingPreview) *appwire.TaskRoutingPreview {
	if p == nil {
		return nil
	}
	out := &appwire.TaskRoutingPreview{EstimateUSD: p.EstimateUSD, OverCap: p.OverCap, Moves: []appwire.TaskRoutingMove{}}
	if p.CapUSD > 0 {
		capUSD := p.CapUSD
		out.CapUSD = &capUSD
	}
	if p.Refusal != nil {
		out.Refused = appRefusedOf(p.Refusal)
	}
	for _, m := range p.Moves {
		out.Moves = append(out.Moves, appwire.TaskRoutingMove{Role: string(m.Seat), From: appRef(m.From, m.FromProvider), To: appRef(m.To, m.ToProvider),
			ToPrice: appTariff(m.ToPrice), ToEstimateUSD: m.ToEstimateUSD})
	}
	return out
}
