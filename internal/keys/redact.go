package keys

import (
	"regexp"
	"strings"
)

// Redacted stands where a secret's value was. It is the same three dots the
// person reads in a command line that was cleaned, so a cleaned line is
// recognisable as one.
const Redacted = "…"

// secretName is a variable or flag name that says its value is a secret. The
// content rules catch a value that looks like a key whatever it is called; this
// catches the value that looks like nothing (`--token abc`), which only its name
// gives away.
var secretName = regexp.MustCompile(`(?i)(token|secret|passw|pwd|credential|api[-_]?key|private[-_]?key|access[-_]?key|authorization|\bauth\b)`)

// Clean returns the command line with every secret value replaced by
// [Redacted]: the value of a `NAME=value` word or a `--flag value` pair whose
// name is a secret's, and any word a content rule recognises as a key. It reads
// a line the way a shell does, cut at blanks outside quotes (a newline stays inside its word, so a script keeps its lines), and never changes
// what it does not clean. A line it cannot read (an unclosed quote) is cut to
// its first word alone, because a line that cannot be understood cannot be shown
// to be clean.
func Clean(command string) string {
	words, ok := splitWords(command)
	if !ok {
		first, _, _ := strings.Cut(strings.TrimSpace(command), " ")
		return first
	}
	for i := range words {
		words[i] = cleanWord(words, i)
	}
	return strings.Join(words, " ")
}

// cleanWord is words[i] with its secret, if it has one, replaced. A flag whose
// name is a secret's takes the word after it as its value, so that word is the
// one replaced when the flag stands alone.
func cleanWord(words []string, i int) string {
	word := words[i]
	if name, _, ok := strings.Cut(word, "="); ok && secretName.MatchString(name) {
		return name + "=" + Redacted
	}
	if i > 0 && isFlagWithSecretName(words[i-1]) {
		return Redacted
	}
	if _, hit := firstRule(contentRules, "", []byte(word)); hit {
		return Redacted
	}
	return word
}

// isFlagWithSecretName reports a bare `--flag` whose value is the next word.
func isFlagWithSecretName(word string) bool {
	return strings.HasPrefix(word, "-") && !strings.Contains(word, "=") && secretName.MatchString(word)
}

// splitWords cuts a line at blanks that are not inside quotes, keeping each
// word's own quotes so the line reads back as it was written. It reports false
// for a line with an unclosed quote.
func splitWords(line string) ([]string, bool) {
	var words []string
	var word strings.Builder
	var quote rune
	flush := func() {
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			word.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
			word.WriteRune(r)
		case r == ' ' || r == '\t':
			flush()
		default:
			word.WriteRune(r)
		}
	}
	flush()
	return words, quote == 0
}
