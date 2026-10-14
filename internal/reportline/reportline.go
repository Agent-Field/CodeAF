// Package reportline holds the ONE reading of a task report's first prose line,
// and the fence-marker detection that reading rests on.
//
// A task report is the model's own Markdown. A model that has just run a command
// or produced a diff opens with the fence around it rather than with a sentence —
// the fence is not a defect in the report, it is how the answer is spelled. Every
// surface that quotes a report's opening (the landed card, the tasks table, the
// home panels, the switcher) wants the first line that SAYS something, not the
// row of punctuation around the answer.
//
// That reading lived in internal/tui3 as firstProseLine (#889), and the fence
// detector it leaned on, mdFenceOpen, is private to that package. internal/session
// writes the same field through taskOutcome and could not reach either, so it read
// the literal first line and the fence came back on the tasks table and the home
// panels while the card was already correct (#931). Copying the loop into session
// would have been the wrong fix: TWO OPINIONS ABOUT WHAT A FENCE IS DRIFT. So the
// reading and the detection live here, in a leaf package both the surface and the
// engine import, and there is exactly one of each.
package reportline

import "strings"

// Fence reports whether line opens a Markdown code fence, returning the delimiter
// run and the info string the way the renderer reads them. The caller passes a
// line already trimmed of leading space: a fence marker says nothing wherever it
// stands, and FirstProse trims before it asks.
//
// This is the renderer's own fence opinion — internal/tui3's mdFenceOpen is a thin
// wrapper over it — so a marker with a language word after it (go, sh, diff, json)
// is a marker here for exactly the reason it is one there. There is no second
// opinion about what a fence is.
func Fence(line string) (delim, info string, ok bool) {
	var mark byte
	switch {
	case strings.HasPrefix(line, "```"):
		mark = '`'
	case strings.HasPrefix(line, "~~~"):
		mark = '~'
	default:
		return "", "", false
	}
	n := 0
	for n < len(line) && line[n] == mark {
		n++
	}
	info = strings.TrimSpace(line[n:])
	// A backtick in a backtick fence's info string is not an info string —
	// CommonMark says so, and the line is prose.
	if mark == '`' && strings.ContainsRune(info, '`') {
		return "", "", false
	}
	// The language is the first word; the rest is metadata no lexer reads.
	if cut := strings.IndexAny(info, " \t"); cut >= 0 {
		info = info[:cut]
	}
	return line[:n], info, true
}

// FirstProse is the first line of s that says something, trimmed: a blank line
// says nothing, a fence marker says nothing, and the first line that is neither is
// what the work came to.
//
// A REPORT THAT IS NOTHING BUT A FENCED BLOCK RETURNS THE FIRST LINE INSIDE IT,
// which is the point rather than an exception to it: the work's answer IS the
// block's contents, and a reading that stepped over the whole block to look for
// prose underneath would return nothing at all where there was something to say.
//
// A REPORT WITH NO PROSE AT ALL RETURNS "", which hands the row to the surface's
// emptiness law: a card draws its subtitle or its start stamp rather than an empty
// pair of quotation marks claiming the work said something.
func FirstProse(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if _, _, fence := Fence(trimmed); fence {
			continue
		}
		return trimmed
	}
	return ""
}
