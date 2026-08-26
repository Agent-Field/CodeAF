package tui3

import (
	"strings"
	"unicode"
)

// ── THE PAYLOAD RULE ────────────────────────────────────────────────────────
//
// A LINE MAY BE QUIET; THE FACT IT CARRIES MAY NOT BE.
//
// This surface says a great many things on its own account — a note, a hint, a
// legend, an announcement — and every one of them is written in the quiet
// reading tiers, because none of them is the conversation. That is right about
// the LINE and it was wrong about what the line is for. `crew → balanced ·
// brain kimi-k3:low · hands deepseek-v4-flash · checks qwen3.8-27b` was drawn
// at one dim value from end to end: the words a person already knew and the
// four model ids they typed the command to learn, at exactly the same weight.
// The sentence was legible and the ANSWER inside it was not.
//
// So: the prose of an informational line stays where it was, and each
// LOAD-BEARING DATUM inside it steps up one role.
//
//	the prose      dim, or whatever quiet role the surface already used
//	a datum        [palette.data] — a model id, a role's model, a figure, a
//	               count, a name, a key chord. A hue of its own rather than a
//	               rung of the reading ladder, because the first draft of this
//	               rule lifted data to ink and ink is the BODY's colour: a
//	               "lifted" id two rows under a paragraph read as ordinary
//	               text. Lightness is loudness; HUE is identity, and a datum
//	               is a different kind of thing ([hueData] tells the rest)
//	something
//	typeable       the chip a slash command already wears everywhere else on
//	               this surface (slashchip.go)
//	the accent     NEVER. THE ACCENT BUDGET STANDS: one lit element per
//	               screen, and a note that appears and scrolls away is not it
//
// ── THE CHIP IS THE SLASH COMMAND'S MARK AND IS NOT LENT OUT ────────────────
//
// A key chord is typeable too, and the obvious move was to give the chords in
// the legend and the key sheet the same lifted run of cells. They do not get
// it. A chip on this surface has meant exactly one thing since slashchip.go
// landed — "this word is a command this surface runs" — and a second kind of
// thing wearing it is a mark that has to be read twice to learn which one it
// is. A chord steps to the data hue instead, which is the same one-step move
// every other datum makes and costs the budget nothing. So `/help` is chipped
// wherever it is written, `ctrl+b` wears the data hue wherever it is written,
// and neither rule has an exception.
//
// ── STRATEGY OVER DECORATION ────────────────────────────────────────────────
//
// ONE LINE CARRIES ONE OR TWO DATA. If everything in a line is bright then
// nothing in it is, and a surface that lifted every noun would have spent the
// whole mechanism buying back the flat line it started with. So the legend's
// hint lifts the KEY and never the verb beside it; /status lifts the figure and
// never its label; the crew line lifts the three model ids and leaves `crew →`,
// the preset word the person just typed, and the three role words dim, because
// those are the question and the ids are the answer.
//
// ── HOW A LINE SAYS WHERE ITS DATA ARE ──────────────────────────────────────
//
// A datum is named by its own text, in the order it appears: [app.noteFacts]
// takes the words, and [paintPayload] finds them left to right, each search
// beginning where the last match ended. Nothing here guesses at a shape — a
// figure is a datum because the call site said so, and a line whose builder
// says nothing is drawn exactly as it was drawn before this file existed.
//
// The one exception is the LEGEND'S HINT SLOT, which is written in a grammar
// tight enough to read: `chord verb · chord verb`, spelled the same way at
// twenty-five call sites across as many files. [chordSpans] reads that grammar
// rather than making twenty-five of them carry a list, and its rules are stated
// on it.

// factWalk is one pass of the payload rule over the wrapped rows of a single
// informational line: which datum is being looked for next.
//
// It is a cursor rather than a plain index because the rows are painted one at
// a time and the data are in TEXT order across all of them — a fact matched on
// row two must not be matched again on row three, and a fact that never turned
// up must not be found later in a word that merely looks like it.
type factWalk struct {
	words []string
	next  int
}

// take finds the next fact of the walk inside one row, and every fact after it
// that also fits in the same row, left to right.
//
// A FACT THAT IS NOT IN THIS ROW STOPS THE WALK FOR THIS ROW AND NOT FOR THE
// LINE. The rows are one wrapped sentence, so the fact is either further down
// or it was split across a wrap boundary — and the honest answer to the second
// case is the quiet one: the datum stays in the prose tier rather than half of
// it being lifted. Under-lifting reads as an ordinary line; a half-lifted word
// reads as a bug.
func (w *factWalk) take(value []rune) []segment {
	var out []segment
	at := 0
	for w.next < len(w.words) {
		from, to, ok := findWord(value, at, w.words[w.next])
		if !ok {
			return out
		}
		out = append(out, segment{from: from, to: to})
		at, w.next = to, w.next+1
	}
	return out
}

// findWord locates word inside value at or after `at`, ON WORD BOUNDARIES: the
// run has to start at the head of the row or after a space, and finish at the
// end of the row or before a space.
//
// The boundaries are what keep a one-character datum honest. `/cost` names the
// count of model calls as `14`, and without them the same two digits inside
// `48.1k` two rows up would be lifted instead — a figure highlighted in the
// middle of another figure, which is worse than no highlight at all.
func findWord(value []rune, at int, word string) (from, to int, ok bool) {
	want := []rune(word)
	if len(want) == 0 {
		return 0, 0, false
	}
	for i := max(at, 0); i+len(want) <= len(value); i++ {
		if i > 0 && value[i-1] != ' ' {
			continue
		}
		if end := i + len(want); end < len(value) && value[end] != ' ' {
			continue
		}
		if string(value[i:i+len(want)]) == word {
			return i, i + len(want), true
		}
	}
	return 0, 0, false
}

// noteLead is how many cells a note's own marker takes at the head of every one
// of its rows — "· " on the first, two spaces on the rest (render.go's
// entryNote). It is a constant because both forms are two cells by construction:
// the continuation row is the lead's width in spaces, so a fact's column inside
// the sentence is the same on every row of the note.
const noteLead = spacingConversationLead

// shifted moves a set of spans right by n cells, for a caller that found them in
// a line and is about to paint that line with something in front of it.
func shifted(spans []segment, n int) []segment {
	if n == 0 || len(spans) == 0 {
		return spans
	}
	out := make([]segment, len(spans))
	for i, s := range spans {
		out[i] = segment{from: s.from + n, to: s.to + n}
	}
	return out
}

// paintPayload paints one row of an informational line: the prose in whatever
// quiet role the caller is already saying this line in, every recognized slash
// command in its chip, and every datum in [palette.data], the hue that means
// "this is the answer inside the sentence".
//
// THE CHIP OUTRANKS THE DATA HUE WHERE THE TWO OVERLAP, and they do overlap on
// purpose: /help's key column is `/task <brief>`, which is one datum containing
// one command. The command keeps its chip and the placeholder beside it keeps
// the data hue, which is the row saying "this part is the word, this part is
// yours".
//
// The runs are painted SEPARATELY, for [paintCommands]'s reason: every sequence
// this palette writes closes with SGR 39, which resets the foreground rather
// than restoring what was under it, so one long prose() call with paints nested
// inside it would come back with everything after the first nested run
// unpainted.
func paintPayload(line string, facts []segment, pal palette, prose func(string) string) string {
	value := []rune(line)
	if len(value) == 0 {
		return prose(line)
	}
	// Informational lines NAME commands rather than accepting a send. Every
	// recognized word they teach keeps its chip; slashchip.go's promise law is
	// about the person's draft and transcript, not /help's key sheet.
	chips := recognizedCommandSpans(value, true)
	if len(chips) == 0 && len(facts) == 0 {
		return prose(line)
	}
	// One paint per rune, coalesced into runs below. A line is at most a
	// terminal wide and this is drawn once per note per frame, so the clear
	// version is the one worth having.
	const (
		asProse = iota
		asFact
		asChip
	)
	role := make([]int, len(value))
	for _, s := range facts {
		for i := s.from; i < s.to && i < len(value); i++ {
			role[i] = asFact
		}
	}
	for _, s := range chips {
		for i := s.from; i < s.to && i < len(value); i++ {
			role[i] = asChip
		}
	}
	paint := func(kind int, run string) string {
		switch kind {
		case asChip:
			return pal.chip(run)
		case asFact:
			return pal.data(run)
		default:
			return prose(run)
		}
	}
	var b strings.Builder
	at := 0
	for i := 1; i <= len(value); i++ {
		if i < len(value) && role[i] == role[at] {
			continue
		}
		b.WriteString(paint(role[at], string(value[at:i])))
		at = i
	}
	return b.String()
}

// ── THE HINT SLOT'S OWN GRAMMAR ─────────────────────────────────────────────
//
// The legend's right end and the mode lines under it are written in one shape
// at every call site that fills them (render.go's [app.hintWord] lists them
// all): `chord verb`, joined by " · ". `esc interrupt`. `y allow · n deny · a
// always`. `ctrl+r reveal · ctrl+y copy · esc`. THE CHORD IS THE PAYLOAD and
// the verb is the prose, every time, which is a rule worth reading off the
// string rather than making twenty-five call sites carry a list of their own
// keys — a list that would be one refactor away from naming a key the handler
// no longer takes.
//
// A TOKEN IS A CHORD WHEN IT IS ONE OF FOUR THINGS.
//
//	a named key         esc, enter, tab, space, del, backspace
//	a modified key      anything with ctrl+, alt+ or shift+ in front of it
//	an arrow run        ↑ ↓ → ← and any run of them: ↑↓, →←
//	a slash             "/", the door the input line advertises
//
// Those four are lifted wherever they appear, because there is no other reason
// for this surface to write them.
//
// A BARE LETTER OR DIGIT IS THE FIFTH, AND IT IS THE ONE THAT NEEDS A FENCE.
// `a block`, `y yank`, `1 yes`, `1-3 shape` are all keys; `a task will stop` is
// an English article and lifting it would put ink on the wrong word in the one
// warning a person reads in a second and a half. So a bare token counts only at
// the HEAD of its segment, and then:
//
//	a digit    always. At the head of a hint segment a digit is a fact whether
//	           it is a key ("1 yes") or a count ("2 conversations") — both are
//	           the thing the eye is there for
//	a letter   only in a segment of at most three tokens. English's own
//	           one-letter words are articles, and an article never leads a
//	           three-word sentence about what a key does
//
// A trailing comma or full stop is not part of a key, so it is trimmed off the
// span before the lift rather than painted with it: "0 or esc, no" lifts `esc`
// and leaves the comma in the prose where it belongs.

// chordWords are the keys this surface writes out in full. It is a closed list
// on purpose — a hint that named a key by a word not on it would be a hint
// nobody can act on, and the fix for that is the hint, never this table.
var chordWords = map[string]bool{
	"esc": true, "enter": true, "tab": true, "space": true,
	"del": true, "backspace": true,
}

// chordMods are the prefixes a modified key is spelled with.
//
// `⌥` IS ONE OF THEM BECAUSE IT IS WHAT A MAC'S KEYCAP SAYS. On macOS every
// `alt+` in a hint is drawn as `⌥` by the one spelling door (chords.go), so a
// list that named only the ASCII prefixes would leave `⌥1` and `⌥enter` painted
// as prose on exactly the platform whose keys most need finding. It carries no
// `+` for the same reason the keycap does not.
var chordMods = []string{"ctrl+", "alt+", "shift+", chordMetaWord}

// hintSegment is the separator the hint slot joins its clauses with. It is
// [legendJoin] without the padding, and it is written here as well because this
// file splits on it where render.go joins on it.
const hintSegment = " · "

// chordSpans finds every key in one hint line, under the grammar stated above.
func chordSpans(line string) []segment {
	var out []segment
	base := 0
	for _, part := range strings.Split(line, hintSegment) {
		out = append(out, chordsIn([]rune(part), base)...)
		base += len([]rune(part)) + len([]rune(hintSegment))
	}
	return out
}

// chordsIn reads one " · " segment, with base the segment's own offset into the
// whole line so the spans it returns index the line rather than the piece.
func chordsIn(part []rune, base int) []segment {
	words := splitTokens(part)
	var out []segment
	for i, w := range words {
		// THE WHOLE TOKEN IS TRIED BEFORE THE TRIM. A trailing comma or full
		// stop is prose punctuation on "0 or esc, no" — but on `ctrl+.` and
		// `ctrl+,` the mark IS the key, and a trim that ran first would hand
		// half a chord to the matcher and lift nothing.
		token := string(part[w.from:w.to])
		to := w.to
		if !isChordWord(token) {
			token = strings.TrimRight(token, ",.")
			to = w.from + len([]rune(token))
		}
		switch {
		case isChordWord(token):
		case i == 0 && isBareKey(token, len(words)):
		default:
			continue
		}
		out = append(out, segment{from: base + w.from, to: base + to})
	}
	return out
}

// splitTokens is one segment's space-separated words, as rune ranges.
func splitTokens(part []rune) []segment {
	var out []segment
	at := 0
	for at < len(part) {
		for at < len(part) && part[at] == ' ' {
			at++
		}
		end := at
		for end < len(part) && part[end] != ' ' {
			end++
		}
		if end > at {
			out = append(out, segment{from: at, to: end})
		}
		at = end
	}
	return out
}

// isChordWord answers the first four of the five: a named key, a modified key,
// a run of arrows, or the slash.
func isChordWord(token string) bool {
	if token == "" {
		return false
	}
	if chordWords[strings.ToLower(token)] {
		return true
	}
	for _, mod := range chordMods {
		if strings.HasPrefix(strings.ToLower(token), mod) && len(token) > len(mod) {
			return true
		}
	}
	if token == "/" {
		return true
	}
	return strings.Trim(token, "↑↓→←") == ""
}

// isBareKey answers the fifth: a single letter or digit, or a digit range, at
// the head of a segment. tokens is how many words that segment has, which is
// the fence the letters need and the digits do not (see the grammar above).
func isBareKey(token string, tokens int) bool {
	runes := []rune(token)
	switch {
	case len(runes) == 1 && unicode.IsDigit(runes[0]):
		return true
	case len(runes) == 3 && unicode.IsDigit(runes[0]) && runes[1] == '-' && unicode.IsDigit(runes[2]):
		return true
	case len(runes) == 1 && unicode.IsLetter(runes[0]):
		return tokens <= chordSegmentWords
	}
	return false
}

// chordSegmentWords is the longest segment a BARE LETTER may lead and still be
// read as a key: the key, the verb, and one word of object — `n not here`. One
// more and the segment is a sentence, and the letter at the head of a sentence
// is an article.
const chordSegmentWords = 3

// paintHint is the legend's right end and every mode line written in its
// grammar: the keys in the data hue, the words around them in the caller's own
// quiet role.
func paintHint(line string, pal palette, prose func(string) string) string {
	if line == "" {
		return line
	}
	return paintPayload(line, chordSpans(line), pal, prose)
}

// ── THE TWO-COLUMN NOTE ─────────────────────────────────────────────────────
//
// /help, /status and /cost are all one shape — a label, a run of spaces, and
// the other half — and the two halves are the payload rule's two directions.
// /help's key sheet carries its datum on the LEFT (the chord; the sentence
// beside it is prose) and /status carries it on the RIGHT (the figure; the word
// in front of it is furniture). Both are built by writers that know exactly
// which column is which (commands.go's [helpText], statusnote.go's
// [labelledLines]), so neither has to be recognized here — this only reads the
// column back off the finished text, which keeps the fact list and the note
// from being two spellings of one thing that could drift apart.

// columnFacts is the named column of every two-column line in text, in order,
// ready to hand to [app.noteFacts].
//
// A line with no column — the wordmark at the top of /help, the blank under it,
// the sentence a listing ends with — contributes nothing, which is the emptiness
// law doing this function's filtering for it.
func columnFacts(text string, lead bool) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		left, right, ok := splitColumns(line)
		if !ok {
			continue
		}
		if lead {
			out = append(out, left)
			continue
		}
		out = append(out, right)
	}
	return out
}

// splitColumns cuts one line at its gutter — the first run of two or more
// spaces — and reports whether there was one. Two spaces rather than one
// because every writer here pads to a column with at least two, and a single
// space is what separates the words INSIDE either half.
//
// AN INDENT IS NOT A GUTTER. /crew's listing sets its four class rows four
// spaces in, and a rule that read the first run of spaces wherever it fell would
// call the indent the gutter and the whole row the second column. So the indent
// is stepped over before the cut, which is also what makes the two halves come
// back as the words they are rather than with the page's own margin attached.
func splitColumns(line string) (left, right string, ok bool) {
	line = strings.TrimLeft(line, " ")
	at := strings.Index(line, "  ")
	if at <= 0 {
		return "", "", false
	}
	end := at
	for end < len(line) && line[end] == ' ' {
		end++
	}
	right = strings.TrimSpace(line[end:])
	if right == "" {
		return "", "", false
	}
	return line[:at], right, true
}
