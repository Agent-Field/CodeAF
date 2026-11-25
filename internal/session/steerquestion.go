package session

// A PERSON WHO TALKS PAST A QUESTION HAS ANSWERED IT: THEY SAID SOMETHING ELSE.
//
// ── THE MEASURED WEDGE (2026-09-10, two conversations) ──
//
// The model called `ask`, the tool parked on the wait (askwait.go), and the person —
// looking at a question about painting genres — typed `portrait` into the
// composer rather than pressing the key beside the answer. A turn was running,
// so the surface sent it down the one door that takes words mid-turn
// ([Agent.Steer]), and the splice took the only road left to it: no generation
// to cut and no bash to adopt, so the correction was accepted as
// `waiting for the running step` and queued for the next step boundary.
//
// THE RUNNING STEP WAS THE QUESTION. It ends when somebody answers it, and the
// person had just spent their answer on the other door. So the turn waited for a
// tool that was waiting for the person, the person waited for a turn that was
// never going to move, and NOTHING WENT TO THE MODEL AT ALL — for seven minutes
// and fifty-two seconds in one of the two, until the person pressed Escape. The
// interrupt then dropped the correction with the stopped turn
// ([Agent.nextFollowUpLocked]'s drain law), so the words they typed were never
// read by anything.
//
// ── SO A STEER RETIRES THE MODEL'S OWN OPEN QUESTION ──
//
// [Agent.talkedPastQuestionsLocked] takes every `ask` this agent is parked on —
// both the wait and the words, under one lock — hands the tool [askTalkedPast]
// instead of an answer, and withdraws the question from every surface drawing
// it. The tool result lands, the batch ends,
// the boundary opens, and the ordinary drain at the head of the loop puts the
// person's words in front of the model — the whole of it inside
// [lane.SpokenWithin] of the steer rather than never.
//
// WHAT THE MODEL READS IS NOT AN ANSWER AND MUST NOT LOOK LIKE ONE. A question
// released this way produces no [Answer], no key, no [DecisionRecord] — nothing
// was decided, and a record claiming otherwise would put a choice the person
// never made in front of every later turn. It says only that the question went
// unanswered and that their words are the next thing to read, which is exactly
// what happened.
//
// ── ONLY THE MODEL'S OWN QUESTION ──
//
// This reaches [QuestionAsk] and nothing else. A consent request, a task card, a
// standing arrangement and a landing each have a subject that outlives the turn
// and a resolver that means something by silence; releasing one because somebody
// typed a sentence would be this file answering a question it was not asked. The
// `ask` lane is the only one whose whole purpose is to hold THIS turn open for
// THIS person, which is why it is the only one a sentence from that person may
// close.
//
// And it is THIS agent's questions and no other. A sentence aimed at a running
// node goes through [Agent.SteerTask], which is that node's own door into its
// own agent (steer.go says why the two never merge), so a node parked on a
// question of its own is retired by the person who is in ITS room and by nobody
// else.
//
// ── WHAT A READER CAN PREDICT FROM THE ABOVE ──
//
// Several questions standing at once are ALL retired, because the sentence is
// the person's response to what is in front of them and not to one row of it.
// An unmistakable `stop` comes down the same road and retires them the same way,
// for the same reason it reaches a command of any age: a person who said stop is
// not waiting to be asked again. And nothing new happens afterwards — there is
// no resume branch here. The turn goes back round [Agent.runTurn]'s own loop,
// through the same drain and the same send every other step uses, carrying the
// question's result and then their words.

import (
	"strconv"
	"strings"
)

// askTalkedPast is what the `ask` tool hands back when the person said
// something else instead of answering.
//
// IT IS NOT [askRefusedLead]. That lead exists to tell a model that nothing was
// ever drawn, so it does not talk about a form nobody saw; here the question was
// drawn, read, and declined in favour of a sentence. The second half is the part
// that stops the model asking the same thing again: their words are already on
// their way, so the next request carries them.
const askTalkedPast = "the person did not answer this question: they said something else instead, and their words are the next thing you will read. Take those words as their answer; ask again only if what they said leaves the decision open."

// askTalkedPastReason is why the question stopped standing, in the words a
// person would use about their own act. It is what [Withdrawal.Reason] carries
// onto every surface still drawing the row.
const askTalkedPastReason = "you said something else instead"

// askTalkedPastNote is what the model reads about a question it was not
// waiting on (a [Question.Later] one, one a task is stopped on) that the
// person's words took down: no call is left to hand [askTalkedPast] to.
const askTalkedPastNote = "the person said something else instead of answering it, and it is no longer on their screen: nothing was decided. Carry on without the answer; ask again later only if you still need it."

// steerTookTheQuestion is where the correction landed when it retired a
// question. It is a [SteerNote.Landing] and therefore person-facing: it names
// what their sentence DID, which is the one thing the three other landings have
// in common.
const steerTookTheQuestion = "took this instead of the question"

// talkedPastQuestionsLocked ends every `ask` this session is parked on because
// the person spoke instead of answering, and answers the questions it retired
// for the caller to say out loud once the lock is down.
//
// The ending itself belongs to [askedOfThePerson] and not to this file — it is
// one of the endings an ask has, beside the answer and the let-go, and it is
// written there with them (askwait.go).
//
// ── IT CLAIMS THE WORDS IN THE SAME BREATH AS THE WAIT ──
//
// TWO MAPS HOLD ONE QUESTION and both have to be taken here. `a.asked` holds
// what the lane is parked on; `a.questionWords` holds what every surface is
// drawing. Closing the channel makes that lane runnable while this lock is still
// held, and the first thing it does on the way out is its own deferred
// withdrawal ([Agent.rememberQuestion]) — with [questionGoneReason]'s sentence,
// `the turn moved on without it`, which is the consent line's fact and not this
// person's act. Claiming the words here makes that road provably find nothing,
// so what they read about their own sentence is a claim rather than a coin flip.
// It is askwait.go's own rule — THE ENTRY IS THE OWNERSHIP — kept for the second
// map as well as the first.
//
// keepSettled is [askedOfThePerson.talkedPastLocked]'s: true leaves answered
// questions kept for a revision standing.
func (a *Agent) talkedPastQuestionsLocked(keepSettled bool) []Question {
	retired := a.asked.talkedPastLocked(keepSettled)
	if len(retired) == 0 {
		return nil
	}
	taken := make([]Question, 0, len(retired))
	for _, id := range retired {
		// A question with nothing banked was never drawn and there is nobody to
		// tell; the wait is ended either way, which is what unwedges the turn.
		if q, said := a.claimQuestionLocked(QuestionAsk, strconv.FormatUint(id, 10), false); said {
			taken = append(taken, q)
		}
	}
	return taken
}

// sayTheQuestionsCameDown tells every surface, with the reason. It runs with
// a.mu DOWN, as [Agent.Steer]'s other announcements do, and through the same
// [Agent.sayWithdrawn] every other withdrawal uses — so there is one spelling of
// what a withdrawn question looks like and this road only supplies the sentence.
func (a *Agent) sayTheQuestionsCameDown(taken []Question) {
	for _, q := range taken {
		a.sayWithdrawn(q, askTalkedPastReason)
		// A call parked on it reads [askTalkedPast]; one nothing waited on is
		// told at the model's next step, as a dismissed one is.
		if q.Later || len(q.Blocking.Tasks) > 0 {
			a.enqueueNextTurnNote("Your question \"" + strings.TrimSpace(q.Head) + "\": " + askTalkedPastNote)
		}
	}
}

// personClosesQuestionsLocked ends every question this agent still has open
// when the person's own new message arrives, whether a turn is running or not,
// and answers the questions it retired for the caller to say out loud once the
// lock is down.
//
// A REPLY FROM THE PERSON IS THEIR ANSWER TO WHAT WAS ON THE SCREEN. A steer
// already retired every ask; a fresh turn retired nothing, so a question the
// turn did not wait for ([Question.Later]) or one a task is stopped on stood in
// the Inbox after the person had moved on. It is the steer's own road
// ([Agent.talkedPastQuestionsLocked]), so the receipt and the Inbox read the
// same either way, and the model is free to ask again if it still needs to.
//
// Only the person's own doors count ([userMessage.fromPerson]): a wake note, a
// runner's note, factory work, a setup turn or an answer to a question is not
// somebody replying.
//
// AN ANSWERED QUESTION IS NOT OPEN. One the person already answered is kept
// only so its receipt can still change the answer ([askSettledKept]); a new
// message leaves it standing, or typing anything after answering would make
// `change` on that receipt refuse.
func (a *Agent) personClosesQuestionsLocked(user userMessage) []Question {
	if !user.fromPerson {
		return nil
	}
	return a.talkedPastQuestionsLocked(true)
}

// notePersonWordsLocked keeps a person's message that joined the running turn
// as that turn's latest words from them ([Agent.personWords]).
func (a *Agent) notePersonWordsLocked(user userMessage) {
	if user.fromPerson {
		a.keepPersonWordsLocked(user.text())
	}
}

// keepPersonWordsLocked makes words the running turn's latest words from the
// person, and lets a server workstream agent's web_fetch read the pages they
// name (tools_web_leash.go).
func (a *Agent) keepPersonWordsLocked(words string) {
	a.personWords = words
	a.webLeash.note(words)
}

// turnPersonWords is the person's latest own words in the running turn, and
// "" when the turn holds none.
func (a *Agent) turnPersonWords() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.TrimSpace(a.personWords)
}
