package agentsession

// What every agent session of one program runs under, all of it the program's.
//
// THIS PACKAGE IS MACHINERY, AND EVERY NUMBER IN IT IS A PROGRAM'S. It began as
// sec's, and its figures were sec-af's: fifty turns, thirty minutes, eight
// sessions at once, two follow-ups, four hundred lines to a read. A second
// program running on it would have inherited every one of them without saying
// so, and a figure tuned for one program's agents would have quietly become the
// other's. So nothing here has a default: a program states each figure, a zero
// is refused where the App is opened ([New]) and where a session is run
// ([RunSession]), and a program that forgets one finds out at once rather
// than running on a figure it never chose. Two programs that choose the same
// figure today each say so in their own package.

import (
	"fmt"
	"strings"
)

// Policy is how every agent session of a program reads and answers.
type Policy struct {
	// FollowUps is how many times an answer that does not meet its schema is
	// asked for again.
	FollowUps int
	// ContextChars is how much a session's transcript may hold before it is
	// told to stop reading and answer.
	ContextChars int
	// AnswerNow is what a session is told when its turns or its room run out:
	// to stop reading and answer with what it has.
	AnswerNow string
	// Tools is what one call of each read-only tool may answer.
	Tools ToolLimits
}

// ToolLimits is the most one call of each tool answers.
type ToolLimits struct {
	// ReadLines and ReadBytes are the most lines and bytes one read_file
	// answers; ReadLineRunes the most of one line it shows.
	ReadLines, ReadBytes, ReadLineRunes int
	// ListEntries is the most entries one list_dir answers, and GlobMatches
	// the most paths one glob answers.
	ListEntries, GlobMatches int
	// GrepMatches is the most lines one grep answers, GrepFileBytes the
	// largest file it reads, and GrepLineRunes how much of a matching line it
	// quotes.
	GrepMatches, GrepFileBytes, GrepLineRunes int
}

// missing names the first figure a program left unset, or "" when it set
// every one.
func (p Policy) missing() string {
	switch {
	case p.FollowUps <= 0:
		return "Policy.FollowUps"
	case p.ContextChars <= 0:
		return "Policy.ContextChars"
	case strings.TrimSpace(p.AnswerNow) == "":
		return "Policy.AnswerNow"
	}
	return p.Tools.missing()
}

func (l ToolLimits) missing() string {
	for _, field := range []struct {
		name  string
		value int
	}{
		{"ReadLines", l.ReadLines}, {"ReadBytes", l.ReadBytes}, {"ReadLineRunes", l.ReadLineRunes},
		{"ListEntries", l.ListEntries}, {"GlobMatches", l.GlobMatches},
		{"GrepMatches", l.GrepMatches}, {"GrepFileBytes", l.GrepFileBytes}, {"GrepLineRunes", l.GrepLineRunes},
	} {
		if field.value <= 0 {
			return "Policy.Tools." + field.name
		}
	}
	return ""
}

// missing names the first figure a program left unset in its Config, or "".
func (c Config) missing() string {
	switch {
	case c.Sessions <= 0:
		return "Sessions"
	case c.Calls <= 0:
		return "Calls"
	case c.MaxTurns <= 0:
		return "MaxTurns"
	case c.SessionWall <= 0:
		return "SessionWall"
	}
	return c.Policy.missing()
}

// unsetError is the refusal of a figure a program did not state.
func unsetError(where, field string) error {
	return fmt.Errorf("agentsession: %s.%s is not set; every program states its own, and this package has no default", where, field)
}
