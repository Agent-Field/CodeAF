package approval

import "strings"

// CommandWords splits a shell command into the commands it is made of.
//
// IT IS A READER AND NOT A SHELL. Everything that ENDS one command and starts
// another — `&&`, `||`, `;`, `|`, a newline, a subshell, a substitution — is a
// break; quotes hold a run of words together and are stripped; a backslash
// escapes the character after it and a backslash-newline joins the lines, which
// is how the measured commands were actually written. Redirections are kept as
// words of their own so the file they name can be read.
//
// What it deliberately does not do is expand anything. `$REPO/file` stays as it
// is written, so a path built out of a variable is a path this scan cannot place —
// stated here because it is the shape of the next escalation, and because a
// reader of this file should not have to discover it by testing.
func CommandWords(command string) [][]string {
	var segments [][]string
	var words []string
	var word strings.Builder
	var quote rune
	// resume is the double quote a substitution was opened INSIDE, kept so the
	// quote goes back on when that substitution closes. See the `$` case below.
	var resume rune
	flushWord := func() {
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	flushSegment := func() {
		flushWord()
		if len(words) > 0 {
			segments = append(segments, words)
			words = nil
		}
	}
	runes := []rune(command)
	for index := 0; index < len(runes); index++ {
		letter := runes[index]
		if quote != 0 {
			// A SUBSTITUTION INSIDE DOUBLE QUOTES IS STILL A COMMAND. The shell
			// runs `"$(gh auth token)"` exactly as it runs it bare — the quotes
			// are about what happens to the ANSWER — and a reader that let the
			// quote swallow it read `curl -d "$(gh auth token)" …` as one word
			// and saw no command there at all. That is the shape the credential
			// law next door is written from, so it is read here rather than
			// worked around there. Single quotes are left alone, because inside
			// them the shell does not substitute either.
			if quote == '"' && letter == '$' && index+1 < len(runes) && runes[index+1] == '(' {
				index++
				resume = quote
				quote = 0
				flushSegment()
				continue
			}
			if letter == quote {
				quote = 0
				continue
			}
			word.WriteRune(letter)
			continue
		}
		// The close of a substitution that was opened inside a double quote puts
		// the quote back, so the rest of the quoted word is read as it was.
		if letter == ')' && resume != 0 {
			flushSegment()
			quote = resume
			resume = 0
			continue
		}
		switch letter {
		case '\'', '"':
			quote = letter
		case '\\':
			if index+1 < len(runes) {
				index++
				if runes[index] != '\n' {
					word.WriteRune(runes[index])
				}
			}
		case ' ', '\t', '\r':
			flushWord()
		case '\n', ';', '&', '|', '(', ')', '{', '}', '`':
			flushSegment()
		case '$':
			if index+1 < len(runes) && runes[index+1] == '(' {
				index++
				flushSegment()
				continue
			}
			// A BRACED VARIABLE IS ONE WORD. Treating its brace as a command
			// boundary read `cat ${HOME}/.netrc` as three commands, none of which
			// named the file.
			if index+1 < len(runes) && runes[index+1] == '{' {
				for index < len(runes) && runes[index] != '}' {
					word.WriteRune(runes[index])
					index++
				}
				if index < len(runes) {
					word.WriteRune('}')
				}
				continue
			}
			word.WriteRune(letter)
		case '>', '<':
			flushWord()
			operator, last := redirectOperator(runes, index)
			index = last
			words = append(words, operator)
		default:
			word.WriteRune(letter)
		}
	}
	flushSegment()
	return segments
}

func redirectOperator(runes []rune, index int) (string, int) {
	operator := string(runes[index])
	if operator == "<" {
		if index+1 < len(runes) && runes[index+1] == '>' {
			return "<>", index + 1
		}
		return operator, index
	}
	for index+1 < len(runes) && strings.ContainsRune(">&|", runes[index+1]) {
		index++
		operator += string(runes[index])
	}
	return operator, index
}
