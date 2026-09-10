package tui3

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE FOUR SHAPES BELOW A QUESTION, which is rung six of the ladder:
// "structured input — blanks · checklist · this-or-this · dial, never free text
// where a key would do" (docs/design/questions/DESIGN.md).
//
// WHY THE RUNG EXISTS AT ALL. A question whose answer is "postgres, port 5432,
// keep the old file" asked as free text is a question the model has to parse,
// and it parses it wrong roughly as often as a person types it differently from
// how they said it last time. Every shape here turns that into keys: a hole with
// a kind that validates it, a row that ticks, a pair that has exactly two sides,
// a dial with a sentence under it saying what the setting DOES. The answer that
// comes back is structured because the input was, not because anything guessed.
//
// ── THE ROWS, DRAWN HERE SO THEY STAY TRUE ──
//
// blanks, at 100 columns, the second hole focused. A choice hole wears the
// disclosure mark and a hole that takes anything does not, so the two are told
// apart without a legend:
//
//	  land them in [ ~/notes ] as [ a new file ] and keep the old copy: [ no ▾ ]
//	  name · anything you like
//
// checklist, after `a` took the suggestion and `shift+↑` lifted the second row:
//
//	  everything ticked goes in one commit.
//	  ✓ 2 move the tests beside them
//	  ✓ 1 rewrite the imports
//	    3 delete the old package
//	    4 rename the module
//
// pairs — one at a time, because a person answers a pair in a quarter second and
// a page of them is a page nobody finishes:
//
//	  which matters more here?
//	  speed against completeness
//	  a  finishing tonight    b  keeping every old row
//	  1 of 3
//
// dial:
//
//	  for questions in this project
//	  ask me everything · [tell me, then act] · just do it
//	  it will tell me, then act
//
// The reader tier never draws a dial as a picture — DESIGN.md: "the reader tier
// never draws a dial (a number input instead)" — so at that tier the middle row
// is `2 of 3 · tell me, then act` and the keys still move it.

// questionBlank is one hole in a sentence: the asker's field and the person's
// answer to it.
type questionBlank struct {
	blank session.Blank
	// value is what has been typed or chosen. It starts at the asker's default,
	// which is the whole reason a default exists — a form that opened empty asks
	// a person to retype what the asker already knew.
	value string
	// at is which of a choice blank's choices the value is, so `←→` walks them
	// without re-searching a list that may hold the same word twice.
	at int
}

// questionPair is one this-or-that: the thing being decided and its two sides.
//
// IT IS BUILT OUT OF [session.Blank] AND NOT OUT OF A FIELD OF ITS OWN. A pair
// IS a hole with exactly two choices — "storage: postgres or sqlite" — so the
// object already had the shape, and adding a second one would have meant two
// answers to "what is a two-way question" that a validator would have to keep
// agreeing about.
type questionPair struct {
	label string
	a, b  string
	// answer is "a", "b", "=" or "" for a pair nobody has reached yet.
	answer string
}

// questionInput is the structured shape a question carries, or the zero value
// for one that carries none. It is a single struct rather than four because the
// page asks it four questions — how many rows, which one is focused, what a key
// does, what to put on the answer — and four types would mean four switches at
// every one of those.
type questionInput struct {
	kind   session.InputKind
	prompt string
	focus  int
	// blanks, ticks, pairs and the dial: exactly one family is ever populated,
	// named by kind.
	blanks []questionBlank
	// ticks is parallel to the question's options.
	ticks []bool
	// order is the options' indices in the order the person put them, and it is
	// nil until a `shift+↑↓` moves one — an order nobody touched is the asker's
	// order and must be recorded as such rather than as a choice.
	order []int
	pairs []questionPair
	dial  *session.Dial
	// notch is where the dial sits, as an index into its labels, or as a step
	// between min and max where it has none.
	notch int
	// notches is how many positions the dial has.
	notches int
}

// newQuestionInput reads the question's own input shape into the page's state.
func newQuestionInput(q session.Question) questionInput {
	in := questionInput{kind: q.Input.Kind, prompt: strings.TrimSpace(q.Input.Prompt)}
	switch q.Input.Kind {
	case session.InputBlanks:
		for _, b := range q.Input.Blanks {
			hole := questionBlank{blank: b, value: strings.TrimSpace(b.Default)}
			for i, choice := range b.Choices {
				if choice == hole.value {
					hole.at = i
				}
			}
			if hole.value == "" && b.Kind == session.BlankChoice && len(b.Choices) > 0 {
				hole.value = b.Choices[0]
			}
			in.blanks = append(in.blanks, hole)
		}
	case session.InputChecklist:
		in.ticks = make([]bool, len(q.Options))
	case session.InputPairs:
		for _, b := range q.Input.Blanks {
			if len(b.Choices) < 2 {
				continue
			}
			in.pairs = append(in.pairs, questionPair{label: strings.TrimSpace(b.Label), a: b.Choices[0], b: b.Choices[1]})
		}
	case session.InputDial:
		in.dial = q.Input.Dial
		if in.dial == nil {
			break
		}
		in.notches = len(in.dial.Labels)
		if in.notches == 0 {
			// A DIAL WITH NO WORDS IS A NUMBER, and it gets eleven notches so the
			// two ends and the middle are all reachable in a few presses.
			in.notches = 11
		}
		in.notch = questionDialNotch(*in.dial, in.notches)
	}
	return in
}

// questionDialNotch is where the asker left the dial, as an index.
func questionDialNotch(dial session.Dial, notches int) int {
	if notches <= 1 || dial.Max <= dial.Min {
		return 0
	}
	share := (dial.Default - dial.Min) / (dial.Max - dial.Min)
	at := int(share*float64(notches-1) + 0.5)
	if at < 0 {
		at = 0
	}
	if at >= notches {
		at = notches - 1
	}
	return at
}

// count is how many rows the focus walks, which is what bounds `up`/`down`.
func (in questionInput) count() int {
	switch in.kind {
	case session.InputBlanks:
		return len(in.blanks)
	case session.InputChecklist:
		return len(in.ticks)
	case session.InputPairs:
		return len(in.pairs)
	}
	return 0
}

// partKey names the row the focus is on, as the key a comment is filed under.
// It is the blank's own label or the pair's, because that is what a person would
// call the thing they commented on — never an index, which means nothing in a
// record read six weeks later.
func (in questionInput) partKey() string {
	switch in.kind {
	case session.InputBlanks:
		if in.focus >= 0 && in.focus < len(in.blanks) {
			return in.blanks[in.focus].blank.Label
		}
	case session.InputPairs:
		if in.focus >= 0 && in.focus < len(in.pairs) {
			return in.pairs[in.focus].label
		}
	}
	return ""
}

// fill writes what the shape holds onto the answer that is about to be spent.
//
// EACH FIELD IS WRITTEN ONLY WHERE IT WAS ANSWERED. [session.Answer.Dial] is a
// pointer for exactly this reason — "nil where there was no dial, which is not
// the same as a dial left at zero" — and the same care is owed to a blank nobody
// filled: an empty string in the record is a claim that somebody typed nothing,
// where leaving it out is the truth.
func (in questionInput) fill(answer *session.Answer) {
	switch in.kind {
	case session.InputBlanks:
		for _, hole := range in.blanks {
			value := strings.TrimSpace(hole.value)
			if value == "" {
				continue
			}
			if answer.Blanks == nil {
				answer.Blanks = map[string]string{}
			}
			answer.Blanks[hole.blank.Label] = value
		}
	case session.InputPairs:
		for _, pair := range in.pairs {
			if pair.answer == "" {
				continue
			}
			if answer.Blanks == nil {
				answer.Blanks = map[string]string{}
			}
			answer.Blanks[pair.label] = questionPairWord(pair)
		}
	case session.InputDial:
		if in.dial == nil {
			return
		}
		at := in.value()
		answer.Dial = &at
	}
}

// questionPairWord is what one pair answered with, in the words of the side that
// won rather than as `a` or `b` — a record that said "a" would be unreadable the
// moment the question is gone.
func questionPairWord(pair questionPair) string {
	switch pair.answer {
	case "a":
		return pair.a
	case "b":
		return pair.b
	}
	return "either"
}

// value is where the dial sits, in the asker's own units.
func (in questionInput) value() float64 {
	if in.dial == nil || in.notches <= 1 {
		return 0
	}
	share := float64(in.notch) / float64(in.notches-1)
	return in.dial.Min + share*(in.dial.Max-in.dial.Min)
}

// dialWord is what the dial's current notch is called, and it is the labels'
// own word where there is one. A dial with no words says its number, which is
// also what the reader tier gets.
func (in questionInput) dialWord() string {
	if in.dial == nil {
		return ""
	}
	if in.notch >= 0 && in.notch < len(in.dial.Labels) {
		return in.dial.Labels[in.notch]
	}
	return strconv.FormatFloat(in.value(), 'g', 3, 64)
}

// ─────────────────────────────────────────────────────────────────────────────
// The keys.

// questionInputKey routes one key into the live input shape and reports whether
// it took it. Every key the shape does NOT own falls through untouched, so the
// page's own keys keep working over a form.
//
// THE KEYS ARE THE SHARED TABLE'S (questionkeys.go) and never spelled here. `a`
// is the one collision in the grammar — "take its suggestion" on a checklist and
// "the first one" on a pair — and it is resolved by WHICH SHAPE IS ON SCREEN
// rather than by giving one of them a second key, because they are the same
// instinct at two shapes: take the thing on the left.
func (a *app) questionInputKey(key string) bool {
	room := a.qroom
	if room == nil || room.input.kind == session.InputNone {
		return false
	}
	in := &room.input
	switch key {
	case questionBlankKey:
		in.focus = questionStep(in.focus, 1, in.count())
	case "shift+tab":
		in.focus = questionStep(in.focus, -1, in.count())
	case questionToggleKey:
		if in.kind != session.InputChecklist || in.focus >= len(in.ticks) {
			return false
		}
		in.ticks[in.focus] = !in.ticks[in.focus]
		a.questionSyncTicks()
	case "shift+up", "shift+down":
		if in.kind != session.InputChecklist {
			return false
		}
		delta := -1
		if key == "shift+down" {
			delta = 1
		}
		in.moveOrder(in.focus, delta)
		in.focus = questionStep(in.focus, delta, in.count())
		// AND WHAT THE FOOT WOULD SEND MOVES WITH THE ROWS. The order IS part of
		// the answer on a checklist that is ordered, so a foot still listing the
		// asker's order after a person has changed it is a foot describing a
		// different answer from the one on screen.
		a.questionSyncTicks()
	case questionSuggestKey, questionPairBKey, questionSameKey:
		if in.kind == session.InputChecklist && key == questionSuggestKey {
			a.questionTakeSuggestion()
			return true
		}
		if in.kind != session.InputPairs || in.focus >= len(in.pairs) {
			return false
		}
		in.pairs[in.focus].answer = map[string]string{
			questionPairAKey: "a", questionPairBKey: "b", questionSameKey: questionSameKey,
		}[key]
		// A PAIR ANSWERED MOVES ON BY ITSELF. That is the whole reason pairs are
		// drawn one at a time: the shape is a rhythm — look, press, look, press —
		// and a cursor a person has to advance themselves halves the speed that
		// makes this shape worth having.
		if in.focus+1 < len(in.pairs) {
			in.focus++
		}
	case "left", "right":
		if in.kind == session.InputDial {
			delta := -1
			if key == "right" {
				delta = 1
			}
			in.notch = questionClamp(in.notch+delta, 0, in.notches-1)
			break
		}
		if in.kind != session.InputBlanks {
			return false
		}
		return questionWalkChoice(in, key)
	default:
		return false
	}
	return true
}

// questionWalkChoice is `←` and `→` on a choice blank, and it reports whether
// there was one under the cursor to walk.
//
// ←→ ON A CHOICE BLANK WALKS ITS CHOICES, which is the same gesture as a dial
// one shape down: the hole has a short list and the arrows are how a person sees
// the list without opening anything.
//
// IT IS A FUNCTION OF ITS OWN BECAUSE TWO PLACES DO IT. The room routes every
// key a shape offers ([app.questionInputKey]); the block routes only the keys it
// DRAWS, and on a task proposal's card that is these two and nothing else — so
// the card reaches the move without inheriting `tab`, `space` and the pair keys
// it never spelled.
func questionWalkChoice(in *questionInput, key string) bool {
	if in == nil || in.focus < 0 || in.focus >= len(in.blanks) {
		return false
	}
	hole := &in.blanks[in.focus]
	if hole.blank.Kind != session.BlankChoice || len(hole.blank.Choices) == 0 {
		return false
	}
	delta := -1
	if key == "right" {
		delta = 1
	}
	hole.at = questionClamp(hole.at+delta, 0, len(hole.blank.Choices)-1)
	hole.value = hole.blank.Choices[hole.at]
	return true
}

// questionSyncTicks copies the ticked rows onto what the foot would send, so the
// compose row and the list are one fact rather than two.
func (a *app) questionSyncTicks() {
	room := a.qroom
	picked := make([]string, 0, len(room.input.ticks))
	for _, i := range room.input.walk() {
		if i < len(room.input.ticks) && room.input.ticks[i] && i < len(room.head.question.Options) {
			picked = append(picked, room.head.question.Options[i].Key)
		}
	}
	room.picked = picked
}

// questionTakeSuggestion is `a`: tick what the asker would tick.
//
// WHAT THAT MEANS TODAY, SAID PLAINLY. [session.Question] can name ONE pick and
// can mark ONE answer safe, so the suggestion this key can honestly take is
// those two — there is no field on the object that means "the asker would tick
// this row", and inventing one here would be this surface deciding what the
// asker meant. Where neither exists the key is not offered at all
// ([app.questionOfferKeys]), which is the emptiness law rather than a key that
// does nothing.
func (a *app) questionTakeSuggestion() {
	room := a.qroom
	for i, opt := range room.head.question.Options {
		suggested := opt.Safe
		if room.head.question.Pick != nil && room.head.question.Pick.Key == opt.Key {
			suggested = true
		}
		if i < len(room.input.ticks) {
			room.input.ticks[i] = suggested
		}
	}
	a.questionSyncTicks()
}

// walk is the rows in the order they are drawn and answered, which is the
// person's order once they have moved one and the asker's until then.
func (in questionInput) walk() []int {
	if in.order != nil {
		return in.order
	}
	out := make([]int, len(in.ticks))
	for i := range out {
		out[i] = i
	}
	return out
}

// moveOrder lifts one row past its neighbour, materialising the person's own
// order the first time they touch it.
func (in *questionInput) moveOrder(at, delta int) {
	order := append([]int{}, in.walk()...)
	to := at + delta
	if at < 0 || at >= len(order) || to < 0 || to >= len(order) {
		return
	}
	order[at], order[to] = order[to], order[at]
	in.order = order
}

// questionStep walks a bounded list, and it STOPS at the ends rather than
// wrapping — a form whose tab key jumped from the last hole to the first is a
// form people fill in twice.
func questionStep(at, delta, n int) int {
	if n <= 0 {
		return 0
	}
	return questionClamp(at+delta, 0, n-1)
}

func questionClamp(at, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if at < lo {
		return lo
	}
	if at > hi {
		return hi
	}
	return at
}

// ─────────────────────────────────────────────────────────────────────────────
// The drawing.

// questionInputRows draws whichever shape the question carries.
func (a *app) questionInputRows(width int) []string {
	room := a.qroom
	if room == nil {
		return nil
	}
	switch room.input.kind {
	case session.InputBlanks:
		return a.questionBlankRows(&room.input, width)
	case session.InputChecklist:
		return a.questionChecklistRows(width)
	case session.InputPairs:
		return a.questionPairRows(width)
	case session.InputDial:
		return a.questionDialRows(width)
	case session.InputText:
		// FREE TEXT IS THE BOX, and it is already on screen. Drawing a second one
		// on the page would be two places to type one answer, so the shape says
		// what to say and points at the box every other message goes into.
		if room.input.prompt == "" {
			return nil
		}
		return []string{questionIndent + a.pal.ink(fit(room.input.prompt, max(1, width-2)))}
	}
	return nil
}

// questionBlankRows is the sentence with holes in it.
//
// THE SENTENCE IS THE ASKER'S WHERE IT WROTE ONE. A prompt naming its holes with
// `{label}` is drawn as one line with the holes in place, which is what makes
// this shape read as a sentence rather than as a form; a prompt that names none
// is drawn above the holes, and a shape with no prompt at all is the holes
// alone. All three are the same rows underneath.
//
// IT TAKES THE SHAPE RATHER THAN READING THE ROOM, because the room is no longer
// the only place a hole is drawn: a task proposal's card carries one choice blank
// for the model the work runs on (task.go's [app.taskShown]), and a card that
// drew its own hole beside this one would be the two-renderings defect the
// question block exists to end.
func (a *app) questionBlankRows(in *questionInput, width int) []string {
	inner := max(1, width-len(questionIndent))
	out := make([]string, 0, len(in.blanks)+3)
	if sentence, inline := a.questionBlankSentence(in, inner); inline {
		out = append(out, questionIndent+sentence)
	} else {
		if in.prompt != "" {
			out = append(out, questionIndent+a.pal.ink(fit(in.prompt, inner)))
		}
		for i, hole := range in.blanks {
			out = append(out, questionIndent+a.questionHole(in, i, hole))
		}
	}
	// The focused hole says what it takes, under the sentence: a hole whose kind
	// is a path says so, and one that will not take what is in it says that
	// instead. It is one row and it is about the hole the cursor is in, because
	// a column of validation notes is a form shouting.
	if note := a.questionHoleNote(in); note != "" {
		out = append(out, questionIndent+a.pal.dim(fit(note, inner)))
	}
	return out
}

// questionCardBlankRows is the same sentence on a CARD, which is one row and not
// the page's shape.
//
// WHAT IS DROPPED IS THE NOTE, AND ONLY WHEN IT HAS NOTHING TO REFUSE. The page
// draws `model · one of these` under the sentence because a page is where a
// person is filling several holes in and has to learn what each of them takes; a
// card carries one hole and its offer row already says `[←→] move it`, so the
// note there is the same fact twice on two rows. A refusal is not the same fact
// twice — it is the one thing the sentence cannot say — so it stays.
func (a *app) questionCardBlankRows(in *questionInput, width int) []string {
	rows := a.questionBlankRows(in, width)
	if len(rows) == 0 || in.focus < 0 || in.focus >= len(in.blanks) {
		return rows
	}
	if questionBlankRefusal(in.blanks[in.focus]) != "" {
		return rows
	}
	return rows[:len(rows)-1]
}

// questionBlankSentence draws the prompt with its holes substituted in, and
// reports false where the prompt names none of them.
func (a *app) questionBlankSentence(in *questionInput, width int) (string, bool) {
	if in.prompt == "" {
		return "", false
	}
	line, found := in.prompt, false
	for i, hole := range in.blanks {
		token := "{" + hole.blank.Label + "}"
		if !strings.Contains(line, token) {
			continue
		}
		found = true
		line = strings.Replace(line, token, a.questionHole(in, i, hole), 1)
	}
	if !found {
		return "", false
	}
	return fit(line, width), true
}

// questionHole is one `[value ▾]` or `[value  ]`, painted so the one the cursor
// is in is unmistakable.
func (a *app) questionHole(in *questionInput, i int, hole questionBlank) string {
	value := strings.TrimSpace(hole.value)
	if value == "" {
		value = strings.TrimSpace(hole.blank.Label)
	}
	body := " " + value + " "
	if hole.blank.Kind == session.BlankChoice && len(hole.blank.Choices) > 0 {
		body = " " + value + " " + a.icon(tokens.GExpanded) + " "
	}
	text := "[" + body + "]"
	if i == in.focus {
		return a.pal.askBold(text)
	}
	return a.pal.dim(text)
}

// questionHoleNote is the one line under the sentence about the hole the cursor
// is in: what it takes, or why what is in it will not do.
//
// EVERY BLANK IS VALIDATED BY ITS KIND, and this row is where that shows. A
// number blank holding letters says so here rather than at the moment enter is
// pressed, which is the difference between a form that helps and one that
// scolds.
func (a *app) questionHoleNote(in *questionInput) string {
	if in.focus < 0 || in.focus >= len(in.blanks) {
		return ""
	}
	hole := in.blanks[in.focus]
	if bad := questionBlankRefusal(hole); bad != "" {
		return bad
	}
	return strings.TrimSpace(hole.blank.Label) + questionSep + questionBlankKindWord(hole.blank.Kind)
}

// questionBlankKindWord is what a hole takes, in the person's words.
func questionBlankKindWord(kind session.BlankKind) string {
	switch kind {
	case session.BlankPath:
		return "a file or folder"
	case session.BlankNumber:
		return "a number"
	case session.BlankChoice:
		return "one of these · ←→ to change it"
	case session.BlankTime:
		return "a time"
	}
	return "anything you like"
}

// questionBlankRefusal is why what is in a hole will not do, or "" where it will.
func questionBlankRefusal(hole questionBlank) string {
	value := strings.TrimSpace(hole.value)
	if value == "" {
		return ""
	}
	switch hole.blank.Kind {
	case session.BlankNumber:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return "that is not a number"
		}
	case session.BlankChoice:
		for _, choice := range hole.blank.Choices {
			if choice == value {
				return ""
			}
		}
		return "that is not one of the answers it offered"
	}
	return ""
}

// questionChecklistRows are the things to tick, in the order they will be
// answered in.
func (a *app) questionChecklistRows(width int) []string {
	room := a.qroom
	in := room.input
	inner := max(1, width-len(questionIndent))
	out := make([]string, 0, len(in.ticks)+2)
	if in.prompt != "" {
		out = append(out, questionIndent+a.pal.ink(fit(in.prompt, inner)))
	}
	for at, i := range in.walk() {
		if i >= len(room.head.question.Options) {
			continue
		}
		opt := room.head.question.Options[i]
		mark := " "
		if i < len(in.ticks) && in.ticks[i] {
			mark = a.icon(tokens.GSettled)
		}
		text := mark + " " + opt.Key + " " + strings.TrimSpace(opt.Label)
		line := questionIndent + a.pal.ink(text)
		if i < len(in.ticks) && in.ticks[i] {
			line = questionIndent + a.pal.askBold(text)
		}
		if at == in.focus {
			line = a.pal.cursor(fit(line, width), width)
		}
		out = append(out, fit(line, width))
		out = append(out, a.questionNoteRows(opt.Key, len(questionBodyIndent), width)...)
	}
	return out
}

// questionPairRows is ONE pair, and the count of how many there are.
func (a *app) questionPairRows(width int) []string {
	room := a.qroom
	in := room.input
	inner := max(1, width-len(questionIndent))
	if len(in.pairs) == 0 {
		return nil
	}
	at := questionClamp(in.focus, 0, len(in.pairs)-1)
	pair := in.pairs[at]
	out := make([]string, 0, 4)
	if in.prompt != "" {
		out = append(out, questionIndent+a.pal.ink(fit(in.prompt, inner)))
	}
	if pair.label != "" {
		out = append(out, questionIndent+a.pal.dim(fit(pair.label, inner)))
	}
	side := func(key, word string) string {
		if pair.answer == key {
			return a.pal.askBold(key + "  " + word)
		}
		return a.pal.ask(key) + "  " + a.pal.ink(word)
	}
	out = append(out, fit(questionIndent+side("a", pair.a)+"    "+side("b", pair.b), width))
	// The count is what makes a run of pairs bearable: a person answering four
	// of them wants to know it is four and not forty.
	count := strconv.Itoa(at+1) + " of " + strconv.Itoa(len(in.pairs))
	if pair.answer == questionSameKey {
		count += questionSep + questionKeyWord(questionSameKey)
	}
	out = append(out, questionIndent+a.pal.dim(fit(count, inner)))
	out = append(out, a.questionNoteRows(pair.label, len(questionBodyIndent), width)...)
	return out
}

// questionDialRows is the dial and the sentence under it saying what the setting
// DOES — which DESIGN.md asks for by name, and which is the difference between
// a slider and a decision.
func (a *app) questionDialRows(width int) []string {
	room := a.qroom
	in := room.input
	inner := max(1, width-len(questionIndent))
	if in.dial == nil {
		return nil
	}
	out := make([]string, 0, 3)
	if in.prompt != "" {
		out = append(out, questionIndent+a.pal.ink(fit(in.prompt, inner)))
	}
	out = append(out, questionIndent+fit(a.questionDialFace(inner), inner))
	if word := questionDialSentence(*in.dial, in.notch); word != "" {
		out = append(out, questionIndent+a.pal.dim(fit(word, inner)))
	}
	return out
}

// questionDialFace is the dial itself.
//
// THE READER TIER GETS A NUMBER, NEVER A PICTURE (DESIGN.md). A row of gauge
// cells says "third of five" to an eye and says nothing at all to a screen
// reader, so at that tier the same fact is spelled `3 of 5 · tell me, then act`
// and the same two keys move it.
func (a *app) questionDialFace(width int) string {
	in := a.qroom.input
	if a.pal.linear || a.pal.ascii {
		return a.pal.ask(strconv.Itoa(in.notch+1) + " of " + strconv.Itoa(in.notches) +
			questionSep + in.dialWord())
	}
	// With words, the dial IS its words: the one it is on in the question hue and
	// the rest dim, which reads at a glance and needs no legend.
	if len(in.dial.Labels) > 0 {
		parts := make([]string, 0, len(in.dial.Labels))
		for i, label := range in.dial.Labels {
			if i == in.notch {
				parts = append(parts, a.pal.askBold("["+label+"]"))
				continue
			}
			parts = append(parts, a.pal.dim(label))
		}
		return strings.Join(parts, questionSep)
	}
	// Without words it is a number, drawn as a bar of the vocabulary's own gauge
	// cells so the shape of the setting is visible beside the figure.
	cells := max(4, min(24, width-12))
	var bar strings.Builder
	for i := range cells {
		share := float64(i) / float64(cells-1)
		filled := share <= float64(in.notch)/float64(max(1, in.notches-1))
		if filled {
			bar.WriteString(a.pal.ask(tokens.Gauge(1)))
			continue
		}
		bar.WriteString(a.pal.dim(tokens.Gauge(0)))
	}
	return bar.String() + a.pal.dim(" "+in.dialWord())
}

// questionDialSentence is what the notch MEANS, and it is the asker's own label
// turned into a sentence about behaviour. A dial whose labels are already
// sentences says them; one whose labels are words says the word and the page's
// own framing around it.
func questionDialSentence(dial session.Dial, notch int) string {
	if notch < 0 || notch >= len(dial.Labels) {
		return ""
	}
	label := strings.TrimSpace(dial.Labels[notch])
	if label == "" {
		return ""
	}
	if strings.Contains(label, " ") && len(label) > 24 {
		return label
	}
	return "it will " + label
}
