package approval

import "strings"

// ShellComposition is every character that can start a second command, redirect
// output, or substitute one. It is a CONSTANT rather than a literal at each gate
// because more than one reader asks the same question of a string, the proposal
// door and the runner among them, and a composition set spelled twice is a
// safety argument with two versions.
const ShellComposition = ";|&<>`$(){}\n\r\\"

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
				// A non-newline byte paired with a backslash is text in double
				// quotes. Refuse incomplete lines and continuations: this reader
				// cannot prove those bytes form one complete command.
				if i+1 == len(text) || text[i+1] == '\n' {
					return i, char, true
				}
				i++
			default:
				// Only expansion markers remain live in double quotes.
				if (char == '$' || char == '`') && live(char) {
					return i, char, false
				}
			}
		default:
			switch char {
			case '\'', '"':
				quote = char
			case '\\':
				// Outside quotes, keep the historical failing-first rule. Shell
				// treatment depends on the following byte, so certainty about a
				// complete one-command shape is deliberately not inferred here.
				return i, char, true
			default:
				if live(char) {
					return i, char, false
				}
			}
		}
	}
	if quote != 0 {
		return len(text), quote, true
	}
	return -1, 0, false
}

// FirstCompositionOutsideQuotes is THE ONE READER of "is this one command", so
// every gate that asks the question asks it here and cannot drift from the
// others. It finds the first live composition byte or the first uncertain quote
// shape, failing first when the scanner cannot prove the line is one command.
func FirstCompositionOutsideQuotes(text string) (byte, bool) {
	_, char, uncertain := scanShellQuotes(text, func(char byte) bool {
		return strings.IndexByte(ShellComposition, char) >= 0
	})
	return char, char != 0 || uncertain
}

// FirstBarOutsideQuotes is where the shell would end a line's first stage: the
// first pipe it would act on, read by the same conservative quote scanner as
// [FirstCompositionOutsideQuotes], or -1 when there is none or the earlier shape
// is uncertain.
func FirstBarOutsideQuotes(line string) int {
	index, _, uncertain := scanShellQuotes(line, func(char byte) bool { return char == '|' })
	if uncertain {
		return -1
	}
	return index
}
