package session

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/roles"
)

const freeRoutesOffAction = "free routes are off · choose a paid model or enable free routes in /crew"

// The run marks the actual seat on each call. A checker sharing a worker's
// model does not acquire that worker's pin, nor revoke its explicit consent.
func (c crewSeatCompleter) freeRouteBlocked(ctx context.Context, current string) bool {
	if config.CrewFreeRoutesAt(c.agent.config.ProfileDir) {
		return false
	}
	decision := c.run.crew.current()
	if seat := crewSeatOf(ctx); seat != "" {
		pick := decision.Seat(seat)
		if pick.Send != current {
			return crewroute.IsFree(current)
		}
		return pick.Send == current && !pick.Pinned && isFreeCrewPick(pick)
	}
	// Unmarked callers have no seat provenance; do not infer consent from a
	// different seat. Production crew calls are marked by SeatCompleter.
	for _, pick := range decision.Crew {
		if pick.Send == current && !pick.Pinned && isFreeCrewPick(pick) {
			return true
		}
	}
	return false
}

func isFreeCrewPick(pick crewroute.Pick) bool {
	return pick.Kind == crewroute.Free || crewroute.IsFree(pick.Send)
}

// Helpers have no crew seat. A stale automatic free choice is allowed only if
// a current explicit helper row or role pin still names that same route.
func (c Config) freeHelperAllowed(model string) bool {
	if !crewroute.IsFree(model) || config.CrewFreeRoutesAt(c.ProfileDir) {
		return true
	}
	// RolesSource may retain a previous automatic choice when a later settings
	// parse fails. A resolved model is not proof of current explicit consent.
	project, err := config.LoadProjectConfig(c.Workspace)
	if err != nil {
		return false
	}
	rawPins, err := project.ResolveString(c.ProfileDir, config.KeyModelRoles)
	if err != nil {
		return false
	}
	pins, err := config.ParseModelRoles(rawPins)
	if err != nil {
		return false
	}
	projectLow, hasProjectLow, err := project.String(config.KeyTierLowModel)
	if err != nil {
		return false
	}
	for _, named := range pins {
		if sameFreeModel(named, model) {
			return true
		}
	}
	if hasProjectLow && sameFreeModel(projectLow, model) {
		return true
	}
	for _, tier := range []string{config.ModelTierReflex, config.ModelTierLow} {
		if tier == config.ModelTierLow && hasProjectLow {
			continue
		}
		seat := config.TierSeatAt(c.ProfileDir, tier)
		if seat.Source == config.SeatPinned && sameFreeModel(seat.Model, model) {
			return true
		}
	}
	return false
}

func sameFreeModel(left, right string) bool {
	left, _ = roles.SplitEffort(strings.TrimSpace(left))
	right, _ = roles.SplitEffort(strings.TrimSpace(right))
	return strings.EqualFold(left, right)
}
