package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE PANEL ───────────────────────────────────────────────────────────────
//
// A QUESTION HANGS ABOVE THE BOX AS ONE OBJECT (owner ruling 2026-09-11, frame
// pick A). It is the one drawing every question with anything to weigh gets —
// the model's own `ask`, a permission, a task proposal, a standing card, a
// landing, a confirmation — and the chooser (questionchooser.go) is what decides
// that a question has anything to weigh.
//
//	╭─ ? Which storage for the session index? ─────────────── model asks ─╮
//	│                                                                     │
//	│ ▸ 1  SQLite        one file beside the conversation   ◆ recommended  │
//	│      already a dependency; survives a crash mid-write · fairly sure  │
//	│   2  JSONL         append-only, no new dependency                    │
//	│   3  BoltDB        fastest reads · adds a dependency                 │
//	│   4  something else…                                                 │
//	│                                                                      │
//	╰─ ↑↓ choose · enter take it · esc later ──────────────────────────────╯
//	  c change · ? ask back · o open full · d you decide · 1–4 jump
//
// THE FIVE DECISIONS THE OWNER MADE, AND WHERE EACH ONE IS:
//
//   - COLOUR IS STROKE (pick C). The three marks — `?`, the pointer `▸`, the
//     recommended `◆` — are amber; every word is ink, every aside dim, the edge
//     dim, and the focused row sits on the `selected` ground.
//     [TestNoQuestionRowIsPaintedInTheQuestionHue] is the law.
//   - THE PICK IS MARKED IN EVERY VIEW (pick A). `◆ recommended` stands at the
//     right edge of the picked row, and the pointer opens on it — except where
//     nobody but a person may answer, where the pointer opens on the answer that
//     loses nothing and the pick keeps its mark ([questionPointerStart]).
//   - TWO TIERS OF KEYS (pick A). The frame's bottom edge carries exactly the
//     keys that answer; one dim row under the frame carries the rest, dropped
//     right to left when the frame is narrow. Both come from the ONE key table.
//   - YOUR OWN ANSWER IS A ROW (pick A). The last row is `something else…`, and
//     the pointer on it turns it into a box you type in. There is no hidden
//     `press c first`; `c` is a shortcut to that row.
//   - THE CLOCK IS AN ASIDE (pick A). It sits in the top edge's right, beside
//     who is asking, and never inside the keys.

const (
	// questionPanelOtherWord is the last row: the answer that is not on the
	// list. It ends in an ellipsis because pressing it opens a box rather than
	// answering, which is what an ellipsis means everywhere else on this surface.
	questionPanelOtherWord = "something else…"
	// questionRecommendedWord is what the asker's pick says beside its mark. The
	// design language's PRESENCE OVER LABELS asks for a word next to every mark,
	// and this is the word: `suggested` was the block's old spelling, in the
	// consequence column, where it read as one more thing the answer would do.
	questionRecommendedWord = "recommended"
	// questionSafeWord is what the answer that loses nothing says on a question
	// nobody but a person may answer. It is the same claim `◆ recommended` makes
	// — "the pointer is here for a reason" — on a shape where the asker is not
	// allowed to have a pick.
	questionSafeWord = "safe answer"
	// questionPanelGap is the one cell of air between a panel's side and its
	// rows. A boundary is made of whitespace on this surface (THE SPACING
	// LADDER), and the frame's edge is the hairline it is allowed one of.
	questionPanelGap = " "
)

// questionPanelRows draws one question as the panel, and records where its
// answers landed for the pointer.
//
// The rows are laid at the frame's inner width and handed to the ONE frame
// (frame.go), which sets each of them to that width so the right edge lands in
// one column.
func (a *app) questionPanelRows(q questionShown, width int) []string {
	inner := frameInner(width)
	if len(q.beat) > 0 {
		return a.questionPanelBeat(q, width, inner)
	}
	first := len(a.questionBands)
	title, headRows := a.questionPanelHead(q, width, inner)
	rows := append(headRows, a.questionPanelBody(q, inner)...)
	keys := a.questionAnswerKeys(q, formsCard)
	keyRow := a.questionKeyRow(q, questionKeysOnTier(keys, keyPrimary), frameEdgeRoom(width))
	if q.writing != "" {
		// THE EDGE SAYS WHAT THE BOX MEANS while the composer is pointed at the
		// question ([app.questionWritingRow]): every letter types, so an edge
		// naming letters would be naming keys that do something else.
		keyRow = a.pal.dim(fit(a.questionWritingRow(q), frameEdgeRoom(width)))
	}
	panel := framed{
		title: title,
		aside: a.questionPanelAside(q, width),
		keys:  keyRow,
	}
	out, _ := panel.draw(a.pal, width, rows)
	// THE FRAME'S TOP EDGE IS A ROW, and so is every row the head took when it
	// would not fit into that edge: each of them puts the body one row further
	// down the block. The bands are what a press resolves against, so they are
	// shifted here rather than guessed at by the body.
	for i := first; i < len(a.questionBands); i++ {
		a.questionBands[i].row += 1 + len(headRows)
	}
	// AND THE SECOND TIER STANDS UNDER THE FRAME, dim, in the same grammar. It
	// is not written into the bottom edge because the edge is for the keys that
	// ANSWER: a row that mixed `esc later` with `D decide these from now on` was
	// the owner's "no hierarchy in the hints".
	if second := a.questionPanelSecond(q, keys, width); second != "" && q.writing == "" {
		out = append(out, second)
	}
	return out
}

// questionPanelBeat is the widening answer asking how far it goes, drawn in the
// same frame rather than in a drawing of its own: the shapes take the rows the
// answers had, and the bottom edge says the two keys that mean anything while
// they are up.
//
// IT IS ONE ROW OF SHAPES AND NOT A ROW EACH, because the shapes are one
// question's answers ([app.questionBeatRow] holds the spans a click resolves
// against, and they are a row's worth).
func (a *app) questionPanelBeat(q questionShown, width, inner int) []string {
	row := a.questionBeatRow(q, 2, inner)
	// The frame's side is one cell, so every span the beat recorded stands one
	// column further right than the row itself counted it.
	for i := range a.questionSpans {
		a.questionSpans[i].from++
		a.questionSpans[i].to++
	}
	title, headRows := a.questionPanelHead(q, width, inner)
	panel := framed{
		title: title,
		aside: a.questionPanelAside(q, width),
		keys: a.pal.data("1–"+itoa(len(q.beat))) + a.pal.dim(" shape") +
			a.pal.dim(questionKeyGap) + a.pal.data(questionLaterKey) + a.pal.dim(" "+questionBeatBack),
	}
	out, _ := panel.draw(a.pal, width, append(headRows, "", row, ""))
	return out
}

// questionPanelBody is the panel WITHOUT its frame: the rows a person reads and
// presses, laid out at `inner` cells.
//
// IT IS SPLIT OFF FOR THE ONE PLACE A QUESTION IS DRAWN WHERE THE PANEL'S KEYS
// ARE NOT THE KEYS. Home's errand pane has a message box of its own pointed at
// another conversation, so `enter` there sends a follow-up and `esc` hands the
// keyboard back to the list — and a bottom edge promising `enter take it · esc
// later` under that box would name two keys that do something else, which is the
// one failure a key row exists to prevent. The pane draws this and names its own
// keys (homeexchange.go's [exchangeHint]).
func (a *app) questionPanelBody(q questionShown, inner int) []string {
	room := max(inner-2*len(questionPanelGap), 1)
	rows := make([]string, 0, len(q.question.Options)+6)
	// THE FIRST ROWS ARE WHAT HAS TO BE READ BEFORE ANSWERING — the command a
	// permission is about, or the sentence the asker gave for asking now — and
	// they are followed by one blank row, which is this surface's own boundary.
	rows = append(rows, a.questionPanelContext(q, room)...)
	// THE SENTENCE WITH A HOLE IN IT GOES ABOVE THE ANSWERS, because it is part
	// of what the answers are about: `start it` on a proposal starts it on the
	// model in the hole, so the hole has to be read before the answer is given.
	// It is the room's own renderer (questioninput.go), not a second one.
	if q.holes.kind == session.InputBlanks {
		for _, line := range a.questionCardBlankRows(&q.holes, room) {
			rows = append(rows, questionPanelGap+line)
		}
	}
	rows = append(rows, "")
	// The widest answer word, which is the column every consequence beside it
	// starts in. It is bounded so one long label cannot push every consequence
	// off the panel.
	pad := 0
	for _, option := range q.question.Options {
		if w := ansi.StringWidth(strings.TrimSpace(option.Label)); w > pad && w <= room/2 {
			pad = w
		}
	}
	for i, option := range q.question.Options {
		for _, line := range a.questionPanelOption(q, i, option, pad, room) {
			// EVERY ROW AN ANSWER TAKES PRESSES THAT ANSWER, which is the sheet's
			// own bargain applied here: an answer whose words wrapped is not a
			// target that shrinks to its first line.
			a.questionBands = append(a.questionBands, questionBand{
				row: len(rows), span: hudSpan{from: 0, to: inner + 2}, at: i,
			})
			rows = append(rows, line)
		}
	}
	if a.questionTakesOther(q) {
		other := questionOtherAt(q.question)
		for _, line := range a.questionPanelOther(q, pad, room) {
			a.questionBands = append(a.questionBands, questionBand{
				row: len(rows), span: hudSpan{from: 0, to: inner + 2}, at: other,
			})
			rows = append(rows, line)
		}
	}
	// AND HOW LONG THE ANSWER LASTS IS ONE ROW UNDER THE ANSWERS, where there is
	// a choice of it to make (questionscope.go). It is below them and not among
	// them because it is not an answer: it says how far the answer above it
	// reaches, and a row a person could land the pointer on would make `enter`
	// mean two things.
	if row := a.questionScopeRow(q, room); row != "" {
		rows = append(rows, "", row)
	}
	// AND THE ONE LINE OVER A FREE-TEXT BOX IS THE LAST ROW, because the box it
	// is about is the message box under the panel ([session.InputShape.Prompt]
	// calls it "the one line above a free-text box"). It is the asker saying
	// which words to type — "paste your Notion key", "the domain in your Datadog
	// address" — and a question that asked for words with nothing saying which
	// words is a box a person guesses at.
	if q.question.Input.Kind == session.InputText {
		if prompt := strings.TrimSpace(q.question.Input.Prompt); prompt != "" {
			for _, line := range wrap(prompt, room) {
				rows = append(rows, questionPanelGap+a.pal.dim(line))
			}
		}
	}
	// AND A QUESTION WHOSE CLOCK ANSWERS SAYS WHAT A PERSON CAN DO ABOUT IT, on
	// one dim line, INSIDE the frame (#954, and this is the place lane A and I
	// agreed it goes).
	//
	// IT IS A SENTENCE, AND THE ROW UNDER THE FRAME IS THE SECOND TIER OF KEYS.
	// The owner's hints ruling is that the bottom edge carries exactly
	// `↑↓ choose · enter take it · esc later` and one dim row under it carries
	// the rest of the KEYS, dropped right-to-left — so a sentence appended to
	// that region is a third kind of thing in a place with a stated grammar, and
	// the hierarchy it creates is the one that ruling exists to prevent. Inside
	// the frame it is what it is: a fact about this question, under this
	// question, in the same dim the panel's other asides wear.
	if aside := a.questionClockAside(q, room); aside != "" {
		rows = append(rows, questionPanelGap+aside)
	}
	return append(rows, "")
}

// questionPanelContext is what has to be read before the answers: the call a
// permission is about, in the payload hue with what it touches beside it, or the
// asker's own sentence for asking now.
//
// THE REASON IS NOT SAID TWICE ON ONE SCREEN. Where the transcript is already
// drawing the thing this question is about with its own sentence under it, the
// panel says nothing here — two renderings of one fact is the defect this block
// was built around.
func (a *app) questionPanelContext(q questionShown, room int) []string {
	reason := strings.TrimSpace(q.question.Reason)
	if command := a.questionPanelCall(q); command != "" {
		// THE COMMAND IS THE PAYLOAD AND WHAT IT TOUCHES IS THE ASIDE. A person
		// allowing a call has to read the call, so it takes the one hue this
		// surface lifts a datum into and the policy's own sentence follows it.
		line := a.pal.data(command)
		if reason != "" {
			line += a.pal.dim(" · " + reason)
		}
		return []string{questionPanelGap + fit(line, room)}
	}
	if reason == "" || a.questionSubjectAt(q.question) >= 0 {
		return nil
	}
	out := make([]string, 0, 2)
	for i, line := range wrap(reason, room) {
		if i >= questionPanelReasonRows {
			break
		}
		out = append(out, questionPanelGap+a.pal.dim(line))
	}
	return out
}

// questionPanelReasonRows is how many rows the asker's sentence may take. Two,
// because it is the one thing a person has to READ before they answer and half
// a sentence is worse than two rows of one — and no more, because the answers
// are what the panel is for.
const questionPanelReasonRows = 2

// questionPanelTool is the name of the tool a permission is about, read off the
// transcript's own row for the call so the panel and the row cannot name it two
// ways. It is empty for every question that is not about a call.
func (a *app) questionPanelTool(q questionShown) string {
	if q.question.Subject.Kind != session.SubjectCall {
		return ""
	}
	at := a.questionSubjectAt(q.question)
	if at < 0 || at >= len(a.entries) || a.entries[at].kind != entryTool {
		return strings.TrimSpace(q.question.Subject.Name)
	}
	name, _ := toolWords(a.entries[at].tool, a.entries[at].text)
	if name = strings.TrimSpace(plainText(name)); name != "" {
		return name
	}
	return strings.TrimSpace(q.question.Subject.Name)
}

// questionPanelCall is the command this question is about, where it is about
// one: the transcript's own words for the row, so the panel and the row cannot
// become two accounts of one call.
func (a *app) questionPanelCall(q questionShown) string {
	if q.question.Subject.Kind != session.SubjectCall {
		return ""
	}
	at := a.questionSubjectAt(q.question)
	if at < 0 || at >= len(a.entries) {
		return strings.TrimSpace(q.question.Subject.Name)
	}
	e := &a.entries[at]
	if e.kind != entryTool {
		return strings.TrimSpace(q.question.Subject.Name)
	}
	name, command := toolWords(e.tool, e.text)
	if target := toolTarget(e.tool, e.detail.Args, e.text); target != "" {
		command = target
	}
	if strings.TrimSpace(command) == "" {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(plainText(command))
}

// questionPanelHead is the question's head: the top edge's words where the edge
// has room for them, and the panel's own first rows where it has not.
//
// A QUESTION'S HEAD IS NEVER CUT. The frame gives up its aside and then cuts its
// title, which is right for a chooser's folder name and wrong for the sentence
// a person is being asked — `Move this conversation her…` is a question nobody
// can answer. So a head too long for the edge stands in the body, wrapped, with
// the mark left in the edge saying what the object is.
func (a *app) questionPanelHead(q questionShown, width, inner int) (string, []string) {
	mark := a.questionMarkFor(q.question)
	head := strings.TrimSpace(q.question.Head)
	if head == "" {
		return mark, nil
	}
	if ansi.StringWidth(head) <= max(frameEdgeRoom(width)-2, 1) {
		return mark + " " + a.pal.ink(head), nil
	}
	room := max(inner-2*len(questionPanelGap), 8)
	rows := make([]string, 0, 3)
	for _, line := range wrap(head, room) {
		rows = append(rows, questionPanelGap+a.pal.ink(line))
	}
	return mark, rows
}

// questionPanelAside is the top edge's right: what is asking, and how the
// silence is being held.
//
// A PERMISSION NAMES ITS TOOL THERE (consent pick B): the title says what it
// wants in a person's words, and the tool's own name is the second fact beside
// it. Anything else names who is asking, as a verb — `model asks`, not `model`.
//
// THE CLOCK IS AN ASIDE AND NEVER A KEY (hints pick A): it follows who is asking,
// on the top edge, so the bottom edge carries only the keys that answer. It is
// the first thing the edge gives up when it is narrow, because it is the last
// words of the aside and the aside goes before the title does.
func (a *app) questionPanelAside(q questionShown, width int) string {
	who := a.questionPanelTool(q)
	if who == "" {
		if asker := questionAskerWord(q.question.Asker); asker != "" {
			who = asker + questionAsksWord
		}
	}
	clock := a.questionClockWord(q)
	switch {
	case who != "" && clock != "":
		return a.pal.dim(who + questionKeyGap + clock)
	case clock != "":
		return a.pal.dim(clock)
	case who != "":
		return a.pal.dim(who)
	}
	return ""
}

// questionAsksWord is what the top edge says after whoever is asking. It is a
// verb rather than a label because the edge is the asking: `model asks`, not
// `model`.
const questionAsksWord = " asks"

// questionPanelOption is one answer's rows: the row itself, and — only while the
// pointer is on it — what the asker said about it.
func (a *app) questionPanelOption(q questionShown, at int, option session.AnswerOption, pad, room int) []string {
	key := questionOptionKeyAt(q.question, at)
	focused := at == q.pick
	picked := q.question.Pick != nil && strings.TrimSpace(q.question.Pick.Key) == key
	word := strings.TrimSpace(option.Label)
	if word == "" {
		word = key
	}
	say := strings.TrimSpace(option.Consequence)
	// A CHECKLIST'S ROWS CARRY THEIR TICKS in a cell of their own between the
	// key and the word, so the digit that toggles a row is still on it and the
	// pointer still says where `space` lands — the old card let a tick stand in
	// the pointer's cell, and a ticked row the pointer was on showed neither.
	tick := ""
	if q.holes.kind == session.InputChecklist {
		tick = questionTickBlank
		if at < len(q.holes.ticks) && q.holes.ticks[at] {
			tick = a.icon(tokens.GSettled)
		}
		focused = at == q.holes.focus
	}
	aside := ""
	switch {
	case picked:
		aside = a.pal.warnBold(a.icon(tokens.GRecommended)) + a.pal.dim(" "+questionRecommendedWord)
	case option.Safe && questionHandsOnly(q.question) && q.question.Pick == nil:
		// THE SAFE ANSWER SAYS SO WHERE THERE IS NO PICK. A question nobody but a
		// person may answer has no recommendation by law ([questionHandsOnly]),
		// and the pointer standing on one answer with nothing saying why reads as
		// the surface having chosen.
		aside = a.pal.dim(questionSafeWord)
	}
	rows := a.questionPanelRow(q, key, tick, word, say, aside, pad, room, focused, a.questionHovering(at))
	if !focused {
		return rows
	}
	// UNDER THE POINTER, AND ONLY THERE: what this answer means, and — where it
	// is the asker's pick — why the asker would take it. Every answer's whole
	// case on every row would be a panel taller than the conversation under it,
	// and the pointer is the person saying which one they are weighing.
	lead := questionPanelLead(q.question, room)
	indent := questionPanelGap + strings.Repeat(" ", lead-len(questionPanelGap))
	for _, line := range a.questionPanelUnder(q, at, option, room-lead) {
		rows = append(rows, indent+line)
	}
	return rows
}

// questionHovering reports whether the mouse is over one answer's row. It asks
// the pointer's own kind as well as the derived index, so a stale index cannot
// light a row on a frame where the pointer is somewhere else entirely.
func (a *app) questionHovering(at int) bool {
	return a.hot.kind == hoverChoices && a.hotAnswer == at
}

// questionTickBlank is the cell an unticked checklist row stands in, so the
// ticked and unticked rows keep one column.
const questionTickBlank = " "

// questionPanelLead is how many cells stand in front of an answer's word: the
// panel's own gap, the pointer's cell and its space, the key and two spaces —
// and on a checklist the tick's cell and its space after the key.
func questionPanelLead(q session.Question, room int) int {
	lead := len(questionPanelGap) + 2 + questionKeyCell + 2
	if q.Input.Kind == session.InputChecklist {
		lead += 2
	}
	return min(lead, room/2)
}

// questionKeyCell is how wide an answer's key is drawn. One, because the engine
// renumbers every question's answers to single digits ([session.Question.Check]
// caps a question at four answers, eight on a checklist).
const questionKeyCell = 1

// questionPanelRow lays one answer out: the pointer, the key, the word in its
// column, what taking it produces, and the aside at the right edge.
//
// AN ANSWER IS NEVER CUT. A label too long for one row wraps onto rows that
// stand in the word's own column, and every one of them presses the same answer
// (questionPanelBody records a band per row) — a label cut at the panel's edge
// is an answer a person cannot read before taking it.
func (a *app) questionPanelRow(q questionShown, key, tick, word, say, aside string, pad, room int, focused, hovered bool) []string {
	mark := "  "
	if focused {
		mark = a.pal.warnBold(a.icon(tokens.GPointer)) + " "
	}
	lead := questionPanelGap + mark + a.pal.data(key) + "  "
	if tick != "" {
		// The tick is one of the question's marks, so it wears the amber the
		// pointer does; an unticked row keeps the cell blank so the words keep
		// their column.
		lead = questionPanelGap + mark + a.pal.data(key) + " " + a.pal.warnBold(tick) + " "
	}
	leadWidth := questionPanelLead(q.question, room)
	asideWidth := 0
	if aside != "" {
		asideWidth = ansi.StringWidth(ansi.Strip(aside)) + 2
	}
	ground := func(line string, used int) string {
		// THE GROUND LADDER, IN ORDER (docs/DESIGN-LANGUAGE.md): a row the mouse
		// is over takes `cursor`, the row the pointer stands on takes `selected`,
		// and everything else stands on the terminal's own ground. THE EMPHASIS
		// LAW, AND NOTHING ELSE: no ring, no second colour, no bolding spreading
		// across the row.
		switch {
		case hovered:
			return a.pal.cursor(line, room+2*len(questionPanelGap))
		case focused:
			return a.pal.background(line, room+2*len(questionPanelGap), a.pal.ramp.selected)
		}
		return line
	}
	indent := strings.Repeat(" ", leadWidth)
	lines := wrap(word, max(room-leadWidth-asideWidth, 8))
	if len(lines) == 0 {
		lines = []string{""}
	}
	if len(lines) > 1 {
		// THE WRAPPED SHAPE HAS NO CONSEQUENCE COLUMN to keep: the answer's own
		// words already run the width of the panel, so what taking it produces
		// follows them on rows of its own rather than in a column that would be
		// four cells wide.
		out := make([]string, 0, len(lines)+2)
		for i, line := range lines {
			row := indent + a.pal.ink(line)
			if i == 0 {
				row = lead + a.pal.ink(line)
				if aside != "" {
					row += strings.Repeat(" ", max(room-leadWidth-ansi.StringWidth(line)-asideWidth, 1)+2) + aside
				}
			}
			out = append(out, ground(row, room))
		}
		if say != "" {
			for _, line := range wrap(say, max(room-leadWidth, 8)) {
				out = append(out, ground(indent+a.pal.dim(line), room))
			}
		}
		return out
	}
	label := lines[0]
	if w := ansi.StringWidth(label); w < pad {
		label += strings.Repeat(" ", pad-w)
	}
	line := lead + a.pal.ink(label)
	used := leadWidth + max(ansi.StringWidth(lines[0]), pad)
	// WHAT AN ANSWER COSTS IS NEVER DROPPED, only moved: it stands beside the
	// word where there is room for it and under the word where there is not.
	// A narrow card that kept the labels and lost every consequence was a card
	// that hid the difference between its answers (measured on home's
	// fifty-column card, where `move it here` lost what moving costs).
	under := ""
	if say != "" {
		if room-used-2-asideWidth >= questionPanelSayFloor {
			line += a.pal.dim("  " + fit(say, room-used-2-asideWidth))
			used += 2 + min(ansi.StringWidth(say), room-used-2-asideWidth)
		} else {
			under = say
		}
	}
	if aside != "" && used+asideWidth <= room {
		line += strings.Repeat(" ", room-used-asideWidth+2) + aside
	}
	out := []string{ground(line, room)}
	if under == "" {
		return out
	}
	for _, row := range wrap(under, max(room-leadWidth, 8)) {
		out = append(out, ground(indent+a.pal.dim(row), room))
	}
	return out
}

// questionPanelUnder is what stands under the focused answer: its body in the
// reading ink, and the pick's own case where this is the pick.
func (a *app) questionPanelUnder(q questionShown, at int, option session.AnswerOption, room int) []string {
	out := make([]string, 0, 3)
	if body := strings.TrimSpace(option.Body); body != "" {
		for i, line := range wrap(body, max(room, 8)) {
			if i >= questionPanelBodyRows {
				break
			}
			out = append(out, a.pal.ink(line))
		}
	}
	if line := questionPickCase(q.question, questionOptionKeyAt(q.question, at)); line != "" {
		out = append(out, a.pal.dim(fit(line, max(room, 8))))
	}
	return out
}

// questionPanelSayFloor is the narrowest a consequence may be drawn beside its
// answer. Below it the words are a stub with an ellipsis on the end, which says
// less than the same words on the row underneath.
const questionPanelSayFloor = 12

// questionPanelBodyRows is how many rows an answer's own words may take under
// the pointer. Two: a note is a note and not the page, and `o open full` holds
// the rest.
const questionPanelBodyRows = 2

// questionPickCase is the asker's case for its pick, on one line: why it would
// take this, how sure it is, and what would change its mind.
//
// THE EMPTINESS LAW. A pick with no reason, no confidence and nothing that would
// change its mind draws no line at all — never an empty one, and never the word
// "unknown".
func questionPickCase(q session.Question, key string) string {
	if q.Pick == nil || strings.TrimSpace(q.Pick.Key) != key {
		return ""
	}
	parts := make([]string, 0, 3)
	if reason := strings.TrimSpace(q.Pick.Reason); reason != "" {
		parts = append(parts, reason)
	}
	if sure := questionConfidenceWord(q.Pick.Confidence); sure != "" {
		parts = append(parts, sure)
	}
	if would := strings.TrimSpace(q.Pick.WouldChange); would != "" {
		parts = append(parts, questionWouldSwitchWord+would)
	}
	return strings.Join(parts, " · ")
}

// questionPanelOther is the last row: the answer that is not on the list, and
// the box it becomes while the pointer is on it.
func (a *app) questionPanelOther(q questionShown, pad, room int) []string {
	at := questionOtherAt(q.question)
	if q.pick != at {
		return a.questionPanelRow(q, itoa(at+1), "", questionPanelOtherWord, "", "", pad, room, false, a.questionHovering(at))
	}
	// THE ROW IS THE BOX. There is no mode to enter and nothing hidden behind a
	// letter: the pointer arriving here is what opens it, `enter` sends what is
	// written as the answer's own words, `↑` goes back to the list, and the
	// composer below is still the person's ([questionOtherKey]).
	lead := questionPanelGap + a.pal.warnBold(a.icon(tokens.GPointer)) + " " + a.pal.data(itoa(at+1)) + "  "
	typed := q.other.words.String()
	caret := questionOtherCaret
	if a.pal.ascii {
		caret = questionOtherCaretASCII
	}
	line := lead + a.pal.dim(a.icon(tokens.GPromptChat)+" ") +
		a.pal.ink(fit(typed, max(room-questionPanelLead(q.question, room)-4, 4))) + a.pal.accent(caret)
	if with := questionOtherWith(q); with != "" {
		line += a.pal.dim("  " + with)
	}
	return []string{a.pal.background(line, room+2*len(questionPanelGap), a.pal.ramp.selected)}
}

// The caret drawn in the row a person is typing into. It is DRAWN and not the
// terminal's — the real one belongs to the message box below, which stays the
// person's while a question is up (NEVER MODAL) — so it has an ascii floor of
// its own like every other mark on this surface.
const (
	questionOtherCaret      = "▏"
	questionOtherCaretASCII = "_"
)

// questionOtherWith is what the row says about the answer the words will travel
// with, where they travel with one: `c` pressed on an answer means "I will take
// this one, but not as it stands", and the row says which.
func questionOtherWith(q questionShown) string {
	at, ok := questionOptionAt(q.question, q.other.with)
	if !ok {
		return ""
	}
	option := q.question.Options[at]
	return questionOtherWithWord + q.other.with + " " + questionAnswerWord(option, q.other.with, true)
}

// questionOptionAt is which row one answer's key stands on.
func questionOptionAt(q session.Question, key string) (int, bool) {
	if strings.TrimSpace(key) == "" {
		return 0, false
	}
	for i := range q.Options {
		if questionOptionKeyAt(q, i) == key {
			return i, true
		}
	}
	return 0, false
}

// questionOtherWithWord leads that clause.
const questionOtherWithWord = "it goes with "

// questionOtherAt is the index the `something else…` row stands at: one past the
// last answer, so the pointer walks onto it and a digit reaches it.
func questionOtherAt(q session.Question) int { return len(q.Options) }

// questionTakesOther reports whether this question draws the `something else…`
// row at all.
//
// A QUESTION THAT TAKES WORDS TAKES THEM HERE. The ladder's last rung is "free
// text: always available, never the only door" (docs/design/questions/DESIGN.md),
// and until this row existed the door was a letter nothing on screen named. The
// two shapes that do NOT get it are the two where the answers ARE the question:
// a confirmation, which takes no words at all ([questionTakesWords]), and any
// question whose answers a person alone may give, where a typed sentence is not
// one of the answers being weighed.
func (a *app) questionTakesOther(q questionShown) bool {
	if !questionTakesWords(q.question) || questionHandsOnly(q.question) {
		return false
	}
	return q.question.Input.Kind == session.InputNone && len(q.question.Options) > 0
}

// questionPanelSecond is the dim row under the frame: every key the question
// offers that is not one of the keys that answer it, plus the digits.
//
// IT IS DROPPED RIGHT TO LEFT BY RANK, which is the answers row's own bargain
// ([questionDropVerb]) applied to the quieter tier — and a tier with nothing
// left in it is no row at all rather than an empty one.
func (a *app) questionPanelSecond(q questionShown, keys []questionVerb, width int) string {
	second := questionKeysOnTier(keys, keySecondary)
	if len(second) == 0 {
		return ""
	}
	room := max(width-2, 1)
	row := a.questionKeyRow(q, second, room-ansi.StringWidth(questionDigitsWord(q.question))-len(questionKeyGap))
	if digits := questionDigitsWord(q.question); digits != "" {
		row += a.pal.dim(questionKeyGap) + a.pal.data(digits) + a.pal.dim(" "+questionJumpWord)
	}
	return "  " + row
}

// questionDigitsWord is how the second tier spells the answers' own keys —
// `1–4`. The digits are deliberately not rows in [questionKeys] (each answer's
// word is already on its own row), and this is the one place they are named.
func questionDigitsWord(q session.Question) string {
	if len(q.Options) < 2 {
		return ""
	}
	// THE KEYS ARE THE ANSWERS' OWN and not a count of them: a question whose
	// second answer was dropped keeps `1` and `3` on the two that are left, and
	// a row that said `1–2` there would name a key nothing answers.
	//
	// AND A RANGE IS ONLY DRAWN WHERE THE KEYS RUN. `1–3` over the answers `1`
	// and `3` names `2` as a key, which is the same lie one row further on: the
	// engine drops the widening answer from a gate it may not offer one on
	// (consent.go), so this is the ORDINARY shape of an irreversible permission
	// rather than an edge. Where they do not run, each key is said.
	keys := make([]string, 0, len(q.Options))
	for at := range q.Options {
		keys = append(keys, questionOptionKeyAt(q, at))
	}
	if questionKeysRun(keys) {
		return keys[0] + "–" + keys[len(keys)-1]
	}
	return strings.Join(keys, " ")
}

// questionKeysRun reports whether these answer keys are the consecutive digits
// a range spelling would claim they are.
func questionKeysRun(keys []string) bool {
	for i, key := range keys {
		if len(key) != 1 || key[0] < '0' || key[0] > '9' {
			return false
		}
		if i > 0 && key[0] != keys[i-1][0]+1 {
			return false
		}
	}
	return true
}

// questionJumpWord is what the digits do: they move the pointer onto an answer
// and take it in one press.
const questionJumpWord = "jump"

// questionOptionKeyAt is one answer's key, falling back to its position where
// the lane wrote none.
func questionOptionKeyAt(q session.Question, at int) string {
	if at < 0 || at >= len(q.Options) {
		return ""
	}
	if key := strings.TrimSpace(q.Options[at].Key); key != "" {
		return key
	}
	return itoa(at + 1)
}
