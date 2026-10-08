package praf

import (
	"regexp"
	"strings"
)

// machineryWords are code review's words this house does not say to a
// person, and the words it says instead. pr-af's prompts and results speak of
// a finding's "verdict" and of claims "verified" or "refuted"; anything a
// person reads — the page, the account, the report — says it plainly, and a
// code identifier keeps its spelling only inside backticks.
var machineryWords = []struct {
	pattern *regexp.Regexp
	plain   string
}{
	{regexp.MustCompile(`(?i)\bverdicts\b`), "decisions"},
	{regexp.MustCompile(`(?i)\bverdict\b`), "decision"},
	{regexp.MustCompile(`(?i)\bunverified\b`), "untested"},
	{regexp.MustCompile(`(?i)\bverified\b`), "checked"},
	{regexp.MustCompile(`(?i)\brefuted\b`), "ruled out"},
	{regexp.MustCompile(`(?i)\bauditors\b`), "checkers"},
	{regexp.MustCompile(`(?i)\bauditor\b`), "checker"},
}

// codeSpan is a backticked identifier, which keeps its spelling.
var codeSpan = regexp.MustCompile("`[^`\n]*`")

// plainWords is text without the machinery words, outside its code spans.
func plainWords(text string) string {
	spans := codeSpan.FindAllStringIndex(text, -1)
	var b strings.Builder
	last := 0
	for _, span := range spans {
		b.WriteString(replaceMachinery(text[last:span[0]]))
		b.WriteString(text[span[0]:span[1]])
		last = span[1]
	}
	b.WriteString(replaceMachinery(text[last:]))
	return b.String()
}

func replaceMachinery(text string) string {
	for _, word := range machineryWords {
		text = word.pattern.ReplaceAllStringFunc(text, func(found string) string {
			// The plain word keeps the found word's capital, so a word that
			// opened a sentence still opens one.
			if found != "" && found[0] >= 'A' && found[0] <= 'Z' {
				return strings.ToUpper(word.plain[:1]) + word.plain[1:]
			}
			return word.plain
		})
	}
	return text
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// firstSentence is text's first line, cut where it runs long.
func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if line, _, ok := strings.Cut(text, "\n"); ok {
		text = line
	}
	if runes := []rune(text); len(runes) > 300 {
		text = string(runes[:300]) + "…"
	}
	return text
}
