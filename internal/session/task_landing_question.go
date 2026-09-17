package session

// A LANDED `your call` IS A QUESTION, AND IT IS PUT TO THE PERSON THE MOMENT IT
// LANDS.
//
// ── THE RUN THIS WAS WRITTEN FROM ──
//
// A divided task landed needing somebody's look, the model spent `accept` on it
// under `task.settle = auto`, and the merge was refused by the person's own
// uncommitted copies of the files the task wrote. The node RE-SETTLED as the
// person's call — a different question, with different answers, now theirs — and
// no surface in the product drew a single answer for it: the card in the
// conversation was frozen in the shape of the first landing, the node's own room
// read `this task has finished — say it to main`, and the rail said `your call`
// with nothing to press. A bright terminal state with no handle (#767).
//
// ── WHY NOTHING DREW IT ──
//
// [Agent.landingQuestion] has built the object all along, and [Agent.OpenQuestions]
// derives it from [Agent.PendingDecisions] — but the ONLY thing that ever put a
// landing question in front of a surface was [Agent.WatchQuestions] replaying
// what was open AT THE MOMENT A WINDOW ATTACHED. A question raised after that,
// or re-shaped after that, reached nobody: no lane emitted [EventQuestion] for
// it, so a window that had been watching all along never heard.
//
// ── THE LAW ──
//
// EVERY MOVE OF A NODE IS A MOVE OF ITS QUESTION. A node that reaches
// [TaskUnverified] raises one; a node that moves while it is there — the decider
// changing hands, a refused accept re-shaping the ask from `nobody could check
// it` into `your folder already has files the task wrote` — RAISES IT AGAIN with
// the new shape, and the surface refreshes the question it is already drawing
// rather than stacking a second (internal/tui3's [app.raiseQuestion] matches on
// the token); a node that settles takes it back.
//
// TWO MOVES ARE NOT NEW QUESTIONS. A move that is the ANSWER'S OWN WORK IN
// FLIGHT — the accept whose merge is still deciding, the re-audit still
// spending its window — raises nothing and withdraws nothing: the notice
// carries [TaskNotice.Settling], and asking again within seconds with no new
// fact is how one card got answered twelve times in an hour (#1077). And a
// question somebody ANSWERED carries that answer's fate when it is raised
// again — what was answered, when, and what became of it — so the re-asked
// card never reads as though the last answer was ignored (question.go's
// [Agent.landingQuestion] consults the decision record, the raise owing the
// record what the replay already owed it).
//
// IT RIDES [Agent.emitTaskUpdate] BECAUSE THAT IS THE ONE DOOR EVERY MOVE GOES
// THROUGH. A second list of which landings have been asked about would be a
// second source of truth for a fact the graph already holds, which is the defect
// pending.go exists against; what is kept here is only WHICH SHAPE was last put
// out, so that a question can be taken back in the kind it was raised in.

import (
	"strconv"
	"strings"
	"time"
)

// landingAsked is the shape one node's landing question was last raised in, or
// "" where none is standing. It is the whole of this file's state.
func (a *Agent) landingAsked(id uint64) QuestionKind {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.landingQuestions[id]
}

// landingAsking records that shape, or forgets it.
func (a *Agent) landingAsking(id uint64, kind QuestionKind) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.landingQuestions == nil {
		a.landingQuestions = map[uint64]QuestionKind{}
	}
	if kind == "" {
		delete(a.landingQuestions, id)
		return
	}
	a.landingQuestions[id] = kind
}

// publishLandingQuestion is the law above, applied to one notice.
//
// A NODE THAT IS NOT WAITING ON ANYBODY RAISES NOTHING, and a node that WAS is
// taken back with the lane's own sentence for a question whose subject went away
// ([questionGoneReason] says `the work settled`). The withdrawal is drawn only
// where the question was actually on screen; a window that never saw it prints
// nothing (internal/tui3's [app.withdrawQuestion]).
//
// AND THE WORDS ARE BANKED ON THE RAISE. Landing questions used to emit without
// [Agent.rememberQuestion], so [Agent.ResolveQuestion] saw `said=false` and
// emitted no [EventQuestionAnswered] — fine for the window that closes itself
// the moment it sends ([app.closeQuestion]), silent for a `--host` replica that
// only drops questions on answered or withdrawn. Banking here is what makes the
// answer event reachable, and [Agent.retireLandingQuestion] clears it on settle.
func (a *Agent) publishLandingQuestion(notice TaskNotice) {
	if notice.ID == 0 {
		return
	}
	standing := a.landingAsked(notice.ID)
	if notice.State != TaskUnverified {
		a.retireLandingQuestion(notice, standing)
		return
	}
	// THE ANSWER'S OWN WORK IN FLIGHT IS NOT A NEW QUESTION: a notice naming a
	// resolution in flight raises nothing and withdraws nothing, leaving the
	// standing map at the last DRAWN shape, which is exactly what a later
	// shape-change withdrawal must take back. The settle's own terminal notice
	// is not blanketed by this: resettle hands the claim back inside its
	// locked write, before the notice is built (task_run.go's
	// [TaskGraph.resettle]).
	//
	// A DECIDER CHANGE IS STILL A NEW FACT, and it survives the flight. `let
	// codeaf decide this one` or `take it back` pressed while a re-audit runs
	// changes who holds the decision — the ask's own shape — and a card drawing
	// `codeaf is deciding` for the five minutes of the flight, after the person
	// took it back, contradicts the person's own act. So a flight holds down
	// only the moves that are the loop (#1077's re-raises with no new fact) —
	// spend, heartbeat, done-phase — and lets a changed holder through to the
	// redraw below, which carries the flight's own stamp (`accepted 18:20 ·
	// still working on it`) so the person reads what they answered AND that it
	// is still running.
	if strings.TrimSpace(notice.Settling) != "" && !a.landingDeciderChanged(notice) {
		return
	}
	// AND THE TAKE-DOWN COMES BEFORE EVERY MINT, because the raise's own law
	// is that a node that moves while it is there raises it again WITH THE
	// NEW SHAPE — and the bank holds the old one, [Agent.said] returning
	// banked words untouched. Left standing, the bank would re-emit the old
	// card verbatim: a flight stamp whose flight has ended, the old holder's
	// policy after the decision was handed back, the ask reason from before
	// the re-check — the stale-card class #1077 is about. The take-down is
	// the answer's own claim ([Agent.claimQuestion]), so the words minted
	// below always carry the notice's own shape and whatever the record says
	// became of the last answer. Nothing is lost by it: a landing's words
	// are derived from the notice and the record, never typed by an asker,
	// so a fresh mint is never the poorer card.
	if standing != "" {
		_, _ = a.claimQuestion(standing, strconv.FormatUint(notice.ID, 10), false)
	}
	q := a.landingQuestion(PendingDecision{Notice: notice})
	// AND A LANDING THAT CHANGED SHAPE TAKES THE OLD SHAPE BACK FIRST. The two
	// lanes are two tokens — `landing:7` and `conflict:7` — so a node that lands
	// unchecked and is then refused a merge would otherwise leave the first
	// question standing beside the second, which is two accounts of one node.
	// The old shape's words are already off the book (the take-down above),
	// so what is left to withdraw is the drawn card.
	if standing != "" && standing != q.Kind {
		stale := q
		stale.Kind = standing
		stale.Withdrawn = &Withdrawal{
			Reason: "what it is waiting on changed",
			By:     q.Asker.Kind,
			At:     time.Now(),
		}
		a.emitQuestion(EventQuestionWithdrawn, stale, nil)
	}
	a.landingAsking(notice.ID, q.Kind)
	// Raised through the one door (question.go's [Agent.raiseQuestion]) and the
	// let-go DROPPED: settle and answer own this question's retirement
	// ([Agent.retireLandingQuestion], [Agent.claimQuestion]), and a deferred
	// let-go would race the graph's next move.
	_ = a.raiseQuestion(q, nil)
}

// retireLandingQuestion takes a settled node's question back.
//
// THE STANDING MAP IS NOT THE ONLY SOURCE. A restored graph, or a window that
// only heard the question through [Agent.WatchQuestions]'s OpenQuestions replay,
// can have banked words (or a drawn question) with landingAsked empty — and the
// old early return left that question on screen forever after the work settled.
func (a *Agent) retireLandingQuestion(notice TaskNotice, standing QuestionKind) {
	token := strconv.FormatUint(notice.ID, 10)
	a.landingAsking(notice.ID, "")
	kind := standing
	if kind == "" {
		if _, ok := a.questionSaid(QuestionLanding, token); ok {
			kind = QuestionLanding
		} else if _, ok := a.questionSaid(QuestionConflict, token); ok {
			kind = QuestionConflict
		}
	}
	if kind == "" {
		return
	}
	gone := a.landingQuestion(PendingDecision{Notice: notice})
	gone.Kind = kind
	if _, said := a.questionSaid(kind, token); said {
		a.WithdrawQuestion(kind, token, questionGoneReason(gone))
		return
	}
	gone.Withdrawn = &Withdrawal{
		Reason: questionGoneReason(gone),
		By:     gone.Asker.Kind,
		At:     time.Now(),
	}
	a.emitQuestion(EventQuestionWithdrawn, gone, nil)
}

// landingDeciderChanged says the ask's own HOLDER moved while a resolution is
// in flight over this node — the decision handed to codeaf, or taken back
// ([TaskAsk.Owner], the one holder task-states keeps, dressed as the
// question's Policy by [landingPolicy]). It is the one change a flight must
// not hold down: every other fact the offer draws (effort, retarget) is
// redrawn by the flight's own terminal notice, but a card saying `codeaf is
// deciding` after the person took it back contradicts the person's own act
// for as long as the flight runs.
//
// THE COMPARISON IS THE BANKED QUESTION'S OWN POLICY, read through
// [Agent.questionSaid] — no second holder is minted here — against the holder
// the notice carries. A flight whose holder did not move answers false, and
// the flight goes on holding the question down.
func (a *Agent) landingDeciderChanged(notice TaskNotice) bool {
	kind := a.landingAsked(notice.ID)
	if kind == "" {
		return false
	}
	q, said := a.questionSaid(kind, strconv.FormatUint(notice.ID, 10))
	if !said {
		return false
	}
	status := ProjectTask(notice.StatusFacts())
	return q.Policy != landingPolicy(status.Ask.Owner)
}

// landingQuestionToken is the string one landing question is known by, for a
// caller holding the id and the shape rather than the object.
func landingQuestionToken(kind QuestionKind, id uint64) string {
	return questionToken(kind, strconv.FormatUint(id, 10))
}

// landingAnsweredStamp is how a re-raised landing says what became of the last
// answer: the lane's own word for the key that was pressed, and the minute it
// landed. `accepted 18:20` under `nobody could check it` is one card carrying
// both halves — the half the twelve identical cards in #1077 never said.
func landingAnsweredStamp(record DecisionRecord) string {
	word := landingAnsweredWord(record)
	// A STAMP FROM ANOTHER DAY SAYS ITS DAY. `accepted 18:20` on a card drawn
	// the next morning reads as an hour ago, and the stamp is the one place
	// the card says when — so the day leads when the day is not this one. The
	// day is judged in THIS machine's local zone, the zone the person reading
	// the card is in: a record written under another offset keeps its words
	// from drifting across midnight.
	at := record.At.Local()
	when := at.Format("15:04")
	if at.Format("2006-01-02") != time.Now().Format("2006-01-02") {
		when = at.Format("Jan 2 15:04")
	}
	stamp := word + " " + when
	return landingAnsweredBy(record, stamp)
}

// landingAnsweredWord is the lane's own word for the key that was pressed.
//
// THE MAP IS THE LANE'S VOCABULARY AND NOTHING ELSE: the keys answers.go fixes
// for a landing, each in the words the receipt lines beside it already use
// (task_audit.go's acceptedLine and family), and a custom ask's own label as
// the fallback.
func landingAnsweredWord(record DecisionRecord) string {
	for _, key := range record.Picked {
		switch key {
		case LandingYesKey:
			if record.Kind == QuestionConflict {
				return "said resolve it"
			}
			return "accepted"
		case LandingNoKey:
			return "said not right"
		case LandingAgainKey:
			return "asked for a re-check"
		case LandingDecideKey:
			return "handed it to codeaf"
		case LandingTakeBackKey:
			return "took it back"
		}
	}
	if len(record.Labels) > 0 && strings.TrimSpace(record.Labels[0]) != "" {
		return "answered " + strings.TrimSpace(record.Labels[0])
	}
	return "answered"
}

// landingAnsweredBy says who answered, in the words a person reads — only
// when it was not this person. The record's own value is machinery vocabulary
// (answers.go's [DecidedBy]), and `window accepted 18:20` is a card a person
// reads; the lane's receipts already say these in plain words (`another
// window`, `the dial`), so the stamp says them the same way.
func landingAnsweredBy(record DecisionRecord, stamp string) string {
	switch record.By {
	case "", DecidedByPerson:
		return stamp
	case DecidedByWindow:
		return "another window " + stamp
	case DecidedByDial:
		return "the dial " + stamp
	case DecidedByRecord:
		return "an earlier decision " + stamp
	case DecidedByAsker:
		return "the asker " + stamp
	default:
		return string(record.By) + " " + stamp
	}
}
