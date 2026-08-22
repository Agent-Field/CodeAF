package tui3

// THE ONE SPINNER.
//
// However many things are moving on this machine, EXACTLY ONE ROW ANIMATES —
// the most recently active — and every other live row holds a still `●`
// (docs/HOME-BRIDGE.md). It is two promises at once, and they happen to be the
// same promise:
//
//   - CALM. A dashboard is a page somebody glances at, and eleven braille cells
//     turning at thirty frames a second in three columns is a page that cannot
//     be glanced at. One moving cell says "this machine is working" exactly as
//     well as eleven do, and it says it without taking the eye off whatever the
//     person came to read.
//
//   - A FLAT WIRE. Home is a still page on a three-second beat ([homeEvery]) and
//     the paint clock is woken only while something genuinely moves
//     ([app.homeAnimating]). What that clock costs over SSH is the frame it
//     redraws, and a frame with one turning cell in it is the same frame however
//     busy the machine gets — so a machine with twenty tasks out costs the wire
//     exactly what a machine with one costs.
//
// ── WHICH ROW ───────────────────────────────────────────────────────────────
//
// THE MOVING ZONE HOLDS THE SPINNER WHENEVER IT IS DRAWN. That zone exists to
// answer "what is running everywhere" (homeattention.go), and the one cell on the
// screen that says "right now" belongs in the one place whose whole subject is
// right now. Only when there is no such zone — under a search, on a machine with
// no conversations — does the spinner fall to the list's own rows.
//
// AND WITHIN A ZONE IT IS THE MOST RECENTLY ACTIVE, which is the one order a
// person can verify: the thing that started last is the thing they just did, and
// a spinner that lands on it is the screen acknowledging the keystroke they are
// still thinking about. The zone's own order is BUSIEST first, deliberately
// ([homeZones]) — the top of the list and the moving cell answer two different
// questions, and neither is the other's summary.

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// spinAt is the line THE ONE SPINNER belongs to, or [homeRest] for a page with
// nothing moving on it at all.
//
// It is settled with the lines themselves ([homeView.buildFor]) rather than at
// the draw, for [homeView.wide]'s reason: the paint asks it once per row and
// thirty times a second, and a choice remade per cell would be this file paying
// for its own law.
func (h *homeView) spinAt() int {
	if at := h.newestMoving(true); at >= 0 {
		return at
	}
	return h.newestMoving(false)
}

// newestMoving is the freshest moving row among the zones' rows, or among
// everything that is not one.
func (h *homeView) newestMoving(zoned bool) int {
	best, when := homeRest, time.Time{}
	for at, line := range h.lines {
		if (line.zone != nil) != zoned {
			continue
		}
		stamp, moving := homeMovingAt(line)
		if !moving {
			continue
		}
		if best < 0 || homeSpinNewer(when, stamp) {
			best, when = at, stamp
		}
	}
	return best
}

// homeMovingAt reports that one line stands for something MOVING THIS INSTANT,
// and when that movement began.
//
// THE THREE KINDS ARE THE THREE THE ZONE ITSELF GATHERS ([homeView.attentionMoving]),
// read here off the row rather than off the world so that a line and the mark it
// wears can never disagree: a conversation with nodes out, an errand mid-turn,
// and a zone row for either of them. A standing item that is firing is moving too
// and is in that zone, and it reaches this through its zone row — the item row in
// the list below has never turned and does not start now ([StandingItemRow]).
func homeMovingAt(line homeLine) (time.Time, bool) {
	if line.zone != nil {
		return line.zone.at, line.zone.word == attentionMovingWord
	}
	switch line.kind {
	case homeSession:
		if line.row.NeedsPerson() || line.row.Tasks.Running == 0 {
			return time.Time{}, false
		}
		return attentionMovingSince(line.row), true
	case homeExchangeRow:
		if line.ex == nil || !line.ex.working || line.ex.waiting() {
			return time.Time{}, false
		}
		return line.ex.turnBegan, true
	}
	return time.Time{}, false
}

// homeSpinNewer reports that the second stamp is a fresher claim on the one
// spinner than the first: later wins, and a stamp NOBODY RECORDED loses to any
// stamp at all — which is [attentionOlder]'s own answer about the unknown, read
// from the other end.
func homeSpinNewer(best, cand time.Time) bool {
	if best.IsZero() != cand.IsZero() {
		return best.IsZero()
	}
	return best.Before(cand)
}

// homeSpins reports that this line is the one that animates this frame.
//
// THE LINEAR TIER NEVER ANIMATES, which is the objection it makes everywhere: a
// claim repeated thirty times a second is heard thirty times a second by a
// surface being read aloud ([glyphRunASCII]'s block). It answers no here rather
// than in each of the three paints, so there is one place the exception lives.
func (a *app) homeSpins(at int) bool {
	return !a.linear && at >= 0 && at == a.home.spin
}

// homeSpinGlyph is the moving cell itself, on the house grid so it never beats
// against the spinner in the status line or the one on a task's row
// ([spinnerStep]).
func (a *app) homeSpinGlyph() string {
	if a.pal.ascii {
		return glyphRunASCII
	}
	return tokens.Spinner(a.paints / spinnerStep)
}
