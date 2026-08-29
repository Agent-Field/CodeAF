package shaped

import (
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ── HOW MUCH ROOM A SHAPED ANSWER GETS ────────────────────────────────────────
//
// Every ceiling this system ever sent a structured call was a literal: 8192 in
// the planner, a reserve-eighth in the delivery gate, eight thousand plus an
// echo in the intent compiler. Each was right about the call its author had in
// front of them and wrong about the next one — and the planner's was wrong in
// the direction that costs a whole run, because a fan-out asking for five parts
// was given the room for one verdict and cut off in the middle of the second
// part. The cap is not a property of the pass. It is a property of THE ASK.
//
// So it is derived, from three things and nothing else:
//
//	room = one object × how many objects were asked for  +  what must be echoed
//
// ONE OBJECT is a share of the completion reserve — the room the whole tree
// keeps for a reply and its reasoning, AFORGE_COMPLETION_RESERVE, which an
// operator may move and which everything else here moves with. A form-shaped
// answer genuinely is small beside a leaf's completion, so it takes a fraction
// rather than the whole; the floor under that fraction is what stops a lowered
// reserve from starving the deliberation a reasoning model does before it writes
// a word. The share and the floor are the delivery gate's own numbers, which
// were measured there and are now stated once for everybody.
//
// HOW MANY OBJECTS is the ask's own figure — the fan-out's permitted width, and
// one everywhere else. Where a prompt tells the model how many parts it may
// return, the same constant feeds both, so the sentence the model reads and the
// room it is given cannot drift apart. This is the term that was missing, and
// the only one that changes a number anybody had already measured: every
// single-object call in the system comes out of this function with exactly the
// ceiling it had before.
//
// WHAT MUST BE ECHOED is the intent compiler's discovery, generalised. Its brief
// carries the user's instruction back twice — inside the goal and under
// "Verbatim request:" — so a reply to a long ask cannot be short however small
// the schema is, and a ceiling that ignored it cut two GAIA questions in half
// for a third of a cent each. A pass whose answer quotes nothing pays nothing
// for this term.
//
// Then the memo has the last word. A model that has been WATCHED overrunning a
// lane's ceiling is never sent that ceiling again in this profile: the seam
// raises it to what the model actually spent, doubled, which is the same
// arithmetic every retry in this tree has always used. Evidence beats
// derivation, because the derivation is an argument and the memo is a
// measurement. Nothing here shrinks a ceiling: a model that answered inside its
// room teaches nothing, and forgetting a cut would buy the same failure twice.
//
// Everything is bounded above by the reserve itself, which is the one figure an
// operator states about how large a completion may be.
const (
	// objectShare is how much of a completion reserve one form-shaped answer
	// takes. Moved here from internal/revision, where it was measured: a
	// judgement genuinely is small, and a cap sized for the visible object alone
	// was spent entirely on deliberation by reasoning models, so the object gets
	// a fraction of the reserve rather than a count of its own fields.
	objectShare = 8

	// objectFloor keeps that fraction from landing back where it started when an
	// operator lowers the reserve. It is the floor under one object, never under
	// the whole answer.
	objectFloor = 4096

	// echoBytesPerToken and echoCopies are the intent compiler's echo term. A
	// third of a byte per token is the conservative reading of English text, and
	// the material comes back twice: once worked into the answer and once
	// verbatim.
	echoBytesPerToken = 3
	echoCopies        = 2
)

// Room is the ceiling one ask is sent with. See the derivation above.
//
// It takes the model rather than reading it from the context because the caller
// has already resolved it — Answer asks the router's slot once — and because a
// test can then state the memo's key without standing up a router.
func Room(ask Ask, model string) int {
	answers := ask.Answers
	if answers < 1 {
		answers = 1
	}
	room := oneObject()*answers + echo(ask.Echo)
	if learned := provider.WidestAnswerCut(model, ask.Lane); learned > 0 {
		// Doubled, for the same reason every other retry in this tree doubles:
		// the figure is what the model spent when it ran out, so it is a lower
		// bound on what it needed, and giving it exactly that much again buys
		// the same cut.
		if raised := learned * 2; raised > room {
			room = raised
		}
	}
	if ceiling := reserve(); room > ceiling {
		room = ceiling
	}
	return room
}

// oneObject is the room a single form-shaped answer gets.
func oneObject() int {
	room := reserve() / objectShare
	if room < objectFloor {
		room = objectFloor
	}
	if ceiling := reserve(); room > ceiling {
		// An operator who set the reserve below the floor has stated what the
		// room is. The floor guards against reasoning; it is not a licence to
		// overrun a reserve somebody named on purpose.
		room = ceiling
	}
	return room
}

// echo is the room the answer needs for material it has to carry back verbatim.
func echo(material string) int {
	if material == "" {
		return 0
	}
	return echoCopies * len(material) / echoBytesPerToken
}

// doubled is the room a re-ask gets: twice what the failed attempt actually
// spent, never less than the ceiling it already had, never more than the
// reserve. This is plan.retryTokenBudget and revision.retryVerdictTokens, which
// were the same arithmetic written twice with different literals, said once.
func doubled(spent, ceiling int) int {
	room := spent * 2
	if room < ceiling*2 {
		room = ceiling * 2
	}
	if limit := reserve(); room > limit {
		room = limit
	}
	return room
}

// reserve is the one figure an operator states about how large a completion may
// be. Read through ctxbudget every time rather than cached, because a test that
// moves it expects the next call to see it.
func reserve() int { return ctxbudget.CompletionReserve() }

// ObjectRoom is the room one form-shaped answer gets, for the one caller whose
// reply is not an object at all.
//
// The planner's brief pass writes prose, so there is no schema to derive from
// and nothing to repair when it arrives — but it travels under the same ceiling
// as everything else in that package, because left unset the ceiling is the leaf
// completion reserve and a model that loops runs to it. Exported so that figure
// is stated in one place rather than in two that will drift.
func ObjectRoom() int { return oneObject() }
