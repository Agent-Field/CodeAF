package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// THE SHELL LEXER, AND WHY IT IS A LEXER AND NOT A PARSER.
//
// A bash row is the one tool line on this surface whose target is a LANGUAGE.
// Every other target is a noun — a path, a pattern — and one ink tier says
// everything there is to say about it. A command is a sentence: it has a verb,
// it has quoted text, it has switches, it has places, and it has the operators
// that join two commands into one. Painted in a single tier it reads as a wall,
// and the whole point of opening a bash row is to read the command.
//
//	╰─▶ bash cd /tmp && echo "hi" | grep -n x # note
//	│ cd /tmp && echo "hi" | grep -n x # note
//	│ ─┬ ──┬─ ─┬ ──┬─ ──┬─ ─┬── ─┬ ┬  └── comment  dim
//	│  │   │   │   │    │   │    │ └───── plain    ink
//	│  │   │   │   │    │   │    └─────── flag     dim
//	│  │   │   │   │    │   └──────────── command  ink
//	│  │   │   │   │    └──────────────── operator dim
//	│  │   │   │   └───────────────────── string   ink
//	│  │   │   └───────────────────────── command  ink
//	│  │   └───────────────────────────── operator dim
//	│  └───────────────────────────────── path     ink, underlined
//	└──────────────────────────────────── command  ink
//
// It is a LEXER — one left-to-right pass, no grammar, no state beyond "is the
// next word in command position" — and that is a ceiling, not a shortcut. A
// parser would have to decide what `$(…)` returns and whether `{` opened a
// block or a brace expansion, and every one of those decisions is a chance to
// paint a command as something it is not. The rules below are all SHAPE rules,
// each one true of every shell anybody runs, and the failure mode of every one
// of them is a token painted plain — which is what the whole line looked like
// before this file existed.
//
// What it deliberately does not do:
//
//   - it does not follow a quote across a newline. Each line is lexed on its
//     own (see [shellLines]) so that a multi-line command's rows can be
//     highlighted independently, which is what lets the expansion wrap without
//     re-lexing; a heredoc body is therefore lexed as ordinary words.
//   - it does not know your $PATH. A word in command position is a command
//     whether or not it exists, because the surface is describing what was RUN,
//     not what would have worked.
//   - it does not expand anything. `$HOME/x` is a path because it has a slash
//     in it, and `$HOME` alone is plain.

// shellKind is one token's role, and the paint follows from it directly.
type shellKind uint8

const (
	// shellPlain is an argument that is none of the below: the honest default,
	// and the failure mode of every rule in this file.
	shellPlain shellKind = iota
	// shellCommand is the word in command position — the start of the line, or
	// the first word after an operator. Builtins are not a separate kind: `cd`
	// and `curl` are both the verb of their sentence, and a table of builtins
	// would be a list to maintain for a distinction nobody reading a command
	// line makes.
	shellCommand
	// shellString is a quoted run, quotes included.
	shellString
	// shellFlag is a word starting with '-'.
	shellFlag
	// shellPath is a word shaped like a location — see [looksPath].
	shellPath
	// shellOperator is what joins or redirects: | && || ; & > >> < <<.
	shellOperator
	// shellNumber is a bare numeric word.
	shellNumber
	// shellComment is '#' at a word boundary and everything after it.
	shellComment
	// shellSpace is the whitespace between tokens. It is a token rather than a
	// gap because the painted line is assembled by joining tokens, and a gap
	// nobody emitted is a command whose words run together.
	shellSpace
)

// shellTok is one token: its role and its exact text. Concatenating every
// token's text returns the input unchanged, which is the invariant that keeps
// the painted line the same WIDTH as the line that was measured.
type shellTok struct {
	kind shellKind
	text string
}

// shellOps are the operators, longest first — the order is the match order, so
// `&&` is never read as two `&`.
var shellOps = []string{"&&", "||", ">>", "<<", "|&", "|", ";", "&", ">", "<"}

// lexShell splits one LINE of shell into tokens.
func lexShell(line string) []shellTok {
	var out []shellTok
	emit := func(kind shellKind, text string) {
		if text != "" {
			out = append(out, shellTok{kind: kind, text: text})
		}
	}
	// command says the next word is the verb of a sentence. It opens true and is
	// re-armed by every operator, which is the whole of this lexer's state.
	command := true
	for at := 0; at < len(line); {
		switch c := line[at]; {
		case c == ' ' || c == '\t':
			end := at
			for end < len(line) && (line[end] == ' ' || line[end] == '\t') {
				end++
			}
			emit(shellSpace, line[at:end])
			at = end

		case c == '#':
			// A '#' is a comment only at a word boundary: `foo#bar` is a word and
			// `curl http://x/#frag` is a URL, and neither is a note to the reader.
			emit(shellComment, line[at:])
			at = len(line)

		case c == '\'' || c == '"':
			end := closingQuote(line, at)
			emit(shellString, line[at:end])
			at = end
			command = false

		case matchOp(line, at) != "":
			op := matchOp(line, at)
			emit(shellOperator, op)
			at += len(op)
			command = true

		default:
			end := at
			for end < len(line) && !isBreak(line, end) {
				end++
			}
			word := line[at:end]
			emit(classify(word, command), word)
			at = end
			command = false
		}
	}
	return out
}

// isBreak reports whether a word ends at this byte: whitespace, a quote, or the
// start of an operator. A '#' does NOT break a word, which is what keeps
// `#frag` inside the URL it belongs to.
func isBreak(line string, at int) bool {
	switch line[at] {
	case ' ', '\t', '\'', '"':
		return true
	}
	return matchOp(line, at) != ""
}

// matchOp returns the operator starting at at, or "".
func matchOp(line string, at int) string {
	for _, op := range shellOps {
		if strings.HasPrefix(line[at:], op) {
			return op
		}
	}
	return ""
}

// closingQuote finds one byte past the run opened at `at`. An unterminated
// quote runs to the end of the line, which is both what the shell would have
// complained about and what the reader needs to see.
func closingQuote(line string, at int) int {
	quote := line[at]
	for end := at + 1; end < len(line); end++ {
		// A backslash escapes inside "…" and does not inside '…' — the one shell
		// rule this file bothers to know, because getting it wrong paints the
		// rest of a command as a string.
		if quote == '"' && line[end] == '\\' {
			end++
			continue
		}
		if line[end] == quote {
			return end + 1
		}
	}
	return len(line)
}

// classify decides what one word is. The order is the order of certainty:
// position first (a word in command position is the verb whatever it looks
// like), then the two shapes that are unambiguous, then the guess.
func classify(word string, command bool) shellKind {
	switch {
	case command:
		return shellCommand
	case strings.HasPrefix(word, "-"):
		return shellFlag
	case isNumeric(word):
		return shellNumber
	case looksPath(word):
		return shellPath
	default:
		return shellPlain
	}
}

func isNumeric(word string) bool {
	dots := 0
	for i := 0; i < len(word); i++ {
		switch {
		case word[i] >= '0' && word[i] <= '9':
		case word[i] == '.' && i > 0 && i < len(word)-1:
			dots++
		default:
			return false
		}
	}
	return word != "" && dots <= 1
}

// looksPath is the one rule in this file that is a guess, and it is bounded to
// two shapes that are a location and nothing else:
//
//	it contains a '/', or starts with '~'   /tmp, ./x, internal/session, ~/.zshrc
//	it ends in a short dotted suffix        loop.go, README.md, x.tar.gz
//
// The second is the guess. It is worth making because a bare filename is the
// commonest target in this tree's own commands (`go test ./…` aside, everything
// is a file), and its failure mode is a word painted as a path that was not one
// — which costs an underline and nothing else. `3.14` never reaches here: a
// numeric word was already claimed above.
func looksPath(word string) bool {
	if strings.ContainsRune(word, '/') || strings.HasPrefix(word, "~") {
		return true
	}
	dot := strings.LastIndexByte(word, '.')
	if dot <= 0 || dot == len(word)-1 {
		return false
	}
	suffix := word[dot+1:]
	if len(suffix) > 5 {
		return false
	}
	for i := 0; i < len(suffix); i++ {
		if c := suffix[i] | 0x20; c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

// paintShell is one token's paint. It is the whole of the theme's shell
// vocabulary, in one switch, so a change of hue here is a change of hue
// everywhere a command is drawn.
func (p palette) paintShell(tok shellTok) string {
	switch tok.kind {
	case shellCommand:
		return p.ink(tok.text)
	case shellString:
		return p.ink(tok.text)
	case shellFlag:
		// A flag qualifies the verb, so it recedes with the other shell grammar.
		return p.dim(tok.text)
	case shellPath:
		return p.underline(p.ink(tok.text))
	case shellOperator:
		return p.dim(tok.text)
	case shellNumber, shellComment:
		return p.dim(tok.text)
	case shellSpace:
		return tok.text
	default:
		return p.ink(tok.text)
	}
}

// shell paints one line of a command. The line arrives already fitted to its
// width where a width applies (the tool line), because measuring through an
// escape sequence is measuring wrong — see [app.toolLine].
func (p palette) shell(line string) string {
	var b strings.Builder
	for _, tok := range lexShell(line) {
		b.WriteString(p.paintShell(tok))
	}
	return b.String()
}

// ── THE EXPANSION ───────────────────────────────────────────────────────────

// shellRows lays a whole command out under an open bash row: every line of it,
// highlighted, WRAPPED rather than truncated.
//
// Truncation is what the tool LINE does, and it is right there — one row, one
// glance, the target clipped when the terminal is narrow. It is wrong here.
// Clicking a bash row is a person asking to read the command, and a command
// whose last argument was cut off at column 78 is a command they have to go
// somewhere else to see. So nothing is dropped: a long line becomes two rows,
// and a fifty-line command becomes fifty rows.
//
// The wrap breaks at token boundaries where it can and inside a token when it
// must, which keeps a quoted string whole until it is longer than the terminal
// — and it wraps the LEXED tokens rather than the text, so a string split
// across two rows is still painted as a string on both of them.
func shellRows(pal palette, command string, width int) []string {
	if width < 8 {
		width = 8
	}
	var out []string
	for _, line := range shellLines(command) {
		out = append(out, wrapShell(pal, lexShell(line), width)...)
	}
	return out
}

// shellLines splits a command into its logical lines, tabs expanded. A trailing
// newline does not open an empty row.
func shellLines(command string) []string {
	command = strings.TrimRight(expandTabs(command), "\n")
	if command == "" {
		return nil
	}
	return strings.Split(command, "\n")
}

// wrapShell folds one line's tokens into rows of at most width cells.
func wrapShell(pal palette, tokens []shellTok, width int) []string {
	var out []string
	var row strings.Builder
	used := 0
	flush := func() {
		if used > 0 {
			out = append(out, row.String())
			row.Reset()
			used = 0
		}
	}
	for _, tok := range tokens {
		for tok.text != "" {
			free := width - used
			if free <= 0 {
				flush()
				continue
			}
			if w := ansi.StringWidth(tok.text); w <= free {
				row.WriteString(pal.paintShell(tok))
				used += w
				break
			}
			// The token does not fit. Move it to the next row whole when it would
			// fit there and this row has something on it already; otherwise it is
			// longer than the terminal and has to be cut somewhere, so it is cut
			// here.
			if used > 0 && ansi.StringWidth(tok.text) <= width {
				flush()
				continue
			}
			head, rest := splitCells(tok.text, free)
			row.WriteString(pal.paintShell(shellTok{kind: tok.kind, text: head}))
			used += ansi.StringWidth(head)
			tok.text = rest
			flush()
		}
	}
	if used > 0 || len(out) == 0 {
		out = append(out, row.String())
	}
	return out
}

// splitCells cuts a plain string at n printable cells. It walks runes rather
// than bytes so a multi-byte character is never halved, and it never returns an
// empty head for a positive n — a cut that made no progress would loop.
func splitCells(s string, n int) (head, rest string) {
	if n < 1 {
		n = 1
	}
	used := 0
	for i, r := range s {
		w := ansi.StringWidth(string(r))
		if used+w > n && i > 0 {
			return s[:i], s[i:]
		}
		used += w
	}
	return s, ""
}
