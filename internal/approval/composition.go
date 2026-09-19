package approval

import "strings"

// ShellComposition is every character that can start a second command, redirect
// output, or substitute one. It is a CONSTANT rather than a literal at each gate
// because more than one reader asks the same question of a string, the proposal
// door and the runner among them, and a composition set spelled twice is a
// safety argument with two versions.
const ShellComposition = ";|&<>`$(){}\n\r\\"

// scanShellQuotes is THE ONE SCANNER UNDER BOTH READERS BELOW. They used to carry
// a quote loop each, and the two disagreed about a backslash: the composition
// reader refused every one inside double quotes, the bar reader had no escape
// rule at all and read `"a\" | b"` as a quotation that closed at the escaped
// quote. Two loops is a safety argument with two versions, so there is one, and
// a reader differs from the other only in which characters it calls live.
//
// THE RULE IS ABOUT WHAT THE SHELL WOULD DO WITH THE CHARACTER, NOT ABOUT THE
// CHARACTER. Inside single quotes every character is text, a backslash included,
// so a bar in a quoted pattern is an argument and not a pipe. Inside double
// quotes the shell still EXPANDS: a dollar or a backtick there runs a command of
// its own and stays live. A backslash there makes the character after it text,
// whichever character that is: the four the shell would otherwise act on are
// escaped by it, and before any other the backslash is itself text. So the pair
// is stepped over whole, which is what keeps an escaped quote from closing the
// quotation and a doubled backslash from hiding the quote that does close it.
// Everything else in double quotes is text.
//
// IT IS CONSERVATIVE BY CONSTRUCTION, and says so through its third answer. A
// shape it cannot prove is one whole command is UNCERTAIN, and both readers
// treat uncertain as refused: a quotation left open, a backslash with nothing
// after it, a backslash before a newline, which joins two lines, and every
// backslash outside quotes, where what it does depends on the character after
// it and this reader does not guess. Nothing a shape law stopped before starts
// running: what is newly read as text is only what the shell passes to the one
// program as an argument, byte for byte.
func scanShellQuotes(text string, live func(byte) bool) (index int, char byte, uncertain bool) {
	var quote byte
	for i := 0; i < len(text); i++ {
		char := text[i]
		switch quote {
		case '\'':
			if char == '\'' {
				quote = 0
			}
		case '"':
			switch char {
			case '"':
				quote = 0
			case '\\':
				// The pair is text. A backslash that ends the line or stands
				// before a newline is not a pair, and is refused as uncertain.
				if i+1 == len(text) || text[i+1] == '\n' {
					return i, char, true
				}
				i++
			default:
				// Only the two characters that expand stay live here.
				if (char == '$' || char == '`') && live(char) {
					return i, char, false
				}
			}
		default:
			switch char {
			case '\'', '"':
				quote = char
			case '\\':
				// Outside quotes a backslash was always refused and still is.
				return i, char, true
			default:
				if live(char) {
					return i, char, false
				}
			}
		}
	}
	// AN UNCLOSED QUOTE IS NOT ONE COMMAND EITHER: the shell would wait for more.
	if quote != 0 {
		return len(text), quote, true
	}
	return -1, 0, false
}

// FirstCompositionOutsideQuotes is THE ONE READER of "is this one command", so
// every gate that asks the question asks it here and cannot drift from the
// others. It finds the first character that makes a line more than one command,
// reading quotes the way the shell that runs the check reads them
// ([scanShellQuotes]), and reports false for a line that is one command. A shape
// the scanner cannot prove whole is answered as composed, naming the character
// that left it unproven.
func FirstCompositionOutsideQuotes(text string) (byte, bool) {
	_, char, uncertain := scanShellQuotes(text, func(char byte) bool {
		return strings.IndexByte(ShellComposition, char) >= 0
	})
	return char, char != 0 || uncertain
}

// FirstBarOutsideQuotes is where the shell would end a line's first stage: the
// first pipe it would act on, read by the same scanner as
// [FirstCompositionOutsideQuotes], or -1 when there is none. AN UNCERTAIN SHAPE
// AHEAD OF ANY BAR ANSWERS -1 TOO, and that is the safe way to be wrong: the line
// is then left whole, the uncertainty still in it, and the composition reader
// refuses it at the door. A cut there would hand the door a shorter command than
// the one that was written.
func FirstBarOutsideQuotes(line string) int {
	index, _, uncertain := scanShellQuotes(line, func(char byte) bool { return char == '|' })
	if uncertain {
		return -1
	}
	return index
}
