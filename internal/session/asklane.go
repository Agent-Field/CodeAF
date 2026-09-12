package session

// The model's question lane: the book of what it has asked, and the one road an
// answer takes back to it.
//
// AN ANSWER IS A MESSAGE TO THE CONVERSATION THAT ALSO RELEASES A WAITING CALL
// WHEN THERE IS ONE (docs/design/questions/DESIGN.md, "An answer is a message").
// Until this lane existed an answer was only the RETURN VALUE of the `ask` call
// that raised the question — the turn parked on a channel, and an answer that
// arrived with nobody parked went nowhere at all. Three things a person asked
// for are the same shape and none of them could be built on that: carrying on
// while a question stands, a clock that takes the pick when it runs out, and
// changing an answer after it was given. Each is an answer arriving when no call
// is waiting.
//
// So the answer is rendered ONCE ([askAnswerText]) and delivered one of two
// ways: handed to the call parked on it, or put on the conversation's own queue
// through [Agent.accept] — the road a background job's ending, a watch's firing
// and a landed task already take, which lands at the next step boundary and
// starts a turn on an idle conversation. There is no second delivery path, and
// the bytes are the same either way (asklane_test.go holds it).
//
// ── THERE IS ONE BOOK, AND IT IS NOT THIS FILE ──
//
// The book of what the model has asked is [askedOfThePerson] in `askwait.go`
// (#870), and this wave did not write a second beside it: two books holding one
// question was the defect that file's own doc warned about, and the lane's
// account of a question was folded INTO it rather than the other way round. So
// `askwait.go` keeps the map, the channels, the four ways an ask ends and the
// ownership rule that makes them safe; what lives here is everything an answer
// needs AFTER somebody has given one — the rendering, the delivery, the lane's
// clock, the hold, and the revision.
//
// Two of that file's rules are worth knowing at this end, because this file is
// where they are felt. The fourth ending is CARRIED ON: the call never waited
// ([askBlocking]), so there is no channel and the answer is a message whenever
// it comes — the ending the other three had no room for, and the one this wave
// is about. And the ownership moved down one field: a settled entry is KEPT so
// its answer can be changed, so the answer claims `open.wait` rather than the
// map entry ([askedOfThePerson.answerLocked]).

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// deliverAskAnswer puts one answer in front of the model when no call is waiting
// for it, through the door every other piece of owed news goes through
// ([Agent.accept]): it lands at the next step boundary of a turn that is
// running, and starts one where the conversation is idle. It reports whether
// anything took it.
func (a *Agent) deliverAskAnswer(answer Answer, text string) bool {
	origin := fromPerson
	if answer.DecidedBy != DecidedByPerson && answer.DecidedBy != DecidedByWindow {
		// The clock taking the pick is the program's own account of something
		// that happened, which is what [fromRuntime] says.
		origin = fromRuntime
	}
	receipt := a.accept(delivery{origin: origin, kind: msgResult, note: answerNote(text)})
	return receipt.state != deliveryClosed
}

// answerNote is an answer on the conversation's queue.
//
// IT CARRIES [relayNote]'S DELIVERY MARKS, and for that function's reasons: the
// wake, because a question answered on an idle conversation is owed a sentence
// and a queue alone was the whole of #690's defect (fixed by #856); `steered`, so a task node's
// runner is released by it and does not close the agent on top of an answer no
// request has carried; `authored`, because the sentence is the engine's account
// of what happened rather than a line the person typed. It is NOT batched under
// "while you worked": an answer somebody is waiting to act on keeps its own
// shape.
func answerNote(text string) userMessage {
	note := wakeNote(text)
	note.authored = true
	note.steered = true
	note.batch = false
	return note
}

// ── what the model reads ────────────────────────────────────────────────────

// askAnswerText is the ONE rendering of an answer for the model: a sentence
// saying what happened, and the whole [Answer] under it in the asker's own keys.
//
// THE BYTES ARE THE SAME WHETHER A CALL WAS PARKED OR NOT. A model that asked
// without waiting must read exactly what a model that waited reads, or the two
// forms are two contracts and the second one is the one nobody tested
// (asklane_test.go holds it).
//
// THE SENTENCE IS THERE BECAUSE THE JSON CANNOT SAY IT. `"decidedBy":"dial"` is
// a field name; "nobody answered before the clock ran out, so your pick stands
// for now" is what it MEANS, and a provisional decision the model reads as a
// settled one is a model that never mentions it again.
func askAnswerText(q Question, answer Answer, theirs map[string]string, prior *Answer) string {
	return askAnswerLead(q, answer, prior) + "\n" + answerJSON(askInTheirKeys(answer, q, theirs))
}

// askAnswerLead is that sentence. Its first word is the fact — answered,
// changed, or taken — and the question's own id and head follow it, because an
// answer that arrives as a message may arrive several steps after the call that
// asked for it and the model has to know which question it is about.
func askAnswerLead(q Question, answer Answer, prior *Answer) string {
	head := strings.TrimSpace(q.Head)
	switch {
	case answer.Revises:
		was := answerWords(q, prior)
		now := answerWords(q, &answer)
		lead := fmt.Sprintf("%s · %s · %s", askChangedWord, askQuestionWord(q), head)
		if was != "" && now != "" {
			lead += fmt.Sprintf(" · was %s, now %s", was, now)
		}
		return lead + " · " + askChangedNote
	case answer.DecidedBy == DecidedByDial:
		return fmt.Sprintf("%s · %s · %s · %s", askAnsweredWord, askQuestionWord(q), head, askProvisionalNote)
	default:
		return fmt.Sprintf("%s · %s · %s", askAnsweredWord, askQuestionWord(q), head)
	}
}

// askedText is what the asker is told when it said the turn does not wait: the
// question is up, and the answer will find it.
func askedText(q Question) string {
	return fmt.Sprintf("%s · %s · %s · %s",
		askAskedWord, askQuestionWord(q), strings.TrimSpace(q.Head), askCarryOnNote)
}

// The words this lane says to the model, spelled once because the manual quotes
// them and because the sentence the model is told to expect
// ([askCarryOnNote]) is the sentence an answer actually opens with.
const (
	askAnsweredWord = "answered"
	askChangedWord  = "changed"
	askAskedWord    = "asked"
	// askProvisionalNote is the whole of what `DecidedBy: dial` means, said in
	// the words a person would use.
	askProvisionalNote = "provisional: nobody answered before the clock ran out, so your pick stands for now and they can still change it"
	// askChangedNote is what a correction obliges: whatever was done on the old
	// answer is now the thing to look at.
	askChangedNote = "they changed their answer; anything you did on the old one needs revisiting"
	// askCarryOnNote is the non-blocking contract, in one sentence.
	askCarryOnNote = "it is on their screen; carry on with anything that does not depend on it — their answer arrives as a message opening `" + askAnsweredWord + "`"
)

// askQuestionWord names the question the way both sentences name it, so a model
// reading an answer several steps later can match it to the call it made.
func askQuestionWord(q Question) string {
	if ref := strings.TrimSpace(q.Ref); ref != "" {
		return "question " + ref
	}
	return "question " + strconv.FormatUint(q.ID, 10)
}

// answerWords is what an answer picked, as a person read it: the labels the
// question gave those keys, and the keys themselves where it gave none. It is
// [DecisionRecord.Words] for an answer that has not been written down yet.
func answerWords(q Question, answer *Answer) string {
	if answer == nil {
		return ""
	}
	words := make([]string, 0, 2)
	for _, key := range answer.Keys() {
		if option, ok := q.Option(key); ok && strings.TrimSpace(option.Label) != "" {
			words = append(words, strings.TrimSpace(option.Label))
			continue
		}
		words = append(words, key)
	}
	if len(words) == 0 {
		return strings.TrimSpace(answer.Words())
	}
	return strings.Join(words, ", ")
}

// answerJSON is the answer itself. An answer that will not encode is a
// programming error rather than anything a person can act on, so what goes out
// is the sentence above it and the one field the model needs.
func answerJSON(answer Answer) string {
	data, err := json.Marshal(answer)
	if err != nil {
		return `{"key":"` + answer.FirstKey() + `"}`
	}
	return string(data)
}

// ── the clock ───────────────────────────────────────────────────────────────

// startAskClock starts the policy's clock on a question that has one.
//
// THE LENGTH IS THE PERSON'S OWN NUMBER AND NOT A NEW ONE. It is the `After`
// they set for this shape of question with `/autonomy` ([Agent.SetAutonomy]),
// and where they set none it is the one default this program already has for the
// shape that gets one — the assumption kind's [awayAfter], which is the boundary
// this program uses everywhere else to conclude that nobody is at the keyboard.
// So a pick is taken for somebody only after exactly as long as it would take to
// decide they are not coming.
//
// THE CLOCK BELONGS TO THE LANE AND NOT TO THE CALL. It used to live in
// `executeAsk`'s select, which meant a question nobody was parked on had no
// clock at all — and a question nobody is parked on is precisely the one this
// wave added.
func (a *Agent) startAskClock(open *askOpen) {
	if open.q.Policy.Kind != PolicyRecommendThenAuto || open.q.Policy.After <= 0 {
		return
	}
	id := open.q.ID
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	open.clock = time.AfterFunc(open.q.Policy.After, func() { a.askClockRanOut(id) })
	a.mu.Unlock()
}

// stopAskClocksLocked is the session's own end of the book's close, called from
// [Agent.Close] beside [Agent.stopSteerGraceLocked] — the two laws are one law,
// and steer_grace.go's file comment states it: NOTHING ARMED OUTLIVES THE
// SESSION THAT ARMED IT.
//
// IT IS A SEAM AND NOT A SECOND RULE. Everything it does is the book's
// ([askedOfThePerson.stopClocksLocked], which stops each armed clock and drops
// each settled entry); this exists so that `agent.go` names an ask-lane verb
// rather than reaching into another file's map, and so that the one place a
// reader looks for what Close lets go of finds this lane in the list.
//
// It is called with a.mu already held, like every other `Locked` verb here.
func (a *Agent) stopAskClocksLocked() { a.asked.stopClocksLocked() }

// askClockRanOut takes the asker's own pick for the person, through the one door
// every other answer goes through — so it is recorded, announced and delivered
// exactly as a key press would be, and marked as what it is.
func (a *Agent) askClockRanOut(id uint64) {
	a.mu.Lock()
	open := a.asked.atLocked(id)
	if open == nil || open.settled != nil || open.held || a.closed {
		// NOTHING THIS CLOCK COULD SAY WOULD REACH ANYBODY once the session has
		// closed, and a decision nobody read is worse than no decision: the next
		// run of this conversation reads the record as something the person
		// settled. [Agent.stopAskClocksLocked] is the other half of that law.
		a.mu.Unlock()
		return
	}
	q := open.q
	a.mu.Unlock()
	_ = a.ResolveQuestion(defaultAnswer(q, ""))
}

// HoldQuestion stops the clock on one question a person is looking at, without
// answering it, and tells every window so.
//
// IT IS [Agent.ResolveQuestion]'S SHAPE FOR THE OTHER THING A KEY CAN MEAN. A
// key pressed on a question is evidence that somebody is there, and a clock that
// answered over their hands would be the one answer this program must never give
// on their behalf (tui3's [app.tickQuestion] states it for the surface's own
// reading clock). The lane's token is the token an answer names, so a surface
// holds a question with what it already has.
//
// THERE IS NO WAY BACK, for that same reading: a clock that resumed after a few
// idle seconds would fire exactly when somebody had looked away mid-decision.
func (a *Agent) HoldQuestion(kind QuestionKind, token string) {
	switch kind {
	case QuestionTask:
		id, err := strconv.ParseUint(strings.TrimSpace(token), 10, 64)
		if err != nil {
			return
		}
		a.HoldTask(id)
	case QuestionAsk:
		id, err := strconv.ParseUint(strings.TrimSpace(token), 10, 64)
		if err != nil {
			return
		}
		a.holdAsk(id)
	}
}

// holdAsk is the ask lane's own hold: the clock stops, the deadline goes, and
// the question is said again with the countdown off it.
func (a *Agent) holdAsk(id uint64) {
	a.mu.Lock()
	open := a.asked.atLocked(id)
	if open == nil || open.settled != nil || open.held || open.q.Deadline.IsZero() {
		a.mu.Unlock()
		return
	}
	open.held = true
	if open.clock != nil {
		open.clock.Stop()
	}
	open.q.Deadline = time.Time{}
	q := open.q
	a.mu.Unlock()
	a.restateQuestion(q)
}

// restateQuestion says one question again because a fact ON it changed — its
// clock stopped — rather than because it is new.
//
// A QUESTION IS RESTATED AND NEVER RE-RAISED. Withdrawing it and asking it again
// would put `no longer needed` on somebody's screen about the question they are
// reading, and would hand it a fresh settle guard and a fresh reading clock
// under their hand (tui3's own replace keeps both). So the word book and the
// desk row are overwritten in place and the lane says it once more; every
// surface's upsert takes the newer object and keeps what it knows.
func (a *Agent) restateQuestion(q Question) {
	a.putBackQuestion(q)
	a.presenceRestateQuestion(q)
	a.emitQuestion(EventQuestion, q, nil)
}

// ── changing an answer ──────────────────────────────────────────────────────

// The refusals a revision can meet, and the two an answer can. Each says what a
// person can do next, on [Question.Check]'s terms.
var (
	errRevisionIrreversible = errors.New(
		"session: that decision cannot be changed — it was answered as irreversible, and what it allowed has already happened")
	errRevisionGone = errors.New(
		"session: that decision is from an earlier run of this conversation and cannot be changed from here — ask again instead")
	errRevisionLane = errors.New(
		"session: only a question this conversation asked, or a permission, can be changed after it is answered")
	errAnswerGone = errors.New(
		"session: nothing is waiting on that question here any more — the turn moved on, or this conversation's engine restarted since it was asked")
	// errAnswerSettled is the loser of a race, and it is never shown to
	// anybody: a second answer to a settled question is not a fault, it simply
	// changes nothing ([Agent.answerAsk] says why it cannot be silent either).
	errAnswerSettled = errors.New("session: that question was already answered")
	// errAnswerClosed is an answer nothing can read.
	errAnswerClosed = errors.New("session: this conversation has closed; the answer could not be given to it")
)

// Revisable reports whether an answer to this question can still be changed
// after it was given — the one reading, used by the surface that offers the key
// and by the door that takes the revision, so a receipt can never offer what the
// engine will refuse.
//
// IT IS KEYED ON WHAT THE ANSWER DID, not on which lane asked. Two properties
// decide it. An irreversible decision is never revisable: what it allowed has
// already happened, and "changing" it would be a second decision wearing the
// first one's receipt. Everything else is revisable exactly where the answer can
// be GIVEN AGAIN — the model's own question, whose answer is a message and can
// therefore be sent a second time saying what changed, and a permission, whose
// widening yes is a grant and can be taken back ([Agent.undoGrant]). Every other
// lane's answer moved work: a task started, a landing landed, a service
// connected.
func (q Question) Revisable() bool {
	if q.Stakes == StakesIrreversible {
		return false
	}
	return q.Kind == QuestionAsk || q.Kind == QuestionConsent
}

// reviseLane applies one revision and answers the question it was about, so the
// caller can record it and announce it exactly as it does a first answer.
//
// It answers what was asked and what was decided BEFORE, so the caller can
// record a line that says what it replaced.
//
// IT IS THE LANE SWITCH [Agent.applyToLane] IS, for the one verb that lane
// switch cannot carry: `applyToLane` hands an answer to a resolver that is
// waiting for it, and a revision is by definition an answer nothing is waiting
// for. [Question.Revisable] is what says which lanes take one, and it is the
// same function the receipt asks before it offers the key.
func (a *Agent) reviseLane(answer Answer) (Question, *Answer, error) {
	switch answer.Kind {
	case QuestionAsk:
		a.mu.Lock()
		open, settled := a.asked.settledLocked(answer.ID)
		var q Question
		var prior *Answer
		if settled {
			q, prior = open.q, open.settled
		}
		a.mu.Unlock()
		if !settled {
			return Question{}, nil, errRevisionGone
		}
		if !q.Revisable() {
			return Question{}, nil, errRevisionIrreversible
		}
		return q, prior, a.answerAsk(answer)
	case QuestionConsent:
		q, found := a.decidedConsent(answer.ID)
		if !found {
			return Question{}, nil, errRevisionGone
		}
		if !q.Revisable() {
			return Question{}, nil, errRevisionIrreversible
		}
		prior := a.decidedConsentAnswer(q, answer.ID)
		a.reviseConsent(q, answer)
		return q, prior, nil
	}
	return Question{}, nil, errRevisionLane
}

// decidedConsentAnswer is what the record says was answered last time, as an
// answer, so a changed permission's line says what it replaced.
func (a *Agent) decidedConsentAnswer(q Question, id uint64) *Answer {
	records := a.Decisions()
	for at := len(records) - 1; at >= 0; at-- {
		record := records[at]
		if record.Kind != QuestionConsent || record.ID != id {
			continue
		}
		return &Answer{Kind: QuestionConsent, ID: id, Picked: record.Picked, DecidedBy: record.By}
	}
	return nil
}

// decidedConsent is the permission this id names, rebuilt from the record this
// conversation kept of it. The gate's own question is long gone — the call ran
// or did not — and what a revision needs is what was asked, what it was about
// and what it cost, all of which the record holds.
func (a *Agent) decidedConsent(id uint64) (Question, bool) {
	records := a.Decisions()
	for at := len(records) - 1; at >= 0; at-- {
		record := records[at]
		if record.Kind != QuestionConsent || record.ID != id {
			continue
		}
		return Question{
			ID: record.ID, Kind: QuestionConsent, Ask: AskPermission, Form: FormLine,
			Asker: Asker{Kind: AskerEngine}, Head: record.Head, Subject: record.Subject,
			Options: AnswerOptions(QuestionConsent), Stakes: record.Stakes,
		}, true
	}
	return Question{}, false
}

// reviseConsent takes back what a widening yes bought, through the one door that
// knows every store a yes was written into ([Agent.undoGrant]).
func (a *Agent) reviseConsent(q Question, answer Answer) {
	tool := strings.TrimSpace(q.Subject.Name)
	if tool == "" {
		return
	}
	action, ok := AnswerFromKey(QuestionConsent, answer.FirstKey())
	if ok && action.Allow && action.Scope == ConsentToolSession {
		// A REVISION THAT WIDENS AGAIN IS NOT A REVOCATION. The person moved
		// from one always to another; what they granted stands as it is.
		return
	}
	a.undoGrant(tool)
}
