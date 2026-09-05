package session

// THE PERSON STEERING FROM THE MAIN CHAT, WITH THEIR OWN AUTHORITY.
//
// A person watching three tasks run does not open three rooms. They say "make
// task 1 write CSV instead" in the conversation they are already in, and until
// this file existed the best the model could do was `tasks … say`, which is
// coordination: it is framed as another agent speaking, it is refused as a basis
// for a revision, and the worker is told in as many words that it grants nothing
// (task_room.go's [Agent.relayToTask], assignment.go's authority note). So the
// correction arrived as a paraphrase with no standing, the done-condition did
// not move, and the checker went on grading the work against the thing the
// person had just changed their mind about.
//
// ── WHAT THIS DOOR IS, AND WHAT IT REFUSES TO BE ──
//
// It forwards THE PERSON'S RECORDED WORDS, verbatim, to ONE task the model
// names. The model chooses the address and nothing else: it does not write the
// words, it cannot edit them, and it cannot label anything of its own as theirs.
// [Agent.personAsk] is the text — the same words a task admitted in this turn
// would be handed as the person's own request — and the model's own sentence is
// still `say`, still framed as this conversation speaking.
//
// THAT SPLIT IS THE WHOLE SAFETY PROPERTY. A model that could supply the text
// could grant itself permissions by quoting a person who never said it; a model
// that can only supply an ADDRESS can, at worst, send the person's real words to
// the wrong task — which is a mistake the person can read, in their own
// sentence, on the task's own record. This file makes no claim to have
// understood who they meant, and the receipt the model gets back says so.
//
// ── WHICH MESSAGE, AND WHOSE TURN ──
//
// The source is bound to THE REQUEST THE MODEL IS ANSWERING and never to
// whatever the session holds at the moment the tool runs
// ([episode.decisionBegins] stamps it, and this file reads it back off the
// context). Two things follow, and both matter:
//
//   - A steer typed while a call is in flight cannot be forwarded by that call.
//     The words it carries are the ones the model actually read; the newer line
//     reaches the model at the next drain and is forwarded, if it should be, by
//     a call that has seen it.
//   - A turn nobody typed into has no source at all. A turn opened by a task
//     landing or a job exiting is answering the harness, not the person
//     ([Agent.personHeard]), and their words from two turns ago are not a live
//     instruction to hand a worker under their authority. The door refuses, and
//     names the reason.
//
// There is no second authority system here: the request generation is the
// [requestEpoch] the handoff gate already reads, the message identity is the
// [personTurn] sequence admission already numbers, and what the words become on
// the far side is the direction receipt every steer already mints.
//
// ── TWO DOORS OF DIFFERENT SPEEDS, AND WHAT IS ACTUALLY GUARANTEED ──
//
// A task can now hear the person twice over: from its own room, the instant they
// press enter, and from here, whenever the model gets round to it. So their
// words can reach one node OUT OF THE ORDER THEY SAID THEM — say A here, walk
// into the room and say B, and A can arrive second. What that must never buy is
// A moving the goal back over B.
//
// The guarantee is exactly this: WHAT THE WORK IS JUDGED BY MOVES IN THE ORDER
// THE SESSION HEARD EACH MESSAGE, whichever door it came through. A forwarded
// line carries its original drain timestamp rather than its forwarding time, and a revision citing a direction said before the one already in
// force is refused (assignment.go's [taskAssignment.lastSpokenApplied]).
//
// It is NOT a guarantee that a late line is never delivered. A forward whose
// message this task has not heard is delivered whenever it arrives, because it
// is genuinely theirs and nothing else has spoken for it — the worker reads it,
// with the harness's line under it, and cannot fold it in over anything later.
// The narrower case where this door DOES refuse is the one it can be certain
// about: a message this same conversation has already been overtaken on, where
// both sentences carry comparable identities from the same life.
//
// ── AND IT IS NOT A ROUTER ──
//
// One session, one target, named by id, resolved in this session's own graph
// through the same lookup every other op on the tool uses. No broadcast, no
// fan-out, nothing addressed to another window's work, and nothing a WORKER can
// call: a node forwarding "the person's words" would be a descendant minting the
// authority it is graded by, which is the one thing the origin model exists to
// prevent.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// personSourceID names ONE message the person typed, durably enough to be
// written on a task's record and read back after a restart.
//
// IT IS AN EVENT NUMBER AND NEVER A FINGERPRINT OF THE WORDS. The same sentence
// typed twice is two instructions — "try it again" said after a failure means
// something the first one did not — so identity is the event, exactly as
// admission's own numbering of the person's turns already has it
// (admission_compile.go's [personTurn]).
//
// AND THE NUMBER IS ONLY MEANINGFUL INSIDE ITS SCOPE. [Agent.personSeq] counts
// the messages one opening of one session has heard, and a reopen recounts them
// from the history it can replay — which a compaction may have folded, so the
// count can legitimately come back lower. Written raw, tomorrow's first message
// would be number 1 and would be recognised as a direction already delivered
// from a record written today. The scope is what stops that: two lives never
// share one, so a number from another life is another life's number and is
// never mistaken for this one's.
type personSourceID struct {
	scope string
	seq   uint64
}

// spokenSource is that identity together with the INSTANT the person said it,
// which is what a task's record orders its corrections by. The two travel
// together from the door to the record and part company there: the identity is
// persisted as the retry key, the instant becomes the direction's own `at`
// (assignment.go).
type spokenSource struct {
	id     personSourceID
	spoken time.Time
}

func (id personSourceID) live() bool { return id.scope != "" && id.seq != 0 }

// after says this id names a LATER message than the other one. Only ids from
// the same life are comparable, and that is the whole of the ordering: a
// direction from an earlier life was heard before this life began, so it can
// never be later than something heard in it.
func (id personSourceID) after(other personSourceID) bool {
	return id.live() && other.live() && id.scope == other.scope && id.seq > other.seq
}

// personSource is one message the person typed, as the request that carried it
// to the model identifies it: which message, what it said, and which request
// generation was answering it.
type personSource struct {
	id personSourceID
	// words is the whole message, exactly as [Agent.personAsk] keeps it.
	words string
	// spoken is when the session heard it, which is the closest this package
	// stands to the keypress ([Agent.rememberAskLocked]).
	spoken time.Time
	// at is the turn and steer this was read at (turnhandoff.go).
	at requestEpoch
}

// live says this source names a message that may be forwarded: the person typed
// it, this turn is the turn they typed it into, and there is something in it.
func (s personSource) live() bool {
	return s.id.live() && s.at.live() && strings.TrimSpace(s.words) != ""
}

// said is the provenance the node's record keeps.
func (s personSource) said() spokenSource {
	return spokenSource{id: s.id, spoken: s.spoken}
}

// askingLocked reads the source off the agent. The caller holds a.mu, which is
// where every field of it is written, so the identity, the words and the request
// generation cannot be torn apart from one another.
//
// A TURN THE PERSON DID NOT OPEN OR STEER HAS NONE. [Agent.personHeard] is the
// turn their words were heard in, and a woken turn leaves it behind: what they
// said before the wake is history this turn is not answering.
func (a *Agent) askingLocked() personSource {
	if !a.running || a.personHeard != a.turnSeq || a.personSeq == 0 {
		return personSource{}
	}
	if a.personScope == "" && !a.mintPersonScopeLocked() {
		return personSource{}
	}
	return personSource{
		id:     personSourceID{scope: a.personScope, seq: a.personSeq},
		words:  a.personAsk,
		spoken: a.personAt,
		at:     requestEpoch{turn: a.turnSeq, steer: a.steerSeq.Load()},
	}
}

// mintPersonScopeLocked names this life of this session, once, and answers
// whether it could.
//
// SIXTEEN BYTES, AND NO FALLBACK. The scope's whole job is that two lives never
// share one — a persisted direction from an earlier run must never be mistaken
// for a message of this run's — and a scope drawn from the clock, the pid or a
// short id would be a guarantee this file states and does not have. If the
// machine cannot produce sixteen random bytes, nothing is minted and no forward
// is possible; the door says exactly that, and every other thing a conversation
// can do is untouched. A harness that cannot read its own entropy source has
// larger problems than a correction it has to be told twice.
func (a *Agent) mintPersonScopeLocked() bool {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		a.personScopeDenied = true
		return false
	}
	a.personScope = a.sessionID() + "/" + hex.EncodeToString(raw[:])
	a.personScopeDenied = false
	return true
}

// askingNow is the same read for a caller that does not hold a.mu.
func (a *Agent) askingNow() personSource {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.askingLocked()
}

// askedFrom is the source a tool call was made under, off the turn's episode.
// A call with no episode — a test calling Execute directly, a road that runs no
// turn — has no source, and the door refuses rather than reaching for the
// session's newest words instead.
func askedFrom(ctx context.Context) personSource {
	return episodeFrom(ctx).askedFrom()
}

// The refusals this door can give. They are sentences a model reads, so each
// says what happened and what to do instead rather than naming a rule.
const (
	forwardNotInTask = "A task cannot forward the person's words: what reaches you is a brief and what you send a piece is coordination. " +
		"Say what you need with say, or report what the person has to decide."
	forwardNoSource = "There is nothing of the person's to forward: this turn was opened by the harness, not by them, and what they said before it is not an instruction they are giving now. " +
		"Forward the correction when they type one; use say for your own line."
	forwardNotYours = "say writes your own words and forward sends theirs, so a call cannot do both. " +
		"Send forward on its own, or say what you want to add as this conversation speaking."
	forwardNoIdentity = "This machine could not produce the random bytes that name one message of the person's apart from another, so nothing can be forwarded under their authority. " +
		"Say what needs saying with say, or ask them to type the correction into the task's own room."
	forwardTurnOver = "That turn is over, so the words you were answering are no longer what the person is asking for. " +
		"Nothing was sent. Read what they have said since and forward that if it still needs forwarding."
)

// errSaidLaterAlready is the ordering refusal, and it is the one the person's
// authority actually rests on. See [taskAssignment.hear].
var errSaidLaterAlready = errors.New("something the person said later has already been forwarded to this task, so these older words were not sent")

// forwardOneTask is the tool's forwarding op: the person's own words into one
// running node in this session, under their authority.
func (a *Agent) forwardOneTask(ctx context.Context, entry TaskIndexEntry, id uint64, here bool) (string, bool, error) {
	if a.config.InTask {
		return forwardNotInTask, true, nil
	}
	if !here {
		return fmt.Sprintf("Task %s ran in an earlier conversation, so there is nobody left to give the person's words to. Propose the work again if it needs doing differently.", entry.ID), true, nil
	}
	source := askedFrom(ctx)
	if !source.live() {
		if a.scopeDenied() {
			return forwardNoIdentity, true, nil
		}
		return forwardNoSource, true, nil
	}
	receipt, err := a.forwardToTask(id, source)
	if errors.Is(err, errTurnMovedOn) {
		return forwardTurnOver, true, nil
	}
	if err != nil {
		return capitalized(err.Error()) + ".", true, nil
	}
	return forwardedText(entry.ID, source.words, receipt), false, nil
}

// forwardToTask sends the person's words down the ordinary steering road, with
// the origin their own door uses and the source stamped on the direction the
// node records ([Agent.sayToTask]). Everything the person's own steer gets —
// the wake for a parked node, the hold while the work is being checked, the
// receipt a worker cites to revise — is what this gets, because it IS that
// road rather than a copy of it.
//
// ── THE TWO WAYS A CALL CAN BE TOO LATE, AND THEY ARE DIFFERENT ──
//
// THE TURN IT WAS MADE IN CAN BE OVER: interrupted, abandoned, closed, or
// simply followed by another. The snapshot is still in the tool's hand, and
// sending it would put words the person has walked away from onto a running
// task under their name. It is refused here, out loud — a call that quietly did
// nothing and answered as though it had is the worse failure of the two.
//
// AND THAT CHECK IS A CHECK AND NOT A LINEARIZATION POINT. It reads the agent
// under a.mu and releases it before the delivery, so a turn that ends in the
// instant BETWEEN the two still delivers. Closing that window would mean holding
// a.mu across the graph and room locks the delivery takes, and this package's
// lock order runs the other way — a room's lock is taken before an agent's
// (mailbox.go) — so the strict version is a deadlock rather than a guarantee.
// What is left in that window is what the person's own room door has always had:
// a line typed as a turn ends is delivered, is on the node's record, and is
// answered by the landing's revalidation rather than by a promise made here.
//
// AND A LATER MESSAGE OF THEIRS CAN HAVE REACHED THIS TASK FIRST. That is the
// ordering hazard, and carrying the older words rather than the newer ones does
// not answer it: the direction ids this node mints are its own arrival order, so
// an older correction admitted after a newer one would hold the HIGHER id and a
// worker citing it would move the goal back to what the person has already
// changed their mind about — and the newer direction, now the lower id, would
// be refused as stale by [taskAssignment.revise]. So the record's own
// chronology decides ([taskAssignment.hear], under the graph lock that mints
// the receipt), and an older message arriving late is refused rather than
// admitted behind the newer one.
//
// WHAT IS NOT REFUSED is a call whose source has merely been SUPERSEDED IN THE
// SESSION — the person typed something else while this call was in flight, about
// anything at all. Those words are still theirs and still unsent, the model read
// them and chose this task for them, and the newer line reaches the model at the
// next drain to be forwarded on its own merits. Refusing here would make the
// door fail exactly when the person is talking quickly, which is when they are
// most likely to be correcting work.
func (a *Agent) forwardToTask(id uint64, source personSource) (SteerReceipt, error) {
	if !source.live() {
		return SteerReceipt{}, errNoPersonWords
	}
	// The turn is read from the agent rather than from the context's deadline: a
	// cancelled turn, an abandoned one and one that simply ended are the same
	// fact here, and [Agent.askingNow] answers all three by having moved on.
	if now := a.askingNow(); !now.live() || now.at.turn != source.at.turn || now.id.scope != source.id.scope {
		return SteerReceipt{}, errTurnMovedOn
	}
	return a.sayToTask(id, source.words, fromPerson, source.said())
}

// errTurnMovedOn is the refusal for a call whose turn ended under it.
var errTurnMovedOn = errors.New("that turn is over")

// errNoPersonWords guards the seam rather than the tool, so a caller reaching
// [Agent.forwardToTask] with a source it did not get from a request cannot send
// anything under the person's name.
var errNoPersonWords = errors.New("there is nothing of the person's to forward")

// forwardedText is what the model is told. It quotes what actually went, because
// the model supplied an address and not the words, and it says plainly that the
// address was its own reading of who they meant.
func forwardedText(id, words string, receipt SteerReceipt) string {
	said := strings.TrimSpace(words)
	if receipt.Again {
		return fmt.Sprintf("task %s already has this, from the same message: %s\nNothing was sent twice. It is direction %d on that task's record.",
			id, said, receipt.Direction)
	}
	landing := "It is on that task's record as the person's own direction, and its worker folds it into what the work is judged by if it changes that."
	if receipt.Held {
		landing = receipt.Landing + ". This receipt does not mean the worker has read or applied the correction."
	} else if receipt.Waiting {
		landing = "It was waiting on the pieces it handed out; their words wake it. On its record as the person's own direction, which its worker folds into what the work is judged by if it changes that."
	}
	return fmt.Sprintf("forwarded to task %s, in the person's own words: %s\n%s Nothing checked that they meant this task — you chose it — so say what you sent and to which task in your reply.",
		id, said, landing)
}

// scopeDenied says the entropy this door's identities are made of was refused
// by the machine, which is a different fact from a turn nobody typed into and
// gets its own sentence.
func (a *Agent) scopeDenied() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.personScopeDenied
}
