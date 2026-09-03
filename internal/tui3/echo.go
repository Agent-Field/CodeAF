package tui3

// echo.go is THE PERSON'S OWN LINE, DRAWN BEFORE THE FAR END HAS AGREED TO IT.
//
// THE DEFECT IT ENDS. At home, pressing enter and seeing your sentence appear
// are the same instant: the surface writes the line and the agent takes it, both
// inside one update. Over `aforge chat --host devbox` the taking is a round trip
// — the engine has to answer with the stream the turn will run on — and until
// this file there was nothing saying what the line on screen MEANT in that gap.
// It was drawn as though it had landed, and when the engine refused it (a turn
// already running, another window driving) it simply stayed there: the person
// was left reading a message the far machine never accepted, in a conversation
// whose record they had no reason to doubt.
//
// SO THE LINE IS ECHOED, MARKED, AND THEN EITHER CONFIRMED OR WITHDRAWN.
//
//   - ECHOED — in the same place it will occupy, with the same glyph, the same
//     column and the same wrap. It does not move when it is confirmed, because
//     a line that jumped would be worse than one that waited.
//   - MARKED — one reading step down, on [palette.narr], which is the tier this
//     surface already uses for words that are one step under the question's own
//     (steerelbow.go). It is deliberately NOT a spinner, a badge or a colour of
//     its own: the mark has to be quiet enough that the ordinary case — a
//     confirmation forty milliseconds later — reads as the line settling rather
//     than as something having gone wrong.
//   - CONFIRMED — the mark comes off the SAME entry. Nothing is appended, ever,
//     which is the whole of "a duplicate never appears": the surface has one
//     line for one sentence from the moment it is typed, and confirmation is a
//     flag on it. The wire's own exactly-once law (internal/remote's client.go,
//     [Frame.Seq]) is what keeps a redial's replay from re-drawing the turn
//     around it, so neither half of the pair can double a message.
//   - WITHDRAWN — the line is taken off the page and the engine's refusal is
//     put where it was. A refused message never happened, and a transcript that
//     kept it would be the only record of the conversation showing a sentence
//     the model was never given.
//
// IT IS ONLY DONE OVER A CONNECTION ([app.hosted]). At home there is no gap to
// mark: the answer is back inside the same update, and a mark that appeared and
// vanished on every message would be a flicker charged to every person who has
// never used --host.

// echoPending marks the line just said as one the far end has not agreed to
// yet, and hands back the TOKEN that names it. It is called from the submit
// path and marks nothing on a surface that is not hosted, where it answers zero.
//
// THE BLOCK IS THE LAST ONE APPENDED and is remembered by index rather than
// searched for later: a search by turn number would find the wrong block on a
// steered turn, where the person's second sentence and their first carry the
// same turn.
//
// THE TOKEN IS NOT THAT INDEX. It counts from one, so the ZERO VALUE of a
// submit's answer means "this answer settles nothing" — which is what every
// door that echoed nothing hands back, and what a struct built without the
// field says by default. An index would have made zero mean "the first block on
// the page", which is a real block and the wrong one to settle.
func (a *app) echoPending() uint64 {
	if !a.hosted() {
		return 0
	}
	at := len(a.entries) - 1
	if at < 0 || a.entries[at].kind != entryUser {
		return 0
	}
	a.entries[at].pending = true
	a.entries[at].stale = true
	a.echoAt, a.echoTok = at, a.echoTok+1
	return a.echoTok
}

// echoConfirmed takes the mark off: the engine has the sentence and the turn it
// opened is the turn on screen.
//
// IT SETTLES THE LINE IT WAS MADE FOR AND NEVER "WHATEVER IS MARKED NOW". The
// answer carries the index the submit stamped on it, and an index that is not
// the one currently outstanding settles nothing — which is what makes a repeated
// answer (a redial's replay, as it looks from up here) harmless, and what stops
// one message's answer from un-marking the next message's line.
//
// It is safe on a surface that echoed nothing, because both of the doors that
// call it — the answer to a submit, and the answer to a steer — are also the
// doors a local surface comes through, and those stamp -1.
func (a *app) echoConfirmed(token uint64) {
	at, ok := a.echoing(token)
	if !ok {
		return
	}
	a.echoAt = -1
	a.entries[at].pending = false
	a.entries[at].stale = true
	a.touch()
}

// echoing resolves one token to the block it marked, and false for a token that
// names no outstanding echo — a local submit's zero, an answer that has already
// been settled, or an answer arriving after the person typed again.
func (a *app) echoing(token uint64) (int, bool) {
	if token == 0 || token != a.echoTok {
		return 0, false
	}
	at := a.echoAt
	if at < 0 || at >= len(a.entries) || !a.entries[at].pending {
		return 0, false
	}
	return at, true
}

// echoWithdrawn takes the line back off the page and reports whether it did,
// so the caller can put the refusal where the line was.
//
// THE BLOCK IS TRUNCATED WHEN IT IS LAST AND EMPTIED IN PLACE OTHERWISE — and an emptied block gives up its pictures with its words, which is
// [feed.dropLive]'s rule and it is here for [feed.dropLive]'s reason: removing an
// entry from the middle would move every index after it, and the forming rows,
// the selection and the thought marker are all held by index. A refused message
// is nearly always the last thing on the page — nothing else has been drawn
// since the person pressed the key — so the ordinary road is the first one.
func (a *app) echoWithdrawn(token uint64) bool {
	at, ok := a.echoing(token)
	if !ok {
		return false
	}
	a.echoAt = -1
	if at == len(a.entries)-1 {
		a.entries = a.entries[:at]
	} else {
		a.entries[at].text, a.entries[at].pictures, a.entries[at].picturesHere,
			a.entries[at].pending, a.entries[at].stale = "", nil, false, false, true
	}
	// AND THE TURN NUMBER GOES BACK WITH IT. [app.submittingShown] opened a turn
	// for a message that never reached the engine, and a counter left one ahead
	// would leave every row of the NEXT turn belonging to a turn number nothing
	// else on the page shares — which is what [app.running] and the fold both
	// read to decide which rows are still live.
	if a.stream == nil && a.turn > 0 {
		a.turn--
	}
	a.touch()
	return true
}
