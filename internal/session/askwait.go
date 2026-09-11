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
	parked map[uint64]chan Answer
}

// parkLocked registers one ask and answers the channel its lane waits on.
func (asked *askedOfThePerson) parkLocked(id uint64) <-chan Answer {
	if asked.parked == nil {
		asked.parked = make(map[uint64]chan Answer)
	}
	wait := make(chan Answer, 1)
	asked.parked[id] = wait
	return wait
}

// claimLocked takes one id out of the map, which is how a road becomes the only
// owner of its channel. It is this type's own primitive and the three endings
// below are the whole of what may call it.
func (asked *askedOfThePerson) claimLocked(id uint64) (chan Answer, bool) {
	wait, parked := asked.parked[id]
	if !parked {
		return nil, false
	}
	delete(asked.parked, id)
	return wait, true
}

// answerLocked is the FIRST ending: the person chose, and the lane reads what
// they chose. It reports whether anything was still parked on this id — false is
// an answer that arrived after the question stopped being one, which every
// caller treats as nothing to do rather than as a fault.
func (asked *askedOfThePerson) answerLocked(id uint64, answer Answer) bool {
	wait, parked := asked.claimLocked(id)
	if !parked {
		return false
	}
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
		wait, parked := asked.claimLocked(id)
		if !parked {
			continue
		}
		close(wait)
		retired = append(retired, id)
	}
	return retired
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

// anyLocked is whether this session is parked on a person at all — the fact the
// presence file publishes so another window can see that a conversation is
// waiting rather than working (taskpresence.go).
func (asked *askedOfThePerson) anyLocked() bool { return len(asked.parked) > 0 }
