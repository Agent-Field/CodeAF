package tui3

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE ROOM: A QUESTION IS A PLACE TOO, AND YOU CAN GO THERE.
//
// docs/design/questions/DESIGN.md gives a question four sizes, and this file is
// the largest of them: "a page over the conversation (the task-room idiom): head
// and attribution, options as sections (▸/▾, bodies, blocks), x compare on the
// asker's dimensions, c comment on the focused part, ? ask back, the foot
// composes the answer (pick · with · notes · scope), d you decide shows the pick
// and reason first, D sets the dial for the kind."
//
// A ROOM IS FOR A QUESTION WHOSE EVIDENCE DOES NOT FIT ON A CARD. The card can
// hold a word and a consequence per answer; it cannot hold the diagram the asker
// drew, the diff each answer produces, or three paragraphs about what a schema
// change costs. A person handed that on a card either answers without reading it
// or asks the model to say it all again in the thread — which is the same
// evidence, unstructured, in the one place it cannot be compared. So the
// evidence sets the size of the drawing, and this is the size it sets when there
// is a lot of it.
//
// ── IT IS A VIEW, NOT A SECOND APP, AND NOT A MODAL ──
//
// room.go's law, kept verbatim: nothing under this page stops. The turn goes on
// streaming into [app.entries], the roster goes on ticking, and the ONLY thing
// that changes is which rows [app.bodyRows] hands the frame. `esc` restores the
// conversation exactly, scroll included, because the conversation was never
// touched — this page keeps an offset of its own.
//
// AND THE BOX STAYS LIVE, which is the questions law the consent block does not
// keep: "NEVER MODAL, NEVER SUSPENDS THE KEYBOARD. The box stays live; typing
// after › is answering with words". Every letter this page takes it takes only
// while the box is EMPTY, exactly as `x` (stop) is taken on this surface today,
// so a person mid-sentence never loses a keystroke to a question.
//
// ── THE PAGE IS DRAWN IN questionpage.go ──
//
// This file is the page's STATE and its KEYS: what the reader is standing on,
// what the foot has composed, and what each key does. How it is laid out — the
// two panes at a hundred columns and wider, the one column narrower, the two
// windows and what a press reaches — is questionpage.go's, drawn with the
// evidence renderer every question drawing shares (questionevidence.go).
//
// ── THE POINTER, AND THE WAYS TO MOVE IT ──
//
// `↑`/`↓` walk the answers and the evidence beside them follows; `enter` takes
// the answer the pointer is on, which is the block's own law arriving on the
// page (#789); a digit moves the pointer straight to its answer. Where there are
// two panes, `→` hands the arrows to the evidence so a long diagram can be read
// to its end and `←` hands them back. And a click on an answer's row moves the
// pointer onto it, with a second click on the same row being `enter` — one press
// to read, one to decide ([app.questionRoomPress] says why it is two).
//
// ── ALIGNMENT, STATED ONCE ──
//
// DESIGN.md: "Head at the gutter; option rows indented one key-cell; answers row
// indented like the options". So everything on the page is indented
// [questionIndent], the answers' rows are the panel's own, and a shape's parts
// are indented one further key-cell ([questionBodyIndent]) so a part and what is
// written under it cannot be misread as two answers.

const (
	// questionIndent is the one key-cell every row under the head is indented by.
	questionIndent = "  "
	// questionBodyIndent is what is written under one part of a shape — a note
	// on a checklist row, the exchange under a pair — one key-cell further in
	// than the part: the key, its space and its mark are what it clears.
	questionBodyIndent = "      "
	// questionCompareFloor is where the compare table stops being a table.
	// DESIGN.md: "compare stacks under 80 cols".
	questionCompareFloor = 80
)

// The words this page says, spelled once. Every one of them is in the person's
// own vocabulary: DESIGN.md bans "prompt", "modal", "dialog" and "approval
// gate", and bans the machinery words the task states already banned.
const (
	// questionSep is the separator every telemetry row on this surface writes,
	// and every row this page draws writes it too — a page that punctuated
	// differently from the status line beside it would read as a different
	// program.
	questionSep = " · "
	// questionWouldSwitchWord opens the line that is the most useful thing on the
	// page: what would change the asker's mind. A person who disagrees with a
	// pick usually disagrees with exactly this.
	questionWouldSwitchWord = "would switch if "
	// questionThenWord and questionWhyWord label two of the dim lines of an
	// answer's case (questionevidence.go), and they exist because four grey sentences stacked in one
	// column read as one grey paragraph nobody can tell apart — the owner,
	// 2026-09-10, on a page exactly like that: "make sure there is some
	// textual hierarchy in design in options like the same line and next line
	// in options look same and a bit weird". What an answer MEANS is ink; what
	// it COSTS and why the asker would take it are dim AND say which they are.
	questionThenWord = "then · "
	questionWhyWord  = "why this one · "
	// questionAttachWord titles the evidence the QUESTION carries, as opposed
	// to the evidence hanging off one answer. An untitled run of diagrams at
	// the foot of the page was three pictures belonging to nobody.
	questionAttachWord = "what it showed you"
	// questionWaitsWord and questionGoesOnWord are the two halves of the third
	// attribution row: what is stopped on this, and what is not. Saying only the
	// first reads as though everything stopped.
	questionWaitsWord   = "the turn waits on it"
	questionGoesOnWord  = " goes on without it"
	questionNothingWord = "nothing is waiting on it"
	// questionAlsoWord joins the two when both are true.
	questionAlsoWord = " and "
	// questionAnsweringWord opens the foot: what pressing enter would send.
	questionAnsweringWord = "answering "
	// questionWithWord introduces what was typed beside the pick, and
	// questionNotesWord how many comments are attached. Both are dim.
	questionWithWord  = "with "
	questionNotesWord = " noted"
	// questionNoPickWord is the foot with nothing chosen yet. The emptiness law
	// forbids drawing `enter →` with no pick behind it, so the foot says what it
	// is waiting for instead of offering a key that would do nothing.
	questionNoPickWord = "nothing chosen yet"
	// questionFilledWord is the same slot on a question whose answer is a SHAPE
	// rather than a list — a sentence with holes, a run of pairs, a dial. There
	// is nothing to choose on one of those and "nothing chosen yet" would be a
	// foot describing a decision the person is not being asked to make.
	questionFilledWord = "enter when it reads right"
	// questionDecideWord is what `d` shows BEFORE it hands over, which is
	// DESIGN.md's own requirement: the pick and its reason first, then the key
	// again. Handing a decision back sight-unseen is how a person finds out later
	// that they agreed to something.
	questionDecideWord      = "it would take "
	questionDecideAgain     = "d again to let it · any other key to keep deciding"
	questionDecideKindWord  = "it would answer every "
	questionDecideKindTail  = " like this one from now on, in this project"
	questionDecideKindAgain = "D again to set it · any other key to leave it alone"
	questionDecideKindDone  = "it answers questions like this one from now on · this project only"
	questionDecideKindGone  = "this conversation has nowhere to keep that setting — it is kept per project"
	// questionAskBackWord is the prompt under an option after `?`. ONE exchange
	// per option is the bound DESIGN.md sets, and the row says so rather than
	// letting a person discover it by being refused.
	questionAskBackWord = "ask it one thing about this answer, then enter"
	questionAskedWord   = "already asked about this one"
	// questionCommentWord is the prompt under whatever `c` is annotating.
	questionCommentWord = "say what you think about this one, then enter"
	// questionReframeWord is `n`: the answer that is not on the list.
	questionReframeWord = "the real question is…"
	// questionCompareSame is what the compare table says when the answers do not
	// actually differ on any axis the asker gave. Drawing an empty table would be
	// the emptiness law broken in the most confusing possible place.
	questionCompareSame = "these answers do not differ on anything it measured"
	questionCompareNone = "it did not say what to compare these on"
	questionCompareOnly = "only what differs is here"
)

// questionDoor is the engine's ONE door for an answer (internal/session's
// [session.Agent.ResolveQuestion]), asserted at the moment an answer is spent
// rather than required of every agent.
//
// It is asserted for room.go's reason: a session that can draw a question but
// cannot resolve one — a hosted read-only view, a test harness — must lose the
// ANSWERING and not the page. Where it is absent the foot says so and the page
// is still readable, which is the honest shape: the evidence is the larger half
// of what a person opened this for.
type questionDoor interface {
	ResolveQuestion(answer session.Answer) error
}

// questionDialDoor is `D`: let it decide every question of THIS SHAPE from now
// on ([session.Agent.SetAutonomy], lane E2).
//
// IT IS A SECOND OPTIONAL INTERFACE AND NOT A METHOD ON THE ONE ABOVE, for
// room.go's stated reason about widening an interface: a session that can
// resolve one question but has no project to keep a setting in must lose the
// SETTING and not the answering. Where it is absent the key says so plainly
// rather than doing nothing, because it is in the shared grammar and a key that
// silently declines is a key a person presses three times.
type questionDialDoor interface {
	SetAutonomy(kind session.AskKind, policy session.Policy) error
}

// questionAsk is the sentence an ask-back sends and the reply it got.
//
// IT IS BOUNDED AT ONE PER ANSWER, which is [session.Exchange]'s own law: a
// question that turned into a conversation is a question that should have been a
// conversation, and the way to have one is to close this and talk. The room
// refuses a second `?` on an option that already has one and says why.
type questionAsk struct {
	asked string
	// replied is the answer, and it is EMPTY while the answer is still arriving:
	// what is on screen is read live off the transcript entry named by [reply],
	// and this field is only filled at the moment the exchange is written into
	// the record.
	//
	// A SNAPSHOT TAKEN THE FIRST TIME THE MODEL SAID ANYTHING WAS THE FIRST BUG
	// THIS DREW: `↳ Let me` sat under the question for the rest of the turn,
	// because the reply was copied out of an entry that was still streaming.
	replied string
	// reply is the transcript entry the answer is in, or -1 for one that has not
	// come back.
	reply int
	// shown is what the row drew last time, and it is the whole of how the page
	// knows a still-streaming reply has grown: the row cache is dropped when what
	// this would draw is not what it drew, and at no other time.
	shown string
	at    time.Time
	// after is where the transcript stood when the sentence went in, so the reply
	// is looked for in what the model said AFTERWARDS and never in what it had
	// already said.
	//
	// IT IS -1 UNTIL THE SENTENCE GOES OUT, because zero is a real answer to
	// "where did the transcript stand" — the commonest one, on the first turn of
	// a session — and a sentinel that a legitimate value can equal is a sentinel
	// that swallows the first exchange anybody has.
	after int
}

// questionRoom is the page: the question, where the reader is in it, and what
// the foot has composed so far. Everything on it is the READER'S state — the
// question itself is never written to, because the same object is drawn by the
// card out in the conversation and by home, and three readers of one object
// cannot each hold a different version of it.
type questionRoom struct {
	// head is the question AND its resolver, exactly as the block was holding it
	// (question.go's [questionShown]). It travels whole rather than being taken
	// apart, because who resolves this question — a lane over the wire, or this
	// surface answering about itself — is a fact about the question and not
	// something a second reader should re-derive.
	head questionShown
	// focus is which option section the reader is on, and it is what `c` and `?`
	// act on. It is an index into the options rather than a key so an empty
	// option list is simply focus 0 on nothing.
	focus int
	// compare swaps the answers for the table `x` draws.
	compare bool
	// reading is the arrows handed to the evidence pane (`→`): `↑`/`↓` scroll it
	// rather than walking the answers, until `←` hands them back or the pointer
	// moves by any other road.
	reading bool
	// picked is what a CHECKLIST's foot would send: the rows ticked, in the order
	// they were ticked. A list of answers has no second state to hold — `enter`
	// takes the answer the pointer is on ([app.questionRoomSends]) — so a page
	// whose choice and pointer could disagree is a page that can send an answer
	// nobody is looking at.
	picked []string
	// scope is how long the answer lasts, and it is only ever one the question
	// offered ([session.Question.Scope]).
	scope session.AnswerScope
	// comments are the person's annotations, keyed by the part they sit under —
	// an option key, a blank's label, a pair's row. They go into the record as
	// [session.Answer.Comments] and never as part of the answer itself.
	comments map[string]string
	// asks are the ask-backs, keyed by option key, one each.
	asks map[string]*questionAsk
	// commenting and asking name the part the box is currently writing to, or "".
	// Exactly one of them is ever set: `c` and `?` are both "the box is pointed
	// at this part now", and a page where both were live would have to ask which.
	commenting string
	asking     string
	// reframing is `n`: the box is writing the question the person thinks should
	// have been asked ([session.Answer.Reframe]).
	reframing bool
	// deciding is `d` showing its pick and reason, waiting for the second press.
	// It is cleared by ANY other key, which is what makes the first press safe.
	deciding bool
	// decidingKind is `D` on the same terms, for the whole SHAPE of question
	// rather than this one.
	decidingKind bool
	// input is the structured input shape, when the question carries one.
	input questionInput
	// shown is when the page was first drawn, and it is the whole of the settle
	// guard: a key that arrived less than [questionSettle] after it is dropped.
	shown time.Time
	// refused is what the engine said when it would not take the answer. It is
	// drawn where the foot was, because a refusal a person cannot see is an
	// answer that silently did nothing.
	refused string
	// The reader's own positions, on room.go's terms: they are HERE so that
	// leaving restores the conversation without having moved it. offset is the
	// list's window (or the one column's), and detail is the evidence pane's.
	offset int
	detail int
	// The row cache, rebuilt on content or size and at no other time.
	rows []row
	// spots is what a press on each cached row reaches (questionpage.go's
	// [questionSpot]).
	//
	// IT IS WRITTEN BY THE DRAWING AND READ BY THE POINTER, which is the bargain
	// every hit-test on this surface keeps (hover.go's law): what lights is
	// exactly what a press acts on, because both of them are reading the row the
	// layout actually wrote.
	spots []questionSpot
	// seam is the column the two panes meet at on the cached rows, or -1.
	seam   int
	height int
	width  int
	dirty  bool
}

// questionRoomOpen reports whether a question page is up. It is the one reading
// every geometric question about the body region goes through, exactly as
// [app.roomOpen] is for a node's page.
func (a *app) questionRoomOpen() bool { return a.qroom != nil }

// questionPageShows reports whether the page is up over this very question.
func (a *app) questionPageShows(q questionShown) bool {
	return a.qroom != nil && a.qroom.head.token() == q.token()
}

// raiseQuestionRoom puts the page over the conversation.
//
// IT IS THE DOOR THE OTHER FORMS PROMOTE THROUGH (DESIGN.md: "Every form folds
// down (room → card → line → chip) and opens up (enter/o)"), so the line, the
// card, the chip and the sheet all reach it through question.go's
// [app.openQuestionRoom] with the object they were already holding, rather than
// each building a page of their own.
func (a *app) raiseQuestionRoom(head questionShown) {
	q := head.question
	room := &questionRoom{
		head:     head,
		comments: map[string]string{},
		asks:     map[string]*questionAsk{},
		scope:    questionDefaultScope(q),
		shown:    a.now(),
		dirty:    true,
		// NO SEAM UNTIL THE BODY HAS DRAWN ONE. The foot's rule reads this rather
		// than working the split out a second time, so before the first lay it
		// must say "one column" — a junction is a claim about where the body
		// split, and on the frame a page opens in the body has not answered yet.
		seam: -1,
	}
	// THE PAGE OPENS WHERE THE BLOCK'S POINTER STOOD. A person who walked to the
	// second answer and pressed `o` to read it opened the page ABOUT the second
	// answer, and the block's pointer itself started where the law puts it
	// ([questionPointerStart]: the asker's pick, or the answer that loses nothing
	// where nobody but a person may answer). `enter` takes the answer the
	// pointer is on, so the two sizes of one question may not disagree about
	// which answer that is.
	room.focus = head.pick
	room.input = newQuestionInput(q)
	a.qroom = room
	if walk := a.questionRoomWalk(); room.focus < 0 || room.focus >= walk {
		room.focus = questionPointerStart(q)
	}
	a.touch()
}

// closeQuestionRoom folds the page away WITHOUT answering anything. It is `esc`,
// and DESIGN.md is explicit about what it means: "esc is later — the question
// folds to the chip and the turn/task stays paused on it". Nothing is decided,
// nothing is lost, and the chip in the status line is where it went.
func (a *app) closeQuestionRoom() {
	if a.qroom == nil {
		return
	}
	a.qroom = nil
	a.touch()
}

// closeQuestionPage closes the page when it is open OVER THIS QUESTION, and
// leaves it alone otherwise.
//
// IT IS THE ONE DOOR FOR "THIS QUESTION IS OVER" (question.go's
// [app.closeQuestion] and [app.withdrawQuestion] both call it), because the two
// ways a question ends are the two ways a page is left standing on a decision
// that has been made: answered here or in another window, and withdrawn by the
// asker. Either way the rows underneath are gone, so a page still drawing them
// is a page answering for a question nobody is waiting on.
func (a *app) closeQuestionPage(token string) {
	if a.qroom == nil || a.qroom.head.token() != token {
		return
	}
	a.closeQuestionRoom()
}

// questionDefaultScope is the scope the foot starts on: the narrowest one the
// question offered, which is always `once` where it offered any.
//
// THE NARROWEST IS THE ONLY HONEST DEFAULT. A foot that opened on `always`
// would turn a person answering one question into a person writing a rule they
// never read — which is the same failure "RULES ARE OFFERED, VISIBLE,
// FORGETTABLE" exists to prevent, arriving through the back door.
func questionDefaultScope(q session.Question) session.AnswerScope {
	if len(q.Scope) == 0 {
		return session.ScopeOnce
	}
	best := q.Scope[0]
	rank := map[session.AnswerScope]int{
		session.ScopeOnce: 0, session.ScopeTask: 1,
		session.ScopeProject: 2, session.ScopeAlways: 3,
	}
	for _, s := range q.Scope {
		if rank[s] < rank[best] {
			best = s
		}
	}
	return best
}

// questionRoomTouched drops the row cache. Every mutation on this page goes
// through it, which is why no drawing function ever has to ask whether what it
// cached is still true.
func (a *app) questionRoomTouched() {
	if a.qroom != nil {
		a.qroom.dirty = true
	}
	a.touch()
}

// ─────────────────────────────────────────────────────────────────────────────
// What the page says about the question, in words other drawings share.

// questionBlockingWord is the third attribution row: what is paused on this
// question and what carries on regardless.
//
// THE SECOND HALF IS THE POINT. A person looking at a question wants to know
// whether the machine has stopped, and "the turn waits on it" alone reads as
// though everything has. Naming what goes on without it is the difference
// between a question a person answers now and one they can leave.
func questionBlockingWord(b session.Blocking) string {
	switch {
	case b.Turn && len(b.Tasks) > 0:
		return questionWaitsWord + questionAlsoWord + questionTasksWord(b.Tasks) + " does"
	case b.Turn:
		return questionWaitsWord
	case len(b.Tasks) > 0:
		// WHAT IS WAITING AND WHAT IS NOT, BOTH. A row that named only the work
		// that stopped reads as though everything had, and the whole reason a
		// person is told this is so they can decide whether to answer it now.
		return questionTasksWord(b.Tasks) + " waits on it" + questionSep + "the conversation" + questionGoesOnWord
	}
	return questionNothingWord
}

// questionTasksWord names the work that is waiting, or counts it past three —
// a row that listed nine ids is a row nobody reads.
func questionTasksWord(ids []string) string {
	switch {
	case len(ids) == 0:
		return ""
	case len(ids) <= 3:
		return "task " + strings.Join(ids, ", ")
	}
	return strconv.Itoa(len(ids)) + " tasks"
}

// questionConfidenceWord is how sure the asker is, in the asker's own three
// words. It is dim beside the pick and never a number: a percentage on a
// judgement is a machine pretending to have measured something.
func questionConfidenceWord(c session.Confidence) string {
	switch c {
	case session.ConfidenceSure:
		return "sure"
	case session.ConfidenceFairly:
		return "fairly sure"
	case session.ConfidenceUnsure:
		return "not sure"
	}
	return ""
}

// questionAfterIf makes the asker's own sentence read as the tail of `would
// switch if `.
//
// IT EXISTS BECAUSE A MODEL WRITES A SENTENCE AND THIS ROW IS A CLAUSE. Asked
// what would change its mind, a model answers "If you are targeting a calm
// audience…" — and the row drew `would switch if If you are targeting…`, which
// is what the owner saw on 2026-09-10. So a leading `if` the person is about to
// read twice is dropped, and the capital that opened a sentence is lowered,
// because what follows the word `if` is mid-sentence wherever it came from.
//
// A WORD WHOSE SECOND LETTER IS ALSO A CAPITAL IS LEFT ALONE — `SQLite`, `HRV`,
// an acronym the asker meant — because lowering one of those is changing what
// the asker wrote rather than how it joins on.
func questionAfterIf(change string) string {
	change = strings.TrimSpace(change)
	for _, lead := range []string{"If ", "if "} {
		if strings.HasPrefix(change, lead) {
			change = strings.TrimSpace(strings.TrimPrefix(change, lead))
			break
		}
	}
	runes := []rune(change)
	switch {
	case len(runes) == 0, !unicode.IsUpper(runes[0]):
		return change
	case len(runes) > 1 && unicode.IsUpper(runes[1]):
		return change
	}
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// questionNoteRows are the person's own marks under a part: the comment `c`
// wrote and the exchange `?` had.
//
// THE COMMENT IS IN THE PERSON'S INK AND LEADS WITH `›`, which is the composer's
// own prompt mark ([tokens.GPromptChat]) — the same mark the box draws, because
// this row IS what the box said. The reply to an ask-back leads with
// [tokens.GReplyIn] and is dim: it is the asker talking, and on this page the
// asker is always dim.
func (a *app) questionNoteRows(part string, indent, width int) []string {
	room := a.qroom
	if room == nil || part == "" {
		return nil
	}
	pad := strings.Repeat(" ", indent)
	inner := max(1, width-indent-2)
	out := make([]string, 0, 3)
	if note := strings.TrimSpace(room.comments[part]); note != "" {
		for i, line := range wrap(note, inner) {
			lead := a.pal.dim(a.icon(tokens.GPromptChat) + " ")
			if i > 0 {
				lead = "  "
			}
			out = append(out, pad+lead+a.pal.ink(line))
		}
	}
	if ask := room.asks[part]; ask != nil {
		for i, line := range wrap(strings.TrimSpace(ask.asked), inner) {
			lead := a.pal.warnBold(a.icon(tokens.GNeedsHuman)) + " "
			if i > 0 {
				lead = "  "
			}
			out = append(out, pad+lead+a.pal.ink(line))
		}
		// AN EXCHANGE THAT HAS NOT COME BACK YET DRAWS NO REPLY ROW. The emptiness
		// law on the smallest possible scale: a bare `↳` under the question is a
		// row claiming the asker answered and said nothing.
		if replied := a.questionReplyWords(ask); replied != "" {
			for i, line := range wrap(replied, inner) {
				lead := a.pal.dim(a.icon(tokens.GReplyIn) + " ")
				if i > 0 {
					lead = "  "
				}
				out = append(out, pad+lead+a.pal.dim(line))
			}
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// The foot: what the answer would be, and the keys.

// questionFootHeight is how many rows the pinned foot takes: two while the page
// is open and none otherwise, whatever the foot is saying.
//
// THERE IS NO ANSWERED HEIGHT, because an answered question has no page: the
// page folds on the answer and the receipt is the block's ([app.questionAnswer]
// says why). AND NO SHORTER ONE: a foot that was one row while it reported a
// refusal moved the whole page up a row under the person reading it.
func (a *app) questionFootHeight() int {
	if a.qroom == nil {
		return 0
	}
	return 2
}

// questionFootRows is the foot, pinned above the box exactly where every other
// question on this surface sits (view.go's chrome stack).
//
//	────────────────────────────────────────┴─── answering 1 SQLite · 1 noted ────
//	  ↑↓ choose · enter take it · esc later · → detail      c change · x compare
//
// TWO ROWS AND NEVER MORE. The first is the rule that closes the page — meeting
// the seam where there are two panes — with what `enter` would send written into
// it the way the frame writes words into an edge; the second is the keys, the
// ones that answer at the left and the ones that say something about the decision
// at the right, which is the panel's two tiers on one row (hints pick A). While
// the box is pointed at a part, or `d` is waiting for its second press, or the
// engine refused, the rule says that instead and the row under it says how to
// leave it. A foot that grew with the question would push the box off a short
// screen, which is the one thing a page that promises never to take the keyboard
// cannot do.
func (a *app) questionFootRows(width int) []string {
	room := a.qroom
	if room == nil || width <= 0 {
		return nil
	}
	// THE RULE THAT CLOSES THE PAGE IS DRAWN IN THE PAGE'S OWN FRAME, NOT THE
	// TERMINAL'S. The body is charged for the task column ([app.bodyWidth]) and
	// the foot is chrome drawn at the full frame width, so a rule spanning
	// `width` ran on under the rail while the page's top rule stopped at the
	// body's edge — two edges of one object ending in different columns. With
	// the column up on a 140-cell terminal the body is still wide enough to
	// split, which is exactly when it showed.
	rule := func(words string) string {
		return besideRule(a.pal, a.bodyWidth(), a.questionRoomSeam(), tokens.GFrameTeeUp, words)
	}
	switch {
	case room.refused != "":
		return []string{rule(a.pal.warn(room.refused)), a.questionRoomOfferRow(width)}
	case room.deciding:
		return []string{rule(a.pal.ink(a.questionDecideLine())), "  " + a.pal.dim(fit(questionDecideAgain, width-2))}
	case room.decidingKind:
		return []string{rule(a.pal.ink(a.questionDecideKindLine())), "  " + a.pal.dim(fit(questionDecideKindAgain, width-2))}
	}
	if prompt := a.questionPromptWord(); prompt != "" {
		return []string{rule(a.pal.ink(prompt)), "  " + a.pal.dim(fit(a.questionSaying(), width-2))}
	}
	return []string{rule(a.questionComposeWords()), a.questionRoomOfferRow(width)}
}

// questionRoomSeam is where the page's two panes meet, or -1 on a page of one
// column.
//
// IT IS WHAT THE BODY ACTUALLY DREW, read back rather than worked out again.
// [app.questionRoomView] stores the seam of the layout it painted, and the body
// is laid before the chrome that carries this foot ([app.chatFrameLines] calls
// bodyRows and then chrome), so the junction the foot draws cannot be a column
// the body did not split at. Recomputing it here was the same arithmetic in a
// second place, and the moment [app.questionPageListWant] changed, the two edges
// of one frame would have met the seam in different columns.
func (a *app) questionRoomSeam() int {
	if a.qroom == nil {
		return -1
	}
	return a.qroom.seam
}

// questionPromptWord is the sentence that replaces the compose words while the
// box is pointed at a part rather than at the answer: `c`, `?` and `n` each say
// what the box is writing now, because a box whose meaning changed silently is a
// box that puts a comment into the answer.
func (a *app) questionPromptWord() string {
	room := a.qroom
	switch {
	case room.commenting != "":
		return questionCommentWord
	case room.asking != "":
		return questionAskBackWord
	case room.reframing:
		return questionReframeWord
	}
	return ""
}

// questionSaying is the dim second row under a prompt: the way out of it.
func (a *app) questionSaying() string {
	return "esc " + questionKeyWord(questionLaterKey)
}

// questionRoomSends is what `enter` would send from the page right now: the
// answers and the words.
//
// IT IS ONE READING, asked by `enter` ([app.questionRoomEnter]) and by the foot
// that says what `enter` would send ([app.questionComposeWords]), so the two
// cannot disagree. On a list of answers it is the answer the pointer is on, with
// whatever is in the box as the words that go with it; on `something else…` it
// is the words alone; on a checklist it is the rows ticked.
func (a *app) questionRoomSends() ([]string, string) {
	room := a.qroom
	words := strings.TrimSpace(a.input.String())
	if room.input.kind != session.InputNone {
		return append([]string{}, room.picked...), words
	}
	if key := questionOptionKeyAt(room.head.question, room.focus); key != "" {
		return []string{key}, words
	}
	return nil, words
}

// questionComposeWords is what the foot's rule says `enter` would send:
// `answering 1 postgres · with keep the sqlite file · 1 noted · just this once`.
// It is ink where there is something to send and dim where there is not.
func (a *app) questionComposeWords() string {
	room := a.qroom
	keys, words := a.questionRoomSends()
	parts := make([]string, 0, 4)
	switch {
	case len(keys) > 0:
		parts = append(parts, questionAnsweringWord+a.questionPickedWord(keys))
	case room.input.kind == session.InputBlanks || room.input.kind == session.InputPairs ||
		room.input.kind == session.InputDial || room.input.kind == session.InputText:
		parts = append(parts, questionFilledWord)
	case words == "":
		parts = append(parts, questionNoPickWord)
	}
	if words != "" {
		parts = append(parts, questionWithWord+words)
	}
	if n := len(room.comments); n > 0 {
		parts = append(parts, strconv.Itoa(n)+questionNotesWord)
	}
	// THE SCOPE IS SAID ONLY WHERE THERE IS A CHOICE OF IT. A question that
	// offered one lifetime has no decision to show, and drawing `once` beside
	// every answer would teach people to stop reading the word.
	if len(questionScopes(room.head.question)) > 0 {
		parts = append(parts, questionScopeWord(room.scope))
	}
	line := strings.Join(parts, questionSep)
	if len(keys) == 0 && words == "" {
		return a.pal.dim(line)
	}
	return a.pal.ink(line)
}

// questionPickedWord is what would be sent, in keys and words: `1 postgres`, or
// `1 postgres, 3 a file per day` on a checklist.
func (a *app) questionPickedWord(keys []string) string {
	room := a.qroom
	words := make([]string, 0, len(keys))
	for _, key := range keys {
		if opt, ok := room.head.question.Option(key); ok && strings.TrimSpace(opt.Label) != "" {
			words = append(words, key+" "+strings.TrimSpace(opt.Label))
			continue
		}
		words = append(words, key)
	}
	return strings.Join(words, ", ")
}

// questionScopeWord is how long an answer lasts, in the person's own words.
func questionScopeWord(s session.AnswerScope) string {
	switch s {
	case session.ScopeTask:
		return "for this task"
	case session.ScopeProject:
		return "for this project"
	case session.ScopeAlways:
		return "from now on"
	}
	return "just this once"
}

// questionDecideLine is what `d` shows before it hands over: the pick and the
// asker's reason for it, so nobody delegates a decision they have not read.
func (a *app) questionDecideLine() string {
	room := a.qroom
	if room.head.question.Pick == nil {
		return questionDecideKindGone
	}
	line := questionDecideWord + room.head.question.Pick.Key
	if opt, ok := room.head.question.Option(room.head.question.Pick.Key); ok && strings.TrimSpace(opt.Label) != "" {
		line += " " + strings.TrimSpace(opt.Label)
	}
	if why := strings.TrimSpace(room.head.question.Pick.Reason); why != "" {
		line += " — " + why
	}
	return line
}

// questionDecideKindLine is what `D` shows before the second press: WHICH shape
// it would answer from now on and where the setting lives. A person handing over
// a whole class of decision has to be told which class.
func (a *app) questionDecideKindLine() string {
	return questionDecideKindWord + questionAskWord(a.qroom.head.question.Ask) + questionDecideKindTail
}

// questionAskWord is the shape of a decision in the person's own words. It is
// the object's own eight kinds, spelled as somebody would say them out loud —
// the manual's `The kinds of question` section is the same list.
func questionAskWord(ask session.AskKind) string {
	switch ask {
	case session.AskPermission:
		return "may-this-happen question"
	case session.AskChoice:
		return "which-of-these question"
	case session.AskJudgement:
		return "is-this-good-enough question"
	case session.AskClarification:
		return "what-did-you-mean question"
	case session.AskConfirmation:
		return "are-you-sure question"
	case session.AskLanding:
		return "your-call row"
	case session.AskAssumption:
		return "assumption"
	case session.AskRatify:
		return "already-done card"
	}
	return "question"
}

// questionSetDial is the second press of `D`.
//
// IT SETS THE SHAPE AND ANSWERS NOTHING. The question on screen stays open and
// still wants an answer: a person saying "you handle these from now on" has said
// something about the FUTURE, and applying it retroactively to the one in front
// of them would be the surface answering a question they were still reading.
func (a *app) questionSetDial() tea.Cmd {
	room := a.qroom
	room.decidingKind = false
	door, ok := a.agent.(questionDialDoor)
	if !ok {
		room.refused = questionDecideKindGone
		a.questionRoomTouched()
		return nil
	}
	// THE RULE IS WRITTEN FROM A COMMAND (offloop.go) and the foot says what the
	// engine said about it — which is the one thing here worth waiting to know,
	// because the engine refuses a rule over a clarification and over anything
	// destructive. What it does not do is hold the frame while it waits.
	kind := room.head.question.Ask
	return a.offLoop(func() func(bool) tea.Cmd {
		err := door.SetAutonomy(kind, session.Policy{Kind: session.PolicyDecide})
		return func(here bool) tea.Cmd {
			if !here || a.qroom == nil || a.qroom != room {
				return nil
			}
			if err != nil {
				room.refused = strings.TrimSpace(err.Error())
			} else {
				room.refused = questionDecideKindDone
				a.autonomyChanged()
			}
			a.questionRoomTouched()
			return nil
		}
	})
}

// questionOfferKeys is which of the grammar's keys this question offers, and it
// is question.go's own reading rather than a second one: the block and this page
// print from one table through one filter, so a key that is on the card is on
// the page and a key the emptiness law drops is dropped in both.
func (a *app) questionOfferKeys() []questionVerb {
	return a.questionAnswerKeys(a.qroom.head, formsRoom)
}

// questionRoomOfferRow is the foot's row of keys: the keys that answer at the
// left, and the ones that say something about the decision at the right — the
// panel's two tiers (hints pick A), laid on one row because the page has the
// width the panel's frame does not.
//
// BOTH HALVES ARE THE ONE KEY ROW ([app.questionKeyRow]), each dropped by rank
// until it fits, and the quieter half is given up whole before the half that
// answers loses anything.
func (a *app) questionRoomOfferRow(width int) string {
	head := a.qroom.head
	keys := a.questionOfferKeys()
	room := max(width-2*len(questionIndent), 1)
	left := a.questionKeyRow(head, questionKeysOnTier(keys, keyPrimary), room)
	row := questionIndent + left
	spare := room - ansi.StringWidth(left) - 2*len(questionKeyGap)
	if right, ok := a.questionKeyRowWithin(head, questionKeysOnTier(keys, keySecondary), spare); ok {
		row = questionRightAlign(row, right, width-len(questionIndent))
	}
	return row
}

// questionKeyRowWithin is the one key row at a width, or false where not even
// its most valuable key fits — a tier that would be cut mid-word is left off.
func (a *app) questionKeyRowWithin(q questionShown, keys []questionVerb, width int) (string, bool) {
	if len(keys) == 0 || width <= 0 {
		return "", false
	}
	for {
		if ansi.StringWidth(questionKeyWords(q, keys)) <= width {
			return a.paintQuestionKeys(q, keys), true
		}
		if len(keys) == 1 {
			return "", false
		}
		dropped, ok := questionDropVerb(keys)
		if !ok {
			return "", false
		}
		keys = dropped
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The keys.

// questionRoomKey routes every key while the page is up, and reports whether it
// took one.
//
// THE BOX OUTRANKS EVERY LETTER. A bare letter is an answer only while the box
// is empty — which is `x` (stop)'s own rule on this surface — so a person
// halfway through a sentence keeps every keystroke, and the page's promise not
// to take the keyboard is kept literally rather than nearly.
func (a *app) questionRoomKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	room := a.qroom
	if room == nil {
		return nil, false
	}
	key := msg.String()
	// THE SETTLE GUARD, and it is read before anything else so that no path can
	// skip it. A key that arrived less than [questionSettle] after the page was
	// drawn was aimed at whatever was on screen before it.
	if a.now().Sub(room.shown) < questionSettle {
		return nil, true
	}
	// The box is answering with words. `enter` sends what it holds — as the
	// comment, the ask-back, the reframe or the words beside the pick — and every
	// other key is text.
	typing := strings.TrimSpace(a.input.String()) != ""
	switch key {
	case "esc":
		// ESC LEAVES THE PART BEFORE IT LEAVES THE PAGE. A person who pressed `c`
		// and changed their mind is asking for the comment to go away, not for the
		// question to fold — and a single esc that did both would lose the page
		// they were reading.
		if room.commenting != "" || room.asking != "" || room.reframing || room.deciding {
			room.commenting, room.asking, room.reframing, room.deciding = "", "", false, false
			a.questionRoomTouched()
			return nil, true
		}
		a.closeQuestionRoom()
		return nil, true
	case "enter":
		return a.questionRoomEnter(), true
	case "up", "down":
		// WHILE THE ARROWS ARE THE EVIDENCE PANE'S (`→`) THEY SCROLL IT, and
		// everywhere else they walk the answers.
		if delta := map[string]int{"up": -1, "down": 1}[key]; room.reading {
			a.questionRoomScrollDetail(delta)
		} else {
			a.questionMoveFocus(delta)
		}
		return nil, true
	}
	// AND THE BOX IS A BOX WHILE IT IS POINTED AT A PART. `c`, `?` and `n` each
	// hand the box to something — a note on one answer, one thing to ask back,
	// the question that should have been asked — and from that moment every key
	// but `enter` and `esc` is TEXT.
	//
	// IT WAS NOT, AND WATCHING IT COST THE FIRST TWO LETTERS OF EVERY COMMENT: a
	// person who pressed `c` and typed "only if…" lost the `o` to the fold and
	// the `n` to the reframe, because the box was still empty and the page was
	// still reading letters as keys. A prompt that says "type it" has to mean it
	// on the first keystroke, not on the third.
	if typing || room.commenting != "" || room.asking != "" || room.reframing {
		return nil, false
	}
	// ANY KEY BUT `d` PUTS THE HAND BACK ON THE WHEEL. The you-decide row is a
	// promise about the NEXT keystroke, and reaching for any other key is that
	// promise being answered.
	if room.deciding && key != "d" {
		room.deciding = false
		a.questionRoomTouched()
	}
	if room.decidingKind && key != "D" {
		room.decidingKind = false
		a.questionRoomTouched()
	}
	// A REFUSAL IS SHOWN UNTIL THE NEXT KEY AND NOT A MOMENT LONGER. It stands
	// where the foot was, so a page that kept one would be a page whose keys are
	// invisible; and the next keypress is the person having read it.
	if room.refused != "" {
		room.refused = ""
		a.questionRoomTouched()
	}
	// `→` HANDS THE ARROWS TO THE EVIDENCE PANE AND `←` HANDS THEM BACK, which
	// is the sideways half of the walk `↑`/`↓` began: the pane beside the list
	// can be taller than the page, and a diagram whose end is below the fold is
	// read by moving INTO the pane rather than by reaching for the wheel.
	//
	// IT IS READ AFTER THE BOX AND BEFORE THE REST OF THE TABLE for two separate
	// reasons. After the box, because `←` inside a sentence somebody is typing is
	// the caret and nothing else. Before the rest, because where the question
	// carries a shape of its own — a dial, a hole with choices — the side arrows
	// are that shape's ([needMoves]), and the two can never both be true on one
	// page. A side arrow the foot is not offering does nothing at all, which is
	// the page's law about keys it did not draw.
	if room.input.kind == session.InputNone && (key == "left" || key == "right") {
		if a.questionRoomOffers(key) {
			room.reading = key == "right"
			a.questionRoomTouched()
		}
		return nil, true
	}
	// NO KEY DOES ANYTHING THAT IS NOT DRAWN ON SCREEN RIGHT NOW, which is the
	// law question.go holds its block to and this page holds itself to through
	// the same reading: [app.questionAnswerKeys] is what the foot printed, and a
	// key that is not on it falls through to the box as the letter it is.
	//
	// THE DIGITS ARE THE ONE EXCEPTION AND THE TABLE SAYS WHY: `1`–`9` are not in
	// it, because each answer's own row carries its digit and a table entry
	// saying "1 — the first answer" would be furniture.
	if !a.questionRoomOffers(key) {
		if key >= "1" && key <= "9" {
			a.questionRoomPick(key)
			a.questionRoomTouched()
			return nil, true
		}
		return nil, false
	}
	if a.questionInputKey(key) {
		a.questionRoomTouched()
		return nil, true
	}
	switch key {
	case questionCompareKey:
		room.compare = !room.compare
	case questionCommentKey:
		room.commenting, room.asking, room.reframing = a.questionFocusKey(), "", false
	case questionAskBackKey:
		a.questionStartAsk()
	case questionReframeKey:
		room.reframing, room.commenting, room.asking = true, "", ""
	case questionDecideKey:
		if room.deciding {
			room.deciding = false
			return a.questionHandOver(), true
		}
		room.deciding = true
	case questionDialKey:
		// TWO PRESSES HERE TOO, AND FOR A SHARPER REASON THAN `d`. That key hands
		// over ONE decision and this one hands over every decision of a shape from
		// now on, so the first press says which shape and what it would do with
		// it, and only the second writes anything down.
		if room.decidingKind {
			room.decidingKind = false
			head := room.head
			a.closeQuestionRoom()
			return a.questionDial(head), true
		}
		room.decidingKind = true
	case questionRuleKey:
		head := room.head
		a.closeQuestionRoom()
		return a.questionMakeRule(head), true
	case questionUndoKey:
		head := room.head
		a.closeQuestionRoom()
		return a.questionUndo(head), true
	case questionScopeKey:
		// `t` CHANGES THE LIFETIME AND NOTHING ELSE, exactly as it does on the
		// panel (questionscope.go): nothing is written and nothing is answered,
		// and the foot's rule says the lifetime the answer would go out with.
		room.scope = questionScopeAfter(room.head.question, room.scope)
	case questionLaterKey:
		a.closeQuestionRoom()
		return nil, true
	default:
		return nil, false
	}
	a.questionRoomTouched()
	return nil, true
}

// questionRoomOffers is whether the foot actually printed this key. It is
// [app.questionAnswerKeys] read a second time rather than a second list, which
// is what keeps a key drawn and a key taken from coming apart.
func (a *app) questionRoomOffers(key string) bool {
	for _, verb := range a.questionAnswerKeys(a.qroom.head, formsRoom) {
		if verb.key == key {
			return true
		}
		// Two rows of the table are SPELLINGS of a pair of keys each — `←→` and
		// `shift+↑↓` are one affordance apiece — and the routing reads the
		// individual keys. `tab` carries its shifted twin for the same reason.
		switch verb.key {
		case questionWalkKey:
			if key == "left" || key == "right" {
				return true
			}
		case questionWalkDownKey:
			if key == "up" || key == "down" {
				return true
			}
		case questionOrderKey:
			if key == "shift+up" || key == "shift+down" {
				return true
			}
		case questionDetailKey:
			if key == "right" {
				return true
			}
		case questionAnswersKey:
			if key == "left" {
				return true
			}
		case questionBlankKey:
			if key == "shift+tab" {
				return true
			}
		}
	}
	return false
}

// questionMoveFocus walks the answers, and it is bounded rather than wrapping:
// a list that jumps from the last row to the first is a list a person loses
// their place in.
func (a *app) questionMoveFocus(delta int) {
	room := a.qroom
	n := a.questionRoomWalk()
	if n == 0 {
		return
	}
	at := questionClamp(room.focus+delta, 0, n-1)
	if room.input.kind != session.InputNone {
		room.input.focus = at
	}
	room.focus = at
	a.questionRoomShowFocus()
	a.questionRoomTouched()
}

// questionRoomScrollDetail moves the evidence pane's window by one row while the
// arrows are the pane's, bounded at both ends.
func (a *app) questionRoomScrollDetail(delta int) {
	room := a.qroom
	lay := a.questionRoomLay(a.bodyWidth(), a.viewHeight())
	room.detail = questionWindowStart(room.detail+delta, len(lay.pane), lay.region)
	a.questionRoomTouched()
}

// questionScopeAfter is the lifetime after `now` in the question's own list of
// them, narrowest first, coming back round to the narrowest after the widest.
// It is the panel's walk (questionscope.go's [questionScopes]) read from the
// page's own state.
func questionScopeAfter(q session.Question, now session.AnswerScope) session.AnswerScope {
	scopes := questionScopes(q)
	for i, one := range scopes {
		if one == now {
			return scopes[(i+1)%len(scopes)]
		}
	}
	if len(scopes) > 0 {
		return scopes[0]
	}
	return now
}

// questionFocusKey is the part the focus is on, as the key a comment is filed
// under.
func (a *app) questionFocusKey() string {
	room := a.qroom
	if room.input.kind != session.InputNone {
		return room.input.partKey()
	}
	if room.focus < 0 || room.focus >= len(room.head.question.Options) {
		return ""
	}
	return room.head.question.Options[room.focus].Key
}

// questionRoomPick is a digit on the page: the pointer goes straight to that
// answer, and the evidence beside the list is that answer's.
//
// ON THE PAGE A DIGIT MOVES AND DOES NOT SEND. The page is where a person reads
// before deciding, and the digit is the fastest way to put an answer's evidence
// in front of them; `enter` is the decision. A CHECKLIST TICKS instead, because
// pressing 2 on a list of things to tick means "and 2".
func (a *app) questionRoomPick(key string) {
	room := a.qroom
	opt, ok := room.head.question.Option(key)
	if !ok {
		return
	}
	if at, found := questionOptionAt(room.head.question, key); found {
		room.focus = at
	}
	if room.input.kind == session.InputChecklist {
		room.picked = questionToggleOne(room.picked, opt.Key)
		return
	}
	room.reading = false
	a.questionRoomShowFocus()
}

// questionToggleOne adds a key or takes it away, keeping the order the person
// pressed them in — which is the order a checklist that cares about order means.
func questionToggleOne(keys []string, key string) []string {
	for i, k := range keys {
		if k == key {
			return append(append([]string{}, keys[:i]...), keys[i+1:]...)
		}
	}
	return append(append([]string{}, keys...), key)
}

// questionStartAsk points the box at one answer, once.
func (a *app) questionStartAsk() {
	room := a.qroom
	part := a.questionFocusKey()
	if part == "" {
		return
	}
	if room.asks[part] != nil {
		// ONE EXCHANGE PER ANSWER is [session.Exchange]'s bound, and the refusal
		// says so rather than doing nothing: a key that silently declines is a key
		// a person presses again.
		room.refused = questionAskedWord
		return
	}
	room.asking, room.commenting, room.reframing = part, "", false
}

// questionRoomEnter spends whatever the box is pointed at.
//
// FOUR THINGS ENTER CAN MEAN, and which one it means is never guessed: the page
// says in its foot which part the box is writing to, so `enter` is always the
// end of the sentence a person can already see they are writing.
func (a *app) questionRoomEnter() tea.Cmd {
	room := a.qroom
	said := strings.TrimSpace(a.input.String())
	switch {
	case room.commenting != "":
		if said != "" {
			room.comments[room.commenting] = said
			a.input.reset()
		}
		room.commenting = ""
		a.questionRoomTouched()
		return nil
	case room.asking != "":
		if said == "" {
			room.asking = ""
			a.questionRoomTouched()
			return nil
		}
		part := room.asking
		room.asks[part] = &questionAsk{asked: said, at: a.now(), after: -1, reply: -1}
		room.asking = ""
		a.input.reset()
		a.questionRoomTouched()
		// THE QUESTION STAYS OPEN WHILE THE ASKER ANSWERS. DESIGN.md gives two
		// seams for this and the ordinary turn is the one that exists today: the
		// sentence goes to the model with the question still up, and the reply
		// lands back on the row through [app.questionReply].
		return a.askBackCmd(part, said)
	case room.reframing:
		if said == "" {
			room.reframing = false
			a.questionRoomTouched()
			return nil
		}
		return a.questionAnswer(session.Answer{Reframe: said, DecidedBy: session.DecidedByPerson})
	}
	// ENTER SENDS WHAT THE FOOT SAYS IT WOULD ([app.questionRoomSends]): the
	// answer the pointer is on with whatever is in the box beside it, which is
	// the block's own law arriving on the page (#789: "every answer has a
	// pointer the arrows walk and enter takes"). It costs nothing on a page
	// nobody has walked, because the page opens where the block's pointer stood.
	//
	// THE EMPTINESS LAW ON THE MOST IMPORTANT KEY ON THE PAGE: `enter` with
	// nothing to send — `something else…` and an empty box — does nothing at
	// all, and the foot already says `nothing chosen yet`.
	keys, words := a.questionRoomSends()
	if len(keys) == 0 && words == "" && room.input.kind == session.InputNone {
		return nil
	}
	return a.questionAnswer(session.Answer{Picked: keys, Change: words, DecidedBy: session.DecidedByPerson})
}

// questionHandOver is the second press of `d`: the asker's own pick, recorded as
// the ASKER's decision and never as the person's.
//
// `DecidedBy` IS THE FIELD THAT MAKES THE RECORD WORTH KEEPING. A line that said
// a person chose what a person handed over is the one thing a record must never
// do, and it is why this path exists at all rather than simply pressing the
// pick's digit.
func (a *app) questionHandOver() tea.Cmd {
	room := a.qroom
	if room.head.question.Pick == nil {
		return nil
	}
	return a.questionAnswer(session.Answer{
		Picked:    []string{room.head.question.Pick.Key},
		DecidedBy: session.DecidedByAsker,
	})
}

// questionAnswer fills in everything the page knows and spends it through the
// engine's one door.
func (a *app) questionAnswer(answer session.Answer) tea.Cmd {
	room := a.qroom
	if room == nil {
		return nil
	}
	answer.At = a.now()
	answer.Kind = room.head.question.Kind
	answer.ID = room.head.question.ID
	answer.Ref = room.head.question.Ref
	answer.Ask = room.head.question.Ask
	answer.Scope = room.scope
	if len(answer.Picked) > 0 {
		answer.Key = answer.Picked[0]
	}
	if len(room.comments) > 0 {
		answer.Comments = map[string]string{}
		for part, note := range room.comments {
			answer.Comments[part] = note
		}
	}
	for _, part := range questionSortedKeys(room.asks) {
		ask := room.asks[part]
		// THE SNAPSHOT IS TAKEN HERE AND NOWHERE EARLIER. Up to this moment the
		// reply on screen is whatever the entry holds right now; the record wants
		// what it held when the question was answered.
		ask.replied = a.questionReplyWords(ask)
		answer.AskedBack = append(answer.AskedBack, session.Exchange{
			Option: part, Asked: ask.asked, Replied: ask.replied, At: ask.at,
		})
	}
	room.input.fill(&answer)
	if _, ok := a.agent.(questionDoor); !ok {
		room.refused = questionNoDoorWord
		a.questionRoomTouched()
		return nil
	}
	// AND THE DOOR IS ASKED THROUGH THE ONE ANSWERING ROAD (question.go's
	// [app.answerQuestions]), which is where the sent stamp, the record, the
	// off-loop call and the reopen all live. This page had a copy of every one
	// of those beside it, and a copy of a road is a second set of rules about
	// what an answer does the first day one of them moves.
	sent := a.answerQuestion(room.head, answer)
	a.input.reset()
	// THE ANSWER IS THE RECORD, AND THERE IS ONE RECORD.
	//
	// This page used to keep its own account of what was decided and leave it
	// where the foot had been, while the block — which still held the question,
	// because nothing here had told it otherwise — wrote the receipt as well. So
	// one answer left two adjacent lines about itself, in two different
	// spellings, and the block's said `another window` about a key pressed on
	// this one: the answer came back down the questions lane, found the question
	// still open here, and [app.foldOthersAnswer] read it — rightly — as
	// somebody else's.
	//
	// The block's is the one that survives, because it is the one a person sees
	// wherever they answered from: it is [session.DecisionRecord.Line], the
	// engine's own rendering, so the model's record and the row above the box
	// are one account of one decision (question.go's THE ANSWER IS THE RECORD).
	// The page's whole job is over at this point, so it folds and the receipt is
	// waiting underneath it. Measured on a real screen before either half of
	// this landed: an answer given on this page, in this window, drew
	// `another window` on its own receipt.
	a.closeQuestionRoom()
	return sent
}

// questionNoDoorWord is what the foot says on a session that can draw a question
// and not resolve one. It says what is true — the page can be read and not
// answered from here — rather than failing silently on a keypress.
const questionNoDoorWord = "this window can read the question but not answer it — answer it in the conversation that raised it"

// questionSortedKeys orders a map of parts so the record is written in the same
// order every time. A record whose lines shuffle between runs is a record two
// readers cannot diff.
func questionSortedKeys(m map[string]*questionAsk) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// questionReplyWords is what the asker said back, read LIVE off the transcript
// entry the reply is in — so the row fills in as the answer arrives rather than
// freezing on whatever the first frame caught.
//
// Once the exchange has been written into a record it is that record's words,
// because the transcript underneath is a conversation that has gone on since.
func (a *app) questionReplyWords(ask *questionAsk) string {
	if ask == nil {
		return ""
	}
	if ask.replied != "" {
		return ask.replied
	}
	if ask.reply < 0 || ask.reply >= len(a.entries) {
		return ""
	}
	return strings.TrimSpace(a.entries[ask.reply].text)
}

// questionHasDimensions reports whether the asker gave axes to compare the
// answers on, or enough `+`/`−` consequence lines to derive them. It is the
// emptiness law's gate on `x`: no dimensions, no compare.
func questionHasDimensions(q session.Question) bool {
	axes := 0
	for _, opt := range q.Options {
		axes += len(opt.Dimensions)
	}
	if axes > 0 {
		return true
	}
	for _, opt := range q.Options {
		if questionConsequenceAxes(opt) > 0 {
			return true
		}
	}
	return false
}

// askBackCmd sends one sentence to the asker WITH THE QUESTION STILL OPEN.
//
// DESIGN.md gives this two seams — "sent to the asker through ResolveQuestion's
// AskedBack seam (E1) or the ordinary turn with the question still open" — and
// BOTH now exist. [app.askBack] is the one place that chooses between them, and
// it chooses by asking [session.AnswerResolves] rather than by listing lanes, so
// the page and the block cannot end up sending the same gesture two ways.
//
// THE ROW REMEMBERS WHERE THE TRANSCRIPT WAS, down either road. That is the
// whole of how the reply is found: everything the model says after the sentence
// went in is a candidate, and the first settled thing it says is the answer.
// Nothing is parsed and nothing is guessed — a person can read both rows and see
// for themselves. The seam does not change that, because what it returns is the
// parked call's RESULT: the model answers in its ordinary reply, in the
// transcript, where this was already looking (tools_ask.go's `askedBackLead`).
func (a *app) askBackCmd(part, text string) tea.Cmd {
	room := a.qroom
	if room == nil {
		return nil
	}
	if ask := room.asks[part]; ask != nil {
		ask.after = len(a.entries)
	}
	return a.askBack(room.head, part, text)
}

// questionDrainReplies fills in any ask-back whose answer has since arrived. It
// is called from the draw rather than from the stream because it is a READING of
// the transcript and not an event: a page that subscribed to the turn would be a
// second subscriber to a lane that already has one.
func (a *app) questionDrainReplies() bool {
	room := a.qroom
	if room == nil {
		return false
	}
	moved := false
	for _, ask := range room.asks {
		if ask.after < 0 {
			continue
		}
		if ask.reply >= 0 {
			// ALREADY FOUND, AND STILL GROWING. The row is redrawn from the entry
			// whenever what it would say has changed, which is why the entry INDEX
			// is what is kept and not a copy of its words.
			if words := a.questionReplyWords(ask); words != ask.shown {
				ask.shown, moved = words, true
			}
			continue
		}
		for i := ask.after; i < len(a.entries); i++ {
			e := a.entries[i]
			if e.kind != entryAssistant || strings.TrimSpace(e.text) == "" {
				continue
			}
			ask.reply, ask.shown = i, strings.TrimSpace(e.text)
			moved = true
			break
		}
	}
	return moved
}
