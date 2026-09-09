package tui3

// ── THE EFFORT LADDER, BOUND TO THE SURFACE A PERSON IS STANDING ON ──────────
//
// THE CONTROL BINDS TO THE SURFACE YOU STAND ON, AND ONE CHORD MOVES IT.
//
// internal/effort holds the ladder and internal/session, internal/config and
// internal/standing hold the three scopes a person can set a rung at. This file
// is the surface's half of all three, and the whole of what it decides is that
// `ctrl+v` means the SAME VERB everywhere and a DIFFERENT SCOPE everywhere:
//
//	the cursor on a task        the task's own rung   (session's SetTaskEffort)
//	a standing item's card      that item's rung      (standing's SetStandingEffort)
//
// A THIRD SCOPE USED TO BE HERE AND ITS SURFACE IS GONE. Home had a resting
// state — the cursor on no row at all, the column a card about the machine — and
// `ctrl+v` moved the INSTALL'S default from it. `↑` off the top row reaches the
// tab bar now (pages.go's [barCursor]), so there is no such card; the install's
// rung is moved from the `thinking` row of the settings panel, which is the
// writer that road went through anyway (config's WriteDefaultEffort).
//
// One chord and two scopes is not two bindings that happen to share a key.
// It is the same binding: the rung this screen is showing is the rung the chord
// moves, and a person who learns it once on a task knows it on home and on a
// standing item without being told. A second chord per scope would have been
// three things to remember for one idea, and the chord budget on this surface is
// spent (docs/DESIGN-LANGUAGE.md's refusal of keyboard speed at
// discoverability's expense — every one of the three surfaces below carries a
// visible legend naming this key beside the rung it moves).
//
// ── THE WHEEL IS THE LADDER, CHEAPEST FIRST, AND IT WRAPS ──
//
// [effortNext] walks [effort.Rungs] and comes back to the bottom off the top.
// ABSENCE IS WHERE THE WHEEL STARTS AND NOT A STOP ON IT: a scope nobody has set
// reads as nothing (the emptiness law) and the first press lands on the cheapest
// rung, but no number of further presses ever puts a rung BACK to absence.
// Clearing one is a deliberate act with real meaning — it hands the scope back
// to whatever stands above it. A conversation rung and a standing item's rung
// therefore keep this wheel; the two scopes the surface itself must be able to
// clear use [effortNextClearing] below.
//
// ── AND THE RUNG IS FURNITURE UNTIL IT MOVES ──
//
// On every surface below the rung is drawn as a quiet clause — `thinking high` —
// in whatever the surface's own dim tier is, because it is a standing fact about
// the thing and not news. When it MOVES, a surface that can lift the clause on
// its own steps it up the reading ladder and lets it come back down on the fade
// the status line's own numbers already use (render.go's [hudFresh] and
// [hudWarm]): ink while it is news, muted while it is recent, dim forever after.
// No new colour, no new timing constant, and no accent — the budget is one lit
// element per screen and a fact that has just changed is not it
// (docs/DESIGN-LANGUAGE.md's THE ACCENT BUDGET).
//
// HOME'S TWO CARDS CAN; THE TASK LINES CANNOT, and it is the lines and not the
// rung that decide. The room's header and the roster's telemetry row are built
// plain and painted once, whole, because a hue nested inside a hue ends at the
// inner one's reset ([app.roomHeadWord] states it). So the task scope emphasizes
// a move the way this surface has always emphasized a decision about one piece
// of work — a note in the conversation, with the rung painted as the datum in it
// (taskeffort.go's [app.cycleTaskEffort], room.go's [app.retargetTask]).

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// effortClauseWord leads the clause on every surface. It is the word the
// settings row already labels this setting with (internal/config's settings.go
// spells the row `thinking`), so the fact a card states and the row a person
// finds in the panel are the same word rather than two.
const effortClauseWord = "thinking "

// effortKeyClause is what a legend says beside it, in the hint grammar every
// other legend on this surface is written in — `chord verb` (payload.go).
//
// The verb is `think harder` and not `thinking` because a legend names what the
// key DOES; four presses out of five climb, and the fifth wraps back to the
// bottom, which is the honest shape of a wheel and is said in full in the
// manual rather than in five cells of a card.
const effortKeyClause = "ctrl+v think harder"

// effortNext is one step of the wheel. See the header for why absence is a
// starting point and never a stop.
func effortNext(rung effort.Rung) effort.Rung {
	for at, step := range effort.Rungs {
		if step == rung {
			return effort.Rungs[(at+1)%len(effort.Rungs)]
		}
	}
	// Absence, and equally a rung written by a build that knew a word this one
	// does not: both mean "nobody here has chosen", and the cheapest rung is
	// where a wheel that has not been turned yet begins.
	return effort.Rungs[0]
}

// effortNextClearing is one step of the wheel for a scope a person can CLEAR
// from the surface: the same climb, except that the top rung comes back to
// absence instead of to the bottom.
//
// TWO SCOPES ARE CLEARABLE AND BOTH ARE CLEARED HERE. A task's own rung and the
// level dialled onto a model are the two scopes whose absence means something:
// hand this piece of work, or this model, back to whatever stands above it. The
// surface is the only door that sets either, so it has to be the door that
// clears them. The conversation rung above ([effortNext]) is cleared from the
// settings row instead, which is why it keeps the wheel that never lands on
// absence.
func effortNextClearing(rung effort.Rung) effort.Rung {
	if rung == effort.Rungs[len(effort.Rungs)-1] {
		return effort.None
	}
	return effortNext(rung)
}

// effortClause is the quiet fact one surface states about its own scope, and ""
// for a scope nobody has set — which the emptiness law draws as nothing at all
// rather than as a word for zero.
func effortClause(rung effort.Rung) string {
	if rung == effort.None {
		return ""
	}
	return effortClauseWord + rung.String()
}

// ── the flash ───────────────────────────────────────────────────────────────

// effortMoved is the ONE rung this window has most recently moved, and when.
//
// IT IS ONE AND NOT A MAP because only one rung can be the newest fact on a
// screen. A person moves the thing under their hand; the previous emphasis
// belonged to a scope they have since walked away from, and two lifted clauses
// would be two answers to "what just changed".
type effortMoved struct {
	// where is the surface's own name for the scope, so a clause can ask whether
	// the emphasis is ITS: a standing item's id for an item, and
	// [effortScopeConversation] for the chip above the message box
	// (effortchip.go). They cannot collide — the sentinel leads with a NUL no id
	// can carry.
	where string
	at    time.Time
}

// effortScopeConversation is the chip's name in that namespace: a word no id
// can be, so a scope with a real id and this one can never be mistaken for one
// another.
//
// THE CONVERSATION'S CHIP SHARES THE FIELD AND NOT THE TIMING. It records its
// move here so that only ONE rung on this window can be the newest fact — moving
// a task's rung takes the emphasis off the chip and vice versa, which is the law
// [effortMoved] is written for — but the emphasis it then wears is its own: the
// tray has one cell to say it in and says it with the selected ground for two
// seconds ([app.effortFlashing]), where a card has a whole clause and lets it
// fade down the reading ladder over the status line's ten.
const effortScopeConversation = "\x00conversation"

// markEffortMoved records the move and schedules the two wakeups the emphasis
// needs to come back down.
//
// THE TICKS ARE THE STATUS LINE'S OWN ([fadeTicks]) and there is deliberately no
// ticker: a surface with nothing happening on it wakes twice and then stops
// (app.go's [hudFadeMsg]).
func (a *app) markEffortMoved(where string) tea.Cmd {
	a.effortLit = effortMoved{where: where, at: a.now()}
	return fadeTicks()
}

// effortInk is the tier one scope's clause is drawn in this frame: the reading
// ladder's ramp for the scope that just moved, and the quiet tier for every
// other clause on the screen and for every frame after the fade.
func (a *app) effortInk(where string, pal palette) func(string) string {
	if where == "" || a.effortLit.where != where || a.effortLit.at.IsZero() {
		return pal.dim
	}
	switch age := a.now().Sub(a.effortLit.at); {
	case age < hudFresh:
		return pal.ink
	case age < hudWarm:
		return pal.muted
	default:
		return pal.dim
	}
}

// ── the install's own rung ──────────────────────────────────────────────────

// effortProfile reports the profile this window writes settings into, and false
// for a window that has none.
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. A surface with no profile
// draws no rung at all, rather than drawing one it could not move.
//
// AND THE WINDOW WITH NO PROFILE IS THE HOSTED ONE, WHICH IS NOT THE ONE WITH AN
// EMPTY PROFILE DIRECTORY. This used to answer false on an empty
// [app.profileDir], which is what AFORGE_PROFILE_DIR being unset looks like —
// so the install's own rung was drawn on the rare launch that exported it and
// for nobody else (#322). The empty string has always meant this process's own
// profile in the state root ([config.ProfilePath]), and that profile is exactly
// the install whose rung this line states. A connection's is another machine's,
// and it is the one asked nothing.
func (a *app) effortProfile() (string, bool) {
	return a.profileDir, !a.hosted()
}
