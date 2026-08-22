package session

// Standing orders: the session-side doors for the altitude work. This file is
// the seam between the engine lane and the surface lanes of the standing/v0
// wave (docs/STANDING-ORDERS.md), written by hand before the lanes started, in
// the shape standing_contract.go already proved.
//
// THE SIGNATURES ARE THE CONTRACT and the surface lanes code against them
// exactly as they stand. A door with nothing behind it answers the way it does
// when the ambient side is off — nothing, calmly — so a surface built against
// it draws nothing rather than an error.

import (
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// errStandingUnwired is what the three writing doors answer on a surface whose
// door wired no way to write. It is a fact rather than a refusal: there is
// genuinely nothing here to change, and saying so is more use than a silence
// the page would have to invent a sentence for.
var errStandingUnwired = errors.New("there is nothing here to change")

// StandingHere answers the items that stand over this conversation — its own,
// its project's, and the machine's, in that order, recent first within a
// shelf — and separately the ones the person excepted from here, so the page
// can draw its dim "not here" lines. Nil and nil when the ambient side is off.
//
// THE ORDER IS THE STORE'S AND NOT THIS FILE'S ([standing.Store.Applicable]).
// A second reading of which shelf an item sits on would be a second answer to
// the question every seam in this wave asks.
//
// THE "NOT HERE" LINES KEEP THE STORE'S OWN ORDER, newest first. They are a
// footnote under shelves the page has already grouped by altitude, and a second
// ordering law for a dim line is a second thing that has to stay true.
func (a *Agent) StandingHere() (stand []standing.Item, excepted []standing.Item) {
	store := a.standingOrders()
	if store == nil {
		return nil, nil
	}
	workspace, sessionID := a.standingPlace()
	stand, err := store.Applicable(workspace, sessionID)
	if err != nil {
		// A STORE THAT CANNOT BE READ DRAWS NOTHING. The documents themselves are
		// skipped one by one ([standing.Store.List]), so an error here is the
		// folder, and a page is not the place a person learns their disk is gone.
		return nil, nil
	}
	all, err := store.List()
	if err != nil {
		return stand, nil
	}
	for _, item := range all {
		switch item.Status {
		case standing.StatusActive:
			if !item.ExceptedFrom(workspace, sessionID) {
				continue
			}
			// The question is exactly "would this have reached here?", and the
			// contract now answers it directly ([standing.Item.Reaches]).
			if item.Reaches(workspace, sessionID) {
				excepted = append(excepted, item)
			}
		case standing.StatusPaused:
			// A PAUSED ORDER STILL STANDS OVER ITS PLACE. The resolver answers what
			// GOVERNS a place, so the seams that spend money never see a paused item
			// — but the page is where resume lives, and a row that vanished on the
			// pause keypress would make the other half of that key unreachable.
			if item.AppliesTo(workspace, sessionID) {
				stand = append(stand, item)
			}
		}
	}
	return stand, excepted
}

// StandingExcept records that the named item does not reach this place: the
// conversation when the item's altitude is wider than it, the workspace when
// the item is machine-wide. The mirror gesture — excepting a place from the
// item's own record — writes the same fact.
//
// THE EXCEPTION IS MADE AT YOUR ALTITUDE RELATIVE TO THE ITEM, which is the
// whole law and the only thing that makes one keypress unambiguous. A
// machine-wide order excepts THIS PROJECT — "not for this project" is what
// somebody means when a rule for everything fires wrongly in one repository. A
// project order excepts THIS CONVERSATION. A conversation order has nowhere
// narrower to go: it reaches only here, so "not here" would be stopping it
// altogether, and it says so rather than retiring something quietly under
// another name.
//
// IT WRITES ONE FACT AND NEVER TWO. Excepting a place already excepted is
// somebody pressing the key twice, and the answer to that is the exception they
// already have.
func (a *Agent) StandingExcept(id string) error {
	store := a.standingItems()
	if store == nil {
		return errStandingUnwired
	}
	item, err := store.Get(strings.TrimSpace(id))
	if err != nil {
		return err
	}
	workspace, sessionID := a.standingPlace()
	except := standing.Exception{At: time.Now()}
	switch item.Level() {
	case standing.AltitudeMachine:
		except.Workspace = workspace
	case standing.AltitudeProject:
		except.SessionID = sessionID
	default:
		return errors.New("this one stands in this conversation and nowhere else — stand it down to stop it")
	}
	if except.Workspace == "" && except.SessionID == "" {
		return errors.New("there is no place here to keep it out of")
	}
	if item.ExceptedFrom(except.Workspace, except.SessionID) {
		return nil
	}
	item.Exceptions = append(item.Exceptions, except)
	return store.Save(item)
}

// StandingStandDown retires the named item, at its own altitude, recording
// that the person stopped it. It is the `stand` tool's own stop, reached from
// the page instead of from a sentence ([Agent.standingMove]) — one path, so the
// two gestures can never write the reason two different ways.
func (a *Agent) StandingStandDown(id string) error {
	store := a.standingItems()
	if store == nil {
		return errStandingUnwired
	}
	item, err := store.Get(strings.TrimSpace(id))
	if err != nil {
		return err
	}
	_, _, err = a.standingMove(store, item, standing.StatusRetired)
	return err
}

// StandingPause pauses an active item or resumes a paused one, and answers
// the status it now has.
//
// THE ITEM SAYS WHICH WAY THE KEY GOES, so one key does both and the page never
// has to hold a state of its own. A retired item is not among the two: it is
// over, and setting it up afresh is a new card.
func (a *Agent) StandingPause(id string) (standing.Status, error) {
	store := a.standingItems()
	if store == nil {
		return "", errStandingUnwired
	}
	item, err := store.Get(strings.TrimSpace(id))
	if err != nil {
		return "", err
	}
	if item.Status == standing.StatusRetired {
		return item.Status, errors.New("that one is stopped for good — setting it up again is a new card")
	}
	status := standing.StatusPaused
	if item.Status == standing.StatusPaused {
		status = standing.StatusActive
	}
	moved, _, err := a.standingMove(store, item, status)
	return moved.Status, err
}

// standingOrders is the store the resolver lives on, and it is the CONCRETE
// store rather than [Agent.standingItems]'s narrow interface for one reason:
// [standing.Store.Applicable] is the one answer to what stands over a place,
// and a second one written against a fake would be a test agreeing with itself.
// Nil is the ambient side being off.
func (a *Agent) standingOrders() *standing.Store {
	if a.config.Standing == nil {
		return nil
	}
	return a.config.Standing.Store
}

// standingPlace is where this conversation is, in the two words the resolver
// asks for. IT IS THE SAME PAIR [Agent.standingOrigin] RECORDS — the workspace
// an item made here is filed under, and the session id it is stamped with — so
// something set up in this conversation is something this conversation finds.
func (a *Agent) standingPlace() (workspace, sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.standingPlaceLocked()
}

// standingPlaceLocked is [Agent.standingPlace] for the callers that already hold
// a.mu — the birth seam's block, rendered inside [Agent.startTurnLocked]. It is
// the same two lines rather than a second reading of them, because a place
// answered differently in two functions is an order that reaches a conversation
// from the page and not from the prompt.
func (a *Agent) standingPlaceLocked() (workspace, sessionID string) {
	return a.standingWorkspace(), a.sessionID()
}
