package tui3

import (
	"sort"
	"strconv"
	"strings"
	"time"

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
// ── THE ROWS, DRAWN HERE SO THEY STAY TRUE ──
//
// The body region, at 92 columns, a choice with three answers and the first one
// open. `?` is [tokens.GNeedsHuman] and is the only amber on the page besides
// the foot; every attribution, reason and confidence is dim; option bodies are
// ink.
//
//	‹ back                                                     question · 1 of 1
//	? which store should the ledger sit on?
//	  the model · a schema change is next and it is cheaper before rows exist
//	  the turn waits on it · task 12 goes on without it
//
//	  ▾ 1 postgres                                         my pick · fairly sure
//	      rows already carry a foreign key into it, and the migration is one file
//	      + one place to back up
//	      − another service to run locally
//	      ┌ what it would look like ─────────────
//	      │  app ──▶ pg ──▶ report
//	      └──────────────────────────────────────
//	      because it is the only store the reporting job already reads
//	      would switch if the ledger ever has to run without a server
//	      › keep the sqlite file as the source of truth
//	      ? does the reporting job read it directly
//	      ↳ it does, over the same connection string
//	  ▸ 2 sqlite
//	  ▸ 3 a file per day
//
// And the foot, pinned above the box exactly where every other question on this
// surface sits (view.go's chrome stack):
//
//	answering 1 postgres · with keep the sqlite file · once
//	1–9 pick · enter take the pick · x compare · c comment · ? ask it · esc later
//
// ── ALIGNMENT, STATED ONCE ──
//
// DESIGN.md: "Head at the gutter; option rows indented one key-cell; answers row
// indented like the options". So the head and the back crumb start at column 0,
// everything under them is indented [questionIndent], and an option's own body
// is indented one further key-cell so the word and the body cannot be misread as
// two answers. The three widths below are the whole of the page's geometry and
// nothing computes its own.

const (
	// questionIndent is the one key-cell every row under the head is indented by.
	questionIndent = "  "
	// questionBodyIndent is an option's body, one key-cell further in: the digit,
	// the space and the fold mark are what it clears.
	questionBodyIndent = "      "
	// questionRoomFloor is the narrowest page that still draws sections. Below it
	// there is no room for a fold mark, a digit and a word, and the page draws
	// the linear shape every form owes the reader tier.
	questionRoomFloor = 34
	// questionCompareFloor is where the compare table stops being a table.
	// DESIGN.md: "compare stacks under 80 cols".
	questionCompareFloor = 80
	// questionLayoutFloor is where a two-pane layout block stops being two panes.
	// DESIGN.md: "two pre-formatted panes side by side above 100 cols, stacked
	// below".
	questionLayoutFloor = 100
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
	// questionBackWord is the crumb out. It names `esc` because a page a person
	// walked into has to say how they leave it, which is room.go's own rule.
	questionBackWord = "back"
	// questionPickWord marks the asker's own pick. It is the asker saying which
	// one it would take, never a default that happens on its own — a question
	// with a clock says so in its own row (S1's card), and this page never
	// pretends the pick is one.
	questionPickWord = "my pick"
	// questionWouldSwitchWord opens the line that is the most useful thing on the
	// page: what would change the asker's mind. A person who disagrees with a
	// pick usually disagrees with exactly this.
	questionWouldSwitchWord = "would switch if "
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
	// open says which sections are expanded. A room OPENS ON ITS PICK — the one
	// answer a person most wants to read is the one the asker would take — and
	// everything else starts folded, which is what makes the page a page rather
	// than a wall.
	open map[int]bool
	// compare swaps the sections for the table `x` draws.
	compare bool
	// picked is what the foot would send. It is a slice because a checklist and a
	// run of pairs both answer with several keys, and [session.Answer.Picked] is
	// the field that carries them.
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
	// The reader's own position, on room.go's terms: it is HERE so that leaving
	// restores the conversation without having moved it.
	offset int
	// The row cache, rebuilt on content or width and at no other time.
	rows  []row
	width int
	dirty bool
}

// questionRoomOpen reports whether a question page is up. It is the one reading
// every geometric question about the body region goes through, exactly as
// [app.roomOpen] is for a node's page.
func (a *app) questionRoomOpen() bool { return a.qroom != nil }

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
		open:     map[int]bool{},
		comments: map[string]string{},
		asks:     map[string]*questionAsk{},
		scope:    questionDefaultScope(q),
		shown:    a.now(),
		dirty:    true,
	}
	// THE PAGE OPENS ON THE PICK. Every other section starts folded, so what a
	// person meets is the asker's own answer with its reason and its evidence
	// under it, and the alternatives one key away — which is the order they will
	// read them in anyway.
	if q.Pick != nil {
		for i, opt := range q.Options {
			if opt.Key == q.Pick.Key {
				room.focus, room.open[i] = i, true
			}
		}
	}
	room.input = newQuestionInput(q)
	a.qroom = room
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
// The page.

// questionRoomRows is the whole body region: the crumb, the head, the
// attribution, and then either the option sections or the compare table.
func (a *app) questionRoomRows(width int) []row {
	room := a.qroom
	if room == nil || width <= 0 {
		return nil
	}
	if a.questionDrainReplies() {
		room.dirty = true
	}
	if !room.dirty && room.width == width && room.rows != nil {
		return room.rows
	}
	out := make([]row, 0, 32)
	add := func(text string) { out = append(out, row{text: text, entry: -1}) }

	add(a.questionCrumb(width))
	for _, line := range a.questionHeadRows(width) {
		add(line)
	}
	add("")
	switch {
	case room.compare:
		for _, line := range a.questionCompareRows(width) {
			add(line)
		}
	case room.input.kind != session.InputNone:
		// A STRUCTURED INPUT REPLACES THE OPTION SECTIONS AND NEVER SITS UNDER
		// THEM. DESIGN.md's ladder puts structured input one rung BELOW the
		// question — blanks, a checklist, this-or-this, a dial are the answer
		// itself, not a garnish on a list of answers — so a question that carries
		// one is drawn as that shape and the options, where it has any, are the
		// shape's own rows.
		for _, line := range a.questionInputRows(width) {
			add(line)
		}
	default:
		for _, line := range a.questionOptionRows(width) {
			add(line)
		}
	}
	for _, line := range a.questionAttachRows(width) {
		add(line)
	}
	room.rows, room.width, room.dirty = out, width, false
	return out
}

// questionCrumb is the way out and where you are, on one row: the crumb at the
// gutter and the question's place in what is open on the right.
func (a *app) questionCrumb(width int) string {
	back := a.pal.dim(a.icon(tokens.GScopeUp) + " " + questionBackWord)
	if width < questionRoomFloor {
		return fit(back, width)
	}
	return back
}

// questionHeadRows are the head and the attribution under it, and they are the
// contract DESIGN.md writes for this page: "asked by · why now · what is paused
// on it · what goes on without it".
//
// ALL FOUR OR AS MANY AS ARE TRUE. The emptiness law governs every one of them —
// a question with no reason draws no reason line, and one that nothing is
// waiting on says so rather than leaving a person to guess whether the silence
// means everything or nothing.
func (a *app) questionHeadRows(width int) []string {
	room := a.qroom
	if room == nil {
		return nil
	}
	out := make([]string, 0, 4)
	mark := a.pal.ask(a.icon(tokens.GNeedsHuman))
	head := strings.TrimSpace(room.head.question.Head)
	lines := wrap(head, max(1, width-2))
	for i, line := range lines {
		if i == 0 {
			out = append(out, mark+" "+a.pal.ink(line))
			continue
		}
		out = append(out, questionIndent+a.pal.ink(line))
	}
	// The asker and the reason share a row: they are one sentence — who is asking
	// and why now — and two rows for it would push the answers off a short page.
	attribution := questionAskerWord(room.head.question.Asker)
	if reason := strings.TrimSpace(room.head.question.Reason); reason != "" {
		if attribution != "" {
			attribution += questionSep
		}
		attribution += reason
	}
	if attribution != "" {
		for _, line := range wrap(attribution, max(1, width-len(questionIndent))) {
			out = append(out, questionIndent+a.pal.dim(line))
		}
	}
	if blocking := questionBlockingWord(room.head.question.Blocking); blocking != "" {
		out = append(out, questionIndent+a.pal.dim(fit(blocking, max(1, width-len(questionIndent)))))
	}
	return out
}

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

// questionOptionRows draws the answers as sections.
//
// ONE ROW PER ANSWER WHEN FOLDED, and the whole of it when open: the fold mark,
// the digit, the word, and — on the asker's own pick — `my pick` with its
// confidence, dim, on the right. Opening one adds its body, its consequence
// lines, its blocks, and, on the pick, the reason and what would change its mind.
func (a *app) questionOptionRows(width int) []string {
	room := a.qroom
	if room == nil || len(room.head.question.Options) == 0 {
		return nil
	}
	out := make([]string, 0, len(room.head.question.Options)*4)
	for i, opt := range room.head.question.Options {
		out = append(out, a.questionOptionHead(i, opt, width))
		if !room.open[i] {
			continue
		}
		out = append(out, a.questionOptionBody(i, opt, width)...)
	}
	return out
}

// questionOptionHead is the one row an answer always has.
func (a *app) questionOptionHead(i int, opt session.AnswerOption, width int) string {
	room := a.qroom
	mark := tokens.GCollapsed
	if room.open[i] {
		mark = tokens.GExpanded
	}
	lead := questionIndent + a.pal.dim(a.icon(mark)) + " "
	key := opt.Key
	if key == "" {
		key = strconv.Itoa(i + 1)
	}
	word := a.pal.ink(key + " " + strings.TrimSpace(opt.Label))
	if a.questionTaken(opt.Key) {
		// WHAT THE FOOT WOULD SEND IS MARKED ON THE ROW ITSELF, because a foot
		// that names a key and a list that does not show it is two places to look
		// for one fact. It is the question hue, which on this page means exactly
		// "this is the thing waiting on you".
		word = a.pal.askBold(key + " " + strings.TrimSpace(opt.Label))
	}
	row := lead + word
	// The pick's badge is right-aligned, dim, and cut before the word is: a page
	// too narrow to say `my pick · fairly sure` still has to say which answers
	// there are.
	if room.head.question.Pick != nil && room.head.question.Pick.Key == opt.Key {
		badge := questionPickWord
		if c := questionConfidenceWord(room.head.question.Pick.Confidence); c != "" {
			badge += questionSep + c
		}
		row = questionRightAlign(row, a.pal.dim(badge), width)
	}
	if i == room.focus {
		row = a.pal.cursor(fit(row, width), width)
	}
	return fit(row, width)
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

// questionOptionBody is everything under an open answer, in the order a person
// reads it: what it means, what it costs, what it would look like, and — where
// this is the pick — why the asker would take it and what would change its mind.
func (a *app) questionOptionBody(i int, opt session.AnswerOption, width int) []string {
	room := a.qroom
	inner := max(1, width-len(questionBodyIndent))
	out := make([]string, 0, 8)
	// AN ANSWER WITH NO BODY DRAWS NO ROW FOR ONE. [wrap] answers an empty string
	// with a single empty line — which is right for a paragraph and wrong here,
	// where it would be a blank row claiming the asker said something.
	if body := strings.TrimSpace(opt.Body); body != "" {
		for _, line := range wrap(body, inner) {
			out = append(out, questionBodyIndent+a.pal.ink(line))
		}
	}
	// The consequence is one line and it is in the future tense: what happens if
	// this one is taken. It wears the `+` of a diff because that is what it is —
	// a thing that will be true afterwards.
	if c := strings.TrimSpace(opt.Consequence); c != "" {
		for _, line := range wrap(c, inner) {
			out = append(out, questionBodyIndent+a.pal.dim(line))
		}
	}
	for _, block := range opt.Blocks {
		out = append(out, a.questionBlockRows(block, len(questionBodyIndent), width)...)
	}
	if room.head.question.Pick != nil && room.head.question.Pick.Key == opt.Key {
		if why := strings.TrimSpace(room.head.question.Pick.Reason); why != "" {
			for _, line := range wrap(why, inner) {
				out = append(out, questionBodyIndent+a.pal.dim(line))
			}
		}
		if change := strings.TrimSpace(room.head.question.Pick.WouldChange); change != "" {
			for _, line := range wrap(questionWouldSwitchWord+change, inner) {
				out = append(out, questionBodyIndent+a.pal.dim(line))
			}
		}
	}
	out = append(out, a.questionNoteRows(opt.Key, len(questionBodyIndent), width)...)
	_ = i
	return out
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
			lead := a.pal.ask(a.icon(tokens.GNeedsHuman) + " ")
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

// questionAttachRows are the blocks the question itself carries, as opposed to
// the ones hanging off one answer. They sit at the foot of the page because they
// are evidence about the WHOLE decision, and a person reads them after the
// answers rather than before.
func (a *app) questionAttachRows(width int) []string {
	room := a.qroom
	if room == nil || len(room.head.question.Attach) == 0 || room.compare {
		return nil
	}
	out := []string{""}
	for _, block := range room.head.question.Attach {
		out = append(out, a.questionBlockRows(block, len(questionIndent), width)...)
	}
	return out
}

// questionTaken reports whether the foot would send this key.
func (a *app) questionTaken(key string) bool {
	room := a.qroom
	if room == nil || key == "" {
		return false
	}
	for _, k := range room.picked {
		if k == key {
			return true
		}
	}
	return false
}

// questionRightAlign puts a badge at the right edge of a row, or drops it where
// the row has no room. It is the one place this page measures, and it measures
// the PAINTED strings, because that is what the terminal draws.
func questionRightAlign(left, right string, width int) string {
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// ─────────────────────────────────────────────────────────────────────────────
// The foot: what the answer would be, and the keys.

// questionFootHeight is how many rows the pinned foot takes: two while the
// question is open, one while it is reporting a refusal, none otherwise.
//
// THERE IS NO ANSWERED HEIGHT, because an answered question has no page: the
// page folds on the answer and the receipt is the block's ([app.questionAnswer]
// says why).
func (a *app) questionFootHeight() int {
	if a.qroom == nil {
		return 0
	}
	if a.qroom.refused != "" {
		return 1
	}
	return 2
}

// questionFootRows is the foot, pinned above the box exactly where every other
// question on this surface sits (view.go's chrome stack).
//
// TWO ROWS AND NEVER MORE. The first says what enter would send — the pick, what
// was typed beside it, how many parts were commented on, and the scope — and the
// second is the keys. A foot that grew with the question would push the box off
// a short screen, which is the one thing a page that promises never to take the
// keyboard cannot do.
func (a *app) questionFootRows(width int) []string {
	room := a.qroom
	if room == nil || width <= 0 {
		return nil
	}
	if room.refused != "" {
		return []string{a.pal.warn(fit(room.refused, width))}
	}
	if room.deciding {
		return []string{
			a.pal.ask(fit(a.questionDecideLine(), width)),
			a.pal.dim(fit(questionDecideAgain, width)),
		}
	}
	if room.decidingKind {
		return []string{
			a.pal.ask(fit(a.questionDecideKindLine(), width)),
			a.pal.dim(fit(questionDecideKindAgain, width)),
		}
	}
	if prompt := a.questionPromptWord(); prompt != "" {
		return []string{
			a.pal.ask(fit(prompt, width)),
			a.pal.dim(fit(a.questionSaying(), width)),
		}
	}
	return []string{
		a.questionComposeRow(width),
		a.questionRoomOfferRow(width),
	}
}

// questionPromptWord is the sentence that replaces the compose row while the box
// is pointed at a part rather than at the answer: `c`, `?` and `n` each say what
// the box is writing now, because a box whose meaning changed silently is a box
// that puts a comment into the answer.
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

// questionSaying is the dim second row under a prompt: what the box holds so
// far, or the way out of it.
func (a *app) questionSaying() string {
	return "esc " + questionKeyWord(questionLaterKey)
}

// questionComposeRow is the first foot row: `pick · with · notes · scope`.
func (a *app) questionComposeRow(width int) string {
	room := a.qroom
	parts := make([]string, 0, 4)
	switch {
	case len(room.picked) > 0:
	case room.input.kind == session.InputBlanks || room.input.kind == session.InputPairs ||
		room.input.kind == session.InputDial || room.input.kind == session.InputText:
		parts = append(parts, questionFilledWord)
	default:
		parts = append(parts, questionNoPickWord)
	}
	if len(room.picked) > 0 {
		parts = append(parts, questionAnsweringWord+a.questionPickedWord())
	}
	if said := strings.TrimSpace(a.input.String()); said != "" {
		parts = append(parts, questionWithWord+said)
	}
	if n := len(room.comments); n > 0 {
		parts = append(parts, strconv.Itoa(n)+questionNotesWord)
	}
	// THE SCOPE IS ON THE ROW ONLY WHERE THERE IS A CHOICE OF IT. A question that
	// offered one scope has no decision to show, and drawing `once` beside every
	// answer would teach people to stop reading the word.
	if len(room.head.question.Scope) > 1 {
		parts = append(parts, questionScopeWord(room.scope))
	}
	line := strings.Join(parts, questionSep)
	if len(room.picked) == 0 {
		return a.pal.dim(fit(line, width))
	}
	return a.pal.ask(fit(line, width))
}

// questionPickedWord is what was chosen, in keys and words: `1 postgres`, or
// `1 postgres, 3 a file per day` on a checklist.
func (a *app) questionPickedWord() string {
	room := a.qroom
	words := make([]string, 0, len(room.picked))
	for _, key := range room.picked {
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
	if err := door.SetAutonomy(room.head.question.Ask, session.Policy{Kind: session.PolicyDecide}); err != nil {
		room.refused = strings.TrimSpace(err.Error())
		a.questionRoomTouched()
		return nil
	}
	room.refused = questionDecideKindDone
	a.questionRoomTouched()
	return nil
}

// questionOfferKeys is which of the grammar's keys this question offers, and it
// is question.go's own reading rather than a second one: the block and this page
// print from one table through one filter, so a key that is on the card is on
// the page and a key the emptiness law drops is dropped in both.
func (a *app) questionOfferKeys() []questionVerb {
	return a.questionAnswerKeys(a.qroom.head, formsRoom)
}

// questionOfferRow spells that offer as the one row a person reads. The words
// and the give-up order are the table's; the painting is
// [app.questionVerbParts]'s, so the foot of a page and the answers row of a card
// are the same row drawn in two places.
func (a *app) questionRoomOfferRow(width int) string {
	head := a.qroom.head
	keys := a.questionOfferKeys()
	for {
		parts := a.questionVerbParts(head, keys, false)
		line := strings.Join(parts, "")
		if ansi.StringWidth(line) <= width {
			return a.questionPaintOffer(parts)
		}
		dropped, ok := questionDropVerb(keys)
		if !ok {
			return a.pal.dim(fit(line, width))
		}
		keys = dropped
	}
}

// questionPaintOffer paints the alternating word/key pairs
// [app.questionVerbParts] builds: the keys are what a person scans for, so they
// are the only bold cells on the row.
func (a *app) questionPaintOffer(parts []string) string {
	var b strings.Builder
	for i, part := range parts {
		if i%2 == 1 {
			b.WriteString(a.pal.dim(part))
			continue
		}
		b.WriteString(a.pal.ask(part))
	}
	return b.String()
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
		a.questionMoveFocus(map[string]int{"up": -1, "down": 1}[key])
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
	case questionOpenKey:
		room.open[room.focus] = !room.open[room.focus]
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
		case questionOrderKey:
			if key == "shift+up" || key == "shift+down" {
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

// questionMoveFocus walks the sections, and it is bounded rather than wrapping:
// a list that jumps from the last row to the first is a list a person loses
// their place in.
func (a *app) questionMoveFocus(delta int) {
	room := a.qroom
	n := len(room.head.question.Options)
	if room.input.kind != session.InputNone {
		n = room.input.count()
	}
	if n == 0 {
		return
	}
	at := room.focus + delta
	if at < 0 {
		at = 0
	}
	if at >= n {
		at = n - 1
	}
	if room.input.kind != session.InputNone {
		room.input.focus = at
	}
	room.focus = at
	a.questionRoomTouched()
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

// questionRoomPick takes one of the answers by its digit.
//
// A CHECKLIST ADDS AND EVERY OTHER SHAPE REPLACES. Pressing 2 on a choice means
// "2, not 1"; pressing 2 on a list of things to tick means "and 2" — and a
// single rule for both would either make a choice accumulate or make a checklist
// impossible to fill.
func (a *app) questionRoomPick(key string) {
	room := a.qroom
	opt, ok := room.head.question.Option(key)
	if !ok {
		return
	}
	for i := range room.head.question.Options {
		if room.head.question.Options[i].Key == key {
			room.focus = i
		}
	}
	if room.input.kind == session.InputChecklist {
		room.picked = questionToggleOne(room.picked, opt.Key)
		return
	}
	room.picked = []string{opt.Key}
	room.open[room.focus] = true
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
	// THE EMPTINESS LAW ON THE MOST IMPORTANT KEY ON THE PAGE: `enter` with
	// nothing chosen and nothing typed does nothing at all, because there is no
	// answer for it to send. The foot already says `nothing chosen yet`.
	if len(room.picked) == 0 && room.head.question.Pick != nil && said == "" {
		room.picked = []string{room.head.question.Pick.Key}
	}
	if len(room.picked) == 0 && said == "" && room.input.kind == session.InputNone {
		return nil
	}
	answer := session.Answer{
		Picked:    append([]string{}, room.picked...),
		Change:    said,
		DecidedBy: session.DecidedByPerson,
	}
	return a.questionAnswer(answer)
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
	door, ok := a.agent.(questionDoor)
	if !ok {
		room.refused = questionNoDoorWord
		a.questionRoomTouched()
		return nil
	}
	if answer.DecidedBy == "" {
		answer.DecidedBy = session.DecidedByPerson
	}
	if err := door.ResolveQuestion(answer); err != nil {
		room.refused = strings.TrimSpace(err.Error())
		a.questionRoomTouched()
		return nil
	}
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
	a.closeQuestion(room.head, answer)
	a.closeQuestionRoom()
	return nil
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

// ─────────────────────────────────────────────────────────────────────────────
// The geometry, on room.go's own terms.

// questionRoomWindow is the page's visible slice and the padding under it. It is
// [app.window]'s shape over this page's rows and this page's offset, which is
// what makes `esc` restore the conversation without having moved it.
//
// THE PADDING FALLS BELOW A SHORT PAGE, exactly as it does under a short
// conversation and under a short room. A question with two answers reads from
// the top like everything else on this surface.
func (a *app) questionRoomWindow(width, height int) ([]row, int) {
	if height <= 0 {
		return nil, 0
	}
	rows := a.questionRoomRows(width)
	offset := a.questionRoomOffsetFor(len(rows), height)
	end := min(offset+height, len(rows))
	visible := rows[offset:end]
	if pad := height - len(visible); pad > 0 {
		return visible, pad
	}
	return visible, 0
}

// questionRoomOffsetFor clamps the reader's position.
//
// IT ANCHORS AT THE TOP AND NEVER AT THE LIVE EDGE, which is where it parts
// company with a node's page. A room follows work that is still arriving, so it
// sticks to the bottom; a question is FINISHED the moment it is drawn — the head
// is the first thing to read and nothing will be appended under it — so a page
// that opened at its foot would open on the least useful row it has.
func (a *app) questionRoomOffsetFor(total, height int) int {
	room := a.qroom
	bottom := max(0, total-height)
	if room == nil || room.offset < 0 {
		return 0
	}
	return min(room.offset, bottom)
}

// questionRoomScroll moves the page's window.
func (a *app) questionRoomScroll(delta int) {
	room := a.qroom
	if room == nil {
		return
	}
	total := len(a.questionRoomRows(a.bodyWidth()))
	bottom := max(0, total-a.viewHeight())
	room.offset = questionClamp(room.offset+delta, 0, bottom)
	a.touch()
}

// askBackCmd sends one sentence to the asker WITH THE QUESTION STILL OPEN.
//
// DESIGN.md gives this two seams — "sent to the asker through ResolveQuestion's
// AskedBack seam (E1) or the ordinary turn with the question still open" — and
// the ordinary turn is the one that exists today: there is no engine door that
// takes a question about a question and answers it without resolving anything.
// So the sentence goes in as a message, the question stays up, and the reply
// lands back on the row it was asked from ([app.questionDrainReplies]).
//
// THE ROW REMEMBERS WHERE THE TRANSCRIPT WAS. That is the whole of how the reply
// is found: everything the model says after the sentence went in is a candidate,
// and the first settled thing it says is the answer. Nothing is parsed and
// nothing is guessed — a person can read both rows and see for themselves.
func (a *app) askBackCmd(part, text string) tea.Cmd {
	room := a.qroom
	if room == nil {
		return nil
	}
	if ask := room.asks[part]; ask != nil {
		ask.after = len(a.entries)
	}
	return a.submit(text)
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
