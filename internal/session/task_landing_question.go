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
// IT RIDES [Agent.emitTaskUpdate] BECAUSE THAT IS THE ONE DOOR EVERY MOVE GOES
// THROUGH. A second list of which landings have been asked about would be a
// second source of truth for a fact the graph already holds, which is the defect
// pending.go exists against; what is kept here is only WHICH SHAPE was last put
// out, so that a question can be taken back in the kind it was raised in.

import (
	"strconv"
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
	q := a.landingQuestion(PendingDecision{Notice: notice})
	// AND A LANDING THAT CHANGED SHAPE TAKES THE OLD SHAPE BACK FIRST. The two
	// lanes are two tokens — `landing:7` and `conflict:7` — so a node that lands
	// unchecked and is then refused a merge would otherwise leave the first
	// question standing beside the second, which is two accounts of one node.
	if standing != "" && standing != q.Kind {
		token := strconv.FormatUint(notice.ID, 10)
		a.mu.Lock()
		delete(a.questionWords, questionToken(standing, token))
		a.mu.Unlock()
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

// landingQuestionToken is the string one landing question is known by, for a
// caller holding the id and the shape rather than the object.
func landingQuestionToken(kind QuestionKind, id uint64) string {
	return questionToken(kind, strconv.FormatUint(id, 10))
}
