package subharness

// SALVAGE: the ladder a model's reply climbs before this package will call it
// JSON.
//
// A design that was RIGHT and a reply that PARSED are two different events, and
// the whole reason this file exists is that the second one keeps failing for
// reasons that have nothing to do with the first. A model asked for a page hands
// back a page wearing a code fence, or a sentence of apology in front of it, or —
// the expensive one — a page whose delimiters are “ and ” because something in the
// generation drifted into prose typography. Every one of those is a good design
// refused, and a refusal costs a full retry: the whole guide, the whole draft, the
// whole bill, to fix punctuation.
//
// So the reply is repaired in rungs, cheapest first, and NOTHING here guesses at
// meaning:
//
//	strict    the text as it arrived
//	extract   the outermost balanced {…}, found by a scanner that knows a brace
//	          inside a string is not a brace
//	sanitize  Unicode punctuation mapped to ASCII OUTSIDE string values, string
//	          delimiters normalised to ", invalid UTF-8 dropped
//	lenient   trailing commas and comments removed, raw control characters inside
//	          strings escaped
//
// THE LAW OF THIS FILE IS THAT A STRING VALUE IS THE MODEL'S. Inside a string,
// only the delimiters are touched: an em-dash in a brief is the writer's em-dash,
// a curly apostrophe in a description is the writer's apostrophe, and a salvage
// pass that "cleaned" either would be editing the design in order to parse it. The
// punctuation table applies where punctuation cannot be prose — between the
// values, where only syntax lives.
//
// encoding/json is the gate at EVERY rung, not a rung of its own: nothing leaves
// Salvage that the strict decoder will not read. What the rungs decide is how much
// repair that took, and the name of the rung that worked is reported so a run can
// say "this reply needed sanitising" out loud instead of quietly accepting a model
// that has started emitting typographic quotes.

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The rung names, in the order they are climbed.
const (
	SalvageStrict   = "strict"
	SalvageExtract  = "extract"
	SalvageSanitize = "sanitize"
	SalvageLenient  = "lenient"
)

// Salvaged is what a successful walk up the ladder leaves behind: the JSON, the
// rung that produced it, and the rungs that were spent getting there.
type Salvaged struct {
	// JSON is text encoding/json has already agreed to read.
	JSON []byte
	// Rung is the name of the step that produced it — [SalvageStrict] when the
	// reply needed nothing, which is the only outcome worth being quiet about.
	Rung string
	// Climbed names every rung attempted, in order, ending at Rung.
	Climbed []string
}

// Clean reports whether the reply arrived needing no repair at all.
func (s Salvaged) Clean() bool { return s.Rung == SalvageStrict }

// Salvage turns a model's reply into JSON, or reports why it could not.
//
// The error is written to be HANDED BACK TO THE MODEL: it names the rungs that
// were tried and quotes the strict decoder's own complaint with the text around
// the offset, because a repair turn that is only told "invalid JSON" is being
// asked to guess which of two thousand characters was wrong.
func Salvage(raw string) ([]byte, error) {
	out, err := SalvageDetail(raw)
	return out.JSON, err
}

// SalvageDetail is [Salvage] with the rung it succeeded at, for callers that log
// what the reply cost them.
func SalvageDetail(raw string) (Salvaged, error) {
	ladder := []struct {
		name string
		fix  func(string) string
	}{
		{SalvageStrict, strings.TrimSpace},
		{SalvageExtract, extract},
		{SalvageSanitize, sanitize},
		{SalvageLenient, lenient},
	}

	text := raw
	out := Salvaged{}
	for _, rung := range ladder {
		// The rungs COMPOSE: each one repairs what the one below it left, so a
		// reply that arrived fenced AND smart-quoted is not asked to be one or
		// the other.
		text = rung.fix(text)
		out.Climbed = append(out.Climbed, rung.name)
		if wellFormed(text) {
			out.JSON = []byte(text)
			out.Rung = rung.name
			return out, nil
		}
	}
	return out, fmt.Errorf("this reply is not JSON, and the salvage ladder (%s) could not make it JSON: %w",
		strings.Join(out.Climbed, " → "), parseError(text))
}

// wellFormed is the gate every rung is measured against: the strict decoder
// reads it, and it is valid UTF-8.
//
// UTF-8 IS PART OF THE GATE ON PURPOSE. encoding/json will happily validate a
// string full of bytes no decoder can turn back into text — they become
// replacement characters on the way into a Go string, and a brief with a U+FFFD
// where a word was is a corrupted page that parsed. Requiring valid UTF-8 here
// is what sends mojibake up to the sanitize rung instead of through the gate.
func wellFormed(text string) bool {
	return utf8.ValidString(text) && json.Valid([]byte(text))
}

// parseError is the strict decoder's own complaint, with the text around the
// offset it stopped at.
func parseError(text string) error {
	err := json.Unmarshal([]byte(text), new(json.RawMessage))
	if err == nil {
		if !utf8.ValidString(text) {
			return fmt.Errorf("the reply carries bytes that are not UTF-8")
		}
		return fmt.Errorf("the reply is not one JSON value")
	}
	syntax, ok := err.(*json.SyntaxError)
	if !ok {
		return err
	}
	at := int(syntax.Offset)
	if at < 0 || at > len(text) {
		return err
	}
	from, to := at-60, at+60
	if from < 0 {
		from = 0
	}
	if to > len(text) {
		to = len(text)
	}
	return fmt.Errorf("%w (at byte %d, around here: %s)", err, at, strings.TrimSpace(text[from:to]))
}

// ── the scanner every rung shares ───────────────────────────────────────────

// role is what one rune is doing: opening a string, inside one, closing one, or
// out in the syntax where only structure lives.
type role int

const (
	roleOutside role = iota
	roleOpen
	roleInside
	roleClose
)

// scan is a string-aware cursor over JSON-ish text. It exists because every
// repair on this ladder is wrong if it is applied inside a string value — a
// brace, a comma, a slash and an em-dash are all ordinary characters in a brief
// — so all three rungs walk the text through the same state machine rather than
// each inventing its own idea of where a string is.
//
// It knows TYPOGRAPHIC QUOTES because the reply that needs salvaging is exactly
// the reply whose delimiters are not ". A string opened with " is closed only by
// ", so a legal page that quotes “an event log” inside a description survives
// untouched; a string opened with a curly quote is closed by the next quote of
// either shape, because a model that has drifted into typography has drifted for
// the whole reply.
type scan struct {
	inString bool
	smart    bool // the open delimiter was typographic
	escaped  bool
}

func (s *scan) step(r rune) role {
	if s.inString {
		switch {
		case s.escaped:
			s.escaped = false
		case r == '\\':
			s.escaped = true
		case r == '"', s.smart && isSmartQuote(r):
			s.inString = false
			return roleClose
		}
		return roleInside
	}
	if r == '"' || isSmartQuote(r) {
		s.inString = true
		s.smart = isSmartQuote(r)
		return roleOpen
	}
	return roleOutside
}

func isSmartQuote(r rune) bool {
	switch r {
	case '\u201c', '\u201d', '\u201e', '\u201f': // “ ” „ ‟
		return true
	}
	return false
}

// ── rung: extract ───────────────────────────────────────────────────────────

// extract takes the object out of a reply that arrived wearing something: a code
// fence, a sentence of introduction, a paragraph of apology after the closing
// brace. It is the cheapest rung because it deletes and never rewrites.
//
// The scan starts AT THE FIRST BRACE and not at the start of the text, so prose
// with an odd number of quotes in it — "here's the harness you asked for" — does
// not leave the cursor believing it is inside a string when the object begins.
func extract(text string) string {
	text = strings.TrimSpace(text)
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return text
	}
	var (
		cursor scan
		depth  int
	)
	for at, r := range text[start:] {
		if cursor.step(r) != roleOutside {
			continue
		}
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : start+at+1]
			}
		}
	}
	// The braces never balanced — the reply was cut off, or a delimiter is so
	// mangled the scan lost the thread. Hand back everything from the first brace
	// to the last one and let the rungs above try; a truncated object still fails
	// the gate, and the error says where.
	if end := strings.LastIndexByte(text, '}'); end > start {
		return text[start : end+1]
	}
	return text[start:]
}

// ── rung: sanitize ──────────────────────────────────────────────────────────

// punctuation is the map applied OUTSIDE string values, where a character that
// is not ASCII cannot be prose and can only be a generation that drifted.
var punctuation = map[rune]string{
	'\u2018': "'",   // left single quote
	'\u2019': "'",   // right single quote, the apostrophe a model reaches for
	'\u201a': "'",   // low single quote
	'\u2013': "-",   // en dash
	'\u2014': "-",   // em dash
	'\u2026': "...", // ellipsis
	'\u00a0': " ",   // no-break space
	'\u2007': " ",   // figure space
	'\u2009': " ",   // thin space
	'\u202f': " ",   // narrow no-break space
	'\ufeff': "",    // a byte-order mark that wandered into the middle
}

// sanitize normalises the syntax and leaves the prose alone.
//
// The two halves of that sentence are the whole design. String DELIMITERS are
// rewritten to " wherever they are, because a page whose quotes are typographic
// is a page nothing can read. Everything INSIDE a string is copied through
// byte for byte — the em-dash a designer wrote in a brief is content, and a
// salvage pass that turned it into a hyphen would have quietly edited the design
// it was trying to rescue.
func sanitize(text string) string {
	// Bytes that are not UTF-8 are dropped rather than replaced: a replacement
	// character is a visible lie about what the model wrote, and a dropped byte
	// at least leaves the surrounding words readable.
	text = strings.ToValidUTF8(text, "")

	var (
		out    strings.Builder
		cursor scan
	)
	out.Grow(len(text))
	for _, r := range text {
		switch cursor.step(r) {
		case roleOpen, roleClose:
			out.WriteByte('"')
		case roleInside:
			out.WriteRune(r)
		default:
			if replacement, mapped := punctuation[r]; mapped {
				out.WriteString(replacement)
				continue
			}
			out.WriteRune(r)
		}
	}
	return out.String()
}

// ── rung: lenient ───────────────────────────────────────────────────────────

// lenient is the last rung: the JSON dialects a model picks up from reading code.
// A trailing comma before a closing brace, a `//` note explaining a field, a
// literal newline in the middle of a long brief — all illegal, all mechanical to
// undo, and none of them a judgement about what the design meant.
//
// It is a scanner and not a regular expression because every one of these
// patterns is legal text inside a string value: a brief that says "10 // 3" or
// ends a sentence with a comma before a quote is not a syntax error, and a
// regular expression cannot tell the difference.
func lenient(text string) string {
	var (
		out    strings.Builder
		cursor scan
		runes  = []rune(text)
	)
	out.Grow(len(text))
	for at := 0; at < len(runes); at++ {
		r := runes[at]
		switch cursor.step(r) {
		case roleInside:
			// A raw control character inside a string is illegal JSON and is
			// almost always a long brief that the model laid out over lines.
			// Escaping it keeps the text; dropping it would lose a paragraph
			// break the writer meant.
			out.WriteString(escapeControl(r))
			continue
		case roleOpen, roleClose:
			out.WriteRune(r)
			continue
		}
		// Outside a string: comments and trailing commas.
		if r == '/' && at+1 < len(runes) {
			switch runes[at+1] {
			case '/':
				for at < len(runes) && runes[at] != '\n' {
					at++
				}
				out.WriteByte('\n')
				continue
			case '*':
				at += 2
				for at+1 < len(runes) && !(runes[at] == '*' && runes[at+1] == '/') {
					at++
				}
				at++
				continue
			}
		}
		if r == ',' && nextIsClose(runes, at+1) {
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// nextIsClose reports whether the next thing that is not whitespace or a comment
// closes an object or an array — which is what makes the comma behind it a
// trailing one.
func nextIsClose(runes []rune, at int) bool {
	for at < len(runes) {
		switch r := runes[at]; {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			at++
		case r == '/' && at+1 < len(runes) && runes[at+1] == '/':
			for at < len(runes) && runes[at] != '\n' {
				at++
			}
		case r == '/' && at+1 < len(runes) && runes[at+1] == '*':
			at += 2
			for at+1 < len(runes) && !(runes[at] == '*' && runes[at+1] == '/') {
				at++
			}
			at += 2
		case r == '}' || r == ']':
			return true
		default:
			return false
		}
	}
	return false
}

func escapeControl(r rune) string {
	switch r {
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\t':
		return `\t`
	}
	if r < 0x20 {
		return fmt.Sprintf(`\u%04x`, r)
	}
	return string(r)
}
