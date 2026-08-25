package tui3

// ── `ctrl+v` ON HOME: TWO SCOPES, ONE CARD AT A TIME ────────────────────────
//
// Home draws one card at a time and the card is always about something
// (homemachine.go): the row under the cursor, or — at rest, with the cursor on
// no row at all — THE MACHINE ITSELF. So the chord needs no state of its own to
// know what it means. It asks the screen the same question the screen asked to
// draw the card, [app.homeSubject], and moves the rung of whatever came back:
//
//	the machine's card    the install's own `effort` row (internal/config)
//	a standing item       that item's `does.effort` (internal/standing)
//
// EVERY OTHER SUBJECT IS LEFT ALONE AND SAYS NOTHING. A conversation's rung is
// that conversation's own sticky setting, kept in its session folder and moved
// from inside it; a project is not a thing that thinks. Neither card's legend
// names this key, and pressing it there does nothing at all — which is what the
// design language asks of a key with no door under it, rather than a message
// explaining why the screen declined.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// The words home says on its own message line after the key lands. They are
// quoted in internal/manual/chat/home.md exactly as they are spelled here.
const (
	// homeEffortMachineWord follows the rung when the install's own default has
	// moved — `thinking high · the default on this machine` — because the card
	// at rest has no name of its own and the sentence has to say what it changed.
	homeEffortMachineWord = " · the default on this machine"
	// homeEffortNoProfile is a window with nowhere to write the row. It is the
	// item's own refusal said about the install, in the same shape.
	homeEffortNoProfile = "this window cannot change it"
)

// cycleHomeEffort is the key. It answers a command because the emphasis on the
// clause it just moved needs the two catch-up ticks to come back down.
func (a *app) cycleHomeEffort() tea.Cmd {
	subject, ok := a.homeSubject()
	if !ok {
		return nil
	}
	switch subject.kind {
	case bandKindMachine:
		return a.cycleDefaultEffort()
	case bandKindItem:
		return a.cycleItemEffort(subject.item.Item)
	}
	return nil
}

// cycleDefaultEffort moves the install's rung one step up the wheel.
//
// IT IS THE SETTINGS ROW'S OWN WRITER AND NOT A SECOND ONE. Everything a person
// could do here they could do in the `thinking` row of the settings panel, and
// both go through [config.WriteDefaultEffort] — which validates against the same
// choices and refuses in the same words. What this key buys is that the machine's
// card is where a person is ALREADY LOOKING when the question occurs to them.
func (a *app) cycleDefaultEffort() tea.Cmd {
	h := &a.home
	dir, ok := a.effortProfile()
	if !ok {
		h.say(homeEffortNoProfile, "")
		return nil
	}
	next := effortNext(config.DefaultEffortAt(dir))
	if err := config.WriteDefaultEffort(dir, next); err != nil {
		// The writer's own sentence, kept: a row that redrew as moved over a
		// profile that refused the write would be the screen lying about the disk
		// (homestanding.go's [app.homeItemWrite] states the same rule).
		h.say(err.Error(), "")
		return nil
	}
	h.say(effortClause(next)+homeEffortMachineWord, "")
	return a.markEffortMoved(machineSubjectID)
}

// cycleItemEffort moves one standing item's rung one step up the wheel.
//
// SENTINELS STAY LOW UNLESS SOMEBODY SAYS OTHERWISE, and this key is that
// somebody. An item with no rung of its own fires at the standing role's own
// floor however deep the install is dialled (internal/effort's resolver), because
// a check that repeats forever and answers to nobody must not be a deep pass. So
// the first press on a card is a deliberate gesture raising ONE item off that
// floor, and the card is where it is made because the card is where a person can
// see what the item is before they decide it deserves thinking about.
func (a *app) cycleItemEffort(item standing.Item) tea.Cmd {
	h := &a.home
	if a.stands.SetEffort == nil {
		h.say(homeItemNoStore, "")
		return nil
	}
	rung, _ := effort.Parse(item.Does.Effort)
	next := effortNext(rung)
	if err := a.stands.SetEffort(item.ID, next); err != nil {
		h.say(err.Error(), "")
		return nil
	}
	h.say(effortClause(next)+" · "+strings.TrimSpace(item.Words), "")
	// THE CARD IS REDRAWN FROM THE STORE AND NEVER FROM THIS FUNCTION'S OPINION.
	// The document on disk is what the clause reads, so a write that landed and a
	// clause that says so are the same fact rather than two.
	a.refreshHome()
	return a.markEffortMoved(item.ID)
}
