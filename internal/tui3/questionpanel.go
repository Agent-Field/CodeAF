package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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

// questionMarksSafe reports whether this question's answer that loses nothing
// should say so on its row.
//
// THE SAFE ANSWER SAYS SO WHERE THERE IS NO PICK. A question nobody but a person
// may answer has no recommendation by law ([questionHandsOnly]), and the pointer
// standing on one answer with nothing saying why reads as the surface having
// chosen. Where the asker DID pick, the pick's own `◆ recommended` is what says
// why, and a second aside on another row would be two answers each claiming the
// pointer.
//
// IT IS THE ONE PREDICATE BEHIND EVERY DRAWING OF THE MARK, over a single
// question and — folded by [questionSetMarksSafe] — over a permission frame.
// The two were spelled apart for a while, and the frame marked `deny all` on
// sets whose own tabs would not mark anything, so `2 one by one` changed what
// the surface claimed about the same four questions.
func questionMarksSafe(q session.Question) bool {
	return questionHandsOnly(q) && q.Pick == nil
}

// questionSafeAside is the mark itself, and the ONLY reading of
// [questionSafeWord] in this package — a law test holds that, so a new drawing
// of a question cannot spell the mark on terms of its own.
func (a *app) questionSafeAside() string {
	return a.pal.dim(questionSafeWord)
}

// questionPanelRows draws one question as the panel, and records where its
// answers landed for the pointer.
//
// The rows are laid at the frame's inner width and handed to the ONE frame
// (frame.go), which sets each of them to that width so the right edge lands in
// one column.
func (a *app) questionPanelRows(q questionShown, width int) []string {
	inner := frameInner(width)
	title, headRows := a.questionPanelHead(q, width, inner)
	// THE FRAME'S TOP EDGE IS A ROW, and so is every row the head took when it
	// would not fit into that edge: each of them puts the body one row further
	// down the block. The marks are what a press resolves against, so the inside
	// is told where it stands rather than left to guess.
	rows, keyRow, keys := a.questionPanelInside(q, width, inner, 1+len(headRows), nil)
	panel := framed{
		title: title,
		aside: a.questionPanelAside(q, width),
		keys:  keyRow,
	}
	out, _ := panel.draw(a.pal, width, append(headRows, rows...))
	// AND THE SECOND TIER STANDS UNDER THE FRAME, dim, in the same grammar. It
	// is not written into the bottom edge because the edge is for the keys that
	// ANSWER: a row that mixed `esc later` with `D decide these from now on` was
	// the owner's "no hierarchy in the hints".
	if second := a.questionPanelSecond(q, keys, width); second != "" && q.writing == "" {
		out = append(out, second)
	}
	return out
}

// questionPanelInside is everything a question draws INSIDE its frame — the rows
// under its head, and the keys on the bottom edge that answer them.
//
// THE BEAT IS BRANCHED ON HERE AND NOWHERE ELSE. A question part-way through the
// widening answer ([app.questionPanelBeat]) replaces its answers with the shapes
// and its edge with the two keys that mean anything while they are up, and it is
// the same question whether it is alone above the box or one tab of the set its
// step raised (questionset.go). A second place deciding that was a panel drawing
// the answers while the digits picked shapes and `esc` backed out of the beat —
// a person typing into a drawing that is not the one they are looking at.
//
// `above` is how many rows already stand inside the frame, counting the frame's
// own top edge, so the marks a press resolves against are recorded where they
// will be drawn. `edge` is what the caller puts in front of the answering keys —
// the set's `←→ question`, and nothing at all for a panel of one.
func (a *app) questionPanelInside(q questionShown, width, inner, above int, edge []questionVerb) (rows []string, keyRow string, keys []questionVerb) {
	if len(q.beat) > 0 {
		return a.questionPanelBeat(q, above, inner)
	}
	first := len(a.questionBands)
	// THE BODY IS THE EVIDENCED ONE, so a tab reads exactly as the panel of one
	// does: where the answers carry blocks, the pointer's own evidence stands
	// beside them or unfolds under the row (#972). Everything the caller has
	// already laid inside the frame is height the evidence may not have, which
	// is what `above` less the frame's own top edge counts.
	rows = a.questionPanelEvidenced(q, width, inner, above-1)
	for i := first; i < len(a.questionBands); i++ {
		a.questionBands[i].row += above
	}
	keys = a.questionAnswerKeys(q, formsCard)
	keyRow = a.questionKeyRow(q, append(append([]questionVerb{}, edge...), questionKeysOnTier(keys, keyPrimary)...), frameEdgeRoom(width))
	if q.writing != "" {
		// THE EDGE SAYS WHAT THE BOX MEANS while the composer is pointed at the
		// question ([app.questionWritingRow]): every letter types, so an edge
		// naming letters would be naming keys that do something else.
		keyRow = a.pal.dim(fit(a.questionWritingRow(q), frameEdgeRoom(width)))
	}
	return rows, keyRow, keys
}

// questionPanelBeat is the widening answer asking how far it goes, drawn in the
// same frame rather than in a drawing of its own: the shapes take the rows the
// answers had, and the bottom edge says the two keys that mean anything while
// they are up. It hands back no key table, so no second tier stands under it.
//
// IT IS ONE ROW OF SHAPES AND NOT A ROW EACH, because the shapes are one
// question's answers ([app.questionBeatRow] holds the spans a click resolves
// against, and they are a row's worth).
func (a *app) questionPanelBeat(q questionShown, above, inner int) (rows []string, keyRow string, keys []questionVerb) {
	row := a.questionBeatRow(q, above+1, inner)
	// The frame's side is one cell, so every span the beat recorded stands one
	// column further right than the row itself counted it.
	for i := range a.questionSpans {
		a.questionSpans[i].from++
		a.questionSpans[i].to++
	}
	return []string{"", row, ""}, a.pal.data("1–"+itoa(len(q.beat))) + a.pal.dim(" shape") +
		a.pal.dim(questionKeyGap) + a.pal.data(questionLaterKey) + a.pal.dim(" "+questionBeatBack), nil
}

// questionPanelEvidenced is the panel's body where its answers brought
// something to look at, and the ordinary body where they did not.
//
// THE CHOOSER HAS ALREADY DECIDED WHICH (questionchooser.go): at a width with
// room for two panes the answers stand in a list with the evidence of the one
// the pointer is on beside them ([viewSplit], preview pick A); narrower, the
// answer the pointer is on unfolds its evidence under its own row
// (preview-narrow pick B). Either way the evidence is given the rows that keep
// the panel inside [app.questionPanelTall], and a cut is said on its last row.
func (a *app) questionPanelEvidenced(q questionShown, width, inner, headRows int) []string {
	if !questionAnswersCarryBlocks(q.question) {
		return a.questionPanelBody(q, inner)
	}
	// The rows the panel spends that are not evidence: its two edges, the
	// second tier of keys under it, and the head where it would not fit the edge.
	spent := headRows + questionPanelFrameRows
	if a.questionViewOf(q, width) == viewSplit {
		return a.questionPanelSplit(q, inner, spent)
	}
	// MEASURED, NOT GUESSED: the body is laid once with no evidence under the
	// pointer, which is exactly the rows the answers themselves take, and what
	// is left of the panel's height is what the evidence may have.
	first := len(a.questionBands)
	bare := a.questionPanelBodyIn(q, inner, 0)
	a.questionBands = a.questionBands[:first]
	return a.questionPanelBodyIn(q, inner, max(a.questionPanelTall()-spent-len(bare), 1))
}

// questionPanelFrameRows is how many rows a panel draws around its body: the top
// edge, the bottom edge, and the one dim row of keys under the frame.
const questionPanelFrameRows = 3

// questionPanelTall is the most rows a panel may take above the box.
//
// HALF THE FRAME, AND THE REASON IS THE CONVERSATION. A question is about the
// work above it, so it never takes more of the screen than that work keeps: a
// panel taller than half the frame is a panel that has pushed the thing it is
// asking about off the screen. What does not fit is cut with its count and the
// way to the rest (`o open full`), where the page has the whole frame.
func (a *app) questionPanelTall() int {
	_, height := a.size()
	return height / 2
}

// questionPanelSplit is the body laid out as two panes: the answers in a list on
// the left, the evidence of the one the pointer is on at the right.
//
//	│                                      │                                       │
//	│ ▸ 1  Stacked         ◆ recommended   │ Stacked                               │
//	│   2  Marks only                      │ every name stays visible              │
//	│   3  Hidden behind a key             │                                       │
//	│   4  something else…                 │ then · costs three rows of transcript │
//
// THE LIST CARRIES ONLY WHAT TELLS THE ANSWERS APART AT A GLANCE — the pointer,
// the key, the word and the pick's mark. What an answer leaves true moves into
// the pane, labelled, because the pane is where the pointer is already saying
// "this one"; drawn on the row as well, it would be said twice on one screen.
func (a *app) questionPanelSplit(q questionShown, inner, spent int) []string {
	rows := make([]string, 0, len(q.question.Options)+8)
	room := max(inner-2*len(questionPanelGap), 1)
	rows = append(rows, a.questionPanelContext(q, room)...)
	top := len(rows)
	lead := questionPanelLead(q.question, room)
	// The list is as wide as its widest row needs — the lead, the word, and the
	// pick's mark with its word at the right — which is the page's own question
	// and is answered in the page's own words ([app.questionListWant]).
	want := 2*len(questionPanelGap) + a.questionListWant(q.question, lead)
	if a.questionTakesOther(q) {
		want = max(want, len(questionPanelGap)+lead+ansi.StringWidth(questionPanelOtherWord)+len(questionPanelGap))
	}
	left, right := besideSplit(inner, want)
	listRoom := max(left-2*len(questionPanelGap), 1)
	pad := questionLabelPad(q.question.Options, listRoom)
	list := []string{""}
	band := func(at int) {
		// A PRESS ON THE LIST PRESSES AN ANSWER, AND A PRESS ON THE EVIDENCE
		// PRESSES NOTHING: the pane is prose and pictures, and a click aimed at
		// a diagram that answered the question would be the worst press on this
		// surface. The span is the frame's side and the list's own cells.
		a.questionBands = append(a.questionBands, questionBand{
			row: top + len(list), span: hudSpan{from: 0, to: left + 1}, at: at,
		})
	}
	for i, option := range q.question.Options {
		for _, line := range a.questionPanelOption(q, i, option, pad, listRoom, questionBeside, questionHoverPanel) {
			band(i)
			list = append(list, line)
		}
	}
	if a.questionTakesOther(q) {
		other := questionOtherAt(q.question)
		for _, line := range a.questionPanelOther(q, pad, listRoom, questionHoverPanel) {
			band(other)
			list = append(list, line)
		}
	}
	if row := a.questionScopeRow(q, listRoom); row != "" {
		list = append(list, "", row)
	}
	list = append(list, "")
	evidence := a.questionEvidenceRows(q.question, q.pick, max(right-2*len(questionPanelGap), 1), true)
	fits := max(len(list)-1, a.questionPanelTall()-spent-len(rows)-1)
	pane := []string{""}
	for _, line := range a.questionCut(evidence, fits, a.questionOpenFullWord()) {
		pane = append(pane, questionPanelGap+line)
	}
	for i := range list {
		list[i] = questionPanelGap + list[i]
	}
	rows = append(rows, besides(a.pal, list, pane, left, inner)...)
	// AND A SENTENCE ABOUT THE WHOLE QUESTION CROSSES THE SEAM. A clock that
	// will answer says what a person can do about it (#954); in a column half
	// the panel wide that sentence is cut mid-word, and in the pane beside the
	// list it would read as the pointer's own answer. So it is drawn under both
	// panes, at the frame's own width, which is the width it was written for.
	if aside := a.questionClockAside(q, room); aside != "" {
		rows = append(rows, "", questionPanelGap+aside)
	}
	return rows
}

// questionOpenFullWord is the way to the rest of a cut, spelled from the key
// table so the offer and the key cannot drift apart.
func (a *app) questionOpenFullWord() string {
	return questionKeySpelling(questionOpenKey) + " " + questionKeyWord(questionOpenKey)
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
// AND IT DRAWS NO EVIDENCE UNDER THE POINTER. The split and the unfold are the
// panel's and the page's, where the drawing owns its own height and can say that
// it cut ([app.questionCut]); both callers here put these rows inside a CARD —
// the task record's ([app.taskRecordLandingRows], which then `fit`s every row to
// one line) and home's errand pane's ([app.homeExchangeRows], a card in a column
// of cards) — and a card that grew by every block an answer brought would push
// the rest of the column off the screen with no way to say so. If either surface
// should show an answer's case, that is a drawing decided for that surface with
// its own bound, not one inherited from this door.
func (a *app) questionPanelBody(q questionShown, inner int) []string {
	return a.questionPanelBodyIn(q, inner, 0)
}

// questionPanelBodyIn is [app.questionPanelBody] with the rows the evidence
// under the pointer may take, where the answers brought any: zero draws none,
// and below zero is no bound at all, which only a drawing that owns its whole
// height may ask for.
func (a *app) questionPanelBodyIn(q questionShown, inner, evidence int) []string {
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
	pad := questionLabelPad(q.question.Options, room)
	for i, option := range q.question.Options {
		under := questionUnder(evidence)
		for _, line := range a.questionPanelOption(q, i, option, pad, room, under, questionHoverPanel) {
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
		for _, line := range a.questionPanelOther(q, pad, room, questionHoverPanel) {
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
	//
	// THIS IS WHERE A QUESTION'S OWN DIM LINES GO, and the reason is the second
	// key tier. The row UNDER the frame carries the quieter keys and nothing
	// else — the owner's hints ruling put exactly `↑↓ choose · enter take it ·
	// esc later` in the bottom edge and the dropped keys on one dim row below —
	// so a sentence mixed in there is the "no hierarchy in the hints" that ruling
	// exists to prevent. A line about the question belongs inside the object: one
	// blank row, then `questionPanelGap + a.pal.dim(fit(line, room))`, which
	// framed.draw then sets to the frame's one width.
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

// questionUnder is how much an answer's rows carry under the pointer, which is
// decided by where the question's evidence is drawn.
type questionUnder int

const (
	// questionBeside is a list whose evidence stands in a pane beside it: the row
	// is the answer's word and its marks, and nothing the pane already says.
	questionBeside questionUnder = -2
	// questionUnbounded unfolds the evidence under the pointer with no bound, for
	// a column that scrolls on its own.
	questionUnbounded questionUnder = -1
	// questionRowOnly is the answer's row with what it leaves true beside its
	// word and nothing under it: the page's one column, which unfolds the
	// evidence under the pointer itself, with the person's own notes beside it.
	questionRowOnly questionUnder = -3
)

// questionPanelOption is one answer's rows: the row itself, and — only while the
// pointer is on it — what the asker said about it. `under` is how many rows of
// evidence may stand under the pointer on a question whose answers brought some,
// or one of the two shapes above.
func (a *app) questionPanelOption(q questionShown, at int, option session.AnswerOption, pad, room int, under questionUnder, hover questionHover) []string {
	key := questionOptionKeyAt(q.question, at)
	focused := at == q.pick
	picked := q.question.Pick != nil && strings.TrimSpace(q.question.Pick.Key) == key
	word := strings.TrimSpace(option.Label)
	if word == "" {
		word = key
	}
	say := strings.TrimSpace(option.Consequence)
	if under == questionBeside || (focused && questionUnfolds(q.question, under)) {
		// WHAT TAKING IT LEAVES TRUE MOVES INTO THE EVIDENCE, as its first
		// labelled line (`then ·`), wherever the evidence is drawn for this
		// answer — beside the list, or unfolded under this row (preview-narrow
		// pick B). Said on the row as well, it would be said twice, and cut the
		// first time.
		say = ""
	}
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
	case option.Safe && questionMarksSafe(q.question):
		aside = a.questionSafeAside()
	}
	rows := a.questionPanelRow(q, key, tick, word, say, aside, pad, room, focused, a.questionHovering(hover, at))
	if !focused || under == questionBeside || under == questionRowOnly {
		return rows
	}
	// UNDER THE POINTER, AND ONLY THERE: what this answer means, and — where it
	// is the asker's pick — why the asker would take it. Every answer's whole
	// case on every row would be a panel taller than the conversation under it,
	// and the pointer is the person saying which one they are weighing.
	lead := questionPanelLead(q.question, room)
	indent := questionPanelGap + strings.Repeat(" ", lead-len(questionPanelGap))
	for _, line := range a.questionPanelUnder(q, at, option, room-lead, under) {
		rows = append(rows, indent+line)
	}
	return rows
}

// questionHovering reports whether the mouse is over one answer's row. It asks
// the pointer's own kind as well as the derived index, so a stale index cannot
// light a row on a frame where the pointer is somewhere else entirely.
func (a *app) questionHovering(from questionHover, at int) bool {
	switch from {
	case questionHoverPage:
		// THE PAGE'S ROWS ARE THE PANEL'S ROWS (questionpage.go), and the
		// pointer over one of them is resolved by the page's own hit-test.
		return a.hoveringQuestionOption(at)
	case questionHoverPanel:
		return a.hot.kind == hoverChoices && a.hotAnswer == at
	}
	return false
}

// questionHover is which hit-test a drawing's rows answer to, passed in the way
// [questionUnder] is rather than sniffed from the app.
//
// A DRAWING KNOWS WHICH DRAWING IT IS AND THE APP DOES NOT. This asked
// `a.qroom != nil` — "is a page open anywhere" — and used the PAGE's hit-test
// whenever one was, so the same answer rows painted by the task record page
// ([app.taskRecordRows]) or the home errand pane ([app.homeExchangeRows])
// through [app.questionPanelBody] lit the row the page's mouse was over, on a
// screen whose own pointer was somewhere else entirely.
type questionHover int

const (
	// questionHoverPanel is the block above the box, and every other surface
	// that paints these rows through [app.questionPanelBody].
	questionHoverPanel questionHover = iota
	// questionHoverPage is the page `o` opens, which resolves a press through
	// its own spots ([app.questionRoomSpotAt]).
	questionHoverPage
	// questionHoverNone is a drawing with no mouse of its own — a screenshot,
	// or rows laid to be measured rather than painted.
	questionHoverNone
)

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

// questionLabelPad is the column every answer's word stands in: the widest label
// that still leaves half the row for what taking it produces.
//
// ONE ACCOUNT OF ONE NUMBER, because the panel's split, the panel's plain body,
// the page's list and the page's one column all draw the SAME list and were each
// computing this themselves — the split with `min(widest, room/2)` and the other
// three with `max(labels that fit)`. Those are different numbers, not two
// spellings of one: labels of 5 and 30 in a forty-cell list give 20 and 5. The
// three agree and the one was the outlier, so the three are the rule here, and
// it is the better rule — a label OVER the bound is not clipped by the pad (it
// simply overhangs into its own row, see [app.questionPanelRow]), so padding the
// short labels out to a bound no label reaches only spends the row on air.
func questionLabelPad(options []session.AnswerOption, room int) int {
	pad := 0
	for _, option := range options {
		if w := ansi.StringWidth(strings.TrimSpace(option.Label)); w > pad && w <= room/2 {
			pad = w
		}
	}
	return pad
}

// questionListWant is how much width a list of answers asks for, gutters aside:
// the lead the pointer and key stand in, the widest answer's word, and — where
// the asker made a pick — its mark and word at the right edge.
//
// ONE FORMULA, because the panel's split and the page both ask this question of
// the same list and answered it differently: the panel counted the word
// `recommended` alone and counted it always; the page counted the MARK and the
// word, and only where there is a pick. The page's reading is the true one — the
// mark is drawn ([app.questionPanelOption]) and neither is drawn without a pick —
// so a question with no recommendation no longer reserves fourteen cells for one.
// Each caller adds its own gutters, which differ because the page indents its
// list one gap inside the seam and the panel does not.
func (a *app) questionListWant(q session.Question, lead int) int {
	words := 0
	for _, option := range q.Options {
		words = max(words, ansi.StringWidth(strings.TrimSpace(option.Label)))
	}
	want := lead + words
	if q.Pick != nil {
		want += 2 + ansi.StringWidth(a.icon(tokens.GRecommended)+" "+questionRecommendedWord) + 2
	}
	return want
}

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

// questionUnfolds reports whether the answer the pointer is on unfolds its whole
// evidence under its row: on the page's one column always, and on a panel
// whose answers brought something to look at.
func questionUnfolds(q session.Question, under questionUnder) bool {
	return under == questionRowOnly || (under != questionBeside && questionAnswersCarryBlocks(q))
}

// questionPanelUnder is what stands under the focused answer: its body in the
// reading ink, and the pick's own case where this is the pick.
//
// WHERE THE ANSWERS BROUGHT SOMETHING TO LOOK AT, IT IS THE EVIDENCE, UNFOLDED
// (preview-narrow pick B): the same account the pane beside a wide panel draws
// ([app.questionEvidenceRows]), without the word the row above already says,
// cut to the rows the panel has left. A panel whose answers are words alone keeps rec A's fold — the
// body and one line of the pick's case — because a labelled column under a row
// of words is a card pretending to be a page.
func (a *app) questionPanelUnder(q questionShown, at int, option session.AnswerOption, room int, under questionUnder) []string {
	if questionAnswersCarryBlocks(q.question) {
		if under == 0 {
			// The measuring pass ([app.questionPanelEvidenced]): the answers alone.
			return nil
		}
		rows := a.questionEvidenceRows(q.question, at, max(room, 8), false)
		if under == questionUnbounded {
			return rows
		}
		return a.questionCut(rows, int(under), a.questionOpenFullWord())
	}
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

// questionPanelOther is the last row: the answer that is not on the list, and
// the box it becomes while the pointer is on it.
func (a *app) questionPanelOther(q questionShown, pad, room int, hover questionHover) []string {
	at := questionOtherAt(q.question)
	if q.pick != at {
		return a.questionPanelRow(q, itoa(at+1), "", questionPanelOtherWord, "", "", pad, room, false, a.questionHovering(hover, at))
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
