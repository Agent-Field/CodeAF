package tui3

import (
	"cmp"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE QUESTION BLOCK ──────────────────────────────────────────────────────
//
// One renderer for every decision this engine hands a person. Before this file
// the surface had one block per lane — the approval gate, the connect offer, the
// harness offer, the task proposal, the standing card, the fuel gate, the stop
// card, the tab-close card — each with its own layout loop, its own key set, its
// own idea of what esc means, and three of them with no drawing at all, so work
// stopped on questions nobody in the product could see. docs/design/questions/
// DESIGN.md is the contract; this is the block half of it.
//
// THE ROWS, EXACTLY AS THEY ARE DRAWN.
//
// The line — a permission, a confirmation, anything whose whole decision fits
// beside its own answers. The row above it is the subject's OWN row, re-used
// rather than re-worded, because two renderings of one call is how a person
// approves something other than what they read:
//
//	  ╰─▶ bash rm -rf build
//	? allow this? [1] allow once · [2] always · [3] deny · [esc] later · 7s
//	  bash pattern "rm -rf *"
//	  2 more
//
// The card — a decision with more than a word behind each answer. Head, then
// the reason and who is asking, then one row per answer with what it costs,
// then the answers row:
//
//	? wants to start a task: rewrite the packer
//	  it will run on its own branch and open a pull request · codeaf
//	    1  start it        on a branch of its own
//	  ▸ 2  not now         nothing runs
//	    3  change it first
//	  [enter] take it · [d] you decide · [esc] later · 9s · [↑↓] choose
//
// The ratify line — the third rung of the ladder, where the work is already
// done and what is being asked is whether it holds. Nothing waits on it:
//
//	✓ renamed 12 files under src/ · [u] undo · [c] change
//
// The receipt, which stays exactly where the question was, dim, because the
// transcript is what happened and "you were asked and said this" is part of it:
//
//	  decided allow this? → allow once · you · 14:02 · c change
//
// And the withdrawn line, once, dim, when the asker took the question back:
//
//	  ⊘ allow this? — no longer needed · the turn moved on without it
//
// ── THE FOUR LAWS THIS FILE IS THE ENFORCEMENT OF ───────────────────────────
//
//   - NEVER MODAL, NEVER SUSPENDS THE KEYBOARD. The box below stays live and
//     typing into it is answering in words. This RETIRES the consent block's
//     "IT SUSPENDS THE KEYBOARD" law, which was the honest design when esc's
//     only two readings were "answer no" and "be trapped": a block a person
//     could not leave had to own every key so that nothing was typed into a
//     conversation that could not move. esc is `later` now, so there is a way
//     out that neither answers nor traps, and the keyboard goes back to the
//     person ([app.questionKey] takes only what it draws).
//   - THE SETTLE GUARD. A key that arrived before the question had been on
//     screen for [questionSettle] is DROPPED, never applied. A question that
//     lands under a hand already moving is a question answered by a keystroke
//     aimed at the sentence somebody was typing.
//   - THE BOX IS NEVER MOVED UNDER A HAND. A question raised while the box
//     holds words waits behind the chip until the words go or the hands stop
//     for [questionQuiet] ([app.questionQuieted]).
//   - THE ANSWER IS THE RECORD. Every answer leaves its line where the question
//     was, and the line is [session.DecisionRecord.Line]'s — the engine's own
//     rendering, so the row a person reads and the line the model reads cannot
//     become two accounts of one decision.

const (
	// questionSettle is how long a question must have been ON SCREEN before it
	// will take a key. 250ms is the settle guard's own number from
	// docs/design/questions/DESIGN.md, and it is about the hand rather than the
	// eye: it is roughly one keystroke at a fast typing speed, which is exactly
	// the window in which a question can arrive between a person deciding to
	// press a key and the key landing.
	questionSettle = 250 * time.Millisecond
	// questionQuiet is how long the box must have been still before a question
	// that arrived on top of a half-typed sentence is allowed to take its rows.
	// Three seconds is a pause somebody has stopped typing in rather than a gap
	// between two words.
	questionQuiet = 3 * time.Second
	// questionRuleAfter is how many same-shaped yeses are given before `r make
	// it a rule` is offered. The third, because two is a coincidence and a rule
	// offered on the first is the surface guessing at a habit somebody has not
	// formed (DESIGN.md's RULES ARE OFFERED, VISIBLE, FORGETTABLE).
	questionRuleAfter = 3
)

// questionAgent is the questions half of the agent under this surface, when it
// has one.
//
// IT IS AN OPTIONAL ASSERTION AND NOT A LINE ON [Agent], for stop.go's reason
// exactly: an agent that has never heard of questions keeps everything else it
// had, and this surface simply draws no engine questions rather than failing to
// compile against every fake in the tree. A CAPABILITY THAT CANNOT WORK IS
// ABSENT, NOT BROKEN — so with no door here the block still draws the questions
// the SURFACE raises (the stop card, the tab-close card), which need no engine
// at all.
type questionAgent interface {
	questionResolver
	// OpenQuestions is every decision this session is waiting on somebody for,
	// oldest first. It is DERIVED from the lanes' own waits; there is no second
	// store (internal/session's question.go).
	OpenQuestions() []session.Question
	// WatchQuestions is a standing subscription to [session.EventQuestion],
	// [session.EventQuestionWithdrawn] and [session.EventQuestionAnswered] that
	// replays everything already open when a surface attaches.
	WatchQuestions() (<-chan session.Event, func())
}

// questionResolver is THE ONE DOOR every answer goes through, on its own.
//
// IT IS NARROWER THAN [questionAgent] ON PURPOSE. Watching for questions and
// answering one are two capabilities and an engine may have either without the
// other — the surface raises the questions it asks about ITSELF with no engine
// at all, and an engine that can apply an answer but not stream a lane can still
// be answered. A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN, so each is
// asked for where it is needed rather than both everywhere.
type questionResolver interface {
	// ResolveQuestion reads the lane off the answer and hands it to that lane's
	// own resolver.
	ResolveQuestion(session.Answer) error
}

// DrawsQuestions reports whether an agent carries the WHOLE questions seam this
// surface needs: the standing lane, the reading of what is already open, and the
// door an answer goes back through.
//
// IT IS EXPORTED FOR [DrawsTasks]'S REASON, AND FOR THE SAME DEFECT. internal/remote
// implements this surface's agent over a wire and cannot import this package to
// check that it kept up, so the door that wires the two together asserts it
// instead (cmd/codeaf). The seam is ALL-OR-NOTHING — [app.questionDoors] is one
// type assertion — so a single method missing on the far half is not a question
// drawn smaller, it is a question that never reaches a screen at all. That is
// exactly what happened: the wire carried ResolveQuestion and neither
// WatchQuestions nor OpenQuestions, and on the road a plain `codeaf` takes every
// `ask` stopped the turn with nothing on any screen.
func DrawsQuestions(agent Agent) bool {
	_, ok := agent.(questionAgent)
	return ok
}

// questionDoors is that half of the agent, when it has one.
func (a *app) questionDoors() (questionAgent, bool) {
	doors, ok := a.agent.(questionAgent)
	return doors, ok && doors != nil
}

// questionShown is one question as this surface holds it: the object the engine
// (or this surface) raised, plus the four facts that are the SURFACE'S and
// belong nowhere else — when it was drawn, where the cursor is, whether a rule
// is on offer, and how to answer it when no engine is behind it.
type questionShown struct {
	question session.Question
	// local answers a question the SURFACE raised — the stop card, the
	// tab-close card, anything this program asks about itself. It is nil on
	// every question that came from the engine, which go through
	// [questionAgent.ResolveQuestion] instead, and the two are never both set:
	// one question has one resolver.
	//
	// IT HANDS BACK A COMMAND because a surface question's answer is often the
	// start of something this program then has to DO — a tab dismissed, a page
	// left — and the loop is where that belongs. An engine answer needs none:
	// it crosses a door and the news comes back on a lane.
	local func(session.Answer) tea.Cmd
	// shown is when this question first had a frame drawn with it on, which is
	// what [questionSettle] is measured from. It is NOT when the question was
	// raised: a question that waited behind a half-typed sentence
	// ([app.questionQuieted]) has been in this program for a while and on
	// screen for none of it, and the guard is about the screen.
	shown time.Time
	// pick is the answer the cursor is on, and it is meaningful only on the
	// confirmation kind — the one shape that keeps stop.go's law that the
	// cursor starts on the answer that loses nothing. Every other form is
	// answered by its digit or by the asker's own pick, so there is no cursor
	// to move and none is drawn.
	pick int
	// beatAt is where the cursor stands on the SECOND BEAT — the shapes offered
	// after `always` ([app.questionWiden]). The beat is a row of answers like
	// any other, so it has a cursor like any other: before this it drew none and
	// swallowed every arrow, which is a row a person could see, could not move,
	// and could only answer by guessing which digit was which.
	beatAt int
	// writing says the box below is writing to THIS question rather than to
	// the conversation: [questionCommentKey] after `c` (the words go with the
	// pointed answer as [session.Answer.Change]) or [questionAskBackKey] after
	// `?` (the words go to the asker with the question still open). The
	// block's answers row becomes a prompt saying so while it is set, because
	// a box whose meaning changed silently was the owner's "typing does not
	// work · is there a separate typing place?" (2026-09-10).
	writing string
	// other is the person's OWN answer: the words typed into the panel's last
	// row, and the answer they will travel with where they travel with one.
	//
	// IT LIVES ON THE QUESTION because it is one question's unfinished answer. A
	// person half-way through writing an answer who is handed the next question
	// in a queue must not find their sentence sitting on somebody else's panel —
	// which is [questionShown.beat]'s reason, said about words.
	other questionOther
	// rule says `r make it a rule` is on this question's answers row: the third
	// same-shaped yes has been given ([app.questionRuleOffered]).
	rule bool
	// scope is HOW LONG the answer this person is about to give will last, and
	// it is only ever one the question itself offered (questionscope.go). The
	// zero value means nobody has touched the row, which reads as the narrowest
	// lifetime offered — never as "no lifetime".
	//
	// IT LIVES ON THE QUESTION for [questionShown.other]'s reason: a person who
	// said "for this project" and is then handed the next question in a queue
	// must not find that choice sitting on somebody else's frame.
	scope session.AnswerScope
	// ruled says this question's SHAPE is answered by a rule this project has
	// written down (`/autonomy`), which is what puts `· your rule` on the row.
	//
	// NEVER A HIDDEN RULE (docs/design/questions/DESIGN.md). A clock ticking on
	// a question because of a setting somebody made three weeks ago, with
	// nothing on the row saying so, is exactly the thing that law forbids.
	ruled bool
	// undoable says the ratify row's `u` would reach something real. A ratify
	// question whose work cannot be taken back does not offer the key, which is
	// the emptiness law applied to an answer rather than to a number.
	undoable bool
	// spans is where this question's answers landed in columns, written by the
	// draw and read by the press — consent.go's own bargain, kept.
	spans []choiceSpan
	// row is which row of the block those spans are on.
	row int
	// staged is the answer held for this question while it is one tab of a SET
	// (questionset.go), and nil otherwise. It is sent with its neighbours from
	// the review, and until then it is only this window's: nothing has been
	// told to the engine, and a question answered anywhere else simply leaves
	// with its held answer.
	//
	// IT LIVES ON THE QUESTION for [questionShown.other]'s reason: a held answer
	// belongs to one question, and a set that re-flows when a neighbour is
	// withdrawn must never move it onto another.
	staged *session.Answer
	// shapes answers what this question's widening answer could be banked as,
	// and it is the second beat's whole door.
	//
	// A WIDENING ANSWER IS SOMETIMES A QUESTION OF ITS OWN. `always` on a shell
	// command used to bank the line exactly as it ran, which bought silence for
	// that string and nothing else; what a person means is a SHAPE, and only
	// they know which one. So a lane that can offer shapes offers them here, the
	// block draws them where the answers row was, and nothing is written until
	// one is picked. A lane with one shape or none has no beat and its widening
	// answer is given straight ([app.questionWiden]).
	shapes func() []string
	// holes is the small form this question carries, or the zero value for one
	// that carries none — the same [questionInput] the room reads, so a hole is
	// drawn and walked by one piece of code wherever it appears.
	//
	// THE BLOCK DRAWS ONLY THE BLANKS. A checklist, a run of pairs and a dial are
	// the ROOM's shapes: each is several rows and a rhythm of its own, and a card
	// pinned above the box has neither the height nor the keyboard for them
	// ([app.questionCardRows] draws the sentence and nothing else). What the block
	// does draw is the sentence with a hole in it, because that is one row — and
	// because the task proposal's model shortlist is exactly that shape
	// ([session.TaskModelShape]).
	//
	// IT LIVES ON THE QUESTION FOR THE BEAT'S REASON: a person half-way through
	// changing a hole who is handed the next question in a queue must not find
	// their choice sitting on somebody else's card.
	holes questionInput
	// beat is the shapes on screen RIGHT NOW, and it is non-empty only while
	// somebody is part-way through choosing one.
	//
	// It lives on the QUESTION rather than beside the block because it is one
	// question's unfinished answer: a person part-way through a shape who is
	// handed the next question in a queue must not find the previous one's offer
	// still on screen.
	beat []string
	// answered is the lane's own hand on an answer, on its way to the door.
	//
	// IT IS WHAT THIS PROGRAM DOES ABOUT AN ANSWER, as against what the ENGINE
	// does about it: a rule written into the person's settings, a transcript row
	// annotated with what was decided. It hands back the answer the engine is
	// actually told, so a lane that has already written a permission down can say
	// so ([session.AnswerBanked]) rather than letting a second, wider one be
	// written beside it.
	answered func(session.Answer) session.Answer
	// commented is the lane's own hand on `c` — the key that says "I will take
	// one of these, but not as it stands" and hands the answer to the box.
	//
	// IT IS NIL ON EVERY QUESTION DRAWN ABOVE THE MESSAGE BOX, which is nearly
	// all of them: the box down there is already the answer lane and the next
	// `enter` carries the sentence ([app.questionEnter]), so the key's whole work
	// is to be taken rather than typed. A question drawn somewhere whose box is
	// NOT that box — home's errand pane, which has a box of its own pointed at
	// another conversation — has to be told that the next enter is an answer
	// rather than a message, and this is where it is told.
	commented func()
	// held is the lane's own hand on the first evidence that somebody is AT THE
	// KEYBOARD, and it is called once per question.
	//
	// IT EXISTS FOR THE ONE CLOCK ON THIS BLOCK THAT ANSWERS. Every other is a
	// reading clock, which holds by itself and never decides anything (F41 is
	// why, and [app.tickQuestion] is where). A task proposal's silence STARTS
	// the work, so the moment a key lands the lane tells the engine to stop
	// counting — and it is a hook rather than a line in the block because
	// stopping that clock is a call over a connection, which is a lane's
	// business and not a renderer's (task.go's [app.holdTask]).
	held func()
	// revising says this question is on the block for the SECOND time, because
	// somebody pressed `c change` on its receipt. The answer it takes carries
	// [session.Answer.Revises], which is what tells the engine a decision is
	// being changed rather than answered late (questionchange.go).
	revising bool
	// clockAt is when the reading time on this question started and clockFor how
	// long it runs; clockHeld says it has stopped for good.
	//
	// A READING CLOCK NEVER ANSWERS. F41 was a hidden ten-second timer that
	// recorded "denied" and killed work nobody refused, and that is the one
	// answer this surface must never give on somebody's behalf. What this clock
	// does at zero is HOLD ([app.tickQuestion]): the tail says paused, the work
	// stays waiting, and the question is still there to answer.
	clockAt   time.Time
	clockFor  time.Duration
	clockHeld bool
}

// questionOther is the answer that is not on the list, being written.
//
// THE ROW IS THE BOX AND THERE IS NO MODE. Before the owner's ruling of
// 2026-09-11 a person had to press `c` first — a letter nothing on screen named,
// which pointed the MESSAGE BOX at the question and said so in a row where the
// answers had been. The last row is `something else…` now, the pointer arriving
// on it is what opens it, and the message box below stays the person's.
type questionOther struct {
	// words is the sentence being typed, in the surface's own one-line editor,
	// so the caret, the word jumps and the undo stack are the ones every other
	// box on this surface has.
	words editor
	// with is the KEY of the answer these words travel with, or empty for words
	// that answer on their own.
	//
	// IT IS A KEY AND NOT AN INDEX, so the zero value is the honest one: a person
	// who walked onto this row is answering in words, and a person who pressed
	// `c` on an answer is saying "this one, but not as it stands" and the row
	// says which one ([questionOtherWith] draws it).
	with string
}

// token is the one string this question is known by across the two maps below
// and across a fold. It is the lane and the lane's own id, which is
// [session.Question.Token] with the lane written in — two lanes may both be
// waiting on id 7.
func (q questionShown) token() string {
	return questionTokenOf(q.question)
}

// questionTokenOf names one question the way the whole product names it — the
// lane it belongs to and that lane's own token ([session.Question.Token]) —
// spelled once here because every place that holds a LIST of questions has to
// agree about when two of them are the same one: the block, a set of tabs, and the
// page reading another conversation's work (taskowner.go).
func questionTokenOf(q session.Question) string {
	return string(q.Kind) + ":" + q.Token()
}

// questionRecord is a question that has stopped being one: answered, and
// keeping its receipt, or withdrawn, and keeping its one dim sentence. Both
// stay where the question was.
type questionRecord struct {
	// record is the answered form — the engine's own [session.DecisionRecord],
	// rendered by the engine's own [session.DecisionRecord.Line] so a person
	// and the model read one account of one decision.
	record session.DecisionRecord
	// withdrawn is the reason the asker took it back, and it is the whole of
	// what distinguishes the two: a record with a withdrawal reason is the
	// ⊘ line, and a record without one is the receipt.
	withdrawn string
	// head is the question's own sentence, kept for the withdrawn line, which
	// has no record to read one off.
	head string
	// at is when it stopped being a question, which is what the fade below
	// [questionRecordFor] measures.
	at time.Time
	// reversible says whether this decision could be walked back at all. An
	// irreversible one says `cannot change` on the row, which is the engine's own
	// [session.DecisionRecord.Line]'s half of it.
	reversible bool
	// question is what was asked, kept so that `c change` can put it back on the
	// block with its answers exactly as they were read the first time
	// (questionchange.go). The record alone carries the head and the keys and
	// not the words on the answers nobody picked, which is what somebody
	// changing their mind is choosing between.
	question session.Question
	// entries is how many rows the conversation held when the answer was given,
	// which is how the receipt's working tail knows the model has not spoken yet
	// ([app.questionReceiptTail]). It is a count and not a clock: the tail is
	// about something ARRIVING, and the thing that arrives is a row.
	entries int
}

// ── what is open, and which one is drawn ────────────────────────────────────

// questionOpen is every question this surface would draw, oldest first, with
// the folded ones and the ones still waiting on a quiet box left out.
//
// THE ORDER IS THE ENGINE'S AND IS NOT RE-DECIDED HERE. [session.Agent.
// OpenQuestions] sorts oldest first and says why (a map has no order, so two
// reads of one unchanged session would otherwise hand a surface two different
// lists). The surface's own questions are sorted into the same sequence by the
// same key, because a person answering down a queue does not care which side of
// the engine boundary each one came from.
func (a *app) questionOpen() []questionShown {
	// NOTHING OPEN ALLOCATES NOTHING. This is on the draw path and the draw
	// path is the one thing on a scrolling screen rebuilt from nothing every
	// frame ([TestOneScreenScrollOfFourThousandLinesStaysInsideTheAllocationLaw]
	// is the law), and the overwhelmingly common state of this block is empty.
	if len(a.questions) == 0 {
		return nil
	}
	out := make([]questionShown, 0, len(a.questions))
	for _, q := range a.questions {
		if a.questionFolded[q.token()] {
			continue
		}
		out = append(out, q)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].question.ClarificationDepth != out[j].question.ClarificationDepth {
			return out[i].question.ClarificationDepth > out[j].question.ClarificationDepth
		}
		if one, two := questionRaisedHere(out[i].question), questionRaisedHere(out[j].question); one != two {
			return one
		}
		return questionOlder(out[i].question, out[j].question)
	})
	return out
}

// questionRaisedHere reports whether this window raised the question ITSELF, by
// the person's own gesture — `x` on a run's page, `ctrl+w` on a working tab
// (stop.go, tabclose.go). Those go to the FRONT of the queue, ahead of anything
// older waiting there.
//
// AGE IS THE ORDER FOR EVERYTHING THE ENGINE ASKS, and it is the right one: the
// oldest thing waiting is the thing holding work up. But a question a person
// raised a quarter-second ago by pressing a key is the one they are looking at
// and the one their next keystroke is aimed at, and putting it behind a
// permission that has been waiting five minutes would send `esc` to the
// permission — folding a question nobody was answering — while the card the
// person meant it for stayed on screen. There is no queue to hold these: a
// confirmation is raised by a gesture and answered in the same breath, so
// nothing can pile up in front.
func questionRaisedHere(q session.Question) bool {
	return q.Asker.Kind == session.AskerSurface && q.Ask == session.AskConfirmation
}

// questionHead is the question the block is drawing, and whether there is one.
//
// WHERE SEVERAL QUESTIONS FROM ONE STEP ARE UP, IT IS THE TAB ON SCREEN
// (questionset.go), so every key, press and hint that reads the head reads the
// question the person is actually looking at.
func (a *app) questionHead() (questionShown, bool) {
	open := a.questionOpen()
	if len(open) == 0 {
		return questionShown{}, false
	}
	if set := a.questionSetOf(open); set != nil {
		return a.questionSetHead(set), true
	}
	return open[0], true
}

// questioning reports whether the block is on screen at all.
func (a *app) questioning() bool {
	_, ok := a.questionHead()
	return ok
}

// questionCount is how many questions are open in this conversation, folded
// ones included. It is what the chip counts, and it counts the folded ones on
// purpose: `esc` is later and not cancelled, so a question a person put off is
// still a question the work is waiting on, and a count that dropped when they
// pressed esc would be the surface telling them they had finished.
//
// AND A SHAPE THAT WAITS ON NOBODY IS NOT COUNTED, because the chip's whole
// sentence is "something is waiting on you". A ratify is the model saying what
// it has already done; nothing is parked on the answer, and
// [session.AskKind.Waits] is the engine's own reading of exactly that — the same
// term [session.Question.Waiting] uses for the waiting desk, the presence file
// and the open-question cap. A chip reading `? 1 question` over a ratify sends
// somebody to a screen where there is nothing for them to decide, which is the
// surface promising work that is not there.
//
// THE BLOCK STILL DRAWS IT. What is settled here is what the STATUS ROW claims
// across every page, not whether the row exists: a ratify is still shown, still
// answerable, and still carries its own keys where it is drawn.
func (a *app) questionCount() int {
	n := 0
	for _, q := range a.questions {
		if q.question.Ask.Waits() {
			n++
		}
	}
	return n
}

// ── raising one ─────────────────────────────────────────────────────────────

// raiseQuestion puts one question on the block.
//
// IT REPLACES BY TOKEN RATHER THAN APPENDING, because the engine re-emits an
// open question whenever a surface attaches ([session.Agent.WatchQuestions]
// replays), and a queue that grew a row on every reattach would say `4 more`
// about one decision.
//
// AND IT NEVER MOVES THE BOX UNDER A HAND. A question that arrives while there
// are words in the box is HELD — it goes on the list, so the chip counts it and
// home can see it, and [app.questionQuieted] is what lets it take its rows.
func (a *app) raiseQuestion(q questionShown) {
	if strings.TrimSpace(q.question.Head) == "" {
		// A QUESTION WITH NOTHING TO READ IS NOT DRAWN. The engine's own gate
		// refuses one at the door ([session.Question.Check]); this is the same
		// refusal on the surface side, for a lane that built its fallback out
		// of a card that carried no words.
		return
	}
	if q.question.Asked.IsZero() {
		q.question.Asked = a.now()
	}
	// A clarification's prerequisite must also take the keyboard when the
	// original decision is expanded. The original remains in the question list.
	if a.qroom != nil && q.question.ClarificationDepth > a.qroom.head.question.ClarificationDepth {
		a.closeQuestionRoom()
	}
	q.pick = questionPointerStart(q.question)
	// THE WAY BACK IS THE ASKER'S OWN CLAIM AND NOT THIS SURFACE'S GUESS. A
	// caller that already knows this window can put the work back says so; for
	// every question that arrived from the engine the object is the only thing
	// that knows, and until this line nothing read it — so `[u] undo` was a row
	// in the key table, a paragraph in the manual, and a key no ratify line ever
	// drew ([questionUndoable] holds the reading).
	q.undoable = q.undoable || questionUndoable(q.question)
	// AND A CLOCK THAT ANSWERS IS STOPPED WHERE IT RUNS, which is in the engine.
	// The hook is attached here, once, off the question's own properties rather
	// than by each lane remembering to set one: a question that says it will take
	// its own pick at a deadline is a question whose clock a key must be able to
	// stop ([app.holdQuestionClocks] calls this for every key the block reads).
	if q.held == nil {
		q.held = a.questionHold(q.question)
	}
	q.holes = newQuestionInput(q.question)
	for i := range a.questions {
		if a.questions[i].token() != q.token() {
			continue
		}
		batch := q.question.Batch
		// The SHOWN stamp survives the replay. A question re-sent by a
		// reattaching watcher has not just arrived, and restamping it would
		// hand it a fresh settle guard every few seconds — which is a question
		// that never becomes answerable on a link that reconnects.
		q.shown = a.questions[i].shown
		q.pick = a.questions[i].pick
		// AND SO DOES EVERYTHING THE SURFACE KNOWS THAT THE OBJECT CANNOT
		// CARRY. The same question arrives twice on purpose — the lane that
		// raised it dresses it with the row it is about, its second beat and
		// its reading clock, and the engine re-sends the bare object on the
		// questions lane whenever a surface attaches. A replace that took the
		// bare one whole would take the beat off the screen under somebody's
		// hand and hand the question a fresh clock every few seconds.
		if q.beat == nil {
			q.beat = a.questions[i].beat
		}
		if q.shapes == nil {
			q.shapes = a.questions[i].shapes
		}
		if q.answered == nil {
			q.answered = a.questions[i].answered
		}
		if q.local == nil {
			q.local = a.questions[i].local
		}
		if q.clockFor == 0 {
			q.clockAt, q.clockFor = a.questions[i].clockAt, a.questions[i].clockFor
		}
		// AND THE WORDS ON THE ANSWERS DO NOT CHANGE UNDER A HAND. A lane that
		// dresses the engine's option list with the card's own spelling — the
		// standing card's `yes, set it up` where the kind's list says `yes`,
		// consent's `always, this command` where it says `always` — would
		// otherwise have that dressing wiped off by the bare object a
		// reattaching watcher re-sends, respelling every answer on screen while
		// somebody read them. THE KEYS ARE THE TEST AND THE WORDS ARE NOT: a
		// list offering the same answers is the same question said twice, and a
		// list offering different ones is a different question and is taken
		// whole.
		if questionSameAnswers(q.question.Options, a.questions[i].question.Options) {
			q.question.Options = a.questions[i].question.Options
		}
		// AND A HOLE SOMEBODY HAS MOVED KEEPS WHAT THEY MOVED IT TO. The bare
		// object re-sent by a reattaching watcher carries the ASKER's default, so
		// taking it whole would walk a person's model choice back to the closest
		// match every few seconds — under their hand, with no key pressed.
		if questionSameHoles(q.holes, a.questions[i].holes) {
			q.holes = a.questions[i].holes
		}
		// AND A BARE RE-SEND NEVER UNDRESSES A QUESTION A LANE DRESSED. Several
		// lanes hand this surface the event a moment before the questions lane
		// carries the question itself — connect, the harness offer, a
		// sub-harness intake card — and the copy this window raised from that
		// event knows three things the engine's copy cannot: the catalog's own
		// sentence over the box, whether what it wants is a SECRET, and the
		// answers a hosted browser trip cannot offer. The engine's copy arrives
		// with a hand of its own for none of them, so taking it whole would draw
		// a key box in the clear over a question somebody was already reading.
		if q.answered == nil && q.local == nil &&
			(a.questions[i].answered != nil || a.questions[i].local != nil) {
			q.question = a.questions[i].question
			q.holes = a.questions[i].holes
		}
		if q.staged == nil {
			q.staged = a.questions[i].staged
		}
		// AND WHICHEVER COPY ARRIVED SECOND, THE STEP STAYS. Only the engine's
		// copy can know which step raised a question — a lane that dressed one
		// from its own event has no batch to put on it — and the two copies race
		// each other onto the block, so a set may not lose a member because the
		// copy without a batch landed last (questionset.go).
		q.question.Batch = cmp.Or(batch, a.questions[i].question.Batch)
		q.clockHeld = q.clockHeld || a.questions[i].clockHeld
		a.questions[i] = q
		a.touch()
		return
	}
	q.ruled = a.autonomyRuled(q.question.Ask)
	// AND A REFUSAL THIS WINDOW COULD NOT SAY AT THE TIME IS SAID NOW. It was
	// kept against this token because the person was looking at another
	// conversation when the door turned their answer down; the question coming
	// back is the moment it means something again.
	a.sayKeptQuestionRefusal(q.token())
	a.questions = append(a.questions, q)
	a.questionRule(&a.questions[len(a.questions)-1])
	a.touch()
}

// questionSameAnswers reports whether two readings of one question offer the
// same answers — the same keys, in the same order. The WORDS are deliberately
// not compared: which spelling is on screen is exactly what this is protecting.
func questionSameAnswers(fresh, held []session.AnswerOption) bool {
	if len(fresh) != len(held) || len(held) == 0 {
		return false
	}
	for i := range fresh {
		if fresh[i].Key != held[i].Key {
			return false
		}
	}
	return true
}

// questionUndoable reads the object for the one thing the ratify row's `u`
// needs to be true: that pressing it would reach something real.
//
// THREE CONDITIONS, AND ALL THREE ARE THE ASKER'S OWN WORDS.
//
//   - IT IS A RATIFY. Nothing else on this block has work already done behind
//     it, so nothing else has anything to take back; a `u` on an open question
//     would be undoing a decision nobody has made yet.
//   - THE STAKES ARE REVERSIBLE. That is the whole content of the ladder's third
//     rung ([session.AskRatify]: "something reversible was DONE") and the whole
//     content of the key: a costly or irreversible act is one the receipt says
//     `cannot change` about, and offering a way back from it would be the row
//     promising something the world will not do.
//   - THE ASKER DESCRIBED THE UNWIND. `u` sends the answer that puts the work
//     back ([app.questionUndo] takes [questionSafeAt]'s option), so an asker
//     that wrote no answers wrote no unwind, and there is nothing for the key to
//     send. THE EMPTINESS LAW APPLIED TO AN ANSWER RATHER THAN TO A NUMBER.
func questionUndoable(q session.Question) bool {
	if q.Ask != session.AskRatify || q.Stakes != session.StakesReversible {
		return false
	}
	at := questionSafeAt(q)
	return at < len(q.Options) && strings.TrimSpace(q.Options[at].Key) != ""

}

// questionSameHoles reports whether two readings of one question's small form
// are asking the same thing — the same holes, each offering the same choices.
//
// It is what decides whether a re-sent question may keep the answers already put
// into the holes on screen. An asker that CHANGED the shape is asking something
// else, and carrying an old value into it would leave a person looking at a
// choice they never made.
func questionSameHoles(fresh, held questionInput) bool {
	if fresh.kind != held.kind || len(fresh.blanks) != len(held.blanks) {
		return false
	}
	for i := range fresh.blanks {
		if fresh.blanks[i].blank.Label != held.blanks[i].blank.Label {
			return false
		}
		if !sameAnswer(fresh.blanks[i].blank.Choices, held.blanks[i].blank.Choices) {
			return false
		}
	}
	return true
}

// questionSafeAt is the index of the answer the cursor starts on: the one
// marked safe, or the last one.
//
// THIS IS stop.go's AND tabclose.go's LAW, VERBATIM AND UNWEAKENED — "the
// answer under `enter` is the one a person gets by pressing the key they press
// to make a question go away, so it has to be the answer that loses nothing."
// The lane says which answer that is by marking it [session.AnswerOption.Safe];
// a lane that marked none gets the last, which is where every card on this
// surface has always put its way out.
// questionPointerStart is where the person's pointer stands when a question
// arrives. EVERY QUESTION WITH ANSWERS HAS A POINTER — the row `↑`/`↓` walk
// and `enter` takes — because a block that answered only to digits, with the
// arrows doing nothing, was one nobody could tell how to work (the owner,
// 2026-09-10: "none of my arrow keys make it obvious"). It starts on the
// asker's pick when there is one, so `enter` alone still takes the
// recommendation; on the first answer otherwise; and on the answer that loses
// nothing for a confirmation, which is the one shape where enter must never
// land on the act by default ([questionSafeAt], stop.go's law).
func questionPointerStart(q session.Question) int {
	if q.Ask == session.AskConfirmation {
		return questionSafeAt(q)
	}
	if q.Pick != nil {
		for i, option := range q.Options {
			if strings.TrimSpace(option.Key) == strings.TrimSpace(q.Pick.Key) {
				return i
			}
		}
	}
	// AND WHERE NOBODY RECOMMENDED ANYTHING AND NOBODY BUT A PERSON MAY ANSWER,
	// THE STAKES SAY WHERE `enter` LANDS (owner ruling, 2026-09-11, shipped as
	// #953). The gate now grades its own questions — internal/approval's
	// always-ask shapes come through as [session.StakesIrreversible] and every
	// other consent stays [session.StakesCostly] — so the pointer opens on the
	// answer that loses nothing for a grave call and on the first answer for an
	// ordinary one. `enter` takes the answer the pointer is on: `deny` over
	// `rm -rf` or a force-push, `allow once` over an `ls`.
	//
	// THE KEY IS THE STAKES AND ONLY THE STAKES. Reading the command's text or
	// the tool's name here would be a second judgement beside internal/approval's
	// that one day disagrees with it — the generic-and-meta law the engine half
	// of #953 keeps by passing the grade through untouched.
	//
	// THE MIDDLE CASE IS #933's DENY-FIRST, KEPT. A permission graded neither
	// way — a hand-built question, a shape approval does not know yet — opens on
	// the answer that loses nothing, because a pointer parked on `allow once`
	// for a call nobody graded is a guess made with the person's one keystroke.
	//
	// IT IS BELOW THE PICK AND NOT ABOVE IT, which is the whole of why a task
	// proposal is unaffected: an asker that recommended an answer said so on the
	// row a person is reading (`suggested`), and `enter` taking the
	// recommendation IS the pointer's law.
	if q.Ask == session.AskPermission && q.Stakes == session.StakesCostly {
		return 0
	}
	if questionHandsOnly(q) {
		return questionSafeAt(q)
	}
	// AND EVERY OTHER QUESTION OPENS ON ITS FIRST ANSWER. The safe mark is not
	// read for one of those: on a landing row or an ordinary choice the answer
	// that loses nothing is the one that does nothing, and a pointer parked
	// there would make `enter` mean "no" on every card this surface draws.
	return 0
}

func questionSafeAt(q session.Question) int {
	for i, option := range q.Options {
		if option.Safe {
			return i
		}
	}
	if len(q.Options) == 0 {
		return 0
	}
	return len(q.Options) - 1
}

// withdrawQuestion takes one off the block and leaves its one dim sentence
// where it was.
func (a *app) withdrawQuestion(q session.Question, reason string) {
	token := questionToken(q)
	kept := a.questions[:0]
	found := false
	for _, open := range a.questions {
		if open.token() == token {
			found = true
			a.rescueQuestionWords(open)
			continue
		}
		kept = append(kept, open)
	}
	a.questions = kept
	delete(a.questionFolded, token)
	a.closeQuestionPage(token)
	if !found {
		// A question this surface never drew leaves no line. The sentence is
		// there to explain a row somebody was looking at; printed under nothing
		// it is a report about a decision they were never asked to make.
		return
	}
	a.questionRecords = append(a.questionRecords, questionRecord{
		head: strings.TrimSpace(q.Head), withdrawn: strings.TrimSpace(reason), at: a.now(),
	})
	a.touch()
}

// rescueQuestionWords moves a half-written `something else…` answer into the
// message box as the question it was written for goes away.
//
// NOTHING SOMEBODY TYPED IS EVER DROPPED ON THE FLOOR. The words are an answer
// to a question that has just stopped existing — answered in another window, or
// taken back by the engine mid-sentence — so they cannot be sent and they cannot
// stay where they were. Deleting them is the one option that is not available:
// this surface keeps a half-typed sentence through a page opening over it and
// through the question block arriving under it, and a question vanishing is not
// a better reason to lose one.
//
// THE MESSAGE BOX IS WHERE THEY GO because it is the person's own space and the
// only one that outlives any question. They are appended rather than written
// over, and the box is left for the person to send, edit or clear: a sentence
// SENT on their behalf would be this surface answering for them.
func (a *app) rescueQuestionWords(open questionShown) {
	words := strings.TrimSpace(open.other.words.String())
	if words == "" {
		return
	}
	if strings.TrimSpace(a.input.String()) != "" {
		words = " " + words
	}
	a.input.end()
	a.input.insert(words)
	a.questionTyped = a.now()
}

// questionQuieted is THE BOX IS NEVER MOVED UNDER A HAND, answered.
//
// A question may take its rows when the box is empty, or when the box has been
// still for [questionQuiet]. Both halves matter: the empty box is the ordinary
// case and costs no wait at all, and the pause is what keeps a question from
// being stuck behind a draft somebody typed and walked away from.
func (a *app) questionQuieted() bool {
	// AND THE `something else…` BOX IS A BOX. A person part-way through an answer
	// in words is as plainly at the keyboard as one part-way through a message,
	// and the block moving under them is the same defect whichever box they are
	// in. It is not softened by [questionQuiet] the way the composer is: those
	// words belong to THIS question and cannot be left behind and come back to,
	// so there is no such thing as having walked away from them.
	if a.questionWording() {
		return false
	}
	if strings.TrimSpace(a.input.String()) == "" {
		return true
	}
	return a.now().Sub(a.questionTyped) >= questionQuiet
}

// questionWording reports whether there are words half-typed into an open
// question's own `something else…` box.
//
// IT ASKS EVERY OPEN QUESTION AND NOT ONLY THE HEAD, because the whole point is
// the moment the head CHANGES: the question somebody was writing an answer to
// has just been answered in another window or taken back by the engine, and the
// one thing that must not happen next is the block treating the keystroke
// already on its way as a key on whatever took its place.
func (a *app) questionWording() bool {
	for _, open := range a.questions {
		if strings.TrimSpace(open.other.words.String()) != "" {
			return true
		}
	}
	return false
}

// questionSettled reports whether this question has been on screen long enough
// to take a key. A question that has never been drawn has no shown stamp and is
// never settled — which is the guard doing its job on the frame the question
// arrives on.
func (a *app) questionSettled(q questionShown) bool {
	if questionRaisedHere(q.question) {
		// A QUESTION THE PERSON RAISED THEMSELVES IS ANSWERABLE AT ONCE, and
		// neither guard applies to it. Both exist for a question that ARRIVES —
		// the draw stamp so the block cannot answer from behind a page nobody
		// is looking at, the quarter-second so a keystroke aimed at whatever was
		// on screen a moment ago is dropped rather than applied to what replaced
		// it. Neither reading is available here: the screen a moment ago was the
		// one they pressed `x` or `ctrl+w` on, their hand is already on the
		// keyboard, and the card is raised OVER whatever page is up rather than
		// waiting behind it. Making them wait a beat to cancel their own gesture
		// is a card that eats the `esc` they pressed to take it back.
		return true
	}
	return !q.shown.IsZero() && a.now().Sub(q.shown) >= questionSettle
}

// markQuestionShown stamps the head question the first time a frame is drawn
// with it on. It is called from the DRAW, which is the only place that knows
// the question was actually on a screen — the settle guard is a claim about
// what a person could have seen.
func (a *app) markQuestionShown(token string) {
	if open := a.questionHeld(token); open != nil && open.shown.IsZero() {
		open.shown = a.now()
	}
}

// ── the rule offer ──────────────────────────────────────────────────────────

// questionRule decides whether this question's answers row carries `r make it a
// rule for <scope>`.
//
// THE THIRD SAME-SHAPED YES, AND NEVER A HIDDEN RULE. The count is per SHAPE —
// the lane and the subject's name together, which is "the same question about
// the same thing" — and it is only ever an OFFER: nothing is written until
// somebody presses the key, and a row that a rule later answers says so out
// loud (`· your rule from <day> · change`).
func (a *app) questionRule(q *questionShown) {
	if q.question.Ask != session.AskPermission || len(q.question.Scope) == 0 {
		return
	}
	q.rule = a.questionYeses[questionShape(q.question)] >= questionRuleAfter-1
}

// questionShape is what "the same question again" means: the lane it came from
// and what it is about. The head is deliberately not in it — a gate that asks
// about `rm -rf build` and then about `rm -rf dist` is asking one question
// twice, and a shape keyed on the sentence would never notice.
func questionShape(q session.Question) string {
	subject := strings.TrimSpace(q.Subject.Name)
	if subject == "" {
		subject = strings.TrimSpace(q.Subject.Ref)
	}
	return string(q.Kind) + "/" + string(q.Ask) + "/" + subject
}

// questionRuleWord is what the `r` key offers, with the scope written into it:
// the widest scope the question said an answer could carry, in that scope's own
// word.
func questionRuleWord(q session.Question) string {
	scope := "this project"
	for _, one := range q.Scope {
		switch one {
		case session.ScopeAlways:
			scope = "everywhere"
		case session.ScopeProject:
			if scope != "everywhere" {
				scope = "this project"
			}
		case session.ScopeTask:
			if scope != "everywhere" && scope != "this project" {
				scope = "this task"
			}
		}
	}
	return "make it a rule for " + scope
}

// ── drawing ─────────────────────────────────────────────────────────────────

// questionHeight is how many rows the block spends. It is COUNTED by laying the
// rows out at the frame's own width rather than derived from a formula, for
// consent.go's reason: a block whose height and whose rows disagree puts the
// caret a row off the box.
func (a *app) questionHeight() int {
	if len(a.questions) == 0 && len(a.questionRecords) == 0 {
		return 0
	}
	width, _ := a.size()
	return len(a.questionRows(width))
}

// questionRows draws the block: the receipts and withdrawals that are still
// worth a row, then the head question in whichever of the three forms its
// evidence asks for.
//
// It is laid out by [app.chrome], directly above the input, because that is
// where this surface puts everything it wants answered.
func (a *app) questionRows(width int) []string {
	// THE NEW-CHAT PAGE OWNS THE WHOLE COMPOSER. It is drawn over the
	// conversation whose question this is, and every key on it is either one of
	// the page's own four keys or part of its first message. Drawing an answer
	// row there would offer keys that cannot honestly act on it. Clear the
	// geometry too: a pointer press in cells where the question stood on the
	// previous frame must not answer it through stale spans.
	if a.startingChat() {
		a.questionBands, a.questionSpans = nil, nil
		return nil
	}
	if len(a.questions) == 0 && len(a.questionRecords) == 0 {
		// The empty block, on the empty path: no spans to clear because none
		// were written, and nothing allocated (see [app.questionOpen]).
		return nil
	}
	a.questionSpans = nil
	if width < 1 {
		return nil
	}
	if a.questionOffFrame() {
		// A QUESTION IS DRAWN WHERE IT CAN BE ANSWERED AND NOWHERE ELSE — the
		// drawing half of the rule [app.questionOffFrame] states for keys. The
		// start page keeps the chrome under it, so without this the approval
		// block sat at the foot of that page offering `allow once` to a
		// keyboard that belonged to the page (#677).
		return nil
	}
	out := make([]string, 0, 8)
	for _, record := range a.questionRecordsShown() {
		out = append(out, a.questionRecordRow(record, width))
	}
	head, ok := a.questionHead()
	if !ok {
		// A QUESTION PUT OFF FOLDS IN PLACE AND NOT TO THE FAR SIDE OF THE
		// FRAME (owner ruling 2026-09-11, fold pick A): one titled rule where
		// the panel was, saying what is waiting and how to open it again.
		//
		// AND NOT UNDER THE PAGE THAT IS SHOWING IT. `o` folds the question it
		// opens, so the block will not draw it a second time with keys of its
		// own ([app.openQuestionRoom]). Its fold rule under the page would still be
		// the same question drawn twice, with `space open` offering what is
		// already open.
		if folded, has := a.questionPutOff(); has && a.questionQuieted() && !a.questionPageShows(folded) {
			// A SET PUT OFF FOLDS TO ONE RULE, which says how many and what
			// each is called (questionset.go).
			if set := a.questionSetFolded(folded); set != nil {
				out = append(out, a.questionSetFoldedRow(set, width))
			} else {
				out = append(out, a.questionFoldedRow(folded, width))
			}
		}
		return out
	}
	if head.shown.IsZero() && !a.questionQuieted() {
		// THE BOX IS NEVER MOVED UNDER A HAND. The question is open, the chip
		// is counting it and home can see it; what it may not do is take rows
		// out from under a sentence somebody is in the middle of.
		//
		// IT IS ABOUT ARRIVING AND NOT ABOUT STANDING. A question that has been
		// drawn stays drawn while somebody types their answer into the box —
		// the words in the box ARE the answer on a question the turn is waiting
		// on ([app.questionEnter]) — and a block that vanished on the first
		// keystroke would take the question away exactly as they started
		// answering it.
		return out
	}
	a.markQuestionShown(head.token())
	// WHICH ANSWER THE MOUSE IS OVER, read off the bands the LAST paint wrote.
	// Hover is one frame behind everywhere on this surface — the pointer is
	// resolved against the chrome marks of the screen a person is looking at —
	// and this is that reading said in the block's own terms, so a row can be
	// painted once, on the ground it belongs on, rather than painted focused and
	// then painted over.
	a.hotAnswer = -1
	if a.hot.kind == hoverChoices {
		for _, band := range a.questionBands {
			if band.row == a.hot.index {
				a.hotAnswer = band.at
			}
		}
	}
	a.questionBands = nil
	// WHERE THE ANSWERS ARE IS COUNTED FROM THE TOP OF THE BLOCK, and the forms
	// below count from the top of themselves. The receipts above them are rows
	// of the block too, so a click was resolved one row out for every receipt
	// standing — press the answers row under a receipt and the press landed on
	// the row above it, which on a card is an answer nobody aimed at.
	// [app.shiftQuestionMarks] is where the two readings are made one, and it is
	// the ONLY place a mark is moved: a form that shifted its own bands as well
	// would move them twice and put every target one row below the row a person
	// aimed at.
	base := len(out)
	// ONE CHOOSER DECIDES WHICH DRAWING THIS QUESTION GETS, and it reads the
	// question's own properties rather than its kind (questionchooser.go).
	switch a.questionViewOf(head, width) {
	case viewNone:
		// Nothing of this window is showing the question; the chip has it.
		return out
	case viewPhone:
		// THE NARROW SHEET SAYS THE QUEUE COUNT ON ITS OWN FOOT, beside the
		// clock, so it takes the rows whole (questionnarrow.go).
		out = append(out, a.questionNarrowRows(head, width)...)
		a.shiftQuestionMarks(base)
		return out
	case viewTabs:
		// SEVERAL QUESTIONS FROM ONE STEP ARE ONE PANEL, and the count under it
		// is of what is waiting BEHIND the set rather than inside it.
		set := a.questionSet()
		out = append(out, a.questionSetRows(set, width)...)
		a.shiftQuestionMarks(base)
		if more := a.questionWaitingCount() - len(set); more > 0 {
			out = append(out, a.pal.dim(fit("  "+itoa(more)+" more", width)))
		}
		return out
	case viewRow:
		if head.question.Ask == session.AskRatify {
			out = append(out, a.questionRatifyRows(head, width)...)
			break
		}
		out = append(out, a.questionLineRows(head, width)...)
	default:
		// THE PANEL'S BANDS ARE NOT SHIFTED HERE. They are counted from the top of
		// the panel, [app.questionPanelRows] moves them past its own frame edge
		// and head rows, and [app.shiftQuestionMarks] below moves them past the
		// receipts — one hand each. This case shifted them by the receipts too
		// for a while, which is the double-shift that put every target one row
		// below the row a person aimed at.
		out = append(out, a.questionPanelRows(head, width)...)
	}
	a.shiftQuestionMarks(base)
	// AND A RATIFY LINE IS NEVER COUNTED HERE: nothing waits on it, so a queue
	// count that included one would put a number on the block no key can clear.
	//
	// (#954's clock aside is NOT appended here. It is a sentence, and the row
	// under the frame is the second tier of KEYS and nothing else — a sentence
	// mixed into it is the hierarchy the owner's hints ruling exists to prevent.
	// It is drawn as the panel's own last body row instead, inside the frame:
	// questionpanel.go's [app.questionPanelBody]. Agreed with lane A before
	// either landed.)
	if more := a.questionWaitingCount() - 1; more > 0 {
		if _, panel := a.questionDialog(width); !panel {
			out = append(out, a.pal.dim(fit("  "+itoa(more)+" more", width)))
		}
	}
	return out
}

// shiftQuestionMarks moves the marks one form wrote onto the block's own rows.
//
// A FORM DRAWS ITSELF AND KNOWS NOTHING ABOUT WHAT IS ABOVE IT — the receipts of
// what was just answered — and the pointer and the click resolve against the
// block ([app.chromeAt]). Rather than teach every form where it happens to be
// standing, the one place that stacks them says so afterwards, once.
//
// IT MOVES BOTH KINDS OF TARGET, which is why no form may move its own. A row
// target is a band ([app.questionBands], whole rows on the panel and the sheet)
// and a column target is a span ([app.questionSpans] on one row of a line), and
// both are read against the block's own coordinates.
func (a *app) shiftQuestionMarks(by int) {
	if by == 0 {
		return
	}
	if a.questionSpanRow >= 0 {
		a.questionSpanRow += by
	}
	for i := range a.questionBands {
		a.questionBands[i].row += by
	}
}

// questionClockAside is the dim line under a question whose clock will ANSWER
// it, and it is there because the tail above says what is about to happen
// without saying what a person can do about it.
//
// BOTH HALVES ARE THE THING SOMEBODY WATCHING A COUNTDOWN WANTS TO KNOW. Any key
// stops it — literally any, because [app.holdQuestionClocks] runs on every key
// the block reads — and a pick the clock takes is provisional: the receipt it
// leaves offers `c change`, and the model is told in so many words that the
// answer may still change (session's `askProvisionalNote`). A countdown nobody
// can stop and nobody can walk back is the shape this program must never have
// (F41), so the row says it is neither.
//
// IT IS ONLY DRAWN WHILE THE CLOCK IS REALLY RUNNING. A held one says `paused`
// in the tail and has nothing left to stop; a question with no clock at all
// would be told about a key that does nothing.
func (a *app) questionClockAside(head questionShown, width int) string {
	if !questionClockAnswers(head.question) {
		return ""
	}
	if head.question.Deadline.Sub(a.now()) <= 0 {
		return ""
	}
	// THE INDENT IS THE CALLER'S. This is drawn as a row INSIDE the frame now
	// (questionpanel.go), which lays its own rows in the panel's gap; a "  " here
	// as well was the block-level indent it wore while it hung under the frame.
	return a.pal.dim(fit(questionClockAsideWord, width))
}

// questionClockAsideWord is that line, spelled once because the manual quotes it
// and the tmux suite waits for it.
const questionClockAsideWord = "any key stops the clock · you can still change the answer afterwards"

// questionFoldedRow is the panel folded IN PLACE: one titled rule above the box,
// saying what is still waiting and how to open it.
//
//	── ? Which storage for the session index? · 3 answers · ◆ SQLite ─── space open ──
//
// `esc` USED TO SEND A QUESTION TO THE STATUS LINE. The rows came off the screen
// and what was left was a chip on the far side of the frame counting questions —
// so a person who pressed the key that means *later* had to learn a chord to
// find what they had put off. It folds here now, where it was, and the rule says
// the three things somebody needs to decide whether to open it again: what was
// asked, how many answers there are, and which one the asker would take.
func (a *app) questionFoldedRow(q questionShown, width int) string {
	parts := []string{strings.TrimSpace(q.question.Head)}
	if n := len(q.question.Options); n > 1 {
		parts = append(parts, itoa(n)+questionAnswersWord)
	}
	if key := questionPickKey(q.question); key != "" {
		if at, ok := questionOptionAt(q.question, key); ok {
			parts = append(parts, a.pal.warnBold(a.icon(tokens.GRecommended))+a.pal.dim(" "+
				questionAnswerWord(q.question.Options[at], key, true)))
		}
	}
	title := a.questionMarkFor(q.question) + a.pal.dim(" "+strings.Join(parts, " · "))
	return framed{title: title, aside: a.pal.dim(questionOpenFoldWord)}.rule(a.pal, width)
}

// questionAnswersWord is the rule's own count of what is behind it.
const questionAnswersWord = " answers"

// questionOpenFoldWord is how a folded question is opened again, and the key is
// `space` for one reason: it is the one key a person presses over an empty box
// that means nothing at all there. It works ONLY over an empty box
// ([app.questionKey]), because a space inside a sentence is a space.
const questionOpenFoldWord = questionToggleKey + " open"

// questionPickKey is the key the asker recommended, or "".
func questionPickKey(q session.Question) string {
	if q.Pick == nil {
		return ""
	}
	return strings.TrimSpace(q.Pick.Key)
}

// questionWaitingCount is how many questions this conversation is holding for a
// person right now — the folded ones counted, because `esc` is later and not
// cancelled, and the ratify lines NOT counted, because nothing waits on one.
func (a *app) questionWaitingCount() int {
	n := 0
	for _, q := range a.questions {
		if questionWaits(q.question) {
			n++
		}
	}
	return n
}

// questionWaits reports whether a question is one somebody is being waited on
// for. Two rungs of the ladder are not: an assumptions card says what the asker
// took for granted and goes on by itself, and a ratify line says what it has
// already done ([questionAskSlot] is the same reading, about the mark).
func questionWaits(q session.Question) bool {
	_, waiting := questionAskSlot(q.Ask)
	return waiting
}

// questionMark is the one-cell glyph at the head of a question, and it is the
// vocabulary's own ([internal/tui2/tokens]) through the one door.
//
// AMBER, ALWAYS, AND ONLY HERE. `?` is [tokens.GNeedsHuman], whose binding says
// "waiting on a human (always amber)" in the vocabulary itself, and amber on
// this surface means that and nothing else.
//
// THE BLOCK'S WORDS ARE ORDINARY INK, and that is the half of this comment the
// owner's colour ruling of 2026-09-11 rewrote. They kept a question hue of their
// own — a violet spent on the head, every answer label and every key — while
// home, the places and the chip said the same "waiting on you" in this amber, so
// one meaning wore two colours and whole rows stood in a status hue. The violet
// is retired from both ladders (docs/DESIGN-LANGUAGE.md), and the marks carry
// the meaning: this one, the pointer and the recommended diamond.
func (a *app) questionMark() string {
	return a.pal.warnBold(a.icon(tokens.GNeedsHuman))
}

// questionAskSlot is the vocabulary slot ONE SHAPE of question wears, and
// whether that shape is one somebody is being waited on for.
//
// TWO RUNGS OF THE LADDER ARE NOT WAITING ON ANYBODY, and until this existed
// both wore the attention mark anyway. An assumptions card says what the asker
// took for granted and goes on after its clock; a ratify line says what it has
// already done. Drawing either in amber tells a person to answer something that
// is not asking them anything — and amber on this surface means waiting-on-you
// and nothing else (docs/design/questions/DESIGN.md's HUE), so a mark that
// spends it on a card nobody has to touch is a mark that spends it everywhere.
func questionAskSlot(ask session.AskKind) (tokens.GlyphID, bool) {
	switch ask {
	case session.AskAssumption:
		return tokens.GAssumed, false
	case session.AskRatify:
		return tokens.GSettled, false
	}
	return tokens.GNeedsHuman, true
}

// questionMarkFor is [app.questionMark] for one question: its own shape's mark,
// in its own shape's hue.
func (a *app) questionMarkFor(q session.Question) string {
	slot, waiting := questionAskSlot(q.Ask)
	if waiting {
		return a.pal.warnBold(a.icon(slot))
	}
	return a.pal.dim(a.icon(slot))
}

// questionLineRows is the ONE-ROW view: the question and its answers on a single
// row above the box, with the keys on a dim row under it.
//
// IT IS WHAT IS LEFT WHEN THERE IS NOTHING TO WEIGH. The chooser
// (questionchooser.go) sends a question here only when its whole decision is its
// answers' own words — no consequence, no body, no evidence, no pick with a
// reason, no command to read — so a frame around it would be a box drawn around
// one sentence.
func (a *app) questionLineRows(q questionShown, width int) []string {
	out := make([]string, 0, 4)
	if row, ok := a.questionSubjectRow(q, width); ok {
		out = append(out, row)
	}
	a.questionSpans, a.questionSpanRow = nil, len(out)
	if len(q.beat) > 0 {
		out = append(out, a.questionBeatRow(q, len(out), width))
	} else {
		out = append(out, a.questionRowOffer(q, width))
	}
	if reason := strings.TrimSpace(q.question.Reason); reason != "" {
		// SUPPRESSED ONLY WHILE IT IS THE SENTENCE THE SUBJECT CARD IS ALREADY
		// DRAWING (questionAttribution): the two are the same sentence while
		// the question is unanswered, and an answered-and-re-raised landing's
		// reason carries the answer's fate, which the card cannot have —
		// suppressing it would draw the twelfth card byte-identical to the
		// first (session's landingAnsweredStamp, #1077).
		if a.questionSubjectAt(q.question) < 0 || a.questionReasonIsNews(q.question, reason) {
			out = append(out, a.pal.dim(fit("  "+reason, width)))
		}
	}
	// AND WHILE THE BOX IS WRITING TO THE QUESTION, that row says what the box
	// means now instead of naming keys that are letters and type.
	if q.writing != "" {
		out = append(out, a.pal.dim(fit("  "+a.questionWritingRow(q), width)))
		return out
	}
	if keys := a.questionRowKeys(q, width); keys != "" {
		out = append(out, keys)
	}
	return out
}

// questionReasonIsNews says the question's reason is NOT the sentence the
// landed subject's own card is already drawing under itself (taskdone.go's
// [app.doneUnder]). While the question is unanswered it is: the reason is the
// ask's own sentence, or that sentence with the decider clause appended
// (session's landingReason) — both are the card's to say. An answered-and-
// re-raised landing's reason LEADS with the answer's fate instead (session's
// landingAnsweredStamp), which the card cannot have — and suppressing it
// would draw the twelfth card byte-identical to the first (#1077). A subject
// with no done card in the transcript has no sentence to double.
func (a *app) questionReasonIsNews(q session.Question, reason string) bool {
	// NEWEST FIRST: the newest done card is the one that says where the work
	// stands now, and older ones are frozen at older states — a state
	// round-trip leaves two done cards on one id, and the oldest-first walk
	// compared against a card no screen draws. [app.doneEntryFor] walks
	// entries the same way for the same reason.
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		if e.kind == entryDone && e.done != nil && e.done.id == q.Subject.ID {
			drawn := strings.TrimSpace(e.done.status.Ask.Reason)
			// A CARD WITH NO ASK REASON STILL SAYS WHO IS DECIDING — the decider
			// is drawn in the card's own chips, and [session.LandingDecidingWord]
			// is that clause — so the question repeating the clause alone beside
			// it is a duplication, not news. The clause with a reason after it IS
			// news the card cannot have.
			if drawn == "" {
				return reason != session.LandingDecidingWord
			}
			return reason != drawn && !strings.HasPrefix(reason, drawn+" · ")
		}
	}
	return false
}

// questionRowOffer is that row: the mark, the head, and every answer beside it,
// the pointed one on the selected ground.
//
// NO ANSWER IS EVER DROPPED OR CUT. A row too narrow for every answer is a row
// the chooser should not have chosen, so it says so by giving the question the
// panel instead ([app.questionRowFits] is asked before this is drawn).
func (a *app) questionRowOffer(q questionShown, width int) string {
	spans := make([]choiceSpan, 0, len(q.question.Options))
	line := a.questionMarkFor(q.question) + " " + a.pal.ink(strings.TrimSpace(q.question.Head))
	at := 2 + ansi.StringWidth(strings.TrimSpace(q.question.Head))
	for i, option := range q.question.Options {
		key := questionOptionKeyAt(q.question, i)
		word := questionAnswerWord(option, key, false)
		mark := "   "
		if i == q.pick && len(q.question.Options) > 1 {
			mark = "  " + a.pal.warnBold(a.icon(tokens.GPointer))
		}
		cell := a.pal.data(key) + a.pal.ink(" "+word)
		if i == q.pick && len(q.question.Options) > 1 {
			cell = a.pal.background(cell, 0, a.pal.ramp.selected)
		}
		spans = append(spans, choiceSpan{from: at + 3, to: at + 3 + ansi.StringWidth(key+" "+word), at: i})
		at += 3 + ansi.StringWidth(key+" "+word)
		line += mark + cell
	}
	a.questionSpans = spans
	return a.questionHovered(fit(line, width), a.questionSpanRow, width)
}

// questionRowFits reports whether the one-row view can hold this question's head
// and every one of its answers at this width. The chooser asks before it sends a
// question here: a form promotes rather than cutting an answer, and an offer
// with an answer missing is an offer that hides an answer.
func questionRowFits(q session.Question, width int) bool {
	room := 2 + ansi.StringWidth(strings.TrimSpace(q.Head))
	for i, option := range q.Options {
		room += 3 + ansi.StringWidth(questionOptionKeyAt(q, i)+" "+questionAnswerWord(option, questionOptionKeyAt(q, i), false))
	}
	return room <= width
}

// questionRowKeys is the dim row under the one-row view: the keys that answer,
// then the keys that say something about the decision, in the ONE key row
// grammar every question on this surface uses ([app.questionKeyRow]).
func (a *app) questionRowKeys(q questionShown, width int) string {
	keys := a.questionAnswerKeys(q, formsLine)
	if len(keys) == 0 {
		return ""
	}
	room := max(width-2, 1)
	row := a.questionKeyRow(q, keys, room)
	if clock := a.questionClockWord(q); clock != "" && ansi.StringWidth(ansi.Strip(row))+len(questionKeyGap)+ansi.StringWidth(clock) <= room {
		row += a.pal.dim(questionKeyGap + clock)
	}
	return "  " + row
}

// questionRatifyRows is the ratify line: the third rung of the ladder drawn as
// what it is — a statement about something already done, with the way back.
//
// NOTHING WAITS ON IT, which is the whole reason it is one row and wears the
// settled mark rather than the attention one. The work happened; a person who
// reads past it has ratified it by saying nothing, and that is the rung's
// bargain rather than a corner cut.
func (a *app) questionRatifyRows(q questionShown, width int) []string {
	head := strings.TrimSpace(q.question.Head)
	a.questionSpans, a.questionSpanRow = nil, 0
	line := a.questionMarkFor(q.question) + " " + a.pal.ink(head)
	// ITS KEYS COME OFF THE ONE TABLE AND WEAR THE ONE GRAMMAR — the key in the
	// payload hue, its word dim, no brackets ([app.questionKeyRow]). A ratify
	// line has no answers to list, so the row is the head and that tail.
	keys := a.questionAnswerKeys(q, formsRatify)
	if len(keys) > 0 {
		room := width - ansi.StringWidth(head) - 4
		if tail := questionKeyWords(q, keys); ansi.StringWidth(tail) <= room {
			return []string{line + "  " + a.paintQuestionKeys(q, keys)}
		}
	}
	return []string{fit(line, width)}
}

// questionSubjectRow is the row the question is ABOUT, drawn by the renderer
// that already drew it in the transcript.
//
// IT SHOWS THE ROW THAT IS ALREADY THERE — consent.go's first decision, kept
// whole and generalised. The question is about something the transcript has
// drawn, so the block re-uses that row rather than describing it a second time
// in different words. Two renderings of one call is how a person ends up
// approving something other than what they read.
func (a *app) questionSubjectRow(q questionShown, width int) (string, bool) {
	at := a.questionSubjectAt(q.question)
	if at < 0 || at >= len(a.entries) {
		return "", false
	}
	if e := &a.entries[at]; e.kind == entryTool {
		return a.toolLine(e, at, true, width), true
	}
	return "", false
}

// questionSubjectAt finds the transcript row a question's subject names, or -1.
//
// THE CALL'S ID IS THE ANSWER WHEREVER THERE IS ONE, which is consent.go's own
// law and matters here for the same reason: a question that landed on the wrong
// row is a person reading one command and answering about another. The gate
// sends it with the question (session's consent.go), the row has been carrying
// it since its first fragment (app.go's [entry.callID]), and pairing on it is
// exact.
//
// THE WALK BY NAME IS WHAT IS LEFT FOR A PROVIDER THAT STREAMS NO IDS — oldest
// still-running row of that tool, the same rule [feed.closeTool] uses, because
// the first call begun is the one a person watching the column expects to be
// asked about first. It is a convention rather than a fact, which is why it is
// second.
//
// AND A ROW ANOTHER OPEN QUESTION IS ALREADY ABOUT IS SKIPPED EITHER WAY. One
// batch can raise three bash questions at once, and every one of them would
// otherwise attach to the first bash row on screen — three questions annotating
// one line and two calls the person never saw asked about.
func (a *app) questionSubjectAt(q session.Question) int {
	if q.Subject.Kind == session.SubjectOrder {
		// A STANDING ORDER'S SUBJECT IS ITS OWN CARD IN THE TRANSCRIPT
		// (standing.go). It is paired on the id the engine minted before anybody
		// was asked, exactly as a node's is, so there is no walk-by-name arm.
		for i := range a.entries {
			if e := &a.entries[i]; e.kind == entryStanding && e.stand != nil && e.stand.id == q.Subject.ID {
				return i
			}
		}
		return -1
	}
	if q.Subject.Kind == session.SubjectNode {
		// A NODE'S SUBJECT IS ITS OWN BLOCK IN THE TRANSCRIPT (task.go). It is
		// paired on the id, which the engine minted before anybody was asked, so
		// there is no walk-by-name arm to fall back to and none is wanted.
		for i := range a.entries {
			if e := &a.entries[i]; e.kind == entryTask && e.card != nil && e.card.id == q.Subject.ID {
				return i
			}
		}
		return -1
	}
	if q.Subject.Kind != session.SubjectCall {
		return -1
	}
	call := strings.TrimSpace(q.Subject.CallID)
	for i := range a.entries {
		if a.entries[i].kind == entryTool && call != "" && a.entries[i].callID == call {
			return i
		}
	}
	if call != "" {
		return -1
	}
	name := strings.TrimSpace(q.Subject.Name)
	if name == "" {
		return -1
	}
	// The rank is which of the questions with NO id about this tool this one is,
	// oldest first, and it takes the row of the same rank. Answering "the oldest
	// live row" to every one of them would put three questions on one line and
	// leave two calls the person never saw asked about; the sequence is the only
	// thing left to pair on when there is no id to pair on.
	rank, token := 0, string(q.Kind)+":"+q.Token()
	for _, open := range a.questions {
		if open.token() == token ||
			strings.TrimSpace(open.question.Subject.CallID) != "" ||
			strings.TrimSpace(open.question.Subject.Name) != name {
			continue
		}
		// THE FOLDED ONES COUNT. `esc` is later and not cancelled, so a question
		// somebody put off is still a question about a row — and a rank that
		// dropped when they pressed it would move the question in front of them
		// onto a different call.
		if questionOlder(open.question, q) {
			rank++
		}
	}
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || !e.status.live() || e.decision != "" || e.tool != name {
			continue
		}
		if rank > 0 {
			rank--
			continue
		}
		return i
	}
	return -1
}

// questionOlder is the block's one ordering of two questions, and it is the
// engine's own ([session.Agent.OpenQuestions]): when they were asked, and their
// tokens where that is the same instant.
func questionOlder(one, two session.Question) bool {
	if !one.Asked.Equal(two.Asked) {
		return one.Asked.Before(two.Asked)
	}
	return string(one.Kind)+":"+one.Token() < string(two.Kind)+":"+two.Token()
}

// questionAttribution is the dim line under a card's head: why now, and who is
// asking, in that order and joined by the surface's own separator.
//
// reason is whether the WHY belongs on this line at all. It is false where the
// transcript is already drawing the thing the question is about and has that
// sentence under it ([app.questionCardRows] says which case); the asker is
// still said, because who asked is a fact no block in the conversation carries.
//
// THE EMPTINESS LAW. No reason and no named asker is no row at all — not an
// empty one, and never the word "unknown".
func (a *app) questionAttribution(q session.Question, reason bool) string {
	parts := make([]string, 0, 2)
	if line := strings.TrimSpace(q.Reason); reason && line != "" {
		parts = append(parts, line)
	}
	if who := questionAskerWord(q.Asker); who != "" {
		parts = append(parts, who)
	}
	return strings.Join(parts, " · ")
}

// questionAskerWord is who is asking, in the words a person would use.
//
// NO MACHINERY VOCABULARY. The engine's own [session.AskerKind] spellings are
// `model`, `engine`, `task`, `surface`, `window`, and three of those are words
// about the program's insides. What a person needs to know is whether a person,
// this program, or a piece of work that is running asked — and the surface
// asking about itself needs no attribution at all, because they are looking at
// it.
func questionAskerWord(asker session.Asker) string {
	if name := strings.TrimSpace(asker.Name); name != "" {
		return name
	}
	switch asker.Kind {
	case session.AskerModel, session.AskerEngine:
		return product
	case session.AskerWindow:
		return "another window"
	}
	return ""
}

// questionAnswerWord is one answer's word on a row, with its parenthetical left
// off where the row has asked for the short spelling.
func questionAnswerWord(option session.AnswerOption, key string, terse bool) string {
	word := strings.TrimSpace(option.Label)
	if word == "" {
		return key
	}
	if !terse {
		return word
	}
	if at := strings.Index(word, " ("); at > 0 && strings.HasSuffix(word, ")") {
		return word[:at]
	}
	return word
}

// questionTerser reports whether any of this question's answers has an aside to
// give up. The emptiness law applied to a degrade: a row that tried the short
// spelling when there is no short spelling would loop.
func questionTerser(q session.Question) bool {
	for _, option := range q.Options {
		word := strings.TrimSpace(option.Label)
		if at := strings.Index(word, " ("); at > 0 && strings.HasSuffix(word, ")") {
			return true
		}
	}
	return false
}

// ── the second beat ─────────────────────────────────────────────────────────
//
// A WIDENING ANSWER SOMETIMES ASKS ONE MORE THING.
//
// `always` on a shell command used to write the line down exactly as it ran,
// which meant an always pressed on `git status --short` bought silence for that
// string and nothing else: the same work with one more flag asked again, and the
// person pressed always forever. What they meant was a SHAPE, and only they know
// which one. So for one keystroke the answers row becomes:
//
//	always? [1] git status*  ·  [2] git *  ·  [3] just this line  ·  [esc] never mind
//
// Three decisions, and each is why this is a beat rather than a guess:
//
//   - NOTHING IS WIDENED WITHOUT BEING READ. The shapes come from the lane
//     ([questionShown.shapes]) and are printed in full before any of them is
//     written. A block that widened a permission on its own would be this
//     surface deciding a permission on somebody's behalf.
//   - IT REPLACES THE ANSWERS ROW, in place, where the answers were. It is the
//     same question one step further in, not a second block appearing under the
//     first; the row above it does not move.
//   - esc LEAVES THE BEAT AND ANSWERS NOTHING. Everywhere else on this block esc
//     is *later*; here the thing on screen is a step inside an answer, and
//     backing out of a step puts the question back — the work is still waiting,
//     and the person still has every answer they had a moment ago.

// questionBeatLead is what the beat's row opens with. It is the widening
// answer's own word with a question mark on it, because the beat is that answer
// asking which of its shapes was meant.
const questionBeatLead = "always? "

// questionBeatBack is the way out of the beat, in the words for backing out of a
// step rather than the words for answering.
const questionBeatBack = "never mind"

// questionBeatRow draws the shapes where the answers row was: by number, the
// line itself last, and the way back out.
//
// It degrades the answers row's way and for the answers row's reason, with one
// swap: the way out is the FIRST thing dropped when the shapes will not fit
// beside it. A person who can see the shapes can still press esc, and a shape
// they cannot see is a shape they cannot choose.
func (a *app) questionBeatRow(q questionShown, row, width int) string {
	spans := make([]choiceSpan, 0, len(q.beat))
	line := a.questionMarkFor(q.question) + " " + a.pal.ink(questionBeatLead)
	at := 2 + ansi.StringWidth(questionBeatLead)
	for i := range q.beat {
		word := questionBeatShapeWord(q.beat, i)
		// THE CURSOR IS ON THE SHAPE THE ARROWS LEFT IT ON (#919). A beat drawn
		// with no cursor ate every arrow aimed at it: `←→` moved
		// [questionShown.beatAt] and nothing on the row said so, so the only way
		// to find out where the cursor was, was to press enter. It is drawn in
		// the answers row's own grammar — the pointer in the gap that is already
		// three cells wide, the chosen shape on the `selected` ground — so the
		// beat and the row it replaced say "you are here" the same way, and the
		// spans below keep measuring the same columns either way.
		mark := "   "
		cell := a.pal.data(itoa(i+1)) + a.pal.ink(" "+word)
		if i == q.beatAt && len(q.beat) > 1 {
			mark = "  " + a.pal.warnBold(a.icon(tokens.GPointer))
			cell = a.pal.background(cell, 0, a.pal.ramp.selected)
		}
		spans = append(spans, choiceSpan{from: at + 3, to: at + 3 + ansi.StringWidth(itoa(i+1)+" "+word), at: i})
		at += 3 + ansi.StringWidth(itoa(i+1)+" "+word)
		line += mark + cell
	}
	// THE WAY OUT IS THE FIRST THING DROPPED when the shapes will not fit beside
	// it: a person who can see the shapes can still press esc, and a shape they
	// cannot see is a shape they cannot choose.
	back := a.pal.dim(questionKeyGap) + a.pal.data(questionLaterKey) + a.pal.dim(" "+questionBeatBack)
	if ansi.StringWidth("  "+ansi.Strip(line+back)) <= width {
		line += back
	}
	a.questionSpans, a.questionSpanRow = spans, row
	return fit(line, width)
}

// questionBeatShapeWord is how one shape reads on the beat. The last one is the
// line itself ([config.BashShapes] promises that), and it is named for what it
// does rather than repeated: the command is already on the row above, and
// printing it twice on two lines is the two-renderings defect this block exists
// to avoid.
//
// IT IS NOT [questionShapeWord], which is the autonomy sheet's row name and takes an
// [session.AskKind]. The two words are about different things — a rule's shape
// and a decision's shape — and sharing one name once cost a build.
func questionBeatShapeWord(shapes []string, at int) string {
	if at == len(shapes)-1 {
		return "just this line"
	}
	return shapes[at]
}

// questionBeating reports whether the beat is on screen, which is what decides
// which keys mean something (render.go's hint reads it too).
func (a *app) questionBeating() bool {
	head, ok := a.questionHead()
	return ok && len(head.beat) > 0
}

// questionWiden is the widening answer pressed: the beat where there is one to
// open, and the answer itself where there is not.
//
// ONE SHAPE IS NOT A CHOICE, and none at all is a command nothing can be derived
// from. Both answer the way this question always did — the lane's own hand
// ([questionShown.answered]) writes down what it can.
func (a *app) questionWiden(head questionShown, key string) tea.Cmd {
	if head.shapes == nil {
		return a.questionAnswerKey(head, key)
	}
	shapes := head.shapes()
	if len(shapes) <= 1 {
		return a.questionAnswerKey(head, key)
	}
	a.setQuestionBeat(head, shapes)
	return nil
}

// setQuestionBeat puts the shapes on screen, or takes them off with nil.
func (a *app) setQuestionBeat(head questionShown, shapes []string) {
	for i := range a.questions {
		if a.questions[i].token() != head.token() {
			continue
		}
		a.questions[i].beat = shapes
		// THE CURSOR OPENS ON THE SHAPE THAT GRANTS LEAST, which is the last of
		// them: the shapes run from the widest pattern to the line itself
		// ([questionBeatShapeWord] says the last one is the line). It is the same
		// law that opens a hands-only question's pointer on the answer that
		// changes nothing ([questionPointerStart]) — a person who walks nowhere
		// and presses enter grants the least this beat can grant.
		a.questions[i].beatAt = max(0, len(shapes)-1)
		a.touch()
		return
	}
}

// questionBeatKey routes one key while the beat is up, and reports whether it
// took it.
//
// THE BEAT TAKES THE KEYS IT DRAWS AND THE ANSWERS' DIGITS WITH THEM. A person
// part-way through choosing a shape who pressed `3` must get the third shape and
// not the third answer — the row under their eyes is the shapes, and a digit
// read against the answers underneath would resolve a question that is no longer
// the one on screen.
func (a *app) questionBeatKey(head questionShown, key string) (tea.Cmd, bool) {
	if len(head.beat) == 0 {
		return nil, false
	}
	if key == questionLaterKey {
		a.setQuestionBeat(head, nil)
		return nil, true
	}
	// Shapes wrap like the options that opened this second choice.
	switch key {
	case "up", "left", "shift+tab":
		a.moveQuestionBeat(head, head.beatAt-1)
		return nil, true
	case "down", "right", questionBlankKey:
		a.moveQuestionBeat(head, head.beatAt+1)
		return nil, true
	case questionEnterKey:
		return a.questionPickShape(head, head.beatAt), true
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		return a.questionPickShape(head, int(key[0]-'1')), true
	}
	return nil, true
}

// moveQuestionBeat wraps the cursor around the available shapes.
func (a *app) moveQuestionBeat(head questionShown, to int) {
	open := a.questionHeld(head.token())
	if open == nil || len(open.beat) == 0 {
		return
	}
	open.beatAt = (to + len(open.beat)) % len(open.beat)
	a.touch()
}

// questionPickShape banks the shape at this index and answers with the widening
// answer it belongs to.
func (a *app) questionPickShape(head questionShown, at int) tea.Cmd {
	if at < 0 || at >= len(head.beat) {
		return nil
	}
	shape := head.beat[at]
	a.setQuestionBeat(head, nil)
	key := questionWideningKey(head.question)
	if key == "" {
		return nil
	}
	answer := session.Answer{
		Key: key, Picked: []string{key}, Scope: questionScopeOf(head, key),
		// THE SHAPE RIDES ON THE ANSWER, which is what lets the lane's own hand
		// write it down and lets the engine know a rule exists rather than
		// writing a wider one of its own ([session.AnswerBanked]).
		Comments: map[string]string{session.AnswerBanked: shape},
	}
	head.beat = nil
	return a.answerQuestion(head, answer)
}

// questionWideningKey is the key of the answer that grants more than the
// question asked about, or "" where the question has none.
func questionWideningKey(q session.Question) string {
	for _, option := range q.Options {
		if option.Widening {
			return strings.TrimSpace(option.Key)
		}
	}
	return ""
}

// questionLaterAt is the answer `esc` gives on a confirmation: THE LAST ONE.
//
// IT IS THE ANSWER THAT CHANGES NOTHING, and the last place is where every card
// on this surface has always put it ([questionSafeAt] says the same about its
// own fallback). It is a separate question from where the CURSOR starts, and the
// tab-close card is why: its cursor rests on `keep running`, which lets the tab
// go and leaves the work running, while its way out is `cancel`, which does not
// touch the tab at all. Reading one off the other would make the dismiss key
// close a tab — and a dismiss that changed something is the one key nobody could
// press safely.
func questionLaterAt(q session.Question) int {
	if len(q.Options) == 0 {
		return 0
	}
	return len(q.Options) - 1
}

// questionLaterWord is what `esc` is called on this question's row.
//
// IT IS THE ANSWER IT GIVES AND NOT ALWAYS THE WORD *later*. On a confirmation
// esc answers with the last answer ([app.questionKey] says why), so the row
// names that answer in the lane's own words — `keep going`, `cancel` — rather
// than promising a *later* the shape has not got.
func questionLaterWord(q session.Question) string {
	if q.Ask != session.AskConfirmation {
		return questionKeyWord(questionLaterKey)
	}
	if at := questionLaterAt(q); at < len(q.Options) {
		if word := strings.TrimSpace(q.Options[at].Label); word != "" {
			return word
		}
	}
	return questionKeyWord(questionLaterKey)
}

// questionDropVerb gives up the least valuable verb still on the row, and
// reports whether it found one. A verb ranked zero is never given up.
func questionDropVerb(keys []questionVerb) ([]questionVerb, bool) {
	at := -1
	for i := range keys {
		if keys[i].giveUp == 0 {
			continue
		}
		if at < 0 || keys[i].giveUp > keys[at].giveUp {
			at = i
		}
	}
	if at < 0 {
		return keys, false
	}
	return append(append([]questionVerb{}, keys[:at]...), keys[at+1:]...), true
}

// questionKeySpelling is how one key is PRINTED, which is not always how
// bubbletea spells it: the space bar is a key with no visible character, and a
// row that said `[ ]` would be a row with a hole in it.
func questionKeySpelling(key string) string {
	// Every key in the table is now spelled as the terminal sends it
	// ([questionToggleKey] says why the space bar was the last exception), so
	// there is nothing left to translate — and this stays as the ONE place a
	// translation would go if a key ever needs one again.
	return key
}

// questionHovered puts the background every pressable row on this surface takes
// under the answers row, where the pointer is on it (hover.go).
func (a *app) questionHovered(line string, row, width int) string {
	if a.hot.kind != hoverChoices || a.hot.index != row {
		return line
	}
	return a.pal.cursor(line, width)
}

// questionClock is the answers row's tail: how the silence is being held.
//
// SILENCE IS NEVER A NO. There is exactly one policy on this surface that acts
// without an answer — [session.PolicyRecommendThenAuto], the task proposal's,
// where the card is a chance to redirect rather than a gate — and its tail says
// what it is going to do, in those words. Every other question's clock says
// `waiting`, and waiting is what it does: F41 was a hidden ten-second timer
// that recorded "denied" and killed work nobody refused, and that is the one
// answer this surface must never give on somebody's behalf.
func (a *app) questionClock(q questionShown) string {
	if word := a.questionClockWord(q); word != "" {
		return " · " + word
	}
	return ""
}

// questionClockDefault is what the clock says will happen for a question that
// named no pick — which is a shape's own sentence and NOT one borrowed from the
// task proposal.
//
// `starts on its own` is a sentence about WORK BEGINNING, and it was written for
// the one lane that had a clock when this row was built: a proposal to start a
// task, where the countdown ends with something running. An assumptions card has
// no pick and never will — everything on it stands, which is the whole of the
// ladder's second rung — so every one of them read `starts on its own in 9m 57s`
// about a card that starts nothing. What the clock actually means there is that
// the asker stops waiting for a strike and carries on with what it said, and
// `goes on` is that in the words a person would use.
func questionClockDefault(ask session.AskKind) string {
	if ask == session.AskAssumption {
		return questionAssumptionClockWord
	}
	return questionProposalClockWord
}

// The two sentences, spelled once because the manual quotes them and the tmux
// suite waits for them.
const (
	// questionAssumptionClockWord ends `… in 9s` on an assumptions card: nothing
	// is decided at the end of it, the asker simply carries on.
	questionAssumptionClockWord = "goes on"
	// questionProposalClockWord ends the same tail where the countdown really
	// does start something, and it is the fallback for every shape that named no
	// pick to put its own word there.
	questionProposalClockWord = "starts on its own"
)

// questionClockWord is that tail without the separator that joins it to a line
// of words.
func (a *app) questionClockWord(q questionShown) string {
	if q.question.Policy.Kind == session.PolicyRecommendThenAuto && q.question.Deadline.IsZero() {
		// A CLOCK THAT WOULD HAVE ANSWERED AND HAS NO DEADLINE LEFT IS PAUSED,
		// and it says so rather than falling back to `waiting`. The deadline is
		// the one source of truth for the countdown (session's [Agent.holdAsk]
		// clears it when a key stops the clock), so a question that still says it
		// would take its own pick and no longer says when is a question somebody
		// is reading.
		return questionPausedWord
	}
	if q.question.Policy.Kind != session.PolicyRecommendThenAuto || q.question.Deadline.IsZero() {
		return a.questionHeldWord(q)
	}
	left := q.question.Deadline.Sub(a.now())
	if left <= 0 {
		return ""
	}
	// THE POLICY LINE, NOT A BARE NUMBER. `auto-starts in 9s` was machinery
	// describing itself; what a person needs is which answer is about to be
	// taken and when, which is the pick's own word and the clock together.
	word := questionClockDefault(q.question.Ask)
	if q.question.Pick != nil {
		if option, ok := q.question.Option(q.question.Pick.Key); ok {
			if label := strings.TrimSpace(option.Label); label != "" {
				word = label
			}
		}
	}
	tail := word + " in " + countdownWord(left)
	if q.ruled {
		// THE ROW WEARS ITS RULE. `D` is already on the answers row and is the
		// door that changes it ([questionKeys]), so what this adds is the fact
		// and not a second key: the clock is running because of something this
		// project was told to do, and a person watching it run is owed that.
		tail += " · " + questionOwnRuleWord
	}
	return tail
}

// questionOwnRuleWord is that half, spelled once and quoted in the manual.
const questionOwnRuleWord = "your rule"

// questionHeldWord is the tail on a question whose clock NEVER ANSWERS: the
// reading time the approval gate has always drawn, and the one word every other
// blocking question says while it stands there.
//
// IT IS NEVER EMPTY ON A QUESTION SOMETHING IS WAITING ON, and that is the whole
// of what it is for. F41 was a hidden ten-second timer that recorded "denied"
// and killed work nobody refused, and what let it hide for a year was a row with
// no tail: a person could not tell a question that would wait for them from one
// that was counting down to an answer they had not given. So the tail says which
// — `7s` while the reading time runs, `paused` the moment a key says somebody is
// at the keyboard, and `waiting` where there is no clock at all.
//
// A QUESTION NOTHING WAITS ON SAYS NOTHING. A ratify line is a statement about
// work already done, and a countdown beside it would be a promise that something
// is about to happen ([app.questionRatifyRows] says why nothing is).
func (a *app) questionHeldWord(q questionShown) string {
	if !q.question.Blocking.Turn && len(q.question.Blocking.Tasks) == 0 {
		return ""
	}
	if q.clockFor <= 0 {
		return session.ConsentWaiting
	}
	if q.clockHeld {
		return questionPausedWord
	}
	if left := q.clockAt.Add(q.clockFor).Sub(a.now()); left > 0 {
		return countdownWord(left)
	}
	return questionPausedWord
}

// questionPausedWord is what the tail says once the reading time has stopped —
// because a key was pressed, or because it ran out and HELD rather than
// answering ([app.tickQuestion]).
const questionPausedWord = "paused"

// questionReading reports whether this question has a reading clock that is
// still running: one that is not held, has not run out, and never answers.
func (a *app) questionReading(q questionShown) bool {
	if q.clockFor <= 0 || q.clockHeld {
		return false
	}
	return a.now().Before(q.clockAt.Add(q.clockFor))
}

// tickQuestion is the reading clock running out, on the frame clock that is
// already turning (app.go's [app.paint]) — no ticker of its own.
//
// AT EXPIRY IT HOLDS. SILENCE IS NOT A NO. The clock used to deny here — F41, a
// hidden ~10s timer recorded "denied" and killed work nobody refused — and that
// is the one answer this surface must never give on a person's behalf. The
// engine is BLOCKED on the question and stays blocked: the call does not run,
// and it is not refused either. The tail says paused, and the work waits.
//
// AND IT DOES NOT RUN ON A WINDOW NOBODY IS LOOKING AT. Ten seconds is "long
// enough to read a command and a rule" (config's DefaultConsentTimeout says so
// in those words), which is a claim about a person READING — and there is nobody
// reading a terminal that does not have the keyboard. A person who starts a turn
// in one window and steps over to another is the ordinary way this surface is
// used, and until the focus gate existed every call that turn made through the
// gate was refused ten seconds later by a clock they could not have beaten. From
// where they were sitting the unfocused session simply stopped working, and the
// reason was on a screen behind them.
func (a *app) tickQuestion() {
	if !a.focused {
		return
	}
	for i := range a.questions {
		q := &a.questions[i]
		if q.clockFor <= 0 || q.clockHeld {
			continue
		}
		if a.now().Before(q.clockAt.Add(q.clockFor)) {
			continue
		}
		q.clockHeld = true
		a.touch()
	}
}

// holdQuestionClocks stops every reading clock, and it is called from EVERY key
// the block reads — the answers included, which cost nothing because they
// resolve the question in the same breath.
//
// THERE IS NO WAY BACK. "Held" here means a person is at the keyboard, and that
// fact does not expire: a clock that resumed after a few idle seconds would be a
// clock that fires exactly when somebody has looked away from the screen
// mid-decision, which is the one moment it must not.
func (a *app) holdQuestionClocks() {
	for i := range a.questions {
		// AND THE LANE'S OWN CLOCK STOPS WITH IT. A reading clock holds by
		// itself; a clock that ANSWERS — the task proposal's, whose silence
		// starts the work — has to be stopped where it runs, which is in the
		// engine ([questionShown.held]). It is called once and then dropped,
		// because a key is evidence about a person and not about a key.
		if held := a.questions[i].held; held != nil {
			a.questions[i].held = nil
			held()
		}
		if a.questions[i].clockFor <= 0 || a.questions[i].clockHeld {
			continue
		}
		a.questions[i].clockHeld = true
		a.touch()
	}
}

// refocusQuestions hands every waiting question its whole reading time back,
// because the window it is drawn on has just got the keyboard.
//
// THE CLOCK MEASURES READING TIME AND NOT WALL TIME. A person returning to a
// window that has been blurred for an hour has read nothing yet, so restamping
// is what gives them the seconds the setting promises rather than an expiry on
// their first frame back.
//
// A HELD QUESTION STAYS HELD. [app.holdQuestionClocks] is a one-way door —
// somebody has touched the keys and is deciding — and alt-tabbing away and back
// is not them changing their mind.
func (a *app) refocusQuestions() {
	for i := range a.questions {
		if a.questions[i].clockFor <= 0 || a.questions[i].clockHeld {
			continue
		}
		a.questions[i].clockAt = a.now()
		a.touch()
	}
}

// questionReadingLeft is what is left of the head question's reading clock,
// whether it has been held, and whether there is a clock at all. It is what a
// switch carries to the conversation the question follows (detach.go).
//
// IT IS THE REMAINDER AND NOT THE STAMP, because a conversation that is not on
// screen has nobody reading it: what a person is owed when they come back is the
// time they had not spent, not the time that passed while they were elsewhere.
func (a *app) questionReadingLeft() (time.Duration, bool, bool) {
	head, ok := a.questionHead()
	if !ok || head.clockFor <= 0 {
		return 0, false, false
	}
	left := head.clockAt.Add(head.clockFor).Sub(a.now())
	if left < 0 {
		left = 0
	}
	return left, head.clockHeld, true
}

// questionHint is the line under the box while the block is up: the keys it has
// actually drawn, in the words it drew them with.
//
// IT IS DERIVED AND NEVER SPELLED TWICE. This slot named `a allow · t always`
// for a year after the block's answers had taken different keys, which is one
// keystroke with two readings and the wrong one widening a permission. So the
// words come from the question in front of the person: its own answers first,
// then the verbs the row is offering.
func (a *app) questionHint() string {
	head, ok := a.questionHead()
	if !ok {
		return ""
	}
	// THE SLOT ABOVE THE BLOCK DOES NOT RE-LIST THE ANSWERS (owner ruling
	// 2026-09-11, hints pick A). Every view but the chip's draws the answers and
	// their keys itself, two rows below this one, and a rule that spelled them
	// again — in a second order, with its own idea of which keys exist — was the
	// owner's "the hint line names keys that are not there". Where the question
	// is NOT on this screen the slot is the only place its keys can be said, and
	// there it still says them.
	if width, _ := a.size(); a.questionViewOf(head, width) != viewNone {
		return ""
	}
	return a.questionHintOn(head)
}

// questionHintOn is that sentence for a question the caller already has, which
// is what home's narrow foot asks for: below [homeCardMin] there is no card at
// all, so the one row home has left is the only place its question can spell its
// own answers (homeconfirm.go).
func (a *app) questionHintOn(head questionShown) string {
	if len(head.beat) > 0 {
		return "1-" + itoa(len(head.beat)) + " shape · " + questionLaterKey + " " + questionBeatBack
	}
	// THE ANSWERS AND NOT THE VERBS. This slot sits on the seam directly above
	// the block, and the block's own row is already spelling every key it
	// offers — so a slot that listed them again would be the same sentence
	// twice, three rows apart. What it is for is the one thing the row cannot
	// say from up here: which key ANSWERS, for somebody whose eyes are on the
	// box rather than on the question.
	parts := make([]string, 0, len(head.question.Options)+1)
	for i, option := range head.question.Options {
		key := strings.TrimSpace(option.Key)
		if key == "" {
			key = itoa(i + 1)
		}
		word := strings.TrimSpace(option.Label)
		if word == "" {
			word = key
		}
		parts = append(parts, key+" "+word)
	}
	if len(parts) == 0 {
		return ""
	}
	// And the way out, NAMED BY WHAT IT DOES HERE. On most questions `esc` is
	// *later* and this slot says so; on a confirmation there is no later, and
	// esc gives the answer that loses nothing ([questionLaterWord] is the one
	// place that reading is made) — so the slot says `esc cancel` or `esc keep
	// going`. A hint naming a key that does something else is the one failure
	// this slot exists to prevent (room.go's [app.roomHint] states it).
	return strings.Join(append(parts, questionLaterKey+" "+questionLaterWord(head.question)), " · ")
}

// questionAnimating reports whether a clock is running down, which is what
// keeps the paint clock turning while a question waits (app.go's [app.paint]).
func (a *app) questionAnimating() bool {
	if len(a.questions) == 0 {
		return false
	}
	head, ok := a.questionHead()
	if !ok {
		return false
	}
	if a.questionReading(head) {
		return true
	}
	// A HELD CLOCK IS NOT ANIMATING and neither is `waiting`: the tail says one
	// word and stops changing, and a frame clock left turning for it would be
	// this surface repainting a still screen forever.
	return head.question.Policy.Kind == session.PolicyRecommendThenAuto &&
		a.questionClockWord(head) != ""
}

// ── the receipt and the withdrawn line ──────────────────────────────────────

// questionRecordsShown is the receipts still worth a row.
//
// THEY ARE BOUNDED AND THEY FADE. A record is a statement about a decision
// somebody has just taken, and it belongs above the box for as long as it is
// news — after that it is history, and history lives in the transcript and in
// `decisions.jsonl` rather than in the chrome. So the block keeps the last
// [questionRecordsKept] and drops each after [questionRecordFor].
func (a *app) questionRecordsShown() []questionRecord {
	if len(a.questionRecords) == 0 {
		return nil
	}
	out := make([]questionRecord, 0, questionRecordsKept)
	for _, record := range a.questionRecords {
		if a.now().Sub(record.at) > questionRecordFor {
			continue
		}
		out = append(out, record)
	}
	if len(out) > questionRecordsKept {
		out = out[len(out)-questionRecordsKept:]
	}
	return out
}

const (
	// questionRecordsKept is how many receipts stand above the box at once. Two
	// is a person answering a queue and seeing what they just did; a column of
	// them is a block that has become a log.
	questionRecordsKept = 2
	// questionRecordFor is how long one stays. Half a minute is long enough to
	// read a line somebody caused and short enough that it is gone before it
	// becomes furniture.
	questionRecordFor = 30 * time.Second
)

// questionRecordRow draws one: the receipt, or the withdrawn line.
func (a *app) questionRecordRow(record questionRecord, width int) string {
	if record.withdrawn != "" {
		// WITHDRAWN, WITH A REASON, and never the word "cancelled": what a
		// person experiences is the thing no longer needing them.
		text := "  " + a.icon(tokens.GWithdrawn) + " " + record.head +
			" — no longer needed · " + record.withdrawn
		return a.pal.dim(fit(text, width))
	}
	return a.pal.dim(fit(a.questionReceiptLine(record, width), width))
}

// questionReceiptLine composes the receipt at the width it has, giving up whole
// clauses rather than letting the row be cut from the right.
//
// IT IS THE ANSWERS ROW'S BARGAIN, SAID ABOUT THE LINE UNDERNEATH IT. That row
// drops its least valuable verb until what is left fits ([questionDropVerb]) and
// never cuts an answer in half; this one had no arrangement at all, so at a
// hundred columns a long `with:` clause carried `· you · 14:02 · c change` off
// the end with it and the receipt stopped saying who answered, when, or that it
// could still be changed. The rank is the record's own
// ([session.DecisionRecord.LineClauses]); what belongs here is the ONE clause
// that is the surface's — `c change` names a key on this keyboard, so it is
// never given up and never counted against the record's own words.
func (a *app) questionReceiptLine(record questionRecord, width int) string {
	// THE RECEIPT OPENS WITH THE SETTLED MARK AND NOT THE WORD `decided` (owner
	// ruling 2026-09-11, after-you-answer pick A): `✓ <head> → <answer> · you ·
	// 14:02`. The mark is the vocabulary's ([tokens.GSettled], the same one the
	// ratify line wears) and it says what the word said in one cell, which is
	// what leaves room for the answer's own words on a narrow row.
	lead := "  " + a.icon(tokens.GSettled) + " "
	// WHAT IS OFFERED IS WHAT THE KEY WILL DO, asked of the same two readings the
	// keys ask (questionchange.go): `c change` puts the question back, and
	// `u undo` hands back a permission that granted something standing. A
	// reversible decision this door would refuse says nothing at all, which is
	// what `cannot change` was for.
	//
	// AND THEY ARE OFFERED BECAUSE THE DOORS EXIST (#954). This row carried
	// `c change` with nothing behind it for a while and the key was taken off
	// rather than left lying; it is back in the same change as the door, which is
	// the bargain that was written down at the time.
	change := ""
	if questionCanUndo(record) {
		change += session.DecisionSep + questionUndoKey + " undo"
	}
	if questionCanChange(record) {
		change += session.DecisionSep + questionNoteKey + " change"
	}
	// AND THE TAIL TURNS UNTIL THE MODEL SAYS SOMETHING, which is the other half
	// of what a person wants from this row: the decision is taken, and the work
	// it was holding up has started again.
	change += a.questionReceiptTail(record)
	clauses := record.record.LineClauses()
	for {
		text := lead + questionJoinClauses(clauses) + change
		if ansi.StringWidth(text) <= width {
			return text
		}
		dropped, ok := questionDropClause(clauses)
		if !ok {
			break
		}
		clauses = dropped
	}
	// WHAT IS LEFT IS RESERVED, AND THE SENTENCE IS CUT AROUND IT. This is
	// place_home.go's law about the address row said about the receipt: `cannot
	// change` and `c change` are not FACTS about the decision that a narrow row
	// may spend, they are what is still possible about it, so a head too long for
	// the row gives way instead of silencing them. Somebody who reads a cut
	// sentence can open the question again; somebody who reads a receipt with no
	// limit on it believes a decision can be walked back.
	keep := change
	if len(clauses) > 1 {
		keep = session.DecisionSep + questionJoinClauses(clauses[1:]) + change
	}
	head := ""
	if len(clauses) > 0 {
		head = clauses[0].Text
	}
	return lead + fit(head, max(0, width-ansi.StringWidth(lead)-ansi.StringWidth(keep))) + keep
}

// questionReceiptTail is what the receipt says after the decision's own words:
// that the turn is moving again.
//
// THE WORK RESUMES ON THE ANSWER AND THE ROW SAYS SO UNTIL THE MODEL'S FIRST
// OUTPUT (owner ruling 2026-09-11). A block that vanished into a still line left
// a person watching an unmoving screen wondering whether their key had landed;
// the tail is that key's receipt, in the one moving glyph this surface spends
// ([tokens.Spinner], on the house grid so it never beats against another).
// It is derived and never a timer: the moment there is anything to read the
// conversation is drawing it, and this stops.
func (a *app) questionReceiptTail(record questionRecord) string {
	if record.withdrawn != "" || a.state != stateWorking || !a.questionQuietSince(record) {
		return ""
	}
	mark := tokens.Spinner(a.paints / spinnerStep)
	if a.linear || a.pal.ascii {
		mark = glyphRunASCII
	}
	return session.DecisionSep + mark + " " + questionResumedWord
}

// questionResumedWord is what that tail says. It is the state in a person's
// words — the same word the status line uses for the same fact — rather than
// machinery describing itself.
const questionResumedWord = "working"

// questionQuietSince reports whether nothing has been written into the
// conversation since a moment: the receipt's tail turns until the first thing
// the model says lands under it.
func (a *app) questionQuietSince(record questionRecord) bool {
	return record.entries > 0 && len(a.entries) <= record.entries
}

// questionJoinClauses is the clauses that are left, as one line.
func questionJoinClauses(clauses []session.DecisionClause) string {
	parts := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		parts = append(parts, clause.Text)
	}
	return strings.Join(parts, session.DecisionSep)
}

// questionDropClause gives up the least valuable clause still on the line, and
// reports whether it found one. It is [questionDropVerb] over the record's own
// ranks rather than the key table's, because the two rows are one arrangement
// applied to two different lists.
func questionDropClause(clauses []session.DecisionClause) ([]session.DecisionClause, bool) {
	at := -1
	for i := range clauses {
		if clauses[i].GiveUp == 0 {
			continue
		}
		if at < 0 || clauses[i].GiveUp > clauses[at].GiveUp {
			at = i
		}
	}
	if at < 0 {
		return clauses, false
	}
	return append(append([]session.DecisionClause{}, clauses[:at]...), clauses[at+1:]...), true
}

// ── answering ───────────────────────────────────────────────────────────────

// answerQuestion sends one answer through the one door, and leaves the receipt.
//
// THE ONE DOOR IS THE ENGINE'S ([session.Agent.ResolveQuestion]) for every
// question the engine raised, and the question's own closure for the ones this
// surface raised about itself. There is no third path and no place that knows
// what "yes" means twice.
//
// A REFUSED ANSWER LEAVES THE QUESTION OPEN, AND SAYS SO. The engine's door
// returns an error for an answer that names nothing and for a lane it does not
// take; neither is a decision, so neither closes anything and neither writes a
// receipt. Swallowing that sentence was the other half of the measured defect:
// the engine applied the key, this window was told the connection was gone,
// and nothing on screen said so.
func (a *app) answerQuestion(q questionShown, answer session.Answer) tea.Cmd {
	return a.answerQuestions([]questionAnswer{{q: q, answer: answer}})
}

// questionAnswer is one question and the answer it is being given.
type questionAnswer struct {
	q      questionShown
	answer session.Answer
}

// answerQuestions sends SEVERAL answers through the one door in ONE command,
// and is the road [app.answerQuestion] takes for its single one — there is no
// second answering path.
//
// IT TAKES A LIST BECAUSE ONE FRAME CAN HOLD SEVERAL QUESTIONS. A step that
// asks for three permissions at once is drawn as one thing to decide (the
// grouped frame, lane P), and the person answering it is making one gesture; a
// loop of single answers would be as many round trips as there are rows, each
// one a separate command, with the refusals arriving over several frames. Here
// every row settles on the keystroke, the doors are asked in order on ONE
// goroutine off the loop, and whatever was refused comes back on one pass with
// its own sentence against its own row.
//
// THE ONE DOOR IS THE ENGINE'S ([session.Agent.ResolveQuestion]) for every
// question the engine raised, and the question's own closure for the ones this
// surface raised about itself. There is no third path and no place that knows
// what "yes" means twice.
//
// A REFUSED ANSWER LEAVES THE QUESTION OPEN, AND SAYS SO. The engine's door
// returns an error for an answer that names nothing and for a lane it does not
// take; neither is a decision, so neither closes anything and neither writes a
// receipt. Swallowing that sentence was the other half of the measured defect:
// the engine applied the key, this window was told the connection was gone,
// and nothing on screen said so.
//
// AND WHILE A KEY ON A SET IS BEING ROUTED, WHAT IT WOULD SEND IS HELD INSTEAD
// ([app.stageAnswers]) — the one place a set's review differs from a panel of
// one, said at the one door every key's answer reaches.
func (a *app) answerQuestions(all []questionAnswer) tea.Cmd {
	return a.sendAnswers(a.stageAnswers(all))
}

// sendAnswers is [app.answerQuestions] with nothing held back: the list goes
// through the door now, in order, in one command. It is what a set's review
// and its permission frame send with.
func (a *app) sendAnswers(all []questionAnswer) tea.Cmd {
	if len(all) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	var sending []questionAnswer
	var door questionResolver
	for _, one := range all {
		q := one.q
		answer := a.dressAnswer(q, one.answer)
		if q.answered != nil && !answer.Clarify {
			// THE LANE'S OWN HAND, BEFORE THE DOOR. Whatever this PROGRAM does
			// about an answer happens here — a rule written into the person's
			// settings, the transcript row annotated with what was decided — and
			// what comes back is the answer the engine is actually told, which is
			// how a permission the surface has already written down stops a wider
			// one being written beside it ([questionShown.answered]).
			answer = q.answered(answer)
		}
		if q.local != nil {
			cmds = append(cmds, q.local(answer))
		} else {
			if door == nil {
				resolver, ok := a.agent.(questionResolver)
				if !ok || resolver == nil {
					// NOTHING IS SETTLED BY A WINDOW WITH NO DOOR. The row stays
					// where it is rather than closing over an answer that reached
					// nobody.
					continue
				}
				door = resolver
			}
			// THE SENDING IS REMEMBERED BEFORE THE DOOR IS ASKED, AND THAT ORDER
			// IS THE WHOLE POINT. The engine runs in its own process even on this
			// machine, so this call crosses a wire: it applies the answer, emits
			// [session.EventQuestionAnswered], and only then writes its reply
			// back. The news of that settling comes down the questions lane
			// looking exactly like somebody else's answer, and this stamp is what
			// tells the two apart ([app.markQuestionSent]).
			a.markQuestionSent(q.token(), answer.Keys())
			sending = append(sending, questionAnswer{q: q, answer: answer})
		}
		if !session.AnswerResolves(answer) {
			// AN ANSWER THAT DID NOT END THE QUESTION LEAVES IT ON THE BLOCK.
			// Two answers on this surface send WORDS rather than settle anything
			// — `tell it` steers a landed task, `change it` hands a finished
			// design back to its designer — and the engine goes on holding the
			// lane for both ([session.AnswerResolves] is the one reading). A
			// block that cleared its rows and wrote a receipt here would tell a
			// person a decision had been made while the thing that has to decide
			// it went on waiting for them.
			a.touch()
			continue
		}
		a.closeQuestion(q, answer)
	}
	if len(sending) > 0 {
		// AND THE DOOR IS ASKED FROM A COMMAND, NEVER FROM HERE (offloop.go).
		// This runs inside Update, and a wire call made here is a window that
		// cannot draw, cannot take a key, and cannot read the news its own
		// answer caused — which deadlocked the reader against this loop and cost
		// a person ten seconds per keystroke (doorbell.go has the measurement).
		// The screen is settled above on this pass; the engine is told on the
		// next goroutine; a refusal comes back and puts that question back.
		cmds = append(cmds, a.offLoop(func() func(bool) tea.Cmd {
			refused := make([]error, len(sending))
			for i, one := range sending {
				refused[i] = door.ResolveQuestion(one.answer)
			}
			return func(here bool) tea.Cmd {
				for i, err := range refused {
					if err == nil {
						continue
					}
					if !here {
						// THE SENTENCE KEEPS THE QUESTION COMPANY. A person who
						// switched conversation between the keystroke and the
						// refusal cannot be told here — this screen is somebody
						// else's conversation now, and saying it would be this
						// surface putting one conversation's news on another. So
						// it is kept against the token: the engine never took the
						// answer, so it re-delivers the question on the next
						// attach, and the reason arrives with it
						// ([app.raiseQuestion]). It used to be dropped on the
						// floor, which left a question that had come back with no
						// account of why.
						a.keepQuestionRefusal(sending[i].q.token(), err)
						continue
					}
					// A REFUSED ANSWER LEAVES ITS OWN QUESTION OPEN, AND SAYS SO,
					// in the door's own words ([app.reopenQuestion]). One refusal
					// among several says nothing about the rest: the answers that
					// were taken stay taken.
					a.reopenQuestion(sending[i].q, sending[i].answer, err)
				}
				return nil
			}
		}))
	}
	return tea.Batch(cmds...)
}

// dressAnswer puts on an answer everything the QUESTION knows and the key that
// sent it does not: which question it is about, when, who by, and whatever the
// shape's own holes were filled with.
func (a *app) dressAnswer(q questionShown, answer session.Answer) session.Answer {
	answer.Kind = q.question.Kind
	answer.ID = q.question.ID
	answer.Ref = q.question.Ref
	answer.Ask = q.question.Ask
	if answer.At.IsZero() {
		answer.At = a.now()
	}
	if answer.DecidedBy == "" {
		answer.DecidedBy = session.DecidedByPerson
	}
	// AND A DECISION BEING MADE AGAIN SAYS SO. Without this bit the engine reads
	// a second answer to a settled question as a late one and drops it in
	// silence, which is right for another window's key a moment too slow and
	// wrong for somebody who has just read the receipt (questionchange.go).
	answer.Revises = q.revising
	// WHAT IS IN THE HOLES TRAVELS WITH THE ANSWER, and it travels whichever key
	// gave it: a proposal approved with `1`, with `enter`, or with a click on the
	// row all start the work on the model the card was showing. The filling is
	// the shape's own ([questionInput.fill]), so the map's keys are the asker's
	// labels rather than anything this file chose.
	if q.holes.kind != session.InputNone {
		q.holes.fill(&answer)
	}
	return answer
}

// closeQuestion takes an answered question off the block, writes its receipt,
// and counts the yes towards the rule offer.
//
// THE RECEIPT IS BUILT HERE AND NOT WAITED FOR. The engine emits
// [session.EventQuestionAnswered] with the record on it, and that event is what
// a SECOND window learns from; this window already knows, and a person who
// pressed a key and watched the row sit unchanged for a round trip would press
// it again.
func (a *app) closeQuestion(q questionShown, answer session.Answer) {
	token := q.token()
	// AND THE SENT STAMP GOES WITH IT. It is only ever about a question still
	// open here with an answer of this window's unaccounted for, and this is
	// where both of those stop being true ([app.markQuestionSent]).
	delete(a.questionSent, token)
	// AND HOME'S OWN CARD GOES WITH IT (homeconfirm.go). A question answered is a
	// question gone from wherever it was drawn, and home is the one place that
	// holds one outside the queue below.
	if a.home.ask != nil && a.home.ask.token() == token {
		a.home.ask = nil
	}
	for _, ex := range a.exchanges {
		// AND THE ERRAND PANE'S (homeexchange.go). It is a third place a question
		// is held, and a lookup that forgot it would leave a settled card still
		// answering to digits.
		if ex != nil && ex.ask != nil && ex.ask.token() == token {
			ex.ask = nil
		}
	}
	kept := a.questions[:0]
	for _, open := range a.questions {
		if open.token() == token {
			continue
		}
		kept = append(kept, open)
	}
	a.questions = kept
	delete(a.questionFolded, token)
	// AND THE PAGE OVER IT GOES TOO (questionroom.go). A question settled
	// anywhere — here, in another window, by the dial — leaves a full page that
	// nothing closes, and that page goes on eating ↑↓ and enter for a decision
	// that has already been made. A page is a way of READING one question; when
	// there is no question there is no page.
	a.closeQuestionPage(token)
	a.recordQuestion(q, answer)
	a.countQuestionYes(q, answer)
	a.touch()
}

// reopenQuestion puts a question back after the engine refused the answer this
// window had already drawn as taken.
//
// A REFUSED ANSWER LEAVES THE QUESTION OPEN, AND SAYS SO. The engine's door
// refuses an answer that names nothing and a lane it does not take; neither is
// a decision, so neither may leave a receipt or a settled row behind it. The
// screen was settled on the keystroke (offloop.go says why), so putting it back
// is three undoings and the door's own sentence:
//
//   - THE RECEIPT GOES, because it is an account of a decision that was not
//     made, and a person reading the row above their box has no other way to
//     know that.
//   - THE YES IS UNCOUNTED, because the rule offer counts permissions a person
//     actually granted, and an offer to make a rule out of a refused answer
//     would be a rule nobody agreed to.
//   - THE SENT STAMP IS KEPT, because a refusal and a deadline reach here by the
//     same road: if the engine did apply it after all, the news that comes down
//     the questions lane a moment later must still close this as YOURS rather
//     than as another window's ([app.markQuestionSent]).
//
// AND IT COMES BACK WITH A FRESH SETTLE GUARD. It went off the screen on a
// keystroke and is arriving again unannounced, which is the exact case the guard
// is for ([questionSettle]): the next key belongs to the person's sentence, not
// to a question that reappeared under their hands.
func (a *app) reopenQuestion(q questionShown, answer session.Answer, err error) {
	// THE WORDS ARE THE ENGINE'S. [app.note] is the same door every other
	// refused act on this surface uses — rewind, autonomy, a connect, a
	// permission — so a question does not grow a second composer.
	a.note(strings.TrimSpace(err.Error()))
	if !session.AnswerResolves(answer) {
		// AN ANSWER THAT NEVER CLOSED ANYTHING HAS NOTHING TO PUT BACK, and
		// putting one back would take a LATER answer with it. `tell it` and
		// `change it` send words and leave the question standing
		// ([session.AnswerResolves]); a person can then answer it outright on
		// the next keystroke, and a refusal of the WORDS arriving after that
		// used to delete the real answer's receipt and re-raise a question that
		// had been settled. The sentence above is still said — the words were
		// refused, and that is the person's to read — and nothing is re-raised.
		return
	}
	if a.questionSettledElsewhere(q.token()) {
		// AND THE LANE OUTRANKS A REFUSAL THAT ARRIVED AFTER IT. A door that
		// deadlined after the engine had already applied the answer refuses this
		// window while the questions lane is carrying that very decision; the
		// engine's word is the one that is true ([app.foldOthersAnswer]).
		return
	}
	if a.questionIsOpen(q.token()) {
		// Still on the block: the door refused something this window had not
		// closed, so there is nothing to put back.
		return
	}
	a.forgetQuestionRecord(q)
	a.noteQuestionYes(q, answer, -1)
	q.shown = time.Time{}
	a.raiseQuestion(q)
	a.markQuestionSent(q.token(), answer.Keys())
	a.touch()
}

// forgetQuestionRecord takes back the newest receipt this window wrote for one
// question.
func (a *app) forgetQuestionRecord(q questionShown) {
	for i := len(a.questionRecords) - 1; i >= 0; i-- {
		record := a.questionRecords[i].record
		if record.Kind == q.question.Kind && record.ID == q.question.ID && record.Ref == q.question.Ref {
			a.questionRecords = append(a.questionRecords[:i], a.questionRecords[i+1:]...)
			return
		}
	}
}

// recordQuestion writes the receipt from the question and the answer together,
// exactly as the engine writes its own record from the same two things
// (internal/session's decisionRecordOf) — so the line above the box and the
// line in `decisions.jsonl` are the same sentence.
func (a *app) recordQuestion(q questionShown, answer session.Answer) {
	if questionRaisedHere(q.question) {
		// A CARD YOU RAISED YOURSELF LEAVES NO RECEIPT, because the act IS the
		// receipt. A receipt exists for a decision that happened out of sight —
		// the model asked, you answered, and the row above the box is what you
		// can point at afterwards. There is nothing out of sight here: the tab
		// is gone, or the work stopped and the engine's own sentence went into
		// the conversation ([app.stopSay]), or nothing happened at all. `decided
		// Close this tab? … → cancel` is a row about a question a person took
		// back, and it says a decision was made where none was.
		return
	}
	labels := questionLabels(q.question, answer.Keys())
	was := []string(nil)
	if prior, changed := a.priorAnswers[q.token()]; changed && answer.Revises {
		// A CHANGED DECISION SAYS WHAT IT REPLACED, here for the reason the
		// engine's own record does it ([session.DecisionRecord.Was]): two lines
		// about one question, with no clause between them, read as two questions.
		was = prior.Labels
		delete(a.priorAnswers, q.token())
	}
	record := session.DecisionRecord{
		Was: was,
		ID:  q.question.ID, Ref: q.question.Ref,
		Kind: q.question.Kind, Ask: q.question.Ask,
		Head: strings.TrimSpace(q.question.Head), Subject: q.question.Subject,
		Picked: answer.Keys(), Labels: labels, Change: answer.Words(),
		By: answer.DecidedBy, Stakes: q.question.Stakes, Scope: answer.Scope,
		At: answer.At,
	}
	a.questionRecords = append(a.questionRecords, questionRecord{
		record: record, head: record.Head, at: answer.At, reversible: record.Reversible(),
		question: q.question,
		entries:  len(a.entries),
	})
}

// countQuestionYes counts one answer towards the third same-shaped yes that
// offers a rule. Only an answer that GRANTED something counts: a person who
// says no three times has not formed a habit worth writing down, they have
// answered three questions.
func (a *app) countQuestionYes(q questionShown, answer session.Answer) {
	a.noteQuestionYes(q, answer, 1)
}

// noteQuestionYes is that count in both directions: one more when a permission
// is granted, one fewer when the engine refused the answer that granted it
// ([app.reopenQuestion]). ONE FUNCTION FOR BOTH so the two readings of "is this
// a yes worth counting" cannot drift apart.
func (a *app) noteQuestionYes(q questionShown, answer session.Answer, by int) {
	if q.question.Ask != session.AskPermission {
		return
	}
	option, ok := q.question.Option(answer.FirstKey())
	if !ok || option.Safe {
		return
	}
	if a.questionYeses == nil {
		a.questionYeses = map[string]int{}
	}
	shape := questionShape(q.question)
	if a.questionYeses[shape] += by; a.questionYeses[shape] <= 0 {
		delete(a.questionYeses, shape)
	}
}

// foldQuestion is `esc`: LATER, and nothing is cancelled.
//
// The question stays open, the turn or the task stays paused on it, the chip
// keeps counting it, and the rows come off the screen so the person can type.
// It is the whole of what makes this block not modal.
func (a *app) foldQuestion(q questionShown) {
	if a.questionFolded == nil {
		a.questionFolded = map[string]bool{}
	}
	a.questionFolded[q.token()] = true
	a.touch()
}

// questionPutOff is the question a fold rule is standing for: the NEWEST one
// put off, which is the one `space` and the chip both open ([app.raiseFolded]
// says why the newest).
func (a *app) questionPutOff() (questionShown, bool) {
	at := -1
	for i, q := range a.questions {
		if !a.questionFolded[q.token()] {
			continue
		}
		if at < 0 || q.question.Asked.After(a.questions[at].question.Asked) {
			at = i
		}
	}
	if at < 0 {
		return questionShown{}, false
	}
	return a.questions[at], true
}

// questionUnfoldKey is `space` over an empty box: the folded question comes back.
//
// THE RULE PRINTS THE KEY AND NOTHING ROUTED IT. `── ? which store · 3 answers ·
// ◆ SQLite ──── space open ──` is what a folded question leaves behind, and
// `internal/manual/chat/questions.md` says the same thing in a person's words —
// and pressing space over the empty box did nothing at all. `alt+y` reopened it,
// so the way out existed; what was missing was the one the screen offered.
//
// THREE GUARDS, AND EACH IS A WAY THIS KEY COULD BE WRONG:
//
//   - A SPACE INSIDE A SENTENCE IS A SPACE. Over a box with words in it the key
//     belongs to the composer, which is the law every printable key on this
//     surface is held to.
//   - ONLY WHERE THE RULE IS ON THE SCREEN. A place, a page or the start screen
//     has the frame and the rule is not drawn under it; a key that acts on
//     something nobody can see is the defect the block's own law names.
//   - AND ONLY WHERE SOMETHING IS ACTUALLY FOLDED, so a space over an empty
//     conversation stays a space.
func (a *app) questionUnfoldKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if msg.String() != questionToggleKey {
		return nil, false
	}
	if strings.TrimSpace(a.input.String()) != "" {
		return nil, false
	}
	if a.questionOffFrame() || a.startingChat() {
		return nil, false
	}
	if _, folded := a.questionPutOff(); !folded {
		return nil, false
	}
	a.raiseFolded()
	return nil, true
}

// raiseFolded is the chip's own key: it unfolds the NEWEST open question and
// puts it back above the box.
//
// THE NEWEST AND NOT THE OLDEST, which is the one place this surface answers a
// queue backwards, and it is the chip's own argument: a person pressing the
// chip has just seen a count change, and what they are asking about is the
// thing that changed it. Answering the head of the queue instead would be the
// surface deciding they meant something else.
func (a *app) raiseFolded() {
	if len(a.questions) == 0 {
		return
	}
	newest, at := time.Time{}, -1
	for i, q := range a.questions {
		if !a.questionFolded[q.token()] {
			continue
		}
		if at < 0 || q.question.Asked.After(newest) {
			newest, at = q.question.Asked, i
		}
	}
	if at >= 0 {
		// A SET COMES BACK WHOLE. `esc` folded every tab of it at once, so
		// opening it again opens every tab, with every answer held still held.
		for _, q := range a.questionSetFolded(a.questions[at]) {
			delete(a.questionFolded, q.token())
		}
		delete(a.questionFolded, a.questions[at].token())
	}
	a.touch()
}

// ── the keyboard ────────────────────────────────────────────────────────────
//
// THE BLOCK TAKES ONLY THE KEYS IT DRAWS, and that sentence is the whole
// difference between this and the block it replaces. consent.go owned every
// keystroke while it was up and said why: with no way out but an answer, a key
// that fell through would be typing into a conversation that could not move.
// esc is `later` now, so there is a way out, so there is no reason to hold the
// keyboard — and holding it was costing the one thing the ladder's last rung
// needs, which is somewhere to type the answer that was not on offer.
//
// A LETTER IS THE QUESTION'S OVER AN EMPTY BOX AND NOWHERE ELSE. That is the
// rule every key on this surface that is also a letter is held to (task.go's
// [app.taskKey], room.go, stop.go), and it is what makes `d`, `r` and `u` safe
// to put on a row: the moment there are words in the box, every printable key
// belongs to the composer and the only key still the question's is `esc`.
//
// AND THE WORDS IN THE BOX ARE THE ANSWER, on a question the turn is waiting
// on. `enter` under a blocking question sends what was typed as the answer's
// own words rather than as a message into a conversation that cannot carry it
// — which is what the box under a task proposal has always been for (task.go
// calls it the redirect lane) said once, for every lane.

// questionKey routes one keypress while the block is up, and reports whether it
// took it.
func (a *app) questionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	head, ok := a.questionHead()
	if !ok {
		// AND WHERE THERE IS NO QUESTION THERE MAY STILL BE A RECEIPT, which
		// offers `c change` and `u undo` in so many words (questionchange.go).
		if cmd, taken := a.questionReceiptKey(msg); taken {
			return cmd, true
		}
		// WHAT IS DRAWN IS WHAT TAKES THE KEY. With nothing on the block, what
		// is drawn is the fold rule, which prints the key that opens it
		// ([questionOpenFoldWord]) — and the same off-frame guard stands in
		// front of it, because a rule behind a place or a page is as invisible
		// as a block behind one.
		if a.questionOffFrame() {
			return nil, false
		}
		return a.questionUnfoldKey(msg)
	}
	// THE START PAGE TAKES EVERY KEY BEFORE THE CONVERSATION BEHIND IT. Unlike
	// the other whole-frame places below, it has a composer of its own, so a
	// digit or escape answered here would resolve a question nobody can see with
	// a key aimed at somebody's first message.
	if a.startingChat() {
		return nil, false
	}
	if a.questionOffFrame() && !questionRaisedHere(head.question) {
		// AND A BLOCK THAT IS NOT ON THE FRAME TAKES NO KEYS EITHER, which is
		// the same law about a different way of being invisible. The stamp below
		// is written by the draw and never cleared, so a question drawn once in
		// the conversation and then covered by a place, a job's page or the
		// rewind timeline would still have been answering digits from behind it
		// — and on home, where every printable key belongs to the box a
		// conversation starts in, that is somebody's first sentence resolving a
		// design they cannot see.
		return nil, false
	}
	if head.shown.IsZero() && !questionRaisedHere(head.question) {
		// A QUESTION THAT HAS NEVER BEEN DRAWN TAKES NO KEYS. The stamp is
		// written by the DRAW ([app.markQuestionShown]), so a zero one means one
		// of the two things that keep the block off the screen: the box has a
		// half-typed sentence in it and the rows have not been taken out from
		// under it ([app.questionQuieted]), or a fullscreen page is up and the
		// chrome is not being drawn at all. Either way the person is looking at
		// something else, and a block that answered for them from behind it —
		// or swallowed the letters they were typing into what they CAN see —
		// would be modal in the one place nobody could tell.
		return nil, false
	}
	// WHAT IS DRAWN IS WHAT TAKES THE KEY, and several questions from one step
	// are drawn as one panel with keys of its own (questionset.go).
	if set := a.questionSet(); set != nil {
		return a.questionSetKey(set, head, msg)
	}
	return a.questionKeyOn(head, msg)
}

// questionOffFrame reports whether a surface that takes the frame WHOLE is up,
// so that none of the block is on the screen.
//
// IT IS THE SAME LIST view.go's [app.frameBody] WALKS, in the same order, and it
// is a list rather than one flag because each of these surfaces is opened by its
// own door. A room is deliberately not on it: a room keeps the chrome under it,
// which is exactly why the block is the only place a design's page is answered
// from now (harnesscard.go).
//
// THE START PAGE IS ON IT, AND IT IS #677 (sev:critical). A shell command was
// waiting for approval; `ctrl+t` opened the new-chat page and `hello there`
// typed into its box left `here` in it — the `t` had granted the tool for the
// whole session and `sleep 300` ran. `esc`, which that page's own legend offers
// as "keeps the chat you were in", denied a task proposal behind it instead.
// The page holds a half-written first message, so a question arriving must not
// take it down (that is the half home does not have); the other half of home's
// rule is exactly this one — A QUESTION IS ANSWERED WHERE IT IS DRAWN AND
// NOWHERE ELSE. It is not cancelled, the tab still wears `?`, and every key
// comes back with the conversation the moment the page is closed.
func (a *app) questionOffFrame() bool {
	return a.startingChat() || a.pasteEdit.open || a.setup.open || a.showing() != nil ||
		a.jobPageOpen() || a.rewSheet.open || a.deck.open || a.expandShowing()
}

// questionKeyOn is the routing itself, against a question this caller has
// already found.
//
// IT IS SPLIT FROM [app.questionKey] BECAUSE THE BLOCK IS NOT THE ONLY PLACE A
// QUESTION IS DRAWN. Home takes the frame whole and draws its own copy of a
// confirmation it raised about a row on its list (homeconfirm.go); that card and
// this block have to answer to the same keys, in the same order, with the same
// law about which of them takes the cursor and which of them takes the answer —
// and two routers for one grammar is the defect the one key table exists to
// prevent. What is above this line is the BLOCK's own two guards (which question
// is the head, and whether it has been drawn), which home answers for itself.
func (a *app) questionKeyOn(head questionShown, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	cmd, took := a.questionKeyTaken(head, msg)
	// THE HAND IS GIVEN BY A KEY THE BLOCK ACTUALLY TOOK, and by nothing else.
	//
	// IT WAS GIVEN BY THE KEY ARRIVING, which is not the same thing and was
	// wrong twice. `enter` over a half-typed sentence is the CONVERSATION's —
	// it sends the message and this routing hands it back — so aiming on the
	// way past handed the block the keyboard for a key it never took: "looks
	// good", enter, then "do the schema first" and the `d` handed the call back
	// to the asker. And a key the settle guard drops was aimed at whatever was
	// on screen before this question, so it says nothing about this one.
	if took && questionAimKey(msg.String()) && a.questionSettled(head) {
		a.aimQuestion(head.token())
	}
	return cmd, took
}

// questionKeyTaken is [app.questionKeyOn]'s routing: what the block does with
// one key, and whether it took it.
func (a *app) questionKeyTaken(head questionShown, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if key == "ctrl+c" {
		// Leaving is never modal, and mid-turn ctrl+c is the interrupt, which
		// releases whatever the question was holding the honest way.
		return nil, false
	}
	// EVERY KEY STOPS THE READING CLOCK, whether or not it answers anything and
	// whether or not the question is old enough to take it. That is the whole of
	// the hold: the clock exists so an unattended session cannot park work on a
	// question forever, and the moment there is evidence of somebody at the
	// keyboard the reason for it is gone.
	a.holdQuestionClocks()
	// THE SETTLE GUARD, AND IT DROPS RATHER THAN DEFERS. A key that arrived
	// before the question had been on screen long enough was aimed at whatever
	// was there before it, and applying it late is applying it to the wrong
	// question rather than to none.
	if !a.questionSettled(head) {
		return nil, true
	}
	if cmd, taken := a.questionBeatKey(head, key); taken {
		return cmd, true
	}
	if open := a.questionHeld(head.token()); open != nil && open.writing != "" {
		return a.questionWritingKey(head, key)
	}
	if key == questionLaterKey {
		if head.question.Ask == session.AskConfirmation {
			// esc ON A CONFIRMATION IS THE ANSWER THAT LOSES NOTHING, and never
			// *later*. Everywhere else on this block esc puts a question off:
			// the work stays waiting, the chip keeps counting it, and nothing is
			// decided. A confirmation is the one shape where there is nothing to
			// come back to — it was raised by the person's own gesture, and a
			// gesture put off is a gesture not made — so folding it would leave a
			// chip counting a question nobody asked for. It answers safely
			// instead, which is stop.go's own law kept whole: `esc` is *keep
			// going* and never the act, and a dismiss key that also ended work
			// would be the one key nobody could press safely.
			return a.questionPick(head, questionLaterAt(head.question)), true
		}
		a.foldQuestion(head)
		return nil, true
	}
	// THE `something else…` ROW IS A BOX WHILE THE POINTER IS ON IT, and it
	// takes the keys a box takes — over an EMPTY message box, which is the law
	// every letter on this surface is held to. With words in the box below, the
	// letters are the composer's and the row is still drawn, still pressable,
	// still answerable the moment the box is clear.
	if a.questionOthering(head) && strings.TrimSpace(a.input.String()) == "" {
		if cmd, taken := a.questionOtherKey(head, msg); taken {
			return cmd, true
		}
	}
	// A HALF-TYPED SENTENCE DOES NOT OUTRANK A CARD THE PERSON RAISED. Every
	// question the engine asks leaves the box alone — the letters are theirs, the
	// question waits, and `enter` sends the sentence rather than answering
	// anything. A confirmation raised by a gesture is the one shape that cannot
	// work that way: it was raised OVER the box on purpose, both cards it belongs
	// to have always taken the whole keyboard while they were up (stop.go,
	// tabclose.go), and `enter` landing in the conversation would send a message
	// to an agent the card is offering to stop. The draft is untouched and
	// unsendable until the card is answered, which is where it was and where it
	// stays.
	typing := strings.TrimSpace(a.input.String()) != "" && !questionRaisedHere(head.question)
	if key == questionEnterKey {
		return a.questionEnter(head, typing)
	}
	if typing {
		// EVERY PRINTABLE KEY BELONGS TO THE COMPOSER. The question is still
		// there, still counted, still answerable the moment the box is clear.
		return nil, false
	}
	if cmd, taken := a.questionTickKey(head, key); taken {
		return cmd, true
	}
	if cmd, taken := a.questionOptionKey(head, key); taken {
		return cmd, true
	}
	// The two advertised text doors work before navigation. Other letter
	// commands still require intent, so an ordinary sentence cannot decide work.
	if questionTextKey(key) && key != questionCommentKey && key != questionAskBackKey && !a.questionHasTheHand(head.question) {
		// AND A VERB IS THE BOX'S UNTIL SOMEBODY AIMS AT THE BLOCK. The `d` at
		// the head of "do the schema first" handed the call back to the asker and
		// left the rest of the sentence in the box. Every key above this line
		// NAMES an answer drawn on the row and is untouched by it; every key
		// below it is a letter a sentence starts with (questionkeys.go's THE BOX
		// KEEPS THE FIRST LETTER).
		return nil, false
	}
	return a.questionVerbKey(head, key)
}

// questionEnter is `enter` under a question: send the words where there are
// some, and take the pick where there are not.
//
// `enter` TAKES THE PICK ONLY WHEN THERE IS ONE (DESIGN.md's key grammar, and
// the emptiness law behind it). A question with no pick draws no `enter →` line
// and this hands the key back, so enter on an empty box does whatever it always
// did.
func (a *app) questionEnter(head questionShown, typing bool) (tea.Cmd, bool) {
	if typing {
		if !questionOwnsBox(head.question) {
			// The conversation can carry this sentence, so it does. A question
			// with no claim on the box has none ([questionOwnsBox] says which
			// two shapes have one and why).
			return nil, false
		}
		words := strings.TrimSpace(a.input.String())
		a.input.reset()
		return a.answerQuestion(head, session.Answer{Change: words}), true
	}
	if head.question.Ask == session.AskConfirmation {
		return a.questionPick(head, head.pick), true
	}
	// A CHECKLIST'S ENTER SENDS WHAT IS TICKED, and nothing when nothing is
	// ([app.questionTickKey]); it never takes a pick a checklist does not have.
	if head.holes.kind == session.InputChecklist {
		return a.questionTickKey(head, questionEnterKey)
	}
	// A QUESTION WHOSE ANSWER IS A SENTENCE TAKES NOTHING FROM AN EMPTY BOX.
	// [session.InputText] is the asker stating that the answer IS words — a
	// connect key, a correction, a running sub-harness's own question — and its
	// options are the way OUT rather than something for a pointer to take. Enter
	// over an empty box on one of those used to send an empty key, which reads
	// as a decline; it does nothing, the question stands, and the way out is the
	// answer that says so ([questionOwnsBox] is the same fact from the other
	// side, and takes this key while there ARE words).
	//
	// AND THE WAY OUT IS NOT THE WHOLE QUESTION. A correction, a connect key or
	// a standing card is still a question WITH ANSWERS, and every question with
	// answers has a pointer the arrows walk ([questionPointerStart]) — a
	// standing card whose Enter did nothing while the pointer stood on
	// `1 keep this rule` was the owner, 2026-09-25 (#1506). So the give-up below
	// yields to a pointer: enter over an empty box takes the answer the pointer
	// is on, and only a question with nothing under the pointer — and no
	// asker's pick beside it — hands the key back untouched.
	if head.question.Input.Kind == session.InputText {
		// THE POINTER STANDS FOR A PICK ONLY WHERE A PICK EXISTS. A connect
		// question keeps exactly one answer, the way out, and that answer is
		// taken by a digit, never by enter — enter there means the words ([#1506
		// broke it wide]). A standing card is a real choice between answers, so
		// a pointer standing on one of them IS the pick enter takes.
		choice := head.question.Ask == session.AskChoice || head.question.Ask == session.AskJudgement
		under := choice && head.pick >= 0 && head.pick < len(head.question.Options)
		picked := head.question.Pick != nil && strings.TrimSpace(head.question.Pick.Key) != ""
		if !under && !picked {
			return nil, false
		}
	}
	// ENTER TAKES THE ANSWER THE POINTER IS ON, through the same door a digit
	// goes through, so a widening answer still gets its second beat and a
	// landing's `tell it` still opens its page. A question with no answers
	// written down has nothing for enter to take.
	if head.pick >= 0 && head.pick < len(head.question.Options) {
		return a.questionOptionKey(head, strings.TrimSpace(head.question.Options[head.pick].Key))
	}
	if head.question.Pick == nil || strings.TrimSpace(head.question.Pick.Key) == "" {
		return nil, false
	}
	return a.questionAnswerKey(head, strings.TrimSpace(head.question.Pick.Key)), true
}

// ── your own answer ─────────────────────────────────────────────────────────

// questionOthering reports whether the pointer is standing on the
// `something else…` row, which is what makes that row a box.
func (a *app) questionOthering(q questionShown) bool {
	return a.questionTakesOther(q) && q.pick == questionOtherAt(q.question)
}

// questionWalkCount is how many rows the pointer walks: every answer, and the
// `something else…` row where the question draws one.
func (a *app) questionWalkCount(q questionShown) int {
	if a.questionTakesOther(q) {
		return len(q.question.Options) + 1
	}
	return len(q.question.Options)
}

// questionOtherKey is every key while the pointer is on the `something else…`
// row: the row is a box, so it takes what a box takes.
//
// `enter` SENDS THE WORDS AS THE ANSWER, `↑` GOES BACK TO THE LIST, and `esc` is
// still *later* (it is read before this, with every other question's esc). A key
// this row does not know is handed back rather than swallowed — the block takes
// only the keys it draws, and a box is not a reason to stop being that.
//
// THE WORD JUMPS AND THE WORD KILL ARE THE SURFACE'S AND NOT THIS BOX'S
// (editkeys.go). This row is a box like every other on the program, so it reads
// the one vocabulary they all read rather than a hand-rolled copy of it — the
// copy this file carried bound `alt+←`/`alt+b` and the two ends of the line and
// nothing else, so ⌘←/⌘→ reached no part of the caret, ⌘⌫ killed no line and
// ⌥⌫/ctrl+⌫ killed no word in the one box a person writes an answer in. A box
// that answers to fewer names than the message box directly beneath it is the
// defect editkeys.go was written for, said about the last box it had not reached.
func (a *app) questionOtherKey(head questionShown, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	open := a.questionHeld(head.token())
	if open == nil {
		return nil, false
	}
	box := &open.other.words
	key := msg.String()
	// THE CARET JUMPS ANSWER TO EVERY NAME A TERMINAL SENDS THEM BY: `alt+←`,
	// `alt+b`, `ctrl+←`, `super+←`/`meta+←` (⌘←), `ctrl+a` for the start of the
	// line, and the same four forward for its end.
	if editorMotion(box, key) {
		a.touch()
		return nil, true
	}
	// AND SO DOES THE WORD KILL: ⌥⌫ (`alt+backspace`), ctrl+⌫ (`ctrl+backspace`).
	// `ctrl+w` is deliberately absent here as it is in the message box — it shuts
	// the tab in front, read far above this box (tabclosekey.go).
	if editorWordKill(box, key) {
		a.touch()
		return nil, true
	}
	switch key {
	case questionEnterKey:
		words := strings.TrimSpace(box.String())
		if words == "" {
			// AN EMPTY ANSWER IS NOT AN ANSWER. The key is taken rather than
			// falling through to a pick this row does not have: enter on an empty
			// box used to send a bare key, which reads as a decline.
			return nil, true
		}
		answer := session.Answer{Change: words}
		if with := strings.TrimSpace(open.other.with); with != "" && questionChangeCarriesThePointer(open.question) {
			// THE WORDS TRAVEL WITH THE ANSWER THE PERSON CAME FROM, which is what
			// `c` on a row means and what the row says while they type
			// ([questionOtherWith]).
			answer.Key, answer.Picked = with, []string{with}
		}
		box.reset()
		open.other.with = ""
		return a.answerQuestion(*open, answer), true
	case "up", "shift+tab":
		open.pick = max(questionOtherAt(open.question)-1, 0)
		open.other.with = ""
	case "down", "tab":
		// The custom answer is the last row of the same circular list.
		open.pick = 0
		open.other.with = ""
	case "left", "ctrl+b":
		box.left()
	case "right", "ctrl+f":
		box.right()
	case "home":
		box.home()
	case "end", "ctrl+e":
		box.end()
	// THE LINE KILL ANSWERS TO BOTH OF ITS NAMES, exactly as it does in the
	// message box and in every filterable overlay (input.go, palette.go's
	// [listNavigate], settings.go): `ctrl+u` is readline's, and `super+backspace`
	// is what ⌘⌫ sends on a terminal that reports the modifier at all.
	case "ctrl+u", "super+backspace":
		box.killToStart()
	case "ctrl+k":
		// AND KILL TO THE END OF IT, the other half of readline's pair and the same
		// key the message box and home's box bind (input.go, home.go). A box that
		// answered `ctrl+u` alone is the half-gesture killpairlaw_test.go holds
		// shut.
		box.killToEnd()
	case "backspace":
		box.deleteBackward()
	case "delete":
		box.deleteForward()
	default:
		text := msg.Key().Text
		if text == "" {
			return nil, false
		}
		box.insert(text)
	}
	a.touch()
	return nil, true
}

// questionWritingKey is every key while the box is pointed at the question
// ([questionShown.writing]): `enter` spends the sentence, `esc` points the box
// back at the conversation, and EVERY OTHER KEY TYPES — a `d` typed into "do
// not delete the old rows" that answered `you decide` instead would be the
// modal trap this block exists to not have.
func (a *app) questionWritingKey(head questionShown, key string) (tea.Cmd, bool) {
	open := a.questionHeld(head.token())
	if open == nil || open.writing == "" {
		return nil, false
	}
	switch key {
	case questionLaterKey:
		open.writing = ""
		a.touch()
		return nil, true
	case questionEnterKey:
		words := strings.TrimSpace(a.input.String())
		if words == "" {
			open.writing = ""
			a.touch()
			return nil, true
		}
		writing := open.writing
		open.writing = ""
		a.input.reset()
		if writing == questionAskBackKey {
			return a.askBack(*open, "", words), true
		}
		if writing == questionNoteKey {
			return a.answerQuestion(*open, session.Answer{Change: words}), true
		}
		return a.replaceQuestion(*open, words), true
	}
	return nil, false
}

// askBack asks the engine for an independent clarification in context. It never
// selects an answer, approves a tool, or releases the original decision.
func (a *app) askBack(q questionShown, option, words string) tea.Cmd {
	return a.answerQuestion(q, session.Answer{Clarify: true, AskedBack: []session.Exchange{{Option: option, Asked: words, At: a.now()}}})
}

// questionWriting reports whether the box under the block is a question's
// right now ([questionShown.writing]), which is what the seam's own hint asks
// before naming enter and esc.
func (a *app) questionWriting() bool {
	for _, q := range a.questions {
		if q.writing != "" {
			return true
		}
	}
	return false
}

// questionWritingRow is the answers row while the box is writing to the
// question: what the box means now, which answer the words go with, and the
// way back. It replaces the keys, because the keys are letters and every
// letter types while this row is up.
// TWO KEYS REACH IT NOW AND NOT FOUR. `?` asks the asker back, where there is
// no answer to attach the words to; `c` reaches it only on a question that asked
// for WORDS, where there is no `something else…` row to write them in — the
// owner's ruling of 2026-09-11 (your own answer, pick A) gave every other
// question that row instead, and the row says which answer the words go with
// rather than a sentence three rows below the pointer.
func (a *app) questionWritingRow(q questionShown) string {
	if q.writing == questionNoteKey {
		return "write your change, then enter" + questionWritingGap + "esc back"
	}
	if q.writing == questionCommentKey {
		return questionCommentKeyWord + questionWritingGap + "esc back"
	}
	return questionAskBackKeyWord + questionWritingGap + "the question stays open" + questionWritingGap + "esc back"
}

// questionChangeCarriesThePointer says whether the words `c` sends travel
// with the pointed answer's key. THEY DO FOR THE MODEL'S OWN ASK — "2, but
// keep the sqlite file" is one answer with a rider, and the asker reads the
// key — and for nothing else: a standing card's change is a correction of
// when or where and approves nothing, a consent's is a sentence beside the
// call, and each of those lanes has read a bare [session.Answer.Change] since
// before the block had a pointer.
func questionChangeCarriesThePointer(q session.Question) bool {
	return q.Kind == session.QuestionAsk
}

// The words the writing row is made of. THEY END IN "then enter", because the
// one fact the row exists to carry is that the box's next enter is the
// question's and not the conversation's.
const (
	questionCommentKeyWord = "other: write an updated request, then enter"
	questionAskBackKeyWord = "clarify: type your question, then enter"
	questionWritingGap     = " · "
)

// questionOptionKey is a key that names one of the question's own answers: a
// digit, or the letter a lane fixed on the option itself (task-states' `a`,
// `n`, `s` are the only letters any lane uses, and they are read off the option
// rather than re-decided here).
func (a *app) questionOptionKey(head questionShown, key string) (tea.Cmd, bool) {
	// THE ARROWS WALK THE POINTER ON EVERY QUESTION WITH ANSWERS — `↑`/`↓`
	// always, `←`/`→` where there is no hole for them to move, `tab` and
	// `shift+tab` beside them for the hand that reaches for those. It was
	// stop.go's and tabclose.go's grammar on the confirmation alone, and every
	// other form answered to digits only, which left a person with a card in
	// front of them and no key that visibly did anything to it.
	walk := 0
	switch key {
	case "up", "down":
		// UNDER A PLACE THE VERTICAL ARROWS ARE THE PLACE'S. Home routes its
		// own card here ([app.homeAskKey]) and `↓` there moves home's cursor,
		// which is what disarms a held row; a card that swallowed it would
		// leave an arming nobody can see. On the block above the box there
		// is no list under the card, and the pair walks the pointer.
		if a.questionOffFrame() {
			break
		}
		walk = 1
		if key == "up" {
			walk = -1
		}
	case "shift+tab":
		walk = -1
	case "tab":
		walk = 1
	case "left", "right":
		// WHERE THERE IS A HOLE THE SIDE ARROWS MOVE IT, and the pointer walks
		// on the other pair — the offer row spells which is on this card
		// ("choose" or "move it") rather than leaving them to guess.
		if a.moveQuestionHole(head, key) {
			return nil, true
		}
		walk = 1
		if key == "left" {
			walk = -1
		}
	}
	if walk != 0 && a.questionWalkCount(head) > 1 {
		count := a.questionWalkCount(head)
		a.moveQuestionPick(head, (head.pick+walk+count)%count)
		return nil, true
	}
	for at, option := range head.question.Options {
		if strings.TrimSpace(option.Key) != key {
			continue
		}
		if head.question.Ask == session.AskConfirmation {
			// NOTHING IS DECIDED BY ONE KEYSTROKE. A confirmation is asked
			// because the act cannot be taken back, so the key that NAMES an
			// answer moves the cursor onto it and `enter` is what takes it —
			// stop.go's law, kept exactly, in the block's own grammar. A digit
			// that answered outright would be the bypass key that whole card was
			// built to not have.
			a.moveQuestionPick(head, at)
			return nil, true
		}
		if option.Widening {
			// THE WIDENING ANSWER MAY HAVE A SECOND BEAT and is the only answer
			// that ever does: it is the one that grants more than the question
			// asked about, so it is the one worth asking how far.
			return a.questionWiden(head, key), true
		}
		// AND `[s] tell it` ANSWERS NOTHING AND OPENS A PAGE. A steer never
		// resolves a task by itself (docs/design/task-states/DESIGN.md), so the
		// landing's third column points the box at the node's own room and leaves
		// the question standing exactly where it was (tasksettle.go's
		// [app.landingTell]).
		if cmd, took := a.landingTell(head.question, key); took {
			return cmd, true
		}
		return a.questionAnswerKey(head, key), true
	}
	return nil, false
}

// focusQuestionTick moves a checklist's cursor, and it moves ONE cursor.
//
// A CHECKLIST HAD TWO. `↑`/`↓` walked [questionShown.pick] while `space`, `tab`
// and the digits acted on [questionInput.focus], and the card drew a pointer for
// each — so the mark a person moved was not the row the next key ticked. They
// are one value now: whichever of the two a renderer reads, it is reading the
// same row, and the arrows reach here rather than [app.questionOptionKey]
// because on a checklist there is nothing else for them to walk.
func (a *app) focusQuestionTick(open *questionShown, at int) {
	open.holes.focus = at
	open.pick = at
	a.touch()
}

// moveQuestionPick walks the cursor on the one form that has one.
func (a *app) moveQuestionPick(head questionShown, to int) {
	if open := a.questionHeld(head.token()); open != nil {
		open.pick = to
		a.touch()
	}
}

// questionHeld is one open question BY REFERENCE, so a key that moves something
// on it moves the one this window is holding rather than a copy.
//
// IT LOOKS IN TWO PLACES, and they are the two places a question is drawn: the
// block's queue above the message box, and the one card home raises about a row
// on its own list (homeconfirm.go). Home takes the frame whole, so its card can
// never be on screen beside the block's — but a lookup that knew about only one
// of them would leave whichever it forgot with a cursor that could not be moved.
func (a *app) questionHeld(token string) *questionShown {
	for i := range a.questions {
		if a.questions[i].token() == token {
			return &a.questions[i]
		}
	}
	if a.home.ask != nil && a.home.ask.token() == token {
		return a.home.ask
	}
	for _, ex := range a.exchanges {
		// AND THE ERRAND PANE'S OWN CARD, which is the third (homeexchange.go's
		// [homeExchange.ask] says why it is not on either of the two above).
		if ex != nil && ex.ask != nil && ex.ask.token() == token {
			return ex.ask
		}
	}
	return nil
}

// questionOpenOn is the block's open question on one lane and one id, or nil.
// It is what a door OTHER than the block uses to answer through the block —
// home's answer band reaching this window's own card (homeband_answer.go).
func (a *app) questionOpenOn(kind session.QuestionKind, id uint64) *questionShown {
	for i := range a.questions {
		if a.questions[i].question.Kind == kind && a.questions[i].question.ID == id {
			return &a.questions[i]
		}
	}
	return nil
}

// moveQuestionHole walks the choices in the hole this question's sentence
// carries, and reports whether there was one to walk.
//
// IT MOVES THE CHOICE AND ANSWERS NOTHING. That is the whole bargain the model
// shortlist was built on and it survives the move onto the block: a proposal
// arrives with the closest match already in the hole and the countdown already
// running, because one word fitting two models is the harness's ambiguity and
// not the person's. What the arrows buy is the seconds in which that choice is
// free to change — the proposal is not approved by changing it.
// questionTickKey is a checklist worked in the card: a digit ticks its row,
// `space` ticks the row under the cursor, `tab` walks the cursor, and `enter`
// sends what is ticked. It is the page's grammar (questioninput.go's
// [app.questionInputKey]) on the block's own copy of the holes, so a person
// who never opens the page can still answer a checklist — which is the whole
// of what a card that draws the ticks is for.
func (a *app) questionTickKey(head questionShown, key string) (tea.Cmd, bool) {
	open := a.questionHeld(head.token())
	if open == nil || open.holes.kind != session.InputChecklist {
		return nil, false
	}
	in := &open.holes
	switch key {
	case questionToggleKey:
		if in.focus < len(in.ticks) {
			in.ticks[in.focus] = !in.ticks[in.focus]
		}
	case questionBlankKey, "down":
		a.focusQuestionTick(open, questionStep(in.focus, 1, in.count()))
	case "shift+tab", "up":
		a.focusQuestionTick(open, questionStep(in.focus, -1, in.count()))
	case questionEnterKey:
		// THE ANSWER IS THE TICKED KEYS IN THE ORDER THE ROWS STAND, which is
		// the page's own reading of a checklist (questionroom.go) made here.
		var picked []string
		for _, i := range in.walk() {
			if i < len(in.ticks) && in.ticks[i] && i < len(open.question.Options) {
				picked = append(picked, strings.TrimSpace(open.question.Options[i].Key))
			}
		}
		if len(picked) > 0 {
			return a.answerQuestion(*open, session.Answer{Picked: picked}), true
		}
		// NOTHING TICKED IS NOTHING TO SEND, and the key is taken rather than
		// falling through to a pick the checklist does not have.
		return nil, true
	default:
		for at, option := range open.question.Options {
			if strings.TrimSpace(option.Key) != key || at >= len(in.ticks) {
				continue
			}
			in.ticks[at] = !in.ticks[at]
			a.focusQuestionTick(open, at)
			return nil, true
		}
		return nil, false
	}
	a.touch()
	return nil, true
}

// forgetQuestions drops every question, record and page this window holds,
// which is what a conversation switch owes the next one: the lane the new
// conversation is watched on replays everything still open there
// ([questionAgent.WatchQuestions]), and a question the OLD conversation was
// asking — or the line saying it was withdrawn — drawn over the new one would
// be a card about work that is not on the screen, answerable by a key aimed at
// something else. Seen on 2026-09-10: a withdrawn `Which painting genres do you
// like?` sat above the box of a conversation about something else entirely.
func (a *app) forgetQuestions() {
	a.questions = nil
	a.questionRecords = nil
	a.questionBands, a.questionSpans = nil, nil
	a.questionSent = nil
	a.qroom = nil
	a.touch()
}

func (a *app) moveQuestionHole(head questionShown, key string) bool {
	open := a.questionHeld(head.token())
	if open == nil || !questionWalkChoice(&open.holes, key) {
		return false
	}
	a.touch()
	return true
}

// questionPick answers with the option at an index, which is what the cursor
// and the pointer both resolve to.
func (a *app) questionPick(head questionShown, at int) tea.Cmd {
	if at < 0 || at >= len(head.question.Options) {
		return nil
	}
	key := strings.TrimSpace(head.question.Options[at].Key)
	if head.question.Options[at].Widening {
		// A PRESS ON THE WIDENING ANSWER OPENS ITS BEAT exactly as the key does.
		// One answer has one meaning whichever hand reaches it.
		return a.questionWiden(head, key)
	}
	return a.questionAnswerKey(head, key)
}

// questionAnswerKey sends one key as the answer.
func (a *app) questionAnswerKey(head questionShown, key string) tea.Cmd {
	if key == "" {
		return nil
	}
	answer := session.Answer{Key: key, Picked: []string{key}}
	if scope := questionScopeOf(head, key); scope != "" {
		answer.Scope = scope
	}
	return a.answerQuestion(head, answer)
}

// questionScopeOf is how far one answer reaches, where the option says.
//
// THE WIDENING ANSWER CARRIES THE WIDEST SCOPE THE QUESTION OFFERED, and every
// other answer carries WHAT THE PERSON SAID ON THE ROW (questionscope.go), which
// is `once` until they touch it. [session.AnswerOption.Widening] is the lane's
// own mark for "this grants more than the question asked about", so the surface
// never has to guess which of a lane's answers is the wide one — and an answer
// that already grants everything is not narrowed by a row about lifetimes.
func questionScopeOf(q questionShown, key string) session.AnswerScope {
	option, ok := q.question.Option(key)
	if !ok || !option.Widening {
		return questionScopeNow(q)
	}
	widest := session.ScopeOnce
	for _, scope := range q.question.Scope {
		switch scope {
		case session.ScopeAlways:
			return session.ScopeAlways
		case session.ScopeProject:
			widest = session.ScopeProject
		case session.ScopeTask:
			if widest == session.ScopeOnce {
				widest = session.ScopeTask
			}
		}
	}
	return widest
}

// questionVerbKey routes the keys from [questionKeys] that are not answers: the
// ones that say something ABOUT the decision rather than giving it.
//
// It refuses a key the row did not draw, which is [app.questionAnswerKeys]
// read a second time rather than a second list — NO KEY DOES ANYTHING THAT IS
// NOT DRAWN ON SCREEN RIGHT NOW (verbstrip.go states the law this surface holds
// itself to).
func (a *app) questionVerbKey(head questionShown, key string) (tea.Cmd, bool) {
	form := a.questionForm(head.question)
	offered := false
	for _, verb := range a.questionAnswerKeys(head, form) {
		if verb.key == key {
			offered = true
			break
		}
	}
	if !offered {
		return nil, false
	}
	switch key {
	case questionWalkKey:
		// The arrows are routed as arrows ([app.questionOptionKey]); the pair's
		// spelling is only ever drawn.
		return nil, false
	case questionOpenKey:
		return a.openQuestionRoom(head), true
	case questionDecideKey:
		// YOU DECIDE hands the decision back to the asker and RECORDS that this
		// is what happened, which is the whole point of the answer: a decision
		// nobody made is a decision nobody can find later.
		return a.answerQuestion(head, session.Answer{
			Key:       questionDecidedKeyOf(head.question),
			DecidedBy: session.DecidedByAsker,
		}), true
	case questionDialKey:
		return a.questionDial(head), true
	case questionScopeKey:
		// `t` CHANGES THE ROW AND NOTHING ELSE. Nothing is written and nothing
		// is answered; the lifetime travels with the answer when one is given
		// ([questionScopeOf]), so the key is safe to press and to press back.
		a.questionScopeNext(head)
		return nil, true
	case questionRuleKey:
		return a.questionMakeRule(head), true
	case questionUndoKey:
		return a.questionUndo(head), true
	case questionNoteKey:
		// An already completed action keeps its correction lane; it has no
		// pending request for Other to withdraw.
		if open := a.questionHeld(head.token()); open != nil {
			open.writing = questionNoteKey
			a.touch()
			return nil, true
		}
		return nil, false
	case questionCommentKey:
		// Other collects a revised request without choosing the highlighted option.
		open := a.questionHeld(head.token())
		if open == nil {
			return nil, false
		}
		if head.commented != nil {
			// A LANE WHOSE BOX IS NOT THE BOX IS TOLD, and it is told FIRST:
			// home's errand pane draws this card beside a message box of its own
			// pointed at another conversation, so the words go there and the
			// pane arms itself ([questionShown.commented]).
			head.commented()
			a.touch()
			return nil, true
		}

		open.writing = questionCommentKey
		a.touch()
		return nil, true
	case questionAskBackKey:
		if head.commented != nil {
			// A LANE WHOSE BOX IS NOT THE BOX IS TOLD ([questionShown.commented]).
			head.commented()
			a.touch()
		}
		// `?` POINTS THE BOX AT THE ASKER RATHER THAN ANSWERING. It is "answer me
		// this first": it needs a sentence, the box is where sentences are typed
		// on this surface, the question stays open, the panel's own keys say what
		// the box is writing now, and the words go to the asker with the next
		// enter ([app.questionWritingKey]).
		//
		// Nothing is opened: the box below is ALREADY live and always was,
		// which is what NEVER MODAL means. What changes is what the box means,
		// and the row says so.
		if open := a.questionHeld(head.token()); open != nil {
			open.writing = key
			a.touch()
		}
		return nil, true
	}
	return nil, false
}

// questionDecidedKeyOf is which answer `d you decide` gives: the asker's own
// pick where it named one, and the safe answer where it did not.
//
// A QUESTION WITH NO PICK HANDED BACK IS THE SAFE ANSWER AND NOT A GUESS. The
// person said "you choose"; with nothing recommended there is nothing to
// choose, and taking the answer that loses nothing is the only reading that
// cannot cost them something they did not agree to.
func questionDecidedKeyOf(q session.Question) string {
	if q.Pick != nil && strings.TrimSpace(q.Pick.Key) != "" {
		return strings.TrimSpace(q.Pick.Key)
	}
	if at := questionSafeAt(q); at < len(q.Options) {
		return strings.TrimSpace(q.Options[at].Key)
	}
	return ""
}

// questionMakeRule is `r`: the third same-shaped yes, taken and written down.
//
// It answers with the widest scope the question offered, which is what makes it
// a rule rather than a yes — and the receipt says so, because A RULE IS NEVER
// HIDDEN.
func (a *app) questionMakeRule(head questionShown) tea.Cmd {
	key := questionDecidedKeyOf(head.question)
	for _, option := range head.question.Options {
		if option.Widening {
			key = strings.TrimSpace(option.Key)
			break
		}
	}
	if key == "" {
		return nil
	}
	scope := session.ScopeProject
	for _, one := range head.question.Scope {
		if one == session.ScopeAlways {
			scope = session.ScopeAlways
		}
	}
	return a.answerQuestion(head, session.Answer{
		Key: key, Picked: []string{key}, Scope: scope,
		Why: "a rule, from the third time this was asked",
	})
}

// questionUndo is `u` on a ratify line: take back what was already done.
//
// It answers with the option the lane marked SAFE, which on a ratify question
// is the one that puts things back — `already done` is the other answer and is
// what happens by saying nothing.
func (a *app) questionUndo(head questionShown) tea.Cmd {
	at := questionSafeAt(head.question)
	if at >= len(head.question.Options) {
		return nil
	}
	return a.questionPick(head, at)
}

// questionDial is `D`: decide questions of this shape from now on, without
// asking.
//
// THE DIAL'S STORAGE IS LANE E2'S (`autonomy.json`, per project) AND THIS IS
// THE SEAM ONTO IT — wired now, through [session.Agent.SetAutonomy] and the wire
// frame that carries it to an engine in another process (internal/remote's
// MethodSetAutonomy, which every local chat window goes through).
//
// IT DOES BOTH HALVES OF THE PROMISE. The standing half writes the shape into
// the project's own settings, so the NEXT question of that shape is answered
// without asking; the immediate half answers the one in front of the person,
// because `D` is pressed while looking at a question and a key that set a
// setting and left that question sitting there would read as having done
// nothing. A session with nowhere to keep the setting still answers this one:
// losing the standing half is not a reason to lose the answer.
func (a *app) questionDial(head questionShown) tea.Cmd {
	key := questionDecidedKeyOf(head.question)
	if key == "" {
		return nil
	}
	// `D` DOES BOTH HALVES OF WHAT ITS WORD SAYS. It answers the question in
	// front of the person, and it writes the rule that answers the next one —
	// through the engine's own door (autonomysheet.go), which refuses the two
	// shapes no rule may ever cover. Before this wave it only did the first, so
	// `decide these from now on` was a key that decided exactly one.
	//
	// AND THE RULE IS SAID OUT LOUD, because NEVER A HIDDEN RULE: the row that
	// answers under it afterwards wears `· your rule`, and [app.dialKind] says
	// the moment it was written, on the frame after the engine took it.
	return tea.Batch(a.dialKind(head.question.Ask), a.answerQuestion(head, session.Answer{
		Key: key, Picked: []string{key}, Scope: session.ScopeProject,
		Why: "decide these from now on",
	}))
}

// dialKind writes this project's rule for one shape of question and says out
// loud what came of it.
//
// IT IS ASKED FROM A COMMAND (offloop.go): writing a rule is a call to the
// engine's process, and the answer in front of the person is settled on the
// keystroke rather than behind this. So the line about the rule lands on the
// frame after the receipt, in the order the two things actually happened.
func (a *app) dialKind(kind session.AskKind) tea.Cmd {
	// IT ASKS FOR THE WRITE AND NOT FOR THE READ. `D` needs somewhere to keep
	// the setting and nothing else; only `/autonomy` needs to read the rows
	// back. Asking for both here would take the key away from a session that
	// can keep a rule perfectly well, which is the narrowing
	// [questionDialDoor]'s own comment exists to protect.
	agent, ok := a.agent.(questionDialDoor)
	if !ok || kind == "" {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		err := agent.SetAutonomy(kind, session.Policy{Kind: session.PolicyDecide})
		return func(here bool) tea.Cmd {
			if !here {
				return nil
			}
			if err != nil {
				// THE REFUSAL IS THE PERSON'S TO READ. The engine turns down a
				// rule over a clarification and over anything destructive, and a
				// key that silently did nothing would be a key that promised a
				// rule and wrote none.
				a.note(strings.TrimSpace(err.Error()))
				return nil
			}
			a.autonomyChanged()
			// AND THE RULE IS SAID OUT LOUD, because NEVER A HIDDEN RULE: the
			// row that answers under it afterwards wears `· your rule`, and this
			// is the moment it was written.
			a.note(questionShapeWord(kind) + " · " + autonomyDecideWord + questionDialFromNowWord)
			return nil
		}
	})
}

// questionDialFromNowWord is the tail of that line: where the rule reaches and
// how to take it back.
const questionDialFromNowWord = " from now on · /autonomy to change it"

// openQuestionRoom walks into the room over this question — lane S2's page.
//
// THE DOOR IS CALLED AND NEVER RE-IMPLEMENTED. The page is questionroom.go's,
// and everything it needs travels in the [questionShown] this block was already
// holding — the question, its resolver, whether a rule is on offer, whether an
// undo would reach anything — so opening one hands over what is already in hand
// rather than building a second reading of the same question.
//
// THE BLOCK GOES WITH IT. A question drawn twice — pinned above the box and
// spread over the page — is one question a person could answer in two places
// with two different sets of keys on screen at once, and the fold is exactly the
// state the chip already knows how to bring back.
func (a *app) openQuestionRoom(head questionShown) tea.Cmd {
	a.foldQuestion(head)
	a.raiseQuestionRoom(head)
	return nil
}

// ── the pointer ─────────────────────────────────────────────────────────────

// questionPress resolves a click on the block and reports whether it took it.
//
// IT DOES NOT SWALLOW WHAT IT DID NOT DRAW, which is the block's own law said
// to the pointer: consent.go swallowed every press inside itself because it was
// modal and a press falling through would expand a tool call while somebody was
// denying one. Nothing here is modal, so a press that hits no answer falls
// through to whatever is under it, exactly as a key does.
func (a *app) questionPress(x, y int) (tea.Cmd, bool) {
	head, ok := a.questionHead()
	if !ok || a.copy.on {
		return nil, false
	}
	set := a.questionSet()
	if set != nil {
		if cmd, took, mine := a.questionSetPress(set, head, x, y); mine {
			return cmd, took
		}
		// A PRESS ON A TAB'S ANSWER IS HELD like a key on it is.
		a.questionStaging = head.question.Batch
		defer func() { a.questionStaging = "" }()
	}
	if cmd, took, sheeted := a.questionBandPress(head, x, y); sheeted {
		return cmd, took
	}
	mark, found := a.chromeAt(y)
	if !found || mark.kind != chromeQuestion || mark.index != a.questionSpanRow {
		return nil, false
	}
	for _, span := range a.questionSpans {
		if x < span.from || x >= span.to {
			continue
		}
		// A CLICK ON AN ANSWER IS AIMING AT THE BLOCK, exactly as an arrow is
		// (questionkeys.go's THE BOX KEEPS THE FIRST LETTER).
		a.aimQuestion(head.token())
		if len(head.beat) > 0 {
			// THE BEAT'S SPANS ARE SHAPES AND NOT ANSWERS. They are drawn where
			// the answers were, so a press there means whichever of the two is on
			// screen — and reading it as an answer would bank the widest one.
			return a.questionPickShape(head, span.at), true
		}
		return a.questionPick(head, span.at), true
	}
	return nil, false
}

// questionMark is what the pointer is over on row i of the block.
func (a *app) questionRowMark(i int) chromeRow {
	return chromeRow{kind: chromeQuestion, index: i}
}

// ── the chip ────────────────────────────────────────────────────────────────

// questionSegment is the status line's chip: `? allow this? · alt+y`, and
// `? 3 questions · alt+y` when there is more than one.
//
// IT IS REACHABLE FROM EVERY PAGE, which is the whole reason it is on the
// status row rather than in the block: the block is above the box in a
// conversation, and a person standing on home, in a room or on the tasks place
// has no block to look at. The chip is the one thing that is always there while
// anything is waiting.
//
// IT CARRIES THE QUESTION'S OWN WORDS (owner ruling 2026-09-11). `1 question`
// says that something is waiting and nothing about whether it is worth crossing
// the room for; the head says which decision is parked, and a person who can
// read it from the status row does not have to open anything to know.
//
// AND IT NEVER COUNTS A LINE THAT IS NOT WAITING. A ratify line is a statement
// about something already done ([questionWaits]); counting it put a number on
// the status row that no key could clear.
//
// THE EMPTINESS LAW. Nothing open is nothing drawn — never `0 questions`.
func (a *app) questionSegment() string {
	count := a.questionWaitingCount()
	if count == 0 {
		return ""
	}
	mark := a.icon(tokens.GNeedsHuman) + " "
	// THE CHORD IS SPELLED FOR THIS KEYBOARD, through the one door every
	// person-facing sentence about a chord goes through (chords.go's
	// [chordSpelling.say]). It was drawn straight out of its constant, so a Mac
	// that says `opt+1…opt+7` on the map and `opt+t` on the roster said `alt+y` on
	// this chip — one modifier under two names, on screens a person moves between
	// in one keystroke.
	key := a.chords.say(questionChipKey)
	if count > 1 {
		return mark + itoa(count) + " questions · " + key
	}
	if head := a.questionChipWords(); head != "" {
		return mark + head + " · " + key
	}
	return mark + "1 question · " + key
}

// questionChipWords is the head the chip carries, cut to what a status segment
// may spend. It is the NEWEST waiting question's, which is the one the chip's
// own key raises ([app.raiseFolded]) — a chip naming one question and opening
// another would be worse than a chip naming none.
func (a *app) questionChipWords() string {
	for i := len(a.questions) - 1; i >= 0; i-- {
		if !questionWaits(a.questions[i].question) {
			continue
		}
		head := strings.TrimSpace(a.questions[i].question.Head)
		if head == "" {
			return ""
		}
		return fit(head, questionChipWordsMax)
	}
	return ""
}

// questionChipWordsMax is how many cells of the head the chip may spend. It is
// the width of the longest thing the status row carries beside it, measured
// rather than chosen: past this the segment pushes the clock and the spend off
// their own row, and the status line's segments each own their width.
const questionChipWordsMax = 34

// questionChipKeyPress is [questionChipKey] from wherever a person is standing:
// it brings the newest open question back above the box, and takes them to the
// conversation it belongs to.
func (a *app) questionChipKeyPress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if msg.String() != questionChipKey || a.questionCount() == 0 {
		return nil, false
	}
	// A QUESTION NOBODY CAN SEE IS A QUESTION NOBODY CAN ANSWER. The block is
	// drawn above the box in the conversation, so the chip's key puts the
	// person there — the same move a question ARRIVING makes on its way in.
	a.closeSettings()
	a.closeExpand()
	a.closeHome()
	// The tasks place and the record card over it are places too, and the
	// chip's promise held from neither ("5 questions · alt+y" on a page that
	// did nothing with the key).
	a.closeTaskSheet()
	a.raiseFolded()
	return nil, true
}

// ── the lane ────────────────────────────────────────────────────────────────

// watchQuestions opens the standing subscription and starts pumping it.
//
// IT IS A LANE OF ITS OWN AND NOT THE TURN'S STREAM, which is the engine's own
// decision read back here: a question outlives the turn that raised it, it is
// re-sent whole to a surface that attaches mid-flight, and half of the lanes
// that raise one are not in a turn at all (a landed task, a run at its fuel
// gate). Folding it into the turn's stream would mean a question that only
// exists while somebody is being spoken to.
//
// It is called wherever [app.watchRuns] is, and for the same reason: the
// channel belongs to the agent that handed it over, so a replaced conversation
// gets a new one.
func (a *app) watchQuestions() tea.Cmd {
	doors, ok := a.questionDoors()
	if !ok {
		return nil
	}
	if a.questionWatch != nil {
		a.questionWatch()
	}
	a.questionGen++
	var lane <-chan session.Event
	lane, a.questionWatch = doors.WatchQuestions()
	a.questionLane = lane
	return waitQuestion(lane, a.questionGen)
}

// waitQuestion takes one event off the lane and asks for the next.
func waitQuestion(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return questionLaneClosedMsg{gen: gen}
		}
		return questionEventMsg{gen: gen, ev: ev}
	}
}

// questionEvent folds one event from the lane in and re-arms the pump.
func (a *app) questionEvent(ev session.Event) tea.Cmd {
	return tea.Batch(a.questionFold(ev), waitQuestion(a.questionLane, a.questionGen), a.wake())
}

// questionFold is what the lane's three kinds DO.
//
// AN ANSWERED QUESTION FROM THE LANE IS SOMEBODY ELSE'S ANSWER. This window
// closes its own the moment it sends one ([app.closeQuestion]) rather than
// waiting for the round trip, so an EventQuestionAnswered that still finds the
// question open here was answered in another window or by the dial — which is
// FIRST ANSWER WINS, and the honest thing to draw is the receipt saying who
// decided and what.
func (a *app) questionFold(ev session.Event) tea.Cmd {
	if ev.Kind == session.EventQuestionDiscussion {
		return a.discussionEvent(ev.Discussion)
	}
	if ev.Question == nil {
		return nil
	}
	switch ev.Kind {
	case session.EventQuestion:
		if !a.questionDrawnHere(*ev.Question) {
			return nil
		}
		if _, ok := a.questionDoors(); !ok {
			// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. With no
			// resolve door there is nowhere for an answer to go, so the honest
			// thing is not to put the question on screen — a row somebody can
			// read and press and never resolve is worse than one they answer in
			// the window that owns it. That is the state a `--host` window is in
			// today: events cross the wire and [session.Agent.ResolveQuestion]
			// does not.
			return nil
		}
		// WHERE IT GOES IS NOT DECIDED HERE. One rule answers that for the
		// block, for home, for the notification and for the bell, and it is the
		// only thing on this surface that knows what "away" means
		// (questiondelivery.go's [app.deliverQuestion]).
		return a.deliverQuestion(*ev.Question)
	case session.EventQuestionWithdrawn:
		reason := ""
		if ev.Question.Withdrawn != nil {
			reason = ev.Question.Withdrawn.Reason
		}
		a.withdrawQuestion(*ev.Question, reason)
	case session.EventQuestionAnswered:
		if ev.Answer == nil {
			return nil
		}
		// AND NEVER TWICE FOR ONE DECISION. This window closes its own question
		// the moment it sends an answer ([app.closeQuestion]) rather than waiting
		// for the round trip, so the event that comes back a moment later is
		// about a question that is already gone here — and recording it again
		// would put the same receipt above the box twice, which is one decision
		// drawn as two. [app.foldOthersAnswer] is where that guard lives, along
		// with the rest of what this event is FOR: an answer given somewhere
		// else.
		a.foldOthersAnswer(*ev.Question, *ev.Answer)
	}
	return nil
}

// questionRaceFor is how long after this window's own answer another window's
// answer to the same question is still worth a row.
//
// A SECOND, WHICH IS THE LAW'S OWN NUMBER (docs/design/questions/DESIGN.md:
// "a differing answer inside a second is shown, not merged"). Past it the other
// window was simply late, and answers.go's own first law — late answers are
// ignored and nothing says so — is the honest reading: somebody who answered a
// minute ago has moved on, and a line about it would be news about nothing.
const questionRaceFor = time.Second

// foldOthersAnswer is FIRST ANSWER WINS, drawn.
//
// Three things can be true when the lane says a question was answered, and they
// are three different rows:
//
//   - THE QUESTION IS STILL OPEN HERE, so somebody answered it somewhere else.
//     The receipt is written with [session.DecidedByWindow] on it, which is what
//     keeps it from saying `you` about a key pressed on another screen.
//   - THIS WINDOW ANSWERED IT, and the lane is telling us what we already know
//     ([app.closeQuestion] does not wait for the round trip). Nothing is drawn.
//   - THIS WINDOW ANSWERED IT DIFFERENTLY, within [questionRaceFor]. Both are
//     shown and NEITHER IS MERGED: the first answer is the decision and the
//     second is a person finding out their key did not land.
func (a *app) foldOthersAnswer(q session.Question, answer session.Answer) {
	shown := questionShown{question: q}
	// AND THE LANE'S WORD IS WHAT SETTLED MEANS. The engine said this question
	// is decided, so nothing this window hears afterwards may raise it again
	// ([app.reopenQuestion] reads this).
	a.questionSettledByLane(shown.token())
	if a.questionIsOpen(shown.token()) {
		// UNLESS IT IS THIS WINDOW'S OWN ANSWER COMING BACK, which is a
		// question still open here only because the door never said whether it
		// took it ([app.markQuestionSent] holds the reason). The keys have to
		// match: an answer this window sent and LOST to another window's is
		// somebody else's decision and wears their name, exactly as it did
		// before.
		if a.questionSentHere(shown.token(), answer.Keys()) {
			a.closeQuestion(shown, answer)
			return
		}
		if answer.DecidedBy == "" || answer.DecidedBy == session.DecidedByPerson {
			answer.DecidedBy = session.DecidedByWindow
		}
		a.closeQuestion(shown, answer)
		return
	}
	mine, ok := a.questionAnswerHere(q)
	if !ok || a.now().Sub(mine.at) > questionRaceFor {
		return
	}
	if sameAnswer(mine.record.Picked, answer.Keys()) {
		return
	}
	a.questionRecords = append(a.questionRecords, questionRecord{
		record: session.DecisionRecord{
			ID: q.ID, Ref: q.Ref, Kind: q.Kind, Ask: q.Ask,
			Head: strings.TrimSpace(q.Head), Subject: q.Subject,
			Picked: answer.Keys(), Labels: questionLabels(q, answer.Keys()),
			By: session.DecidedByWindow, Stakes: q.Stakes, At: answer.At,
		},
		head: strings.TrimSpace(q.Head), at: a.now(),
	})
	a.note(questionRaceWord)
	a.touch()
}

// questionRaceWord is what a person is told when two windows answered one
// question at almost the same moment. It says which answer counted, because
// that is the only thing they cannot see from the two rows above it.
const questionRaceWord = "two windows answered that · the first one is the decision"

// questionSettledByLane remembers that the ENGINE said a question is decided,
// and questionSettledElsewhere reads it back.
//
// IT IS A SET AND NOT A LOOK AT THE RECEIPTS because a receipt is bounded and
// faded ([app.questionRecordsShown]) — it is news, and news goes — while "the
// engine has settled this" has to stay true for as long as anything could
// arrive claiming otherwise. What arrives is a door's refusal from a call that
// deadlined, and that can be ten seconds behind.
// keepQuestionRefusal holds a door's sentence against the question it was about,
// for a window that had moved on when it arrived, and sayKeptQuestionRefusal
// spends it when that question comes back.
func (a *app) keepQuestionRefusal(token string, err error) {
	if token == "" || err == nil {
		return
	}
	if a.questionRefused == nil {
		a.questionRefused = map[string]string{}
	}
	a.questionRefused[token] = strings.TrimSpace(err.Error())
}

func (a *app) sayKeptQuestionRefusal(token string) {
	said, kept := a.questionRefused[token]
	if !kept {
		return
	}
	delete(a.questionRefused, token)
	a.note(said)
}

func (a *app) questionSettledByLane(token string) {
	if token == "" {
		return
	}
	if a.questionDone == nil {
		a.questionDone = map[string]bool{}
	}
	a.questionDone[token] = true
}

func (a *app) questionSettledElsewhere(token string) bool {
	return token != "" && a.questionDone[token]
}

// questionIsOpen reports whether this block still holds one question.
func (a *app) questionIsOpen(token string) bool {
	for _, open := range a.questions {
		if open.token() == token {
			return true
		}
	}
	return false
}

// questionAnswerHere is the receipt this window already wrote for one question,
// when it wrote one recently enough to still be above the box.
func (a *app) questionAnswerHere(q session.Question) (questionRecord, bool) {
	for i := len(a.questionRecords) - 1; i >= 0; i-- {
		record := a.questionRecords[i]
		if record.withdrawn != "" {
			continue
		}
		if record.record.Kind == q.Kind && record.record.ID == q.ID && record.record.Ref == q.Ref {
			return record, true
		}
	}
	return questionRecord{}, false
}

// markQuestionSent remembers the answer this window has just handed to the
// engine's door for one question, before the door has said anything about it.
//
// IT IS THE ONE FACT THE LANE'S NEWS CANNOT CARRY. [session.EventQuestionAnswered]
// says a question was answered by a person; it does not and cannot say WHICH
// SCREEN the key was pressed on, and [app.foldOthersAnswer]'s whole reading of
// "somebody else answered it" is that the question is still open here. That
// reading is right for every window that did nothing and wrong for the one that
// answered and was not told, so this is what tells the two apart.
func (a *app) markQuestionSent(token string, keys []string) {
	if token == "" {
		return
	}
	if a.questionSent == nil {
		a.questionSent = map[string][]string{}
	}
	a.questionSent[token] = append([]string(nil), keys...)
}

// questionSentHere reports whether this window sent exactly this answer to this
// question and has not seen it close since.
func (a *app) questionSentHere(token string, keys []string) bool {
	sent, ok := a.questionSent[token]
	return ok && sameAnswer(sent, keys)
}

// sameAnswer reports whether two answers picked the same keys, in the same
// order. Order matters on a checklist, where `1,3` and `3,1` are one answer and
// `1,3` and `1,2` are not.
func sameAnswer(one, two []string) bool {
	if len(one) != len(two) {
		return false
	}
	for i := range one {
		if one[i] != two[i] {
			return false
		}
	}
	return true
}

// questionLabels is each picked key in the question's OWN word for it, falling
// back to the key where a lane offered none.
func questionLabels(q session.Question, keys []string) []string {
	labels := make([]string, 0, len(keys))
	for _, key := range keys {
		if option, ok := q.Option(key); ok && strings.TrimSpace(option.Label) != "" {
			labels = append(labels, strings.TrimSpace(option.Label))
			continue
		}
		labels = append(labels, key)
	}
	return labels
}

// questionDrawnHere is which lanes THIS BLOCK draws, and it is the migration's
// seam rather than a permanent shape.
//
// EVERY LANE ENDS UP HERE. Until each older block is deleted its lane is left
// to it, because a question drawn twice on one screen is worse than a question
// drawn in the older place: a person answering the second copy of a decision
// they already made is the exact failure the one-renderer wave exists to end.
// So this list GROWS as blocks are retired, and it is the one place that says
// which are done.
func (a *app) questionDrawnHere(q session.Question) bool {
	switch q.Kind {
	case session.QuestionLanding:
		// AND THE LANDED `your call`, which had an older block and has now lost
		// it. Its card in the transcript still says how the work came home and
		// what is being asked; what left the card is the ANSWERS ROW and its
		// keys, because a question drawn twice on one screen is worse than a
		// question drawn in the older place — and because a card frozen in the
		// shape of a landing that has since been re-settled offered a person the
		// wrong answers to the right question (#767, tasksettle.go).
		return true
	case session.QuestionSubharnessAsk, session.QuestionFuel, session.QuestionConflict:
		// The three lanes the audit found with a resolver and NOTHING ANYWHERE
		// that drew them: work stopped on a question no surface in this product
		// could put to a person. They are drawn here first because there is no
		// older block to retire — this block is the only one they have ever had.
		return true
	case session.QuestionAsk:
		// AND THE MODEL'S OWN DOOR, for the same reason and more sharply. The
		// `ask` tool (session's tools_ask.go) is the last rung of the ladder,
		// it has no older block anywhere, and until it is drawn here every call
		// to it stops the turn on a question no window in this product can show
		// — which was observed on a real run: two `ask` calls waiting, the step
		// saying `still waiting for an answer`, and nothing on any screen to
		// answer with.
		return true
	case session.QuestionConsent:
		// THE APPROVAL GATE, whose 1388-line block is deleted (consent.go). What
		// is left there is the lane's own three things — the row, the widening
		// write, the reading clock's length — and they ride on the question this
		// block draws.
		return true
	case session.QuestionTask:
		// THE TASK PROPOSAL, whose own choices row, draining meter and keyboard
		// lane are deleted (task.go). What is left there is the assignment in
		// the transcript — the brief, where the work will run, what it will run
		// on — which is what the question is ABOUT rather than a second copy of
		// the asking.
		return true
	case session.QuestionStanding:
		// THE STANDING CARD, whose chip row, cursor, digits and hint line are
		// deleted (standing.go, pickrow.go). What is left there is the head, the
		// bands, the draining meter and the news line — what the card SHOWS, as
		// against what it ASKS.
		return true
	case session.QuestionHarness:
		// THE HARNESS LANE'S TWO QUESTIONS, which are one lane and were two
		// grammars. The offer to run a saved program had a row and a pair of
		// digits of its own (harness.go); the finished design had `enter`/`e`/`esc`
		// on the card in the feed and a second row with `ctrl+k`/`ctrl+x` pinned
		// in its room (harnesscard.go, roomapproval.go). All three are deleted.
		// What is left is the PAGE in the transcript, which is what is being
		// judged, and the one line it keeps afterwards saying what became of it.
		return true
	case session.QuestionConnect:
		// THE ACCOUNT OFFER, whose own three-row block, answers row, click
		// targets and key router are deleted (connect.go). What is left there is
		// the lane's own facts — the service id, the catalog's sentence, whether
		// the answer is a secret — and the browser flow an answer raises, which is
		// a REPORT and not a question.
		return true
	}
	return false
}
