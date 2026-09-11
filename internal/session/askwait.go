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
}

// parkLocked registers one ask and answers the channel its lane waits on, which
// is nil for a question nothing waits on — a ratify. The [askOpen] is the
// caller's; nothing outside this file writes to it again.
func (asked *askedOfThePerson) parkLocked(open *askOpen) <-chan Answer {
	if asked.parked == nil {
		asked.parked = make(map[uint64]*askOpen)
	}
	if open.q.Ask.Waits() {
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

// atLocked is what is standing under one id, without taking it. It is for the
// readings — the presence file's, the sweep's — and never for an ending.
func (asked *askedOfThePerson) atLocked(id uint64) *askOpen { return asked.parked[id] }

// answerLocked is the FIRST ending: the person chose, and the lane reads what
// they chose. It hands back the entry, so the caller can deliver to a model
// whose call is long gone; false is an answer that arrived after the question
// stopped being one, which every caller treats as nothing to do rather than as
// a fault.
func (asked *askedOfThePerson) answerLocked(id uint64, answer Answer) (*askOpen, bool) {
	open, parked := asked.claimLocked(id)
	if !parked {
		return nil, false
	}
	if open.wait != nil {
		// The claim above is the ownership: the entry is out of the map, so no
		// other road can reach this channel and the field is left standing as
		// the caller's own reading of WHO GOT THE ANSWER — a lane still parked,
		// or nobody, which is a question that outlived its call.
		open.wait <- answer
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
		if open == nil || open.wait != nil || questionOutlivesTurn(open.q) {
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
	for id := range asked.parked {
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
