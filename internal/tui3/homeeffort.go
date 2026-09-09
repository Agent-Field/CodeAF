package tui3

// ── `ctrl+v` ON HOME: ONE SCOPE, ON THE CARD THAT HAS A RUNG ────────────────
//
// Home draws one card at a time and the card is always about a row under the
// cursor. So the chord needs no state of its own to know what it means: it asks
// the screen the same question the screen asked to draw the card,
// [app.homeSubject], and moves the rung of whatever came back.
//
//	a standing item       that item's `does.effort` (internal/standing)
//
// EVERY OTHER SUBJECT IS LEFT ALONE AND SAYS NOTHING. A conversation's rung is
// that conversation's own sticky setting, kept in its session folder and moved
// from inside it; a project is not a thing that thinks. The card's legend does
// not name this key there, and pressing it does nothing at all — which is what
// the design language asks of a key with no door under it, rather than a message
// explaining why the screen declined.
//
// IT USED TO MOVE A SECOND RUNG. Home had a resting state — the cursor on no row
// at all, the column a card about the machine — and this chord moved the
// install's own default from it. That state is retired (`↑` off the top row
// reaches the tab bar now, pages.go's [barCursor]), so the install's rung is
// moved where it has always also been moved: the `thinking` row of the settings
// panel, which is the writer both roads went through anyway.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// cycleHomeEffort is the key. It answers a command because the emphasis on the
// clause it just moved needs the two catch-up ticks to come back down.
func (a *app) cycleHomeEffort() tea.Cmd {
	subject, ok := a.homeSubject()
	if !ok {
		return nil
	}
	if subject.kind == bandKindItem {
		return a.cycleItemEffort(subject.item.Item)
	}
	return nil
}

// cycleItemEffort moves one standing item's rung one step up the wheel.
//
// AN ITEM'S OWN RUNG IS THE ONLY ONE THAT REACHES ITS FIRINGS, and this key is
// how it gets one. Nothing else on the machine reaches them — a standing run
// does not inherit the conversation's dial or the install's row
// (internal/session's standing_run.go), so an item nobody has dialled asks for
// nothing at all, and a check that repeats forever and answers to nobody is not
// turned into a deep pass by a rung somebody set months ago in a conversation.
// So the first press on a card is a deliberate gesture raising ONE item, and the
// card is where it is made because the card is where a person can see what the
// item is before they decide it deserves thinking about.
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
