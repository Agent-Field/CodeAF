package tui3

// THE ATTENTION RULES — WHICH ARE THE SORT ORDER NOW.
//
// Home's left column used to carry TWO STRIPS over the list: `needs you` and
// `moving`, each gathering the same conversations, items and errands the list
// below it already held, each with its own label, teaching line, fold and mark.
// At the widest tier they left the list entirely and took a column of their own.
//
// THEY ARE GONE, AND WHAT THEY WERE FOR IS NOT. A summary standing over a list
// earns its cells when the list is in a different order — a project tree sorted
// by project genuinely cannot answer "what needs me". The list is one flat
// ranked list now (SCREEN 1a, homeswitch.go), and what it is ranked BY is
// exactly what the two strips gathered: needs-you first, then moving, then
// quiet. A strip over that list is the same reading twice, and every row it held
// was already one row further down the screen.
//
// So what survives here is the RULES, and every one of them is now read by
// [readSwitcher] instead of by a strip:
//
//   - NEEDS-YOU ORDER IS WAITED-LONGEST FIRST ([attentionOlder],
//     [attentionWaitedSince]). It is the only order that cannot be argued with:
//     the thing that has been stopped longest has cost the most already. THE
//     DESTRUCTIVE PIN IS STILL NOT BUILT — see [attentionWaitedSince].
//   - MOVING IS BUSIEST FIRST, OLDEST WITHIN A TIER ([attentionMovingSince]).
//   - AN UNKNOWN STAMP GOES LAST and never to the top of a list ordered by how
//     long something has been standing still.
//
// EVERY FACT IS ONE HOME ALREADY READ. The world, the standing bands and the
// errands are the same three readings the list is built from, on the same
// three-second beat ([homeEvery]). This file opens nothing, stats nothing and
// asks the engine for nothing.

import (
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// attentionOlder is the age comparison the needs-you rank sorts with. A stamp
// nobody recorded is not the oldest thing on the machine — it is an unknown, and
// it goes last rather than to the top of a list ordered by how long something has
// been standing still.
func attentionOlder(a, b time.Time) bool {
	if a.IsZero() != b.IsZero() {
		return b.IsZero()
	}
	return a.Before(b)
}

// attentionWaitedSince is when a conversation's question was put.
//
// [session.PresenceQuestion.Asked] is the stamp the session that is waiting
// wrote, which is exactly the fact this rank sorts on. A window on an older
// build, or a lane whose question describes no card, records none — and the
// fallback is when the PERSON LAST SPOKE, which is not the wait but is a bound
// on it: the question came after they spoke, so the row can never claim to have
// waited longer than it has.
//
// ── AND THE DESTRUCTIVE PIN IS NOT BUILT ────────────────────────────────────
//
// The design asks for one exception to waited-longest-first: a consent card for
// something DESTRUCTIVE pins to the top for as long as it waits. Nothing on this
// machine can say which one that is. The classification exists — internal/
// approval's critical-command table, [approval.AlwaysAsks] — but it never
// reaches a question: [session.PresenceQuestion] carries a kind, an id, the
// options and the line `needs your ok to run bash`, which is the TOOL's name and
// not the command's text (session's consent.go). So this surface could only
// guess, and a pin that guessed would put a `rm -rf` row and a `git status` row
// in the same place at the top of the one list a person trusts.
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. The pin lands the day the
// engine writes the fact down, and not before.
func attentionWaitedSince(row session.SessionRow) time.Time {
	if asked := row.Presence.Question.Asked; !asked.IsZero() {
		return asked
	}
	return row.At
}

// attentionMovingSince is when a live conversation's work began: the oldest node
// it has out, or — for one that is merely mid-turn — when the person last spoke,
// which is when a turn begins ([session.SessionRow.At]).
//
// A TURN A WAKE STARTED IS OLDER THAN ITS AGE SAYS. Nothing records when such a
// turn began, and the last thing said is the nearest stamp there is; it is never
// NEWER than the truth, which is the direction an approximation on this screen
// is allowed to be wrong in.
func attentionMovingSince(row session.SessionRow) time.Time {
	var oldest time.Time
	for _, out := range row.Presence.RunningTasks {
		if out.StartedAt.IsZero() {
			continue
		}
		if oldest.IsZero() || out.StartedAt.Before(oldest) {
			oldest = out.StartedAt
		}
	}
	if !oldest.IsZero() {
		return oldest
	}
	if row.Tasks.Running > 0 {
		// Nodes are out and none of them recorded a start — a session under a
		// build older than the stamp. It draws no age rather than borrowing the
		// conversation's, which is [app.homeTaskTail]'s own choice about the same
		// missing fact.
		return time.Time{}
	}
	return row.At
}

// attentionErrandSince is when an errand's card arrived, as nearly as an errand
// records it: the turn that produced the card is the one that was in flight when
// it appeared, and an errand is one short turn (homeexchange.go). It is the same
// stamp the row's own working clock counts from, so the two never disagree.
func attentionErrandSince(ex *homeExchange) time.Time {
	if !ex.turnBegan.IsZero() {
		return ex.turnBegan
	}
	if !ex.said.IsZero() {
		return ex.said
	}
	return ex.began
}

// ── the cursor ──────────────────────────────────────────────────────────────

// pointAt puts the cursor on the first line a test accepts, and leaves it where
// it is when no line matches at all.
//
// IT USED TO PREFER A ROW OUTSIDE THE ZONES, because one live object had two
// rows on the screen — a strip's and the list's — and the restores that find a
// thing by its identity would otherwise lift the cursor into a strip on every
// keystroke and every three-second rescan. WITH ONE FLAT LIST THERE IS ONE ROW
// PER THING, so the preference has nothing left to prefer and the walk is the
// plain one it always wanted to be.
func (h *homeView) pointAt(is func(homeLine) bool) {
	for at, line := range h.lines {
		if is(line) {
			h.cursor = at
			return
		}
	}
}
