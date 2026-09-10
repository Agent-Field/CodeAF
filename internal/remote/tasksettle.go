package remote

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// tasksettle.go carries THE ANSWER TO A LANDING over the wire.
//
// A task that comes home with nobody able to say whether it holds is the
// person's call, and the card in front of them offers `[a] accept`, `[n] not
// right`, `[s] tell it` and the one-time hand-over (internal/tui3's
// tasksettle.go). Every one of those spends an engine door — ResolveUnverified,
// ResolveConflict, HandUnverifiedToModel, TakeBackDecision — and until this file
// existed NOT ONE OF THEM CROSSED. The surface asserts the doors on the agent it
// holds; a window attached to an engine host holds a *remote.Agent, the
// assertion answered no, and the absence law then did exactly what it is written
// to do: it drew the card with its reason and NO ANSWERS ROW AT ALL.
//
// THAT IS #706, AND THE PATH LENGTH IS WHY IT LOOKED LIKE A FIXTURE BUG. A host
// is only reachable where its socket path fits (internal/enginehost's
// socketLimit), so the same binary drew four chips for one seeded landing and
// none for another whose temporary home was a few characters shorter — the
// second had a host, the first had fallen back to the in-process engine.
//
// So the four verbs cross here, in the shape the rest of this wire uses: a door
// interface the host asserts on its own agent, a [Welcome] flag so a surface can
// tell an engine that has them from one that does not, and one argument type for
// all four.

// taskSettleDoor is the whole of what deciding a landing needs from an engine.
//
// IT IS ONE INTERFACE AND NOT FOUR because they are one capability: an engine
// that can take an accept can take a refusal, hand the question to its model and
// take it back again — they are four entry points into task_audit.go's single
// resolution path. The merge round is the one that could honestly be absent
// (internal/tui3's conflictAgent says why), and it is asserted separately at the
// far end rather than splitting the advertisement in two: a session agent has
// all four or none.
type taskSettleDoor interface {
	ResolveUnverified(uint64, session.TaskResolution, string) error
	HandUnverifiedToModel(uint64) error
	TakeBackDecision(uint64) error
}

// taskSettleKnown answers whether the engine behind this session can be asked
// any of it, which is what the welcome carries.
func taskSettleKnown(agent any) bool { _, ok := agent.(taskSettleDoor); return ok }

// errTaskSettleUnreachable is the refusal a call gets that should never have
// been made: internal/tui3 asks [Agent.SettleSupported] before it draws a chip,
// so reaching this means a door was spent on an engine that never advertised
// one. It is kept rather than trusted away — a refusal is cheaper than a call
// the far end will reject with a stranger sentence.
var errTaskSettleUnreachable = errors.New("deciding a landing needs a newer engine; update the engine and reconnect")

// SettleSupported is the surface's own question, answered from the welcome
// rather than from a type assertion.
//
// THE ASSERTION CANNOT SEE ACROSS THE WIRE — every *Agent has these methods, on
// every connection, whatever the machine at the other end is — which is
// [Welcome.Folders]'s stated reason for existing and applies here word for word.
// internal/tui3 asks this before it draws a chip, so a window talking to an
// older engine draws the card with no answers, exactly as it does against an
// engine that has no task doors at all: absent, not broken.
func (a *Agent) SettleSupported() bool { return a.c.Welcome().TaskSettle }

// settleTask is the host's half: one landing, one answer, spent on the engine's
// own doors.
//
// THE FLAGS ARE READ BEFORE THE RESOLUTION, in [TaskSettleArgs]'s stated order,
// because the three acts that move a question are not resolutions and an engine
// that read the field first would take a hand-over as an accept.
//
// AND THE MERGE ROUND IS ASSERTED ON ITS OWN. It spends a worker, a worktree and
// a round, and an engine can perfectly well settle a landing and have no such
// door — so it is refused by name rather than taking the whole method down with
// it (internal/tui3's conflictAgent keeps the same split at the other end).
func settleTask(agent any, args TaskSettleArgs) ([]byte, error) {
	door, ok := agent.(taskSettleDoor)
	if !ok {
		return nil, errors.New("engine: this session has no landings to decide")
	}
	var err error
	switch {
	case args.Merge:
		merge, has := agent.(interface{ ResolveConflict(uint64) error })
		if !has {
			return nil, errors.New("engine: this session cannot spend a merge round")
		}
		err = merge.ResolveConflict(args.ID)
	case args.Hand:
		err = door.HandUnverifiedToModel(args.ID)
	case args.Back:
		err = door.TakeBackDecision(args.ID)
	default:
		err = door.ResolveUnverified(args.ID, session.TaskResolution(args.Resolution), args.Why)
	}
	// SOMEBODY GETTING THERE FIRST IS AN ANSWER AND NOT A FAULT, so it crosses as
	// a field of the reply. Every other refusal is the engine's own sentence and
	// crosses as the call's error, exactly as it does in process.
	if errors.Is(err, session.ErrTaskDecided) {
		return json.Marshal(TaskSettled{Decided: true})
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(TaskSettled{})
}

// ResolveUnverified spends the person's answer on one landing.
//
// IT WAITS FOR THE ENGINE, unlike the proposal doors beside it ([Agent.ResolveTask]
// answers nothing and returns nothing). The card reads the error: a question
// somebody else has already answered stops asking and says `already answered`,
// and anything else keeps the choices and says one dim line, so an answer that
// went nowhere may not be reported as one that landed.
func (a *Agent) ResolveUnverified(id uint64, resolution session.TaskResolution, why string) error {
	return a.settleCall(TaskSettleArgs{ID: id, Resolution: string(resolution), Why: why})
}

// HandUnverifiedToModel gives one landing's decision to the model. Nothing is
// resolved by it: what moves is who is holding the question.
func (a *Agent) HandUnverifiedToModel(id uint64) error {
	return a.settleCall(TaskSettleArgs{ID: id, Hand: true})
}

// TakeBackDecision is that in reverse, and resolves nothing either.
func (a *Agent) TakeBackDecision(id uint64) error {
	return a.settleCall(TaskSettleArgs{ID: id, Back: true})
}

// ResolveConflict spends one more merge round on a branch that would not
// fasten. It is the conflict card's `[a] resolve it`, and it takes nothing as
// done on the way past.
func (a *Agent) ResolveConflict(id uint64) error {
	return a.settleCall(TaskSettleArgs{ID: id, Merge: true})
}

// settleCall is the one road all four take, and the one place the engine's
// "somebody got there first" is turned back into the sentinel the card reads.
//
// THE FACT CROSSES AS A FIELD AND NOT AS A SENTENCE ([TaskSettled.Decided]),
// which is [Agent.steerWith]'s own bargain with the same problem: an error over
// this wire arrives as text, and a card that told the two refusals apart by
// comparing prose would stop telling them apart the day somebody rewrote the
// sentence.
func (a *Agent) settleCall(args TaskSettleArgs) error {
	if !a.SettleSupported() {
		return errTaskSettleUnreachable
	}
	payload, err := a.c.call(nil, MethodTaskSettle, args)
	if err != nil {
		return err
	}
	var settled TaskSettled
	if err := json.Unmarshal(payload, &settled); err != nil {
		return err
	}
	if settled.Decided {
		return session.ErrTaskDecided
	}
	return nil
}
