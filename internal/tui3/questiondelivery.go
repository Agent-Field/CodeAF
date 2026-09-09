package tui3

// ── PRESENCE-AWARE DELIVERY, DECIDED IN ONE PLACE ───────────────────────────
//
// A question is raised by a lane that has no idea where the person is standing.
// This file is the only thing on this surface that answers that, and it answers
// it once for the block above the box, for home, for the desktop notification
// and for the bell — because a rule spelled four times is four definitions of
// "away" the first week one of them moves.
//
// ── THE ROWS THIS FILE CAUSES, EXACTLY AS THEY ARE DRAWN ────────────────────
//
// ON THE PAGE nothing here is drawn at all. The question goes to the block over
// the message box (question.go), which is where every question this surface can
// see is answered; this file only said that it may.
//
// ON ANOTHER PAGE of this same window — home, the tasks place, a room — the
// question stays counted by the chip and one dim row says where it went, on
// whichever line that place already has for saying things ([app.pageMsg], or
// home's own [homeView.say]):
//
//	allow rm -rf build? · waiting in this conversation · alt+a
//
// AWAY — ten minutes with nobody at this keyboard — and the project's rule
// could not take it: the same row on home, the desktop notification this
// surface already sends for a question nobody can see (notify.go), and, for a
// question something is BLOCKED on, one bell. Once, ever, per question:
//
//	allow rm -rf build? · waiting in this conversation · alt+a
//
// AWAY, and the project's rule took it: NOTHING ON SCREEN, which is not the
// same as nothing said. The answer goes through the one door and comes back as
// the receipt every answer leaves, with the dial named as who decided:
//
//	decided allow rm -rf build? → allow once · the rule · 14:02
//
// AND AT A STEP'S BOUNDARY, where several quiet questions were raised inside one
// piece of work, they arrive together as the sheet (questionsheet.go) instead of
// one at a time on top of each other.
//
// ── THE TWO LAWS THIS FILE IS THE ENFORCEMENT OF ────────────────────────────
//
//   - PRESENCE-AWARE DELIVERY. docs/design/questions/DESIGN.md: "On the page:
//     pinned now. On home or another page: in the row, plus the chip. Away
//     ([awayAfter], 10m without a key): dial resolves what it may and records
//     DecidedBy: dial; the rest go to home and the phone; the terminal bell
//     rings once for a blocking question only."
//   - BATCHED AT THE BOUNDARY. "Non-blocking questions raised inside one step
//     arrive together as a sheet at the step's end; a blocking one arrives at
//     once with the count of what is behind it."

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// awayAfter is THE ONE ABSENCE BOUNDARY, and it is deliberately measured from
// the last KEY rather than from the last frame or the last focus event.
//
// A window can be focused and unwatched — a second monitor, a tab behind a
// browser — and it can be blurred with somebody reading it. The only thing this
// program can honestly claim is that somebody touched this keyboard, so that is
// what the clock is on. Ten minutes is DESIGN.md's own number.
const awayAfter = 10 * time.Minute

// questionPresence is where the person is, as far as this window can tell.
type questionPresence uint8

const (
	// questionOnPage is the conversation itself, with a hand on the keyboard.
	questionOnPage questionPresence = iota
	// questionOtherPage is this window, somewhere else in it — home, a place, a
	// room. The question is still THIS person's; they are simply not looking at
	// the block.
	questionOtherPage
	// questionAway is [awayAfter] with nobody at this keyboard.
	questionAway
)

// questionDelivery is what the rule asks its caller to do with one question.
//
// IT IS A DESCRIPTION AND NOT AN ACT. Sending an answer, painting a row, ringing
// a terminal and posting a notification are four different doors with four
// different failure modes, and a rule that called them itself could not be
// tested without a terminal. So the rule answers in facts and [app.deliverQuestion]
// is the one place those facts become commands.
type questionDelivery struct {
	// Pin is the question to put on the block above the box, or empty.
	Pin *session.Question
	// Note is the one dim row for a place that is not the conversation.
	Note string
	// Answer is the project's rule taking the question while nobody is here. It
	// carries [session.DecidedByDial], which is what the receipt says out loud.
	Answer *session.Answer
	// Phone is the desktop notification, for a question nobody can see.
	Phone bool
	// Bell is the terminal bell, and it is true at most once per question and
	// only for one something is blocked on.
	Bell bool
}

// questionDeliveryRule owns presence, the boundary batch and the bell's memory.
//
// pending is keyed by a STEP TOKEN because only questions born inside the same
// step batch together: two steps' questions arriving as one sheet would be a
// sheet about two different pieces of work, which is the thing the sheet exists
// to stop being.
type questionDeliveryRule struct {
	pending map[string][]session.Question
	// rung is every question that has already had its bell. A question is
	// re-emitted whenever a surface attaches ([session.Agent.WatchQuestions]
	// replays), and a terminal that rang on every reattach would be a terminal
	// that rings for a decision somebody made an hour ago.
	rung map[string]bool
}

func newQuestionDeliveryRule() questionDeliveryRule {
	return questionDeliveryRule{pending: map[string][]session.Question{}, rung: map[string]bool{}}
}

// questionToken is the one string a question is known by across this file and
// the sheet: the lane and the lane's own id, because two lanes may both be
// waiting on id 7 ([questionShown.token] is the same string from the block's
// side).
func questionToken(q session.Question) string { return string(q.Kind) + ":" + q.Token() }

// questionPresenceNow is where this window thinks the person is.
//
// THE ORDER OF THE THREE TESTS IS THE LAW'S OWN. Away outranks everything: a
// window left open on home for an hour is not "on another page", it is a window
// nobody is at, and delivering to a row nobody will read is the failure the away
// rule exists to prevent.
func (a *app) questionPresenceNow() questionPresence {
	if a.now().Sub(a.lastQuestionKey) >= awayAfter {
		return questionAway
	}
	if a.pageShowing() {
		return questionOtherPage
	}
	return questionOnPage
}

// deliver places one question, and is the whole of the rule.
func (d *questionDeliveryRule) deliver(q session.Question, step string, presence questionPresence, now time.Time) questionDelivery {
	if q.Withdrawn != nil {
		// A WITHDRAWN QUESTION IS NEVER DELIVERED. Its one dim line is the
		// block's ([app.withdrawQuestion]); arriving here it is simply not a
		// question any more, and nothing is pinned, noted, rung or answered.
		return questionDelivery{}
	}
	if presence == questionAway {
		if answer, ok := dialAnswer(q, now); ok {
			return questionDelivery{Answer: &answer}
		}
		out := questionDelivery{Note: questionWaitingLine(q), Phone: true}
		if q.Blocking.Blocks() && !d.rung[questionToken(q)] {
			d.rung[questionToken(q)] = true
			out.Bell = true
		}
		return out
	}
	if step != "" && !q.Blocking.Blocks() {
		// BATCHED AT THE BOUNDARY. A quiet question raised inside a step is
		// held for [questionDeliveryRule.boundary]; nothing is drawn now,
		// because a question landing on top of the last one is the thing the
		// sheet replaces.
		d.pending[step] = appendQuestionOnce(d.pending[step], q)
		return questionDelivery{}
	}
	if presence == questionOtherPage {
		return questionDelivery{Pin: &q, Note: questionWaitingLine(q)}
	}
	return questionDelivery{Pin: &q}
}

// boundary releases every quiet question one step gathered, oldest first. The
// caller turns two or more of them into the sheet and one of them into the
// ordinary block ([app.questionBoundary]).
func (d *questionDeliveryRule) boundary(step string) []session.Question {
	held := d.pending[step]
	delete(d.pending, step)
	return held
}

// forget drops a withdrawn question from whatever step is still gathering, so a
// boundary cannot release a decision that stopped needing to be made.
func (d *questionDeliveryRule) forget(q session.Question) {
	token := questionToken(q)
	for step, held := range d.pending {
		kept := held[:0]
		for _, one := range held {
			if questionToken(one) == token {
				continue
			}
			kept = append(kept, one)
		}
		if len(kept) == 0 {
			delete(d.pending, step)
			continue
		}
		d.pending[step] = kept
	}
}

// appendQuestionOnce replaces by token rather than appending, for
// [app.raiseQuestion]'s reason: the engine re-emits an open question whenever a
// surface attaches, and a batch that grew a row on every reattach would arrive
// as a sheet with one decision on it four times.
func appendQuestionOnce(questions []session.Question, q session.Question) []session.Question {
	for i := range questions {
		if questionToken(questions[i]) == questionToken(q) {
			questions[i] = q
			return questions
		}
	}
	return append(questions, q)
}

// dialAnswer is what the project's rule may take while nobody is here, and it
// is deliberately the NARROWEST reading of that permission.
//
// FOUR REFUSALS, AND EACH IS A LAW RATHER THAN A CAUTION. Irreversible stakes
// are never taken by a clock (the engine's own gate refuses to put one on such a
// question at all, [session.Question.Check]); a clarification never runs on one,
// because the answer is information only the person has; a confirmation never
// does, because it is what is asked before something destructive; and a question
// with NO PICK has nothing to take — inventing one would be the surface deciding
// on somebody's behalf, which is F41's defect exactly.
func dialAnswer(q session.Question, now time.Time) (session.Answer, bool) {
	if q.Stakes == session.StakesIrreversible || q.Pick == nil {
		return session.Answer{}, false
	}
	if q.Ask == session.AskClarification || q.Ask == session.AskConfirmation {
		return session.Answer{}, false
	}
	key := strings.TrimSpace(q.Pick.Key)
	if key == "" {
		return session.Answer{}, false
	}
	if _, ok := q.Option(key); !ok {
		return session.Answer{}, false
	}
	ready := q.Policy.Kind == session.PolicyDecide
	if q.Policy.Kind == session.PolicyRecommendThenAuto {
		deadline := q.Deadline
		if deadline.IsZero() && !q.Asked.IsZero() {
			deadline = q.Asked.Add(q.Policy.After)
		}
		ready = !deadline.IsZero() && !now.Before(deadline)
	}
	if !ready {
		return session.Answer{}, false
	}
	return session.Answer{
		At: now, Kind: q.Kind, ID: q.ID, Ref: q.Ref, Ask: q.Ask,
		Key: key, Picked: []string{key}, DecidedBy: session.DecidedByDial,
	}, true
}

// The words this file says, spelled once and quoted in the manual exactly.
const (
	// questionWaitingWord is the tail of the row a place that is not the
	// conversation draws. It says WHERE the question is and how to reach it,
	// because a row that only said a question existed would be a row that made
	// somebody hunt for it.
	questionWaitingWord = " · waiting in this conversation · " + questionChipKey
)

// questionWaitingLine is that row: the asker's own sentence, and where to answer
// it. It is never the answers — those are on the block, and a place that offered
// keys it could not take would be offering an answer that goes nowhere.
func questionWaitingLine(q session.Question) string {
	return strings.TrimSpace(q.Head) + questionWaitingWord
}

// ── the door ────────────────────────────────────────────────────────────────

// deliverQuestion is the ONE place the rule's facts become acts.
func (a *app) deliverQuestion(q session.Question) tea.Cmd {
	out := a.questionReach.deliver(q, a.questionStep(), a.questionPresenceNow(), a.now())
	var cmds []tea.Cmd
	if out.Answer != nil {
		// THE RULE'S ANSWER GOES THROUGH THE ONE DOOR, exactly as a person's
		// does, so a decision taken while nobody was here is recorded, resolved
		// and drawn by the same code that draws every other one.
		if doors, ok := a.questionDoors(); ok {
			answer := *out.Answer
			cmds = append(cmds, func() tea.Msg { _ = doors.ResolveQuestion(answer); return nil })
		}
	}
	if out.Pin != nil {
		// A QUESTION OUTRANKS A PANEL, on question.go's terms: a block drawn
		// under a fullscreen overlay is a question nobody can see to answer.
		a.closeSettings()
		a.closeExpand()
		a.raiseQuestion(questionShown{question: *out.Pin})
	}
	if out.Note != "" {
		a.sayWhereQuestionWent(out.Note)
	}
	if out.Phone {
		cmds = append(cmds, a.notifyAsk())
	}
	if out.Bell {
		cmds = append(cmds, tea.Raw("\a"))
	}
	return tea.Batch(cmds...)
}

// sayWhereQuestionWent puts the note on whichever line the place a person is
// standing on already has for saying things. There is no third line invented
// for this: home has [homeView.say] and every other place has [app.pageMsg].
func (a *app) sayWhereQuestionWent(note string) {
	if a.at(pageHome) {
		a.home.say(note, "")
		return
	}
	a.pageMsg = note
}

// questionStep is the token quiet questions gather under, and "" when there is
// no step to gather inside.
//
// NOTHING BATCHES OUTSIDE A LIVE TURN, and that is the boundary rule read
// literally: a question raised while nothing is running has no boundary coming,
// so holding it would be holding it forever.
//
// AND THE STEP THIS SURFACE CAN HONESTLY SEE IS "UNTIL THE MODEL SPEAKS AGAIN".
// The engine has no step event, and what a person experiences as one step is a
// batch of tool calls between two things the model said — so that is what is
// counted ([app.questionStepEnded] is called from the reducer on the first text
// of the next reply and on the turn ending). It is coarser than the engine's own
// idea of a step and it is named for what it actually measures rather than for
// what would have been convenient.
func (a *app) questionStep() string {
	if a.stream == nil {
		return ""
	}
	return "step:" + itoa(a.questionStepAt)
}

// questionStepEnded moves the token on, so the next quiet question gathers into
// a new batch rather than into the one that has just been handed over.
func (a *app) questionStepEnded() { a.questionStepAt++ }
