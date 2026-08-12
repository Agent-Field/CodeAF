package composer

import (
	"strconv"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The attachment grammar (5.11, 7.2, 12.5.1): how a file named in the draft
// stops being prose and becomes a thing the send carries.
//
// # The composer never touches the filesystem
//
// This package knows what a TOKEN is — the shell-shaped word a person types or
// a terminal emits when a file is dragged in — and it knows what a CHIP looks
// like. It does not know what a file is, whether one exists, how big it is, or
// which kinds may ride along. [Options.Attach] answers all of that, exactly as
// [Options.Targets] answers what may be addressed: the composer never discovers
// a task on its own, and by the same rule it never discovers a file on its own.
//
// That is not fastidiousness. os.Stat on a keystroke path is a decision about
// latency and about which HOME a `~` means, and both belong to the wiring that
// knows which process this is. It also keeps every test in this package a pure
// function of its inputs — an attachment test here builds a draft and a stub,
// and touches no disk.
//
// # Capture removes the token from the draft
//
// A captured path leaves the text. This is the one place the attachment grammar
// and the `@` grammar deliberately differ, and the difference is not an
// oversight: a mention is a word the sentence still needs ("ask @wisp-parity to
// stop"), so it is DERIVED and left in place, while a dragged path is not
// prose — nobody means to say `/Users/me/Downloads/Screenshot 2026-08-10.png`
// out loud. v1 removes it (internal/tui/attachments.go) and the message the
// head reads is the sentence without the path, so removing it here is the lens
// law rather than a taste: the same act on either surface must journal the same
// row.
//
// One adjacent space goes with the token, so pulling a path out of the middle
// of a sentence does not leave the double space behind that would be the only
// visible trace of an edit the person did not make.

// attachChipMax bounds how many chip rows the composer will spend on
// attachments, however many are held. The draft is the thing a person is
// looking at; a composer that grew a scrolling list of files under it would
// have taken the screen away from the sentence being written to describe its
// own bookkeeping. Past the bound the rows fold into one honest count.
const attachChipMax = 3

// Attachment is one file the draft has captured. Every field is the wiring's
// answer, not this package's discovery — see the note above.
type Attachment struct {
	// Path is the file on disk, as the wiring resolved it. It is what the
	// wiring is handed back at send; nothing here parses it.
	Path string
	// Name is the short label the chip shows. An empty Name falls back to Path,
	// so a wiring that has nothing better to say still draws a legible chip.
	Name string
	// Glyph leads the chip. Empty means [tokens.GlyphCollapsed] — v2's own
	// reference-row mark (12.5.1), so the chip previews the row this file will
	// become in the transcript rather than introducing a second vocabulary for
	// the same object.
	Glyph string
	// Bytes is the file's size, shown beside the name when the row is wide
	// enough. Zero means unknown and renders nothing — 8.2.20's law is that
	// missing data is missing, never a zero pretending to be a measurement.
	Bytes int64
}

// Send is what a send that carries attachments hands [Options.OnSend].
type Send struct {
	// Text is the trimmed draft. It may be EMPTY: a person who drags in an
	// image and presses enter has said something, and refusing the send because
	// they said it with a file rather than with words would be the composer
	// deciding what counts as speech. The wiring supplies the body in that case
	// — it is the half that knows what was attached.
	Text string
	// Attachments are the captured files, in the order they were captured.
	Attachments []Attachment
}

// Attachments is what the draft is currently holding. It is exported for the
// wiring's own rendering and for tests; the returned slice is the model's own
// and must not be retained across an edit.
func (m *Model) Attachments() []Attachment { return m.attachments }

// syncAttachments captures every attachable token in the draft. It runs from
// [Model.afterEdit] before the mention scan, because it MUTATES the draft and
// the mention spans must be derived from what the text finally is.
//
// The rescan after each capture is deliberate: a splice invalidates every index
// behind it, and re-tokenizing a draft-sized buffer a handful of times is
// cheaper to reason about than an offset walk that has to be right. A draft
// with no attachable token — every draft, almost always — pays one tokenizer
// pass, and a draft with no [Options.Attach] pays nothing at all.
func (m *Model) syncAttachments() {
	if m.attach == nil {
		return
	}
	for {
		captured := false
		for _, tok := range draftTokens(m.value) {
			if tok.value == "" {
				continue
			}
			attachment, ok := m.attach(tok.value)
			if !ok {
				continue
			}
			m.captureAttachment(attachment, tok)
			captured = true
			break
		}
		if !captured {
			return
		}
	}
}

// captureAttachment splices one token out of the draft and files it.
//
// The cursor follows the text rather than the index: a person who dragged a
// file into the middle of a sentence goes on typing where they were, and a
// cursor that stayed on a raw offset would land somewhere they did not put it.
func (m *Model) captureAttachment(attachment Attachment, tok draftToken) {
	start, end := tok.start, tok.end
	switch {
	case start > 0 && m.value[start-1] == ' ':
		start--
	case end < len(m.value) && m.value[end] == ' ':
		end++
	}
	m.value = append(m.value[:start], m.value[end:]...)
	switch {
	case m.cursor >= end:
		m.cursor -= end - start
	case m.cursor > start:
		m.cursor = start
	}
	if m.cursor > len(m.value) {
		m.cursor = len(m.value)
	}
	for _, held := range m.attachments {
		if held.Path == attachment.Path {
			// The same file named twice is one attachment. The token still
			// leaves the draft — the person did type it — but the send carries
			// one copy, because two references to one object is a claim about
			// the message that is not true.
			return
		}
	}
	m.attachments = append(m.attachments, attachment)
}

// removeLastAttachment is 7.2's backspace-at-the-edge row: backspace on an
// EMPTY draft takes the last attachment off rather than doing nothing.
//
// The empty-draft condition is the whole of it. A chip is not in the text, so
// there is no caret position that means "just after the chip"; the honest edge
// is the one place backspace has nothing else to delete. It reports whether it
// consumed the key so the caller can fall through to an ordinary delete.
func (m *Model) removeLastAttachment() bool {
	if len(m.attachments) == 0 {
		return false
	}
	m.attachments = m.attachments[:len(m.attachments)-1]
	return true
}

// -- tokens -------------------------------------------------------------------

// draftToken is one shell-shaped word in the draft, with the span it occupies.
type draftToken struct {
	// value is the word with its quoting and escapes resolved — what a
	// filesystem would be asked about.
	value string
	// start and end are rune indices into the draft, covering the token's raw
	// text INCLUDING its quotes and backslashes, so removing [start:end] removes
	// exactly what the person sees.
	start int
	end   int
}

// draftTokens splits the draft the way a shell — and therefore every terminal's
// drag-and-drop — spells a path: whitespace separates, single and double quotes
// group, and a backslash escapes the rune after it.
//
// This exists because the one filename shape that matters most is the one with
// a space in it. A terminal that drops `/Users/me/My Screenshots/dot.png` emits
// it quoted or backslash-escaped, and a splitter that only knew about spaces
// would hand the wiring two half-paths that do not exist and capture neither.
func draftTokens(value []rune) []draftToken {
	var out []draftToken
	var buf []rune
	start := -1
	var quote rune
	escaped := false

	flush := func(end int) {
		if start >= 0 {
			out = append(out, draftToken{value: string(buf), start: start, end: end})
		}
		buf, start = buf[:0], -1
	}

	for i, r := range value {
		switch {
		case escaped:
			buf = append(buf, r)
			escaped = false
		case r == '\\':
			if start < 0 {
				start = i
			}
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			buf = append(buf, r)
		case r == '\'' || r == '"':
			if start < 0 {
				start = i
			}
			quote = r
		case unicode.IsSpace(r):
			flush(i)
		default:
			if start < 0 {
				start = i
			}
			buf = append(buf, r)
		}
	}
	// A token still inside a quote, or ending on a dangling backslash, is one
	// the person is in the MIDDLE of typing. It is dropped rather than offered.
	//
	// This is not tidiness, it is the difference between working and not: capture
	// runs on every keystroke, so `see "/tmp/my shots/dot.png` — the draft one
	// keystroke before the closing quote — would otherwise be a complete,
	// existing path, get captured, and leave the closing quote the person types
	// next stranded in the draft as a lone `"`. The rule is that a quote is a
	// promise of a second quote, and nothing is taken until it is kept.
	if quote == 0 && !escaped {
		flush(len(value))
	}
	return out
}

// -- chips ---------------------------------------------------------------------

// attachRows draws the held attachments as chips above the draft (5.11's
// grammar, in the shape hint.go already uses for the dispatch chip).
//
// A composer with no [Options.Attach] can hold nothing and draws nothing, which
// is the same byte-identity guarantee hintRows makes for [Options.Targets]: a
// wiring that has not adopted this pays no row and no arithmetic.
func (m *Model) attachRows(sty *tokens.Styler, width, budget int) []string {
	if m.attach == nil || len(m.attachments) == 0 || budget <= 0 || width <= 0 {
		return nil
	}
	if budget > attachChipMax {
		budget = attachChipMax
	}
	shown, folded := len(m.attachments), 0
	if shown > budget {
		shown = budget - 1
		folded = len(m.attachments) - shown
	}
	rows := make([]string, 0, budget)
	for _, attachment := range m.attachments[:shown] {
		rows = append(rows, attachChip(sty, attachment, width))
	}
	if folded > 0 {
		// "more" only reads as an addition when something was shown above it.
		word := " more"
		if shown == 0 {
			word = " attached"
		}
		rows = append(rows, plainRow(sty, strconv.Itoa(folded)+word, width))
	}
	return rows
}

// attachChip draws one chip: the reference glyph, the file's name, and — when
// the row has cells to spare — its size.
//
// It sheds in that order for the reason chipRow does: the name is the thing the
// reader checks ("is that the right screenshot?"), and a size that displaced
// half a filename would have traded the answer for the trivia.
func attachChip(sty *tokens.Styler, attachment Attachment, width int) string {
	l := hintLine{sty: sty, max: width}
	l.add(hintIndent, tokens.TextTertiary)
	glyph := attachment.Glyph
	if glyph == "" {
		glyph = tokens.GlyphCollapsed
	}
	l.add(glyph, tokens.TextTertiary)
	l.add(" ", tokens.TextTertiary)
	name := attachment.Name
	if name == "" {
		name = attachment.Path
	}
	l.addClipped(name, tokens.TextSecondary)
	if attachment.Bytes > 0 {
		size := tokens.Count(attachment.Bytes) + "B"
		if l.room() >= ansi.StringWidth(hintSep)+ansi.StringWidth(size) {
			l.add(hintSep, tokens.TextTertiary)
			l.add(size, tokens.TextTertiary)
		}
	}
	return l.String()
}
