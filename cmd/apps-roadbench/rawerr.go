package main

import "regexp"

// rawShape matches text that is a machine error and not owner words: Go error
// shapes, format verbs gone wrong, panics, stacks, file:line, deadlines.
var rawShape = regexp.MustCompile(`(?i)\bjson:|unexpected |%!|panic|\berror:|\b[\w./-]+\.go:\d+|goroutine \d+|\n\s+at |context deadline|context canceled|\bEOF\b|nil pointer|invalid character`)

// looksRaw says whether one line shown to the owner reads like a raw error.
func looksRaw(text string) bool { return rawShape.MatchString(text) }

// countRaw counts the lines that look like raw errors.
func countRaw(lines []string) int {
	n := 0
	for _, l := range lines {
		if looksRaw(l) {
			n++
		}
	}
	return n
}
