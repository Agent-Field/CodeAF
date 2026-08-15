package chat

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/footer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The question block (13.3.1, 5.20 rule 2).
//
// A question is the most expensive row this product can draw: a blocked human
// is the state 5.9 puts above every other. It is also the row v1 got wrong in
// the most instructive way — the producer smuggled its options through the
// message BODY, and v1's brace-scanner quietly hid the mess by parsing prose
// back into structure. v2 does not scan braces and never will: an option that
// is not in a field is an option this surface does not know about, and saying
// so honestly is what makes the producer fix itself.
//
// So everything here reads FIELDS. [store.Message.Options] is the ordered,
// selectable answer set; [store.MessagePart] of kind [store.PartQuestion] and
// [store.Message.QuestionSeq] are the two places a row admits to being a
// question at all. A message with neither is not a question no matter what its
// prose looks like.
//
// Two shapes, one rule — the affordance names the key you actually press:
//
//   - CONSENT (5.20 rule 2): a two-option yes/no question renders the inline
//     y/n strip, each key beside the consequence it buys. No modal, no vague
//     confirm; the blast radius is the question's own words, one row above.
//   - CHOICE: numbered rows, `1`..`9`, in the order the producer set them —
//     which is the order the answer router reads them back in, so the number on
//     screen IS the number to type.
//
// Neither shape is drawn as a button. Until the click layer lands (13.3), an
// affordance that looked pressable and was not would be the same lie the
// awaiting line's interrupt hint exists to avoid.

// questionOptionCap bounds how many options are drawn as rows. Past nine the
// single-keystroke answer stops existing, and a list that long is a menu the
// producer should have made a free-text question.
const questionOptionCap = 9

// consentKeys are the two keys the y/n strip names.
var consentKeys = [2]string{"y", "n"}

// pendingAsk is one answerable question, as the keyboard needs it.
//
// It holds the SAME option slice the rows were drawn from, which is the whole
// reason it exists: 12.9.1's numbering is positional, so the number on screen
// and the number a key answers are the same index or they are a bug. Deriving
// the options a second time at keystroke would be two readings of one fact.
type pendingAsk struct {
	// seq is the durable agent_questions row this answer aims at. Zero is legal
	// and meaningful — the ask lives on the message alone — and it is carried
	// through so a bare "3" is never guessed at when two questions are open.
	seq int64
	// options are the answer set, in producer order.
	options []store.QuestionOption
	// consent says the row was drawn as the y/n strip, so y and n are the keys
	// that answer it and the digits are not what the reader is looking at.
	consent bool
	// answered marks a row whose answer has been sent, so a second keystroke
	// on the same question cannot post a second reply.
	answered bool
}

// answerable reports that this ask still takes a keystroke.
func (a *pendingAsk) answerable() bool {
	return a != nil && !a.answered && len(a.options) > 0
}

// option resolves a one-based row number to what the answer must carry.
//
// The body is the option's VALUE and falls back to its label, matching what the
// v1 surface posts for the same row: the answer router reads a reply against
// the durable options column, and a label is what a person would have typed.
func (a *pendingAsk) option(number int) (store.QuestionOption, bool) {
	if !a.answerable() || number < 1 || number > len(a.options) || number > questionOptionCap {
		return store.QuestionOption{}, false
	}
	return a.options[number-1], true
}

// answerBody is what a chosen option sends as user speech.
func answerBody(option store.QuestionOption) string {
	if value := strings.TrimSpace(option.Value); value != "" {
		return value
	}
	return strings.TrimSpace(option.Label)
}

// dressQuestion draws the askback: the amber marker row that says a person is
// blocked, and then whichever option shape the message actually carries.
//
// It reports nothing: the caller has already counted the question for the
// footer's attention column, because "how many questions are open" is a fact
// about the thread and not about how this one is drawn.
func (b *messageBlock) dressQuestion(message store.Message, part *store.QuestionPart) {
	b.segs = append(b.segs, segment{
		kind: segRef, glyph: tokens.GlyphNeedsHuman, text: "waiting on you",
		hue: blocks.HueAttention, state: blocks.StateSettled, indent: bodyIndent,
	})
	options := message.Options
	if len(options) == 0 {
		return
	}
	consent := consentShape(options, part)
	b.ask = &pendingAsk{seq: questionSeq(message, part), options: options, consent: consent}
	b.optionRows()
}

// questionSeq is the durable row an answer aims at. The part is authoritative
// where it exists — it is written off the durable row — and the message's own
// column answers for a producer that predates the parts model.
func questionSeq(message store.Message, part *store.QuestionPart) int64 {
	if part != nil && part.Seq != 0 {
		return part.Seq
	}
	return message.QuestionSeq
}

// optionRows draws the option rows, and redraws them when the cursor moves.
//
// It is a rebuild rather than a mutation of individual segments because the two
// shapes have different row counts and the cap row appears or does not: keeping
// one function that produces the whole run means the cursor can never land on a
// row the shape does not have.
func (b *messageBlock) optionRows() {
	b.segs = b.segs[:b.optionSeam()]
	ask := b.ask
	if ask == nil {
		return
	}
	if ask.consent {
		for i, option := range ask.options[:2] {
			b.segs = append(b.segs, b.optionRow(i+1, consentKeys[i], optionLabel(option)))
		}
		return
	}
	for i, option := range ask.options {
		if i >= questionOptionCap {
			b.segs = append(b.segs, segment{
				kind: segRef, glyph: tokens.GlyphCollapsed,
				text: strconv.Itoa(len(ask.options)-questionOptionCap) + " more, answer in words",
				hue:  blocks.HueNone, state: blocks.StateChrome,
				indent: bodyIndent + optionIndent,
			})
			break
		}
		b.segs = append(b.segs, b.optionRow(i+1, strconv.Itoa(i+1), optionLabel(option)))
	}
}

// optionSeam is where the option rows begin: everything up to and including the
// "waiting on you" marker is the dressing, and everything after it is the run
// optionRows owns.
func (b *messageBlock) optionSeam() int {
	for i := range b.segs {
		if b.segs[i].kind == segRef && b.segs[i].text == waitingRow {
			return i + 1
		}
	}
	return len(b.segs)
}

// waitingRow is the marker's words, named once so the seam and the row cannot
// drift apart.
const waitingRow = "waiting on you"

// optionRow is one answerable row. The cursor is shown by promoting the row's
// own glyph cell to the selection mark — not by a background band, because the
// row sits inside a transcript whose rows are not a list and a band would claim
// it is one.
func (b *messageBlock) optionRow(number int, key, label string) segment {
	row := segment{
		kind: segRef, glyph: key, text: label,
		hue: blocks.HueAttention, state: blocks.StateSettled,
		indent: bodyIndent + optionIndent,
	}
	// THE MARK GOES IN THE GUTTER AND THE WORDS DO NOT MOVE (§20).
	//
	// It used to be prepended to the row's TEXT, which pushed the selected
	// option's label from column 6 to column 8 — so walking the cursor down a
	// list made every label it touched jump two cells sideways, and the one row
	// a reader was looking at was the one row out of the grid. §20 puts a
	// selection rail in cols 0–1 with the rest of the chrome, which is where the
	// board has always drawn the same mark for the same reason.
	row.rail = b.chosen == number
	return row
}

// optionAtLine answers which option a line inside this block is, by NUMBER —
// the same one-based number the row draws and the keyboard answers with.
//
// It is arithmetic on the block's own shape rather than a second render. The
// option rows are the tail of the segment run (optionRows rebuilds them there
// and nowhere else), every one of them is a segRef and so exactly one screen
// row, and what follows them is the cut rule and the trailing blank. So the
// run's last row is len(rows) minus that trailing, and the run's length is what
// optionRows would have drawn. Nothing about the width enters into it, which is
// why a click lands on the same option at 80 columns and at 200.
//
// A capped list's final row ("N more, answer in words") is drawn and is not an
// option; it answers zero, and a click there does nothing rather than answering
// the last real row by accident.
func (b *messageBlock) optionAtLine(line, width int) (int, bool) {
	if b == nil || !b.ask.answerable() {
		return 0, false
	}
	drawn := b.drawnOptionRows()
	if drawn == 0 {
		return 0, false
	}
	rows := len(b.Rows(width))
	trailing := 1 // the blank row Rows always appends
	if blocks.CutRule(b.end, width, b.styler()) != "" {
		trailing++
	}
	// A card's dress closes with one grounded blank INSIDE its plane (§16's
	// padding rhythm), which is one more row between the last option and the end
	// of the block. The count is arithmetic on the block's own shape, so it has
	// to know the shape it is on.
	trailing += b.cardPad()
	last := rows - trailing
	first := last - drawn
	if line < first || line >= last {
		return 0, false
	}
	number := line - first + 1
	if _, ok := b.ask.option(number); !ok {
		return 0, false
	}
	return number, true
}

// drawnOptionRows is how many rows optionRows put at the tail, INCLUDING the
// "N more" row when the list was capped. It restates that function's own
// branching rather than counting segments, because the segment slice is rebuilt
// whenever the cursor moves and a count taken at the wrong moment would be a
// click that landed one row off.
func (b *messageBlock) drawnOptionRows() int {
	if b.ask == nil {
		return 0
	}
	if b.ask.consent {
		return 2
	}
	if n := len(b.ask.options); n > questionOptionCap {
		return questionOptionCap + 1
	}
	return len(b.ask.options)
}

// SetChosen moves the answer cursor and reports whether anything moved. Zero
// clears it, which is what an answered or superseded question wants.
func (b *messageBlock) SetChosen(number int) bool {
	if b.ask == nil || b.chosen == number {
		return false
	}
	if number < 0 || number > len(b.ask.options) || number > questionOptionCap {
		return false
	}
	b.chosen = number
	b.optionRows()
	b.measured = false
	b.version++
	return true
}

// optionIndent sets the options one step under the marker row that introduced
// them (5.13's two-space rhythm), so a question with options reads as one group
// rather than as four unrelated reference rows.
//
// The number is §20's own step ([blocks.IndentStep]) rather than a `2` this file
// chose, for the reason the law was written: four surfaces had each picked their
// own, and a question's options are a level of descent like any other.
const optionIndent = blocks.IndentStep

// optionLabel is what one option row says: the label, and its hint behind the
// telemetry separator when the producer wrote one. The Value is machine-facing
// continuation data and is never drawn (5.14).
func optionLabel(option store.QuestionOption) string {
	label := strings.TrimSpace(option.Label)
	if label == "" {
		label = strings.TrimSpace(option.Value)
	}
	if hint := strings.TrimSpace(option.Hint); hint != "" {
		label += " " + tokens.GlyphSeparator + " " + hint
	}
	return label
}

// consentShape recognizes the two-option consent question.
//
// THE REQUESTED SEAM IS CLOSED, so the first question this asks is the right
// one: 12.9.1 put the class and the kind on [store.QuestionPart], written off
// the durable row by the one producer every ask goes through. A part that says
// confirm IS a confirm, and a part that says consent is a consent gate, and
// neither of those is a guess about prose.
//
// The word heuristic survives underneath it for the producers that predate the
// part — a message with options and no typed part is still a question this
// surface has to draw. What it does NOT do any more is demand that a label BE a
// word: the real consent gate's options read "yes, start it" and "hold it —
// I'll trim it first" (internal/consent), so an exact match on the whole label
// meant the y/n strip never fired on the one gate 5.20 rule 2 was written for.
// It now reads the FIRST WORD, which is the word the key stands for and the
// only part of the label a key could ever have meant.
//
// It is still not a brace-scanner and never becomes one: the input is the typed
// options column, one word of one field, and never the message body.
func consentShape(options []store.QuestionOption, part *store.QuestionPart) bool {
	if len(options) != 2 {
		return false
	}
	if part != nil {
		switch {
		case part.Kind == store.QuestionConfirm:
			return true
		case part.Kind == store.QuestionChoose:
			// The producer said this is a numbered choice. Two options that
			// happen to start with "yes" and "no" do not outrank it.
			return false
		}
	}
	return affirmative(options[0]) && negative(options[1])
}

func affirmative(option store.QuestionOption) bool {
	return leadsWith(option, "y", "yes", "ok", "okay", "approve", "approved",
		"confirm", "go", "do", "start", "proceed")
}

func negative(option store.QuestionOption) bool {
	return leadsWith(option, "n", "no", "nope", "cancel", "stop", "hold", "don't",
		"do not", "abort", "leave", "wait", "skip")
}

// leadsWith matches the first word of the option's value or label.
//
// A label is a sentence a person reads ("hold it — I'll trim it first") and a
// value is a token a router reads ("hold"). Both are checked, and only the
// leading word of either, because a word further in is not what a key stands
// for — "leave it running" must not read as a refusal because it contains
// "leave" in the middle.
func leadsWith(option store.QuestionOption, words ...string) bool {
	for _, field := range []string{option.Value, option.Label} {
		lead := leadWord(field)
		if lead == "" {
			continue
		}
		for _, word := range words {
			if lead == word {
				return true
			}
		}
	}
	return false
}

// emDash is the dash a sentence hangs off, taken from the vocabulary rather
// than spelled here: tokens.GlyphMissing is the same rune, and one spelling per
// rune is the whole of §16's glyph discipline even where the rune is being READ
// rather than drawn.
var emDash = []rune(tokens.GlyphMissing)[0]

// leadWord is the first word of a field, lowercased and stripped of the
// punctuation a sentence hangs off it.
func leadWord(field string) string {
	field = strings.ToLower(strings.TrimSpace(field))
	for i, r := range field {
		if r == ' ' || r == ',' || r == ';' || r == ':' || r == '.' || r == '!' ||
			r == emDash || r == '-' {
			return field[:i]
		}
	}
	return field
}

// -- answering -----------------------------------------------------------------
//
// JOURNEY 6, and the reason it was a PARTIAL: the question rendered honestly
// and no key answered it. A digit fell into the draft, so every consent,
// charter and spend flow degraded to type-a-number-then-enter — which works,
// and which is not what the row on screen says. An affordance that names a key
// must bind it (5.20 rule 3, 5.22).
//
// The bare keys are claimed only where they cannot mean anything else: an EMPTY
// draft, no overlay raised, and a question actually open at the tail. A reader
// mid-sentence keeps their digits, which is the same rule v1 settled on
// (internal/tui/model.go's `m.input.Value() == ""` guard) for the same reason.

// openAsk is the question the keyboard acts on: the newest unanswered one above
// the reader's own last turn.
//
// It walks backwards and stops at the reader's own speech, which is the same
// walk the attention count makes and for the same reason — a question stops
// being open the moment the reader answers, and the row that proves they did is
// their own next message.
func (a *App) openAsk() *messageBlock {
	for i := a.transcript.Len() - 1; i >= 0; i-- {
		block, ok := a.transcript.Block(i).(*messageBlock)
		if !ok {
			continue
		}
		if block.user {
			return nil
		}
		if block.ask.answerable() {
			return block
		}
	}
	return nil
}

// answerKey is the answering ladder. It reports whether it claimed the key.
func (a *App) answerKey(key string) (tea.Cmd, bool) {
	if a.overlay != overlayNone || a.railFocus {
		return nil, false
	}
	if a.drafting() {
		// A draft is a sentence in progress. Digits belong to it.
		return nil, false
	}
	block := a.openAsk()
	if block == nil {
		return nil, false
	}
	ask := block.ask
	switch {
	case ask.consent && (key == consentKeys[0] || key == consentKeys[1]):
		// 5.20 rule 2's inline strip: each key beside the consequence it buys.
		number := 1
		if key == consentKeys[1] {
			number = 2
		}
		return a.answer(block, number), true

	case len(key) == 1 && key[0] >= '1' && key[0] <= '9' && !ask.consent:
		return a.answer(block, int(key[0]-'0')), true

	case key == "up" || key == "down":
		return nil, a.moveChoice(block, key == "down")

	case key == "enter" && block.chosen > 0:
		return a.answer(block, block.chosen), true
	}
	return nil, false
}

// moveChoice walks the answer cursor. It reports whether the key was claimed,
// which it is only while there is somewhere to walk: an arrow over a question
// with one option is an arrow the transcript should still get.
func (a *App) moveChoice(block *messageBlock, down bool) bool {
	count := min(len(block.ask.options), questionOptionCap)
	if count < 2 {
		return false
	}
	next := block.chosen
	switch {
	case down:
		next++
	case next == 0:
		next = count
	default:
		next--
	}
	if next > count {
		next = 1
	}
	if next < 1 {
		next = count
	}
	if !block.SetChosen(next) {
		return false
	}
	a.shell.Invalidate()
	return true
}

// answer sends one option as user speech.
//
// The question's own sequence rides with it, because a bare "3" is the right
// answer only by luck once a second question is open: what the reader did was
// point at a row of a PARTICULAR question, and the store honours an explicit
// reference rather than guessing (the rule v1 states at cards.go:1692).
//
// The ask is marked answered before the post is even attempted, and that is
// deliberate: the row must stop taking keystrokes at the moment the reader
// pressed one, or a second press posts a second answer to a question that
// already has one. If the post fails, the status line says so — the question is
// still in the journal, and the next poll draws it again.
func (a *App) answer(block *messageBlock, number int) tea.Cmd {
	option, ok := block.ask.option(number)
	if !ok {
		return nil
	}
	block.ask.answered = true
	block.SetChosen(0)
	a.shell.Invalidate()
	return a.answerCmd(block.ask.seq, answerBody(option))
}

// answerCmd posts the answer through the same single door every other draft
// takes. Answering IS speaking: the head is watching that door, and a surface
// that also poked it would be a second way to end a wait.
func (a *App) answerCmd(seq int64, body string) tea.Cmd {
	body = strings.TrimSpace(body)
	if body == "" || a.backend == nil {
		return nil
	}
	backend, session := a.backend, a.session
	return func() tea.Msg {
		posted, err := thread.Post(backend, store.Message{
			SessionID:   session,
			Role:        store.RoleUser,
			Body:        body,
			QuestionSeq: seq,
		})
		return postResultMsg{message: posted, err: err}
	}
}

// keyMode is what a bare digit does right now, for the footer's own column.
//
// It is derived from exactly the state answerKey consults, so the row that says
// "1-3 answer" and the key that answers cannot disagree. Rail focus wins because
// the map binds the digits as jumps there, and an empty draft is required
// because a digit typed into a sentence is a digit.
func (a *App) keyMode() (footer.KeyMode, int) {
	if a.railFocus {
		return footer.KeyModeRooms, min(a.railModel.Len(), 9)
	}
	if a.overlay != overlayNone {
		return footer.KeyModeNone, 0
	}
	if a.drafting() {
		return footer.KeyModeNone, 0
	}
	block := a.openAsk()
	if block == nil || block.ask.consent {
		// The consent strip names y and n, not digits, so the digit column would
		// be advertising keys that do nothing (5.20 rule 3).
		return footer.KeyModeNone, 0
	}
	return footer.KeyModeAnswer, min(len(block.ask.options), questionOptionCap)
}
