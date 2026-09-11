package control

import (
	"strings"
	"sync"
	"time"
)

// ── ONE BUDGET, ONE ORDER, ONE OWNER ────────────────────────────────────────
//
// THE MEASURED FAILURE (docs/design/recovery/DESIGN.md §2 problem 1). Eleven
// controllers on one request path each owned a budget none of the others could
// see: three transport faults, six or sixty paced sends, four arms, one ladder
// arm, seven relaxation rungs, two models, and four different wall clocks over
// the top of them. Their product — 3 × 4 × 5 × 6 × 9 — is nobody's number, and
// the call census of 2026-09-10 measured what it produced: chains of sixteen
// and seventeen identical sends to one machine, running eleven minutes, ending
// refused.
//
// THE LAW: A CALL HAS ONE DEADLINE AND ONE LIST OF MOVES. [Plan.Deadline] is
// the whole of how long, in the person's own time (`lane.Role`'s patience);
// [Plan.Moves] is the whole of what has already been tried, shared by every arm
// of the same question so two arms can never demand one machine; and [Next] is
// the whole of what comes next. Nothing below the plan owns a retry count.
//
// THIS FILE HOLDS NO CLOCK, exactly as the rest of the package does not: every
// question takes the moment as an argument. That is what lets a scenario
// written in minutes be tested in microseconds.

// Move is one thing a failed call may do next: a different machine, the same
// machine after the wait it asked for, or the same machines with one field of
// the request taken off. It is the (machine, shape, model) triple the design
// names, plus the one legal wait.
//
// IT NAMES THE MACHINE WE EXPECT AND NEVER THE MACHINE WE COMMAND. `Lane` is
// read for what the person is told (`trying another machine · 2 of 5`) and for
// the claim that stops two arms of one race demanding the same endpoint; what
// actually reaches the wire is the REFUSING machines excluded, because
// `provider.order` is advisory once `allow_fallbacks` is true and R1 measured
// the router fanning past it (#850).
type Move struct {
	// Kind is which of the four the design's order this move is.
	Kind MoveKind
	// Model is the model this move is on. The dispatcher never changes it —
	// there is exactly one model hop in this build and it belongs to the
	// session (docs/design/recovery/DESIGN.md §4) — so every move of one plan
	// carries the same name, and it is here so a row can say which.
	Model string
	// Lane is the machine, spelled as the wire spells it.
	Lane string
	// Shape is which rung of the relaxation ladder the request is in, zero
	// being the request as the caller wrote it. It is an OPAQUE COUNT here on
	// purpose: what a rung takes off is the transport's business
	// (internal/provider's relaxSet), and this package compares shapes for
	// equality and orders them, nothing more.
	Shape uint8
	// Wait is the comeback the machine itself named, and it is non-zero on
	// exactly one kind of move ([MoveWait]). A move to a DIFFERENT machine
	// costs no wait at all: a backoff is what is paid to ask the same machine
	// again, and it is the only thing it is for (retry.go's `moved`).
	Wait time.Duration
}

// MoveKind is which of the design's four answers a move is.
type MoveKind uint8

const (
	// MoveNone is the plan spent: there is no machine left, no wait left and no
	// rung left. The session hops the model or ends the turn, and that is the
	// ONLY place either happens.
	MoveNone MoveKind = iota
	// MoveMachine is another machine in the serving set, best belief first. It
	// is the first answer to every failure and it costs no wait.
	MoveMachine
	// MoveWait is the same machine again after the comeback it named itself. It
	// is legal ONCE in the life of a plan and only when the serving set is one
	// machine wide — which is the whole of "the same bytes go to the same
	// machine a second time only when there is nowhere else to send them".
	MoveWait
	// MoveShape is the same machines with one more field of the request taken
	// off: today's relaxation ladder, one rung at a time.
	MoveShape
)

// String is the machine word a row carries. It is never shown to a person —
// the surface composes its own sentence from the ordinal (failurerow.go).
func (k MoveKind) String() string {
	switch k {
	case MoveMachine:
		return "machine"
	case MoveWait:
		return "wait"
	case MoveShape:
		return "shape"
	default:
		return "none"
	}
}

// sameAs reports whether two moves are the same act. The WAIT is deliberately
// not part of the identity: how long a machine asked for is the machine's
// answer to this call, not a property of the act of going back to it.
//
// AND A MOVE THAT NAMES NO MACHINE IS NEVER A REPEAT. It is the open set's move
// (see [Next]): the router picks, and the body it picks from carries a longer
// exclusion list every time, so "the same machine again" is precisely what it
// cannot be. Calling two of them equal would stop an open walk after one send.
func (m Move) sameAs(other Move) bool {
	if m.Lane == "" || other.Lane == "" {
		return false
	}
	return m.Kind == other.Kind && m.Shape == other.Shape &&
		strings.EqualFold(m.Model, other.Model) && strings.EqualFold(m.Lane, other.Lane)
}

// MoveLog is the moves one question has already made.
//
// THE DESIGN WRITES IT `Moves []Move` AND IT IS A LOG RATHER THAN A SLICE FOR
// ONE REASON: the arms of a race share it. A hedge is [Next] launched a second
// time while the first move is still in flight, so two arms deciding at the
// same instant must not both be handed the same machine — and a slice copied
// onto each arm's own plan is exactly how they would be.
type MoveLog struct {
	mu    sync.Mutex
	moves []Move
}

// NewMoveLog is an empty log. A nil log is legal everywhere and reads as empty,
// so a plan built by hand — a test, a bench — decides nothing by omission.
func NewMoveLog() *MoveLog { return &MoveLog{} }

// Add records one move and reports whether it was NEW. A repeat records nothing
// and answers false, which is how two arms claiming at once are separated: the
// loser asks [Next] again rather than sending the same request twice.
func (l *MoveLog) Add(move Move) bool {
	if l == nil || move.Kind == MoveNone {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, made := range l.moves {
		if made.sameAs(move) {
			return false
		}
	}
	l.moves = append(l.moves, move)
	return true
}

// Release gives a claim back, and it is for the one thing [Add] is also used
// for: RESERVING a machine before anything has been sent to it.
//
// A RACE CLAIMS BEFORE IT PRICES (internal/provider's hedge.go). Two arms
// deciding at the same instant must not both take the last machine, so the claim
// is taken under the log's own lock and the purse is asked afterwards — and a
// purse that says no leaves a machine written down as tried that nothing was
// ever sent to. That is a lie to [Next], which would skip it, so it is taken
// back here. Nothing else may call this: a move that reached the wire is a fact
// and facts are not withdrawn.
func (l *MoveLog) Release(move Move) {
	if l == nil || move.Kind == MoveNone {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for index := len(l.moves) - 1; index >= 0; index-- {
		if l.moves[index].sameAs(move) {
			l.moves = append(l.moves[:index], l.moves[index+1:]...)
			return
		}
	}
}

// Tried reports whether this question has already been sent to a machine, in any
// shape. It is the claim half of [Add] asked as a question — what a race needs
// to know before it offers a machine to an arm — and it reads the same log the
// dispatcher's own walk reads, which is the whole of why two arms cannot take
// one machine.
func (l *MoveLog) Tried(lane string) bool {
	lane = strings.TrimSpace(lane)
	if l == nil || lane == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, made := range l.moves {
		if strings.EqualFold(made.Lane, lane) {
			return true
		}
	}
	return false
}

// List is a copy of what has been tried, in the order it was tried.
func (l *MoveLog) List() []Move {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.moves) == 0 {
		return nil
	}
	return append([]Move(nil), l.moves...)
}

// Count is how many moves have been made, which is the numerator of the ordinal
// a person reads.
func (l *MoveLog) Count() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.moves)
}

// ── THE MOVE GENERATOR ──────────────────────────────────────────────────────

// Next is the ONE place this build decides what a failed call does next, and it
// is a pure function of the plan and what has already been tried.
//
// ITS ORDER IS THE RULE'S ORDER (docs/design/recovery/DESIGN.md §3 and §4):
//
//  1. another machine in the serving set, best belief first;
//  2. the same machine after its own comeback, ONLY if it is the last one, and
//     only once in the life of the call;
//  3. the same machines with a relaxed shape, one rung;
//  4. none — the plan is spent, and the session hops the model or ends.
//
// IT NEVER RETURNS A MOVE THAT HAS ALREADY BEEN MADE, which is the second
// clause of the rule and the whole of what the census says was missing: 53 % of
// multi-attempt chains in ten days never left the lane they started on.
//
// THE DEADLINE IS NOT ASKED HERE. [Plan.Spent] is the one place a clock decides
// anything, and it is asked by the dispatcher before this is; separating them is
// what lets the order be table-tested with no clock at all.
func Next(plan Plan, history []Move) Move {
	shape := shapeReached(history)
	for {
		candidates := plan.serving()
		// 0. A REFUSAL ABOUT THE SHAPE HAS ALREADY ANSWERED FOR EVERY MACHINE.
		// The router read our own bytes and said no endpoint can serve them, so
		// walking machines would buy the identical sentence from each of them;
		// the only move such a refusal earns is the rung below
		// ([Plan.ShapeRefused] says where that is known).
		if !plan.ShapeRefused {
			// 0a. AN OPEN SET HAS ANOTHER MACHINE UNTIL THE DEADLINE SAYS
			// OTHERWISE. Nobody named the pool, so the router picks — and each
			// body carries a longer exclusion list than the last, which is what
			// makes the next send a different request rather than the same one.
			// There is nothing here to walk and nothing to run out of, so the
			// deadline is the whole bound.
			//
			// AND THE EXCLUSION LIST IS THE WHOLE PREMISE, so a refusal that named
			// nobody takes the premise away: there is nothing to add to the next
			// body, the next body is the one that was just refused, and this
			// answer would be the identical bytes to the identical machine for the
			// length of the deadline ([Plan.Unattributed]).
			if len(candidates) == 0 && !plan.Unattributed {
				return Move{Kind: MoveMachine, Model: plan.Model, Shape: shape}
			}
			// 1. ANOTHER MACHINE. The head of the choice first, then the
			// frontier's own order, which is already best-belief-first.
			for _, lane := range candidates {
				move := Move{Kind: MoveMachine, Model: plan.Model, Lane: lane, Shape: shape}
				if !made(history, move) {
					return move
				}
			}
			// 2. THE SAME MACHINE, ONCE, AND ONLY WHEN THERE IS NO OTHER. A set one
			// machine wide has no other machine for the next body to go to — a
			// person's strict pin, a rescue's demand, an account-wide hold — and
			// so has an open set whose refusal named nobody, because a body that
			// can exclude nothing is a body that can only go back where it came
			// from. The comeback the refusal named itself is the only legal repeat
			// there is, and it is legal once.
			if len(candidates) <= 1 && plan.Comeback > 0 && !madeKind(history, MoveWait) {
				alone := ""
				if len(candidates) == 1 {
					alone = candidates[0]
				}
				return Move{
					Kind: MoveWait, Model: plan.Model, Lane: alone,
					Shape: shape, Wait: plan.Comeback,
				}
			}
		}
		// 3. A RELAXED SHAPE, ONE RUNG, and then the machines are all admissible
		// again: the request that goes out is a different request, so a machine
		// that refused the old shape has said nothing about this one.
		if uint8(len(plan.Shapes)) <= shape {
			return Move{Kind: MoveNone, Model: plan.Model}
		}
		shape++
		if move := (Move{Kind: MoveShape, Model: plan.Model, Lane: plan.head(), Shape: shape}); !made(history, move) {
			return move
		}
	}
}

// shapeReached is which rung the request is already in: the highest shape any
// move has reached, because the ladder ACCUMULATES — each rung is climbed on
// top of the last, since a refusal never says which field it objected to.
func shapeReached(history []Move) uint8 {
	var reached uint8
	for _, move := range history {
		if move.Shape > reached {
			reached = move.Shape
		}
	}
	return reached
}

func made(history []Move, move Move) bool {
	for _, past := range history {
		if past.sameAs(move) {
			return true
		}
		// A LANE TRIED IN ANY WAY AT THIS SHAPE IS TRIED. A wait move sent the
		// same bytes to the same machine, so asking it again as a fresh machine
		// would be the repeat this whole file exists to refuse.
		if move.Kind == MoveMachine && past.Shape == move.Shape &&
			strings.EqualFold(past.Lane, move.Lane) && past.Lane != "" {
			return true
		}
	}
	return false
}

func madeKind(history []Move, kind MoveKind) bool {
	for _, past := range history {
		if past.Kind == kind {
			return true
		}
	}
	return false
}
