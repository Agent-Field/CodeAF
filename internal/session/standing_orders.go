package session

// Standing orders: the session-side doors for the altitude work. This file is
// the seam between the engine lane and the surface lanes of the standing/v0
// wave (docs/STANDING-ORDERS.md), written by hand before the lanes started, in
// the shape standing_contract.go already proved.
//
// EVERY BODY IN THIS FILE IS A STUB the engine lane (so/engine) replaces; the
// signatures are the contract, and the surface lanes (so/page, so/threshold)
// code against them exactly as they stand. A stub answers the way the built
// door answers when the ambient side is off — nothing, calmly — so a surface
// built against it draws nothing rather than an error.

import (
	"errors"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// StandingHere answers the items that stand over this conversation — its own,
// its project's, and the machine's, in that order, recent first within a
// shelf — and separately the ones the person excepted from here, so the page
// can draw its dim "not here" lines. Nil and nil when the ambient side is off.
func (a *Agent) StandingHere() (stand []standing.Item, excepted []standing.Item) {
	// STUB — so/engine replaces this with Store.Applicable over the
	// conversation's workspace and session id.
	return nil, nil
}

// StandingExcept records that the named item does not reach this place: the
// conversation when the item's altitude is wider than it, the workspace when
// the item is machine-wide. The mirror gesture — excepting a place from the
// item's own record — writes the same fact.
func (a *Agent) StandingExcept(id string) error {
	// STUB — so/engine replaces this.
	return errors.New("standing orders are not built yet")
}

// StandingStandDown retires the named item, at its own altitude, recording
// that the person stopped it.
func (a *Agent) StandingStandDown(id string) error {
	// STUB — so/engine replaces this.
	return errors.New("standing orders are not built yet")
}

// StandingPause pauses an active item or resumes a paused one, and answers
// the status it now has.
func (a *Agent) StandingPause(id string) (standing.Status, error) {
	// STUB — so/engine replaces this.
	return "", errors.New("standing orders are not built yet")
}
