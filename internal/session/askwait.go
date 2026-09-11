package session

// THE PARKED ASK, AND THE THREE WAYS ONE ENDS.
//
// `ask` is the only tool on the belt that stops the work and waits for a person
// (tools_ask.go). While it waits, the turn is parked: no request is out, no
// boundary is coming, and the whole of what will start the work again is
// somebody at a keyboard. That wait used to be a bare `map[uint64]chan Answer`
// on the agent, parked in one file, delivered to in a second, counted in a
// third and enumerated in a fourth — four places that each knew part of the rule
// and none of which held it.
//
// SO IT IS ONE TYPE WITH THE RULE IN IT, and the rule is that A PARKED ASK ENDS
// EXACTLY THREE WAYS:
//
//	ANSWERED    the person chose ([askedOfThePerson.answerLocked], from the one
//	            resolver every surface calls). The lane reads an [Answer].
//	TALKED PAST the person said something else instead
//	            ([askedOfThePerson.talkedPastLocked], from the splice —
//	            steerquestion.go says what was measured). The lane reads a closed
//	            channel, which is the one shape that cannot be mistaken for a
//	            choice they made.
//	LET GO      the lane itself stopped waiting ([askedOfThePerson.letGoLocked]):
//	            the turn was stopped, or the question was never drawn at all.
//
// There is no fourth, and nothing outside this file may reach the channels.
//
// ── AND AN ANSWERED QUESTION IS KEPT, BECAUSE IT CAN BE CHANGED ──
//
// A decision the person made is still theirs afterwards: while its receipt is on
// screen they may answer it again (`c change`, tui3's questionchange.go), and a
// permission they widened may be handed back whole (`u undo`). So the entry does
// not leave this book when it is answered — [askOpen.settled] is filled in and
// the entry stays, which is the only way the revision can be rendered against
// what was actually asked and say what it replaced. It leaves when the turn
// retires it, when the call that asked is abandoned, or with the session.
//
// THE OWNERSHIP MOVES DOWN ONE FIELD WITH IT. `THE ENTRY IS THE OWNERSHIP` holds
// for the three endings that take the entry out of the map; the answer claims
// the WAIT instead ([askedOfThePerson.answerLocked] takes `open.wait` and leaves
// nil behind), which is the same primitive one field lower and is what
// [askedOfThePerson.askedBackLocked] already did.
//
// ── AND A QUESTION CAN OUTLIVE THE CALL THAT ASKED IT ──
//
// Two shapes do it: a RATIFY, which nothing waits on ([AskKind.Waits]), and a
// question the person ASKED BACK on, which stays open while the model answers
// them. Both are still open questions — still drawn, still answerable in any
// window, still coming down through the one door — and neither has anything
// parked on it, so the entry below holds a nil wait and the answer reaches the
// model as a message instead ([Agent.answerAsk]).
//
// That is why this book holds an [askOpen] and not a bare channel. The question
// itself, the asker's own names for its answers and the way to take it back down
// have to survive the call, or a question with nobody parked on it is one
// nothing in this engine can describe, deliver to, or withdraw.
//
// ── THE ENTRY IS THE OWNERSHIP ──
//
// Every road here removes the id from the map under a.mu BEFORE it touches that
// id's channel, so at most one road can ever hold it. That is what makes closing
// a channel safe rather than a send-on-closed waiting to happen, and it is why
// the three endings can be written as three verbs instead of one flag every
// caller has to get right. A second answer to an already-ended ask finds
// nothing and says so, which is what every caller already wanted.
//
// ── AND THE DELIVERY NEVER BLOCKS ──
//
// The channel is buffered to one and at most one thing is ever put on it,
// because the id is gone from the map before the put. So the send happens with
// a.mu held, like everything else here, without the unlock-and-hope the raw map
// needed around it.

import "time"

// askedOfThePerson is every question the model has put that this session is
// still parked on, keyed by the ask's own id. The zero value is usable and holds
// nothing.
//
// It is guarded by [Agent.mu] — hence the Locked suffix on every method — for
// the reason the steering queue is: whether a question is still open and whether
// a turn is still running are one fact, and two locks would let them disagree.
type askedOfThePerson struct {
	parked map[uint64]*askOpen
}

// askOpen is one question the model raised, as this book holds it for as long as
// it stands: the question itself, the asker's own names for its answers, the
// call parked on it, and the way to take it back down.
//
// wait IS NIL ON A QUESTION THAT OUTLIVED ITS CALL, and that is the whole of what
// this type adds over the channel it replaced (see the header).
type askOpen struct {
	q      Question
	theirs map[string]string
	wait   chan Answer
	letGo  func()
	// clock is the policy's own timer where the question has one. It belongs to
	// the LANE and not to the call, so it runs on a question nobody is parked on
	// — which is every ratify and every ask whose turn did not wait
	// (asklane.go's [Agent.startAskClock]).
	clock *time.Timer
	// held says a person pressed a key on this question and the clock will never
	// answer it. There is no way back: a clock that resumed would fire exactly
	// when somebody looked away mid-decision.
	held bool
	// settled is the answer that ended it, and nil while it is still open. It is
	// kept because a settled reversible decision can still be CHANGED, and the
	// correction says what it was changed from.
	settled *Answer
}

// parkLocked registers one ask and answers the channel its lane waits on, which
// is nil for a question nothing waits on — a ratify. The [askOpen] is the
// caller's; nothing outside this file writes to it again.
func (asked *askedOfThePerson) parkLocked(open *askOpen) <-chan Answer {
	if asked.parked == nil {
		asked.parked = make(map[uint64]*askOpen)
	}
	// THE FIELD IS THE DERIVATION. `Blocking.Turn` is built in [Agent.executeAsk]
	// from whether this call really parks — the kind's own answer AND the asker's
	// own word — so reading it here is reading that one decision rather than
	// making a second one that could disagree with the row on the screen.
	if open.q.Blocking.Turn {
		// Buffered to one and read at most once, so the road that applies the
		// answer never blocks on the lane having got to its select.
		open.wait = make(chan Answer, 1)
	}
	asked.parked[open.q.ID] = open
	return open.wait
}

// claimLocked takes one id out of the map, which is how a road becomes the only
// owner of its entry. It is this type's own primitive and the endings below are
// the whole of what may call it.
func (asked *askedOfThePerson) claimLocked(id uint64) (*askOpen, bool) {
	open, parked := asked.parked[id]
	if !parked || open == nil {
		return nil, false
	}
	delete(asked.parked, id)
	return open, true
}

// askSettledKept is how many ANSWERED questions this book keeps for a revision.
//
// IT IS THE BOUND ON THE ONE THING HERE THAT OUTLIVES BEING ANSWERED. A settled
// entry is kept so `c change` can put the question back and say what it
// replaced; kept forever, the book would grow for the life of a session with
// every question ever answered in it. The number is the surface's own: at most
// [tui3's questionRecordsKept] receipts are drawn at once and the key is live
// only while the receipt is, so a handful more than can ever be on screen is
// exactly enough to make every offer on the screen work and nothing more.
const askSettledKept = 4

// forgetOldSettledLocked drops the oldest settled entries past that bound. Ids
// are minted in order ([Agent.askSeq]), so the smallest settled id is the oldest
// decision and the first to go.
func (asked *askedOfThePerson) forgetOldSettledLocked() {
	for {
		settled, oldest := 0, uint64(0)
		for id, open := range asked.parked {
			if open == nil || open.settled == nil {
				continue
			}
			settled++
			if oldest == 0 || id < oldest {
				oldest = id
			}
		}
		if settled <= askSettledKept || oldest == 0 {
			return
		}
		if open := asked.parked[oldest]; open != nil && open.clock != nil {
			open.clock.Stop()
		}
		delete(asked.parked, oldest)
	}
}

// stopClocksLocked stops every clock this book has armed, because NOTHING ARMED
// OUTLIVES THE SESSION THAT ARMED IT (steer_grace.go states the same law for a
// steer's grace). A clock that fired after the door shut would answer a question
// with nobody to read the answer: the note would be refused by a closed queue
// and the decision would still be in the record, where the next run of this
// conversation reads it as something the person settled.
func (asked *askedOfThePerson) stopClocksLocked() {
	for id, open := range asked.parked {
		if open == nil {
			continue
		}
		if open.clock != nil {
			open.clock.Stop()
		}
		if open.settled != nil {
			// AND NOTHING SETTLED OUTLIVES THE SESSION EITHER. A receipt kept so
			// its decision could be changed is a receipt nobody can change once
			// the door has shut; the questions still OPEN are left exactly where
			// they are, because a session leaving does not answer them and the
			// next one has its own book.
			delete(asked.parked, id)
		}
	}
}

// atLocked is what is standing under one id, without taking it. It is for the
// readings — the presence file's, the sweep's — and never for an ending.
func (asked *askedOfThePerson) atLocked(id uint64) *askOpen { return asked.parked[id] }

// answerLocked is the FIRST ending: the person chose, and the lane reads what
// they chose. It hands back the entry, so the caller can deliver to a model
// whose call is long gone; false is an answer that arrived after the question
// stopped being one, which every caller treats as nothing to do rather than as
// a fault.
func (asked *askedOfThePerson) answerLocked(id uint64, answer Answer) (*askOpen, chan Answer, error) {
	open, parked := asked.parked[id]
	if !parked || open == nil {
		// A QUESTION THIS BOOK HAS NO ENTRY FOR IS NOT A LATE ANSWER, AND THE
		// DIFFERENCE IS WORTH A SENTENCE. What reaches here is a row on somebody
		// screen that this engine is not asking any more — the turn was
		// interrupted and took the question with it, they talked past it, or the
		// session host has restarted since it was drawn and the book went with
		// the process.
		return nil, nil, errAnswerGone
	}
	if open.settled != nil && !answer.Revises {
		// THE FIRST ANSWER WINS, AND THE SECOND CHANGES NOTHING AT ALL —
		// including the record. The clock and a person can reach the door in the
		// same instant, and a loser that said nothing was recorded and announced
		// as the decision while the model had already been told the other one.
		return nil, nil, errAnswerSettled
	}
	settled := answer
	open.settled = &settled
	if open.clock != nil {
		open.clock.Stop()
	}
	asked.forgetOldSettledLocked()
	// THE WAIT IS CLAIMED AND THE ENTRY STAYS. Nothing else can reach this
	// channel afterwards, so a correction that arrives once the call has taken
	// its answer and gone is a MESSAGE rather than a send into a channel nobody
	// reads — which is measurable: without it, a change to a question the turn
	// had waited for was recorded, drawn, and never heard by the model
	// (the Spark, 2026-09-11).
	wait := open.wait
	open.wait = nil
	if wait != nil {
		wait <- answer
	}
	return open, wait, nil
}

// settledLocked is one question this book holds and somebody has already
// answered, for the revision that wants to know what was asked and what was
// decided last time.
func (asked *askedOfThePerson) settledLocked(id uint64) (*askOpen, bool) {
	open, parked := asked.parked[id]
	if !parked || open == nil || open.settled == nil {
		return nil, false
	}
	return open, true
}

// askedBackLocked is the one road that takes the CALL off a question and leaves
// the QUESTION standing: they asked something about it instead of answering it,
// so the call comes back with their words — a model parked in a tool cannot say
// a thing — and the decision is still theirs to make.
//
// It obeys the same primitive the endings do, one field lower: the channel is
// taken off the entry before anything is put on it, so at most one road ever
// holds it. It reports false where nothing is parked, and false where the
// question outlived its call already — a second ask-back on a question the model
// is already answering has no call to come back.
func (asked *askedOfThePerson) askedBackLocked(id uint64, answer Answer) bool {
	open, parked := asked.parked[id]
	if !parked || open == nil || open.wait == nil {
		return false
	}
	wait := open.wait
	open.wait = nil
	wait <- answer
	return true
}

// talkedPastLocked is the SECOND ending: the person said something else instead
// of answering, so every question they spoke past stops standing at once. It
// answers the ids it retired, for the caller to withdraw from the surfaces
// drawing them.
//
// IT CLOSES RATHER THAN ANSWERING, and that is the whole of how the lane tells
// the two apart. There is no [Answer] that honestly means "they chose nothing";
// a zero one would be read as a pick, and a sentinel field on the answer would
// put a shape on the wire that every surface would then have to know not to
// draw. A closed channel is the ending with no value in it, which is exactly
// what happened.
func (asked *askedOfThePerson) talkedPastLocked() []uint64 {
	if len(asked.parked) == 0 {
		return nil
	}
	retired := make([]uint64, 0, len(asked.parked))
	for id := range asked.parked {
		// Claimed rather than closed off the range variable, so this road obeys
		// the same primitive the other two do and nothing here can reach a
		// channel it does not own.
		open, parked := asked.claimLocked(id)
		if !parked {
			continue
		}
		// A QUESTION THAT OUTLIVED ITS CALL COMES DOWN TOO, and there is nothing
		// to close: a ratify, or one they asked back on, has no lane reading it.
		// It still stops standing — talking past a question is talking past
		// every question on the screen — and its id is still reported, so the
		// caller withdraws it from the surfaces drawing it.
		if open.wait != nil {
			close(open.wait)
			open.wait = nil
		}
		retired = append(retired, id)
	}
	return retired
}

// retireLocked is the LET-GO applied to every question the turn that raised it
// is not allowed to leave behind, and it answers the entries it took so the
// caller can take their rows down with a.mu released.
//
// IT IS THE TRIGGER A QUESTION THAT OUTLIVED ITS CALL NEVER HAD. Every other
// lane in this engine withdraws its question when its own wait ends — the call
// returns and the deferred let-go runs (question.go's [Agent.rememberQuestion])
// — but an entry with a nil wait has no such moment, so an unanswered ratify and
// a question somebody asked back on and never came back to stood in
// [Agent.OpenQuestions], on the presence desk and against [QuestionCap] for the
// rest of the session, about a turn that ended long ago.
//
// WHAT MAY STAY IS ONE PREDICATE AND NOT A LIST OF KINDS
// ([questionOutlivesTurn]). Nothing parked is ever taken: a lane still reading
// its channel is a turn that has not ended.
func (asked *askedOfThePerson) retireLocked() []*askOpen {
	var gone []*askOpen
	for id, open := range asked.parked {
		if open == nil || open.wait != nil {
			continue
		}
		if open.settled != nil {
			// AN ANSWERED QUESTION IS NOT RETIRED AT ALL. Nothing is drawn for it
			// — the answer took its row down — and withdrawing it would put `no
			// longer needed` on somebody's screen about their own decision. What
			// is left is the entry a revision reads, and its bound is
			// [askedOfThePerson.forgetOldSettledLocked] rather than this sweep:
			// a turn very often ENDS on the answer that settled it, and clearing
			// it here would take `c change` away one frame after the receipt
			// offered it.
			continue
		}
		if questionOutlivesTurn(open.q) {
			continue
		}
		if taken, parked := asked.claimLocked(id); parked {
			gone = append(gone, taken)
		}
	}
	return gone
}

// letGoLocked is the THIRD ending: the lane itself stopped waiting — its turn
// was stopped, or the question was refused before anybody saw it. Nothing is put
// on the channel and nothing is closed, because the only reader is the lane that
// is leaving.
func (asked *askedOfThePerson) letGoLocked(id uint64) {
	delete(asked.parked, id)
}

// openLocked is the ids still parked, and it is what [Agent.OpenQuestions] walks
// to rebuild the model's own questions.
func (asked *askedOfThePerson) openLocked() []uint64 {
	if len(asked.parked) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(asked.parked))
	for id, open := range asked.parked {
		if open == nil || open.settled != nil {
			// AN ANSWERED QUESTION IS NOT AN OPEN ONE. The entry is kept so the
			// decision can be changed while its receipt is on screen (the header
			// says why); what is drawn, counted and replayed is what is still a
			// question.
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// anyLocked is whether this session is STOPPED on a person — the fact the
// presence file publishes so another window can see that a conversation is
// waiting rather than working (taskpresence.go).
//
// IT IS NOT "IS ANYTHING IN THE BOOK", and the difference is the two shapes that
// outlive their call. A ratify and a question somebody asked back on are both
// open, drawn and answerable, and NOTHING IS WAITING ON EITHER — the turn went
// on, or the model is replying — so counting one would say `waiting on you`
// about work that is carrying on. Both terms are needed: the entry has to still
// have a lane reading it, and the question itself has to be one anything waits
// on ([Question.Waiting]).
func (asked *askedOfThePerson) anyLocked() bool {
	for _, open := range asked.parked {
		if open != nil && open.wait != nil && open.q.Waiting() {
			return true
		}
	}
	return false
}
