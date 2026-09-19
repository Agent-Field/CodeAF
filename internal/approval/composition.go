package approval

import "strings"

// ShellComposition is every character that can start a second command, redirect
// output, or substitute one. It is a CONSTANT rather than a literal at each gate
// because more than one reader asks the same question of a string, the proposal
// door and the runner among them, and a composition set spelled twice is a
// safety argument with two versions.
const ShellComposition = ";|&<>`$(){}\n\r\\"

// FirstCompositionOutsideQuotes is THE ONE READER of "is this one command", so
// every gate that asks the question asks it here and cannot drift from the
// others. It finds the first character that makes a line more than one command,
// reading quotes the way the shell that runs the check reads them, and reports
// false for a line that is one command.
//
// THE RULE IS ABOUT WHAT THE SHELL WOULD DO WITH THE CHARACTER, NOT ABOUT THE
// CHARACTER. Inside single quotes every character is text, so a bar in a quoted
// pattern is an argument and not a pipe. Inside double quotes the shell still
// EXPANDS: a dollar or a backtick there runs a command of its own, and a
// backslash escapes, so those three stay composition in double quotes exactly as
// they are outside any quote. Everything else in double quotes is text. Nothing a
// shape law stopped before starts running: what is newly allowed is only what the
// shell passes to the one program as an argument, byte for byte.
func FirstCompositionOutsideQuotes(text string) (byte, bool) {
	const liveInDoubleQuotes = "$`\\"
	var quote byte
	for i := 0; i < len(text); i++ {
		char := text[i]
		switch {
		case quote == '\'':
			if char == quote {
				quote = 0
			}
		case quote == '"':
			if char == quote {
				quote = 0
			} else if strings.IndexByte(liveInDoubleQuotes, char) >= 0 {
				return char, true
			}
		case char == '\'' || char == '"':
			quote = char
		case strings.IndexByte(ShellComposition, char) >= 0:
			return char, true
		}
	}
	// AN UNCLOSED QUOTE IS NOT ONE COMMAND EITHER: the shell would wait for more.
	if quote != 0 {
		return quote, true
	}
	return 0, false
}

// FirstBarOutsideQuotes is where the shell would end a line's first stage: the
// first pipe it would act on, read with the same quoting as
// [FirstCompositionOutsideQuotes], or -1 when there is none.
func FirstBarOutsideQuotes(line string) int {
	var quote byte
	for i := 0; i < len(line); i++ {
		char := line[i]
		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '\'' || char == '"':
			quote = char
		case char == '|':
			return i
		}
	}
	return -1
}
