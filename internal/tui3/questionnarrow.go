package tui3

import (
	tea "charm.land/bubbletea/v2"

	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE NARROW SHEET ────────────────────────────────────────────────────────
//
// UNDER SIXTY COLUMNS THE BLOCK BECOMES A BOTTOM SHEET.
//
// THIS IS NOT questionsheet.go. That file is the sheet of MANY questions one
// step raised, drawn at every width; this one is ONE question laid out for a
// narrow frame. They were briefly the same filename in two lanes, which is why
// every name here says `narrow` and every name there says `sheet`.
//
// The block above is a line of words with the answers in it, and on a
// phone-sized frame it is the wrong object twice over. It is too wide — the
// answers row's own narrow degrading exists because at that width the sentence
// stops fitting — and it is untappable, because there is no keyboard on a phone
// and `[1]` is three cells for a thumb that covers ten. So at [tierPhone] the
// same facts are laid out as a sheet over the bottom of the screen:
//
//	───── ? bash ─────────────
//	 git commit -m "wave"
//	 bash pattern "git *"
//	──────────────────────────
//	 [1] allow once
//	 [2] always, this command
//	 [3] deny
//	 2 more                 8s
//
// Four decisions, and each is the phone's own:
//
//   - THE ANSWERS ARE BANDS, NOT COLUMNS. Three answers across forty-four
//     columns is fourteen cells each before the gaps, and a phone tier reaches
//     down to twenty columns where three columns is four cells each — a target
//     that misses. A band is the WHOLE ROW, at every width this tier has, and it
//     is the only shape that holds the floor everywhere.
//   - THE CLOCK IS ON ITS OWN CORNER. It stays bottom-right where the sketch put
//     it, but off the bands: a countdown drawn inside a full-width target is a
//     word you cannot touch without answering, and the one answer nobody means
//     to give is the one they were reaching past the clock for.
//   - THE SUBJECT GETS ROOM. The block above re-uses the subject's transcript
//     row, one line, cut to fit — and one line cut to fit at forty-four columns
//     is an approval prompt with the interesting half of the command missing.
//     Here it wraps, up to [questionNarrowLines], and says so with an ellipsis
//     when even that was not enough.
//   - NOTHING NEW IS ASKED. Same answers, same keys, same clock, same queue
//     count, same reason — the sheet is a LAYOUT and not a second question. esc
//     is still *later* from the keyboard, and it has no band, because folding a
//     question away is not an answer and a full-width target that looked like
//     one would be read as the no.

const (
	// questionNarrowLines caps the subject region. Six lines is about two hundred
	// and fifty characters at this tier — longer than any command a person reads
	// before deciding — and the cap is what keeps one pathological argument from
	// pushing the answers off a short screen.
	questionNarrowLines = 6
	// questionNarrowFloor is the narrowest frame the sheet is drawn on. Under it a
	// band cannot hold its own label, and the line of words the block has always
	// drawn — which degrades by truncating rather than by breaking — is the
	// better shape.
	questionNarrowFloor = 16
	// questionBandPad is the one cell of margin every row of the sheet opens
	// with. The bands are pressable edge to edge regardless: the margin is
	// breathing room for the eye, not a gap for the thumb.
	questionBandPad = " "
)

// questionBand is one answer's whole row on the sheet: which row of the block it
// is, the columns it holds, and which answer it gives.
//
// IT IS A ROW AND A SPAN RATHER THAN A SPAN ALONE, which is the one thing the
// block's own [choiceSpan] cannot say: the answers row puts every answer on ONE
// row and resolves a press by column, and the sheet puts every answer on a row
// of its own and resolves it by row.
type questionBand struct {
	row  int
	span hudSpan
	at   int
}

// questionNarrowed reports whether a question is drawn as the narrow sheet.
func (a *app) questionNarrowed(q questionShown, width int) bool {
	if width < questionNarrowFloor || layoutTier(width) != tierPhone {
		return false
	}
	// A RATIFY LINE IS ALREADY ONE ROW AND NOTHING WAITS ON IT. Turning a
	// statement about finished work into a full-height sheet over the bottom of
	// a phone screen would be this surface shouting about something that has
	// already happened.
	return q.question.Ask != session.AskRatify && len(q.question.Options) > 0
}

// questionNarrowRows draws the narrow form and records where its answers landed.
func (a *app) questionNarrowRows(q questionShown, width int) []string {
	a.questionBands = nil
	out := make([]string, 0, questionNarrowLines+8)
	title, body := a.questionNarrowWords(q)
	out = append(out, a.questionNarrowTitle(title, width))
	out = append(out, a.questionNarrowBody(body, width)...)
	if reason := strings.TrimSpace(q.question.Reason); reason != "" {
		out = append(out, a.pal.dim(fit(questionBandPad+reason, width)))
	}
	out = append(out, a.pal.dim(strings.Repeat("─", width)))

	bands := make([]questionBand, 0, len(q.question.Options)+1)
	// WHERE THE KEYBOARD IS STANDING, ON THIS TIER TOO. `↑↓` walk this form's
	// answers exactly as they walk the card's, and nothing was drawn for it: a
	// person on a phone-width terminal moved a cursor they could not see and
	// pressed `enter` on whichever row it had reached. The background under the
	// hot row is the MOUSE's ([app.questionBandRow]) and says nothing on a
	// screen nobody is hovering.
	band := func(key, word string, i int) {
		row := len(out)
		// THE BAND IS RECORDED BEFORE IT IS PAINTED, because the paint asks the
		// bands which answer this row is ([app.questionBandMarks]) — the marks
		// belong to the answer and not to the row's position.
		//
		// AND `i` IS THE ANSWER THIS BAND IS, never where the cursor happens to
		// be. A band carries the index a PRESS resolves to ([app.questionBandAt]),
		// so recording the cursor here made every row claim to be the answer under
		// the pointer: the pointer drew on the first band whatever the arrows had
		// done, and a tap on `allow once` resolved as `deny`.
		bands = append(bands, questionBand{row: row, span: hudSpan{from: 0, to: width}, at: i})
		a.questionBands = bands
		out = append(out, a.questionBandRow(row, key, word, width))
	}
	if len(q.beat) > 0 {
		// THE BEAT IS BANDS HERE TOO. The wide form replaces the answers row with
		// the shapes; the sheet replaces the answer BANDS with them, because on
		// this tier an answer is a row a thumb lands on and a shape is an answer.
		for i := range q.beat {
			band(itoa(i+1), a.questionBandWord(questionBeatShapeWord(q.beat, i), itoa(i+1), width), i)
		}
		band(questionLaterKey, questionBeatBack, questionBandBack)
		a.questionBands = bands
		if foot := a.questionNarrowFoot(q, width); foot != "" {
			out = append(out, foot)
		}
		return out
	}
	for i, option := range q.question.Options {
		key := strings.TrimSpace(option.Key)
		if key == "" {
			key = itoa(i + 1)
		}
		word := strings.TrimSpace(option.Label)
		if word == "" {
			word = key
		}
		band(key, a.questionBandWord(word, key, width), i)
	}
	a.questionBands = bands
	// AND THE KEYS ARE SAID, from the ONE key table, even here. A phone tier has
	// no keyboard, which is why the bands are targets — but this tier is every
	// frame under sixty columns, and most of them are terminals on a laptop with
	// a window dragged narrow. A sheet that named no key at all was the one view
	// where `esc` was a secret ([app.questionKeyRow] is the one composer).
	if keys := a.questionNarrowKeys(q, width); keys != "" {
		out = append(out, keys)
	}
	if foot := a.questionNarrowFoot(q, width); foot != "" {
		out = append(out, foot)
	}
	return out
}

// questionNarrowKeys is that row: the keys that ANSWER, dropped by rank until
// they fit, and nothing else — the second tier the panel draws under its frame
// is rows this tier does not have to spend.
func (a *app) questionNarrowKeys(q questionShown, width int) string {
	keys := questionKeysOnTier(a.questionAnswerKeys(q, formsCard), keyPrimary)
	if len(keys) == 0 {
		return ""
	}
	return questionBandPad + a.questionKeyRow(q, keys, max(width-len(questionBandPad), 1))
}

// questionBandBack is the band index the beat's way out carries. It is not an
// answer and must not be read as one: pressing it puts the question back exactly
// as it was.
const questionBandBack = -2

// questionNarrowWords is what the sheet is headed by and what fills it.
//
// THE SUBJECT NAMES THE SHEET WHERE THERE IS ONE. A question about a call is
// about the call, so the tool names the sheet and the command fills it — which
// is the same row the wide form re-uses, split in two because a title is a word
// and a body is a paragraph. A question about nothing the transcript drew is
// headed by whoever is asking and filled by its own sentence.
func (a *app) questionNarrowWords(q questionShown) (string, string) {
	if at := a.questionSubjectAt(q.question); at >= 0 && at < len(a.entries) {
		if e := &a.entries[at]; e.kind == entryTool {
			name, command := toolWords(e.tool, e.text)
			if target := toolTarget(e.tool, e.detail.Args, e.text); target != "" {
				command = target
			}
			if name == "" {
				name = strings.TrimSpace(q.question.Subject.Name)
			}
			return name, command
		}
	}
	if name := strings.TrimSpace(q.question.Subject.Name); name != "" {
		return name, strings.TrimSpace(q.question.Head)
	}
	return questionAskerWord(q.question.Asker), strings.TrimSpace(q.question.Head)
}

// questionNarrowTitle is the sheet's head: a rule with the question's own word
// written into it, behind the mark every question wears. It is the same move the
// seam above the draft makes (render.go's legend) — a line that was already
// there, carrying the one word that says what this is.
func (a *app) questionNarrowTitle(name string, width int) string {
	// AMBER IS THE MARK AND NOTHING ELSE (colour pick C), which is the hue law
	// said about a title: the `?` carries "somebody is waiting on you" and the
	// word beside it is ORDINARY INK. It was painted in the hue too — bold, in
	// the rule, on the one drawing a person reads with a thumb over half of it —
	// and a title shouting what the mark beside it already says is this law
	// broken where it is least affordable.
	mark := a.icon(tokens.GNeedsHuman)
	word := strings.TrimSpace(name)
	label := " " + mark + " " + word + " "
	lead := 3
	if room := width - lead - 1; ansi.StringWidth(label) > room {
		label = fit(label, room)
		word = strings.TrimSpace(strings.TrimPrefix(label, " "+mark+" "))
	}
	rest := width - lead - ansi.StringWidth(label)
	if rest < 0 {
		rest = 0
	}
	return a.pal.dim(strings.Repeat("─", lead)) +
		a.pal.dim(" ") + a.questionMark() + a.pal.ink(" "+word+" ") +
		a.pal.dim(strings.Repeat("─", rest))
}

// questionNarrowBody is what is actually being decided about, wrapped rather than
// cut, because the tail of a command is where the reason to say no usually is.
func (a *app) questionNarrowBody(body string, width int) []string {
	// SCRUBBED BEFORE IT IS DRAWN. The region is built from a call's arguments,
	// which are the model's own text, and a terminal reads an escape in them as
	// an instruction rather than as a character — one that can move the cursor
	// onto the answers below and rewrite them. The gate scrubs the gloss and the
	// arguments at their source (session's loop.go); this is the byte a surface
	// makes again when it unmarshals one.
	body = strings.TrimSpace(plainText(body))
	if body == "" {
		// Nothing to show — the title above already says what this is about. A
		// blank region would be a row spent saying nothing (the emptiness law).
		return nil
	}
	lines := wrap(body, width-len(questionBandPad))
	if len(lines) > questionNarrowLines {
		lines = lines[:questionNarrowLines]
		last := lines[questionNarrowLines-1]
		lines[questionNarrowLines-1] = fit(last+glyphMore, width-len(questionBandPad))
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, a.pal.ink(fit(questionBandPad+line, width)))
	}
	return out
}

// questionBandWord is one answer's word, cut to what a band can hold. The
// consequence the card draws beside it is dropped here: a band is a target and
// reads at a glance, and a word cut off mid-reach says less than a short one.
func (a *app) questionBandWord(word, key string, width int) string {
	room := width - len(questionBandPad) - ansi.StringWidth(key) - 4
	if ansi.StringWidth(word) <= room {
		return word
	}
	// A YES THAT CARRIES A CADENCE LOSES THE CADENCE BEFORE ANY CHARACTER IS
	// CUT. "Set it up · every 3 hours" becomes "Set it up", and only then is
	// the stem truncated.
	plain := session.StandingPlainLabel(word)
	if ansi.StringWidth(plain) <= room {
		return plain
	}
	return fit(plain, room)
}

// questionBandRow is one answer, drawn as a row: the pointer where the pointer
// is, the key it also answers to, its word, and `◆ recommended` at the right
// edge where the asker named this one.
//
// IT IS THE PANEL'S ROW GRAMMAR ON A PHONE (owner ruling 2026-09-11, colour
// pick C and recommended pick A): the marks are amber, the key is the payload
// hue, the word is ordinary ink, and the row the pointer stands on takes the
// GROUND ladder rather than a colour. The sheet used to paint every band in the
// question hue with its key in brackets, which was the one surface still
// shouting — and it was the one surface where the pick was not said at all.
//
// Under the mouse it takes the hover background every pressable row on this
// surface takes (hover.go) — an affordance for a mouse and a no-op for a thumb,
// so the row says what it is in WORDS as well.
func (a *app) questionBandRow(row int, key, word string, width int) string {
	pointed, picked := a.questionBandMarks(row)
	mark := "  "
	if pointed {
		mark = a.pal.warnBold(a.icon(tokens.GPointer)) + " "
	}
	text := " " + mark + a.pal.data(questionKeySpelling(key)) + "  " + a.pal.ink(word)
	if picked {
		aside := a.pal.warnBold(a.icon(tokens.GRecommended)) + a.pal.dim(" "+questionRecommendedWord)
		if gap := width - ansi.StringWidth(text) - ansi.StringWidth(ansi.Strip(aside)) - 2; gap > 0 {
			text += strings.Repeat(" ", gap) + aside
		}
	}
	switch {
	case a.hot.kind == hoverChoices && a.hot.index == row:
		return a.pal.cursor(text, width)
	case pointed:
		return a.pal.background(text, width, a.pal.ramp.selected)
	}
	return text
}

// questionBandMarks answers the two marks one band may wear: whether the pointer
// is on it, and whether the asker picked it. It reads the open question rather
// than taking them as arguments, so one reading serves the row's paint and the
// press that lands on it.
//
// THE BEAT WEARS THE POINTER AND NEVER THE RECOMMENDATION. Its bands are the
// widening answer's SHAPES rather than the question's answers, so no asker ever
// picked one and `◆ recommended` on a shape would be a recommendation nobody
// made — but the cursor is as real there as anywhere, and a beat drawn without
// it ate every arrow aimed at it (#919). That defect is the same one on both
// tiers; this is the phone's half of the fix and [app.questionBeatRow] is the
// wide one.
func (a *app) questionBandMarks(row int) (bool, bool) {
	head, ok := a.questionHead()
	if !ok {
		return false, false
	}
	if len(head.beat) > 0 {
		for _, band := range a.questionBands {
			if band.row == row && band.at >= 0 && band.at < len(head.beat) {
				return band.at == head.beatAt, false
			}
		}
		return false, false
	}
	for _, band := range a.questionBands {
		if band.row != row || band.at < 0 || band.at >= len(head.question.Options) {
			continue
		}
		picked := head.question.Pick != nil &&
			strings.TrimSpace(head.question.Pick.Key) == questionOptionKeyAt(head.question, band.at)
		return band.at == head.pick, picked
	}
	return false, false
}

// questionNarrowFoot is the sheet's bottom line: what is still queued behind this
// question on the left, and the clock on the right. Both are dim and neither is
// a target — it is the row that reports, under the rows that act.
func (a *app) questionNarrowFoot(q questionShown, width int) string {
	var left string
	if more := len(a.questionOpen()) - 1; more > 0 {
		left = questionBandPad + itoa(more) + " more"
	}
	right := a.questionClockWord(q)
	if left == "" && right == "" {
		return ""
	}
	if right != "" {
		right += questionBandPad
	}
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		// No room for both: the clock goes, on the answers row's own rule — a
		// countdown a person cannot see is still a countdown, and the queue count
		// is the one of the two that says something about their next decision
		// rather than about this one.
		return a.pal.dim(fit(left, width))
	}
	return a.pal.dim(left + strings.Repeat(" ", gap) + right)
}

// questionBandPress resolves a press on one of the sheet's bands, and reports
// whether it took it — with whatever the answer it took handed back, because an
// answer is sent from a command now and a press that dropped it would be a tap
// the engine never heard (offloop.go).
func (a *app) questionBandPress(head questionShown, x, y int) (tea.Cmd, bool, bool) {
	if len(a.questionBands) == 0 {
		// NO BANDS IS NOT THE SHEET, and saying so is what keeps this from
		// swallowing a press the wide form's own answers row is about to
		// resolve ([app.questionPress] asks here first).
		return nil, false, false
	}
	mark, found := a.chromeAt(y)
	if !found || mark.kind != chromeQuestion {
		return nil, false, false
	}
	for _, band := range a.questionBands {
		if band.row != mark.index || !band.span.holds(x) {
			continue
		}
		switch {
		case band.at == questionBandBack:
			a.setQuestionBeat(head, nil)
		case len(head.beat) > 0:
			return a.questionPickShape(head, band.at), true, true
		default:
			return a.questionPick(head, band.at), true, true
		}
		return nil, true, true
	}
	return nil, false, true
}

// plainText drops the bytes a terminal takes as orders rather than as text. It
// DROPS rather than escapes, on notify.go's reasoning: there is no escape form
// that reads better here, and a row with a missing byte says more than a row
// with a stray backslash in it.
func plainText(text string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, text)
}
