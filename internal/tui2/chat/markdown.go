package chat

import (
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The markdown renderer for message text (13.1 item 1).
//
// The approach is ported from the old surface's internal/tui/markdown.go, not
// the code: one line-oriented pass that recognizes the subset a model actually
// emits — fences, headings, quotes, bullets, inline code, bold, italic — and
// lets everything it does not recognize fall through as plain wrapped text. A
// half-formed document degrades to exactly what was written, which is the only
// honest failure mode for a renderer standing between a journal and a reader.
//
// What changed in the port, and why:
//
//   - Colour comes from the token layer, never from an ad-hoc lipgloss style.
//     The old file named four styles at the top of the file and reached for
//     them; here a face resolves to a [tokens.Token] and nothing else, so the
//     contrast gate covers this renderer for free (5.16).
//   - Wrapping is span-aware. The old file styled a line and then wrapped the
//     styled string, which is only safe because lipgloss re-measures; here the
//     wrap runs over unstyled fragments and paints the survivors, so a row can
//     never carry half an escape sequence and a bold word can never be counted
//     as its escape bytes wide.
//   - Text arrives already sanitized. The chokepoint (engine.go) runs every
//     body and every text part through internal/sanitize before a block is
//     built, so this file treats its input as plain and never has to think
//     about what an escape sequence in the middle of a bullet would mean.
//
// Emphasis is the one place this renderer writes an escape sequence that did
// not come from the token layer: SGR 1 for bold and SGR 3 for italic. Weight is
// an axis 5.13 names explicitly ("type hierarchy = weight + colour + indent")
// and the token layer carries colour only, so the alternative was to spend a
// colour on bold — which 5.16 forbids, because a hue that meant "bold" would be
// a sixth word in a five-word vocabulary.

// face is one inline typographic role. It is resolved to a token (and, for the
// two emphasis faces, to an SGR weight) by [prose.paint] and nowhere else.
type face uint8

const (
	// faceBody is running prose at the block's own tier.
	faceBody face = iota
	// faceStrong is **bold**: the block's tier, promoted, plus SGR 1.
	faceStrong
	// faceEmphasis is *italic*: the block's tier plus SGR 3.
	faceEmphasis
	// faceCode is an inline `code` span: primary text on the raised band
	// ground, which is the one background 5.16 sanctions and costs no hue.
	faceCode
	// faceHeading is a "# heading": primary tier, bold.
	faceHeading
	// faceQuote is the "│ " gutter of a block quote and its text: secondary
	// tier, because a quotation is not this turn's own speech.
	faceQuote
	// faceChrome is structure the reader does not read: fence markers, bullet
	// glyphs, the quote gutter itself.
	faceChrome
)

// The weight escapes. They are written around an already-painted span, which
// is safe in both directions: the token layer's reset is SGR 39 (foreground
// only) and never clears weight, and SGR 22/23 clear weight without touching
// the foreground the layer set.
const (
	sgrBold      = "\x1b[1m"
	sgrBoldOff   = "\x1b[22m"
	sgrItalic    = "\x1b[3m"
	sgrItalicOff = "\x1b[23m"
)

// bodyIndent is 5.13's spacing rhythm: two spaces per depth. A message body
// sits one depth under the header that names who is speaking.
//
// It is [blocks.ContentEdge] and not a `2` this file gets to pick: §20 is ONE
// geometry for the whole product, and the law was written because four surfaces
// had each spelled their own answer to the same question. The alias survives
// because the WORD is what this file's rows read as — a body's indent — and the
// number behind it is the grid's.
const bodyIndent = blocks.ContentEdge

// prose renders markdown at one tier, for one styler.
//
// It is a value, not a state machine: two fields, no cursor, no buffer. Blocks
// hold one and call [prose.rows] whenever their width moves, which for a
// finalized message is once per width and never again.
type prose struct {
	// style paints. A nil style renders plain text, which is what the golden
	// harness and every headless test see.
	style *tokens.Styler
	// base is the tier running prose is drawn at: primary for speech (5.13
	// tier 1), secondary for a receipt's own body (tier 2).
	base tokens.Token
}

// coloured reports whether this renderer may write escape sequences at all.
// Under [tokens.NoColor] a bold span is drawn as its own words and nothing
// else — which is the honest rendering, and is what keeps the assertions in
// chat_test.go readable as text.
func (p prose) coloured() bool {
	return p.style != nil && p.style.Profile() != tokens.NoColor
}

// paint draws one fragment in one face.
func (p prose) paint(text string, f face) string {
	if text == "" {
		return ""
	}
	switch f {
	case faceCode:
		if p.style == nil {
			return text
		}
		return p.style.PaintOn(text, tokens.TextPrimary, tokens.Band)
	case faceStrong:
		return p.weight(p.token(text, tokens.Promote(p.base)), sgrBold, sgrBoldOff)
	case faceHeading:
		return p.weight(p.token(text, tokens.TextPrimary), sgrBold, sgrBoldOff)
	case faceEmphasis:
		return p.weight(p.token(text, p.base), sgrItalic, sgrItalicOff)
	case faceQuote:
		return p.token(text, tokens.TextSecondary)
	case faceChrome:
		return p.token(text, tokens.TextTertiary)
	default:
		return p.token(text, p.base)
	}
}

func (p prose) token(text string, t tokens.Token) string {
	if p.style == nil {
		return text
	}
	return p.style.PaintToken(text, t)
}

func (p prose) weight(text, on, off string) string {
	if !p.coloured() {
		return text
	}
	return on + text + off
}

// -- the line pass -----------------------------------------------------------

// rows renders text as screen rows appended to dst, wrapped to width and
// indented by indent cells. It never returns a row wider than width and never
// returns a row containing a newline.
func (p prose) rows(dst []string, text string, width, indent int) []string {
	if width < 1 {
		width = 1
	}
	if indent >= width {
		indent = 0
	}
	fenced := false
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for index := 0; index < len(lines); index++ {
		line := strings.TrimRight(lines[index], " \t")
		trimmed := strings.TrimSpace(line)

		// A table is recognized before anything else outside a fence, because
		// its rows are ordinary text to every other rule here and the ordinary
		// rules would wrap them into soup (13.3.2). Inside a fence it is
		// preformatted content and stays exactly as it was written.
		if !fenced {
			if grid, used, ok := tableAt(lines, index); ok {
				dst = p.tableRows(dst, grid, width, indent)
				index += used - 1
				continue
			}
		}

		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			// The fence marker is structure, not content: it says "what follows
			// is not prose" and then gets out of the way. Keeping the info
			// string (```go) is what makes that claim checkable.
			dst = append(dst, p.flat(trimmed, faceChrome, width, indent))
			continue
		}
		if fenced {
			// Code is preformatted: wrapping it would invent line breaks the
			// author did not write, and a re-flowed shell command is a command
			// that no longer runs. It is cut instead, visibly.
			dst = append(dst, p.flat(line, faceCode, width, indent+bodyIndent))
			continue
		}

		switch {
		case trimmed == "":
			dst = append(dst, "")

		case isHeading(trimmed):
			head := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			dst = p.wrap(dst, parseInline(nil, head, faceHeading), "", "", width, indent)

		case strings.HasPrefix(trimmed, "> "), trimmed == ">":
			body := strings.TrimPrefix(strings.TrimPrefix(trimmed, ">"), " ")
			quote := tokens.GlyphProseQuote + " "
			dst = p.wrap(dst, parseInline(nil, body, faceQuote), quote, quote, width, indent)

		default:
			if marker, rest, ok := listMarker(trimmed); ok {
				lead := marker
				contd := strings.Repeat(" ", blocks.Width(marker))
				dst = p.wrap(dst, parseInline(nil, rest, faceBody), lead, contd, width, indent)
				continue
			}
			dst = p.wrap(dst, parseInline(nil, line, faceBody), "", "", width, indent)
		}
	}
	return dst
}

// flat draws one unwrapped row: indent, text, cut to fit. It is the shape
// preformatted content takes, and the shape a fence marker takes.
func (p prose) flat(text string, f face, width, indent int) string {
	if indent >= width {
		indent = 0
	}
	body := blocks.Truncate(text, width-indent)
	if body == "" {
		return ""
	}
	return strings.Repeat(" ", indent) + p.paint(body, f)
}

// isHeading reports an ATX heading: one to six hashes and then a space or
// nothing. "#tag" is not a heading, which is why the space is checked.
func isHeading(line string) bool {
	hashes := 0
	for hashes < len(line) && line[hashes] == '#' {
		hashes++
	}
	if hashes == 0 || hashes > 6 {
		return false
	}
	return hashes == len(line) || line[hashes] == ' '
}

// listMarker recognizes a bullet or an ordered item and returns the marker to
// draw and the text after it. The bullet is normalized to "• " because three
// spellings of one idea is three shapes on screen; an ordered item keeps its
// own number, because the number is the information.
func listMarker(line string) (marker, rest string, ok bool) {
	if len(line) > 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
		return "• ", strings.TrimSpace(line[2:]), true
	}
	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits > 3 || digits+1 >= len(line) {
		return "", "", false
	}
	if (line[digits] != '.' && line[digits] != ')') || line[digits+1] != ' ' {
		return "", "", false
	}
	return line[:digits+1] + " ", strings.TrimSpace(line[digits+2:]), true
}

// -- the inline pass ---------------------------------------------------------

// fragment is one run of text in one face, before wrapping.
type fragment struct {
	text string
	face face
}

// parseInline splits text into faced fragments: `code`, **strong**, *emphasis*.
//
// An unpaired delimiter is left exactly where it was found and rendered as the
// character it is. That is deliberate and it is the old renderer's rule too: a
// reader who typed a lone asterisk should see a lone asterisk, and a renderer
// that swallowed it would be editing the journal. What must never survive is a
// PAIRED delimiter — the literal `**` 13.1 opened on.
// The flanking rules are CommonMark's, reduced to the two that actually matter
// for model output: a run that OPENS emphasis is not followed by a space, and a
// run that CLOSES it is not preceded by one — which is what keeps "2 * 3 * 4"
// arithmetic — and an underscore additionally may not open or close inside a
// word, which is what keeps snake_case_names intact.
func parseInline(dst []fragment, text string, base face) []fragment {
	prev := byte(0)
	emit := func(run string, f face) {
		if run == "" {
			return
		}
		dst = append(dst, fragment{run, f})
		prev = run[len(run)-1]
	}
	for len(text) > 0 {
		i := strings.IndexAny(text, "`*_")
		if i < 0 {
			emit(text, base)
			return dst
		}
		if i > 0 {
			emit(text[:i], base)
			text = text[i:]
		}
		switch delim := text[0]; delim {
		case '`':
			if end := strings.IndexByte(text[1:], '`'); end > 0 {
				emit(text[1:1+end], faceCode)
				text = text[end+2:]
				continue
			}
		case '*', '_':
			word := delim == '_' && isWordByte(prev)
			if len(text) > 2 && text[1] == delim && !isSpaceByte(text[2]) && !word {
				if end := strings.Index(text[2:], text[:2]); end > 0 &&
					!isSpaceByte(text[end+1]) && !closesInsideWord(delim, text, end+4) {
					dst = parseInline(dst, text[2:2+end], strongUnder(base))
					text = text[end+4:]
					prev = delim
					continue
				}
			}
			if len(text) > 1 && !isSpaceByte(text[1]) && !word {
				if end := strings.IndexByte(text[1:], delim); end > 0 &&
					!isSpaceByte(text[end]) && !closesInsideWord(delim, text, end+2) {
					dst = parseInline(dst, text[1:1+end], emphasisUnder(base))
					text = text[end+2:]
					prev = delim
					continue
				}
			}
		}
		// An unmatched delimiter. One byte of literal text, and the scan
		// advances — which is what makes this loop terminate on every input.
		emit(text[:1], base)
		text = text[1:]
	}
	return dst
}

// closesInsideWord reports that an underscore run at `after` would be closing
// in the middle of a word, which CommonMark forbids and snake_case relies on.
func closesInsideWord(delim byte, text string, after int) bool {
	return delim == '_' && after < len(text) && isWordByte(text[after])
}

func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' }

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b >= 0x80
}

// strongUnder and emphasisUnder keep a heading a heading and a quote a quote:
// emphasis inside them changes weight, never tier. Bold inside bold is bold.
func strongUnder(base face) face {
	if base == faceBody {
		return faceStrong
	}
	return base
}

func emphasisUnder(base face) face {
	if base == faceBody {
		return faceEmphasis
	}
	return base
}

// -- the wrap pass -----------------------------------------------------------

// wrap greedily fills rows with the fragments, painting each fragment as it
// lands. lead prefixes the first row and contd every row after it — the hanging
// indent a bullet or a quote gutter needs.
//
// Painting happens per fragment, after the fit is decided, so no row is ever
// cut through an escape sequence. Width is measured on the plain text for the
// same reason.
func (p prose) wrap(dst []string, frags []fragment, lead, contd string, width, indent int) []string {
	pad := strings.Repeat(" ", indent)
	leadW, contdW := blocks.Width(lead), blocks.Width(contd)
	if indent+leadW >= width || indent+contdW >= width {
		// No room for the affordance: the text itself is what matters, so the
		// gutter is what goes.
		lead, contd, leadW, contdW = "", "", 0, 0
	}

	var row strings.Builder
	rowW := 0
	pending := false
	opened := false

	open := func(prefix string, prefixW int) {
		row.Reset()
		row.WriteString(pad)
		if prefix != "" {
			row.WriteString(p.paint(prefix, faceChrome))
		}
		rowW = indent + prefixW
		opened = true
		pending = false
	}
	flush := func() {
		if opened {
			dst = append(dst, row.String())
		}
		opened = false
	}

	open(lead, leadW)
	floor := rowW
	for _, frag := range frags {
		for _, word := range splitWords(frag.text) {
			if word == " " {
				if rowW > floor {
					pending = true
				}
				continue
			}
			wordW := blocks.Width(word)
			need := wordW
			if pending {
				need++
			}
			if rowW+need > width && rowW > floor {
				flush()
				open(contd, contdW)
				floor = indent + contdW
				need = wordW
			}
			for rowW+need > width && wordW > width-floor {
				// A word longer than a whole row. It is broken where the row
				// ends rather than dropped: a 200-character URL is information,
				// and an ellipsis in the middle of it is not.
				if pending {
					row.WriteByte(' ')
					rowW++
					pending = false
				}
				head, tail := cutCells(word, width-rowW)
				if head == "" {
					break
				}
				row.WriteString(p.paint(head, frag.face))
				flush()
				open(contd, contdW)
				floor = indent + contdW
				word, wordW = tail, blocks.Width(tail)
				need = wordW
			}
			if word == "" {
				continue
			}
			if pending {
				row.WriteByte(' ')
				rowW++
				pending = false
			}
			row.WriteString(p.paint(word, frag.face))
			rowW += blocks.Width(word)
		}
	}
	flush()
	if len(dst) == 0 {
		return append(dst, "")
	}
	return dst
}

// splitWords splits on spaces, keeping each run of spaces as one " " token, so
// the wrapper can decide whether a break lands on it. Tabs became spaces at the
// sanitizer; anything else is a word.
func splitWords(text string) []string {
	if text == "" {
		return nil
	}
	out := make([]string, 0, 8)
	start, space := 0, false
	for i := 0; i < len(text); i++ {
		isSpace := text[i] == ' ' || text[i] == '\t'
		if i == start {
			space = isSpace
			continue
		}
		if isSpace != space {
			if space {
				out = append(out, " ")
			} else {
				out = append(out, text[start:i])
			}
			start, space = i, isSpace
		}
	}
	if space {
		out = append(out, " ")
	} else {
		out = append(out, text[start:])
	}
	return out
}

// cutCells splits s at n printable cells, returning the head and the rest. It
// never splits a rune and never returns a head wider than n.
func cutCells(s string, n int) (head, tail string) {
	if n <= 0 {
		return "", s
	}
	cells := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		_ = r
		w := blocks.Width(s[i : i+size])
		if cells+w > n {
			return s[:i], s[i:]
		}
		cells += w
		i += size
	}
	return s, ""
}
