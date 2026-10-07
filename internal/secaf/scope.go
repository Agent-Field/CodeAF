package secaf

// What a brief asks the audit to look at.
//
// A run started from the chat is handed nothing but its brief — the flags of a
// shell line never reach it — so the brief carries the scope in a few plain
// words, the ones the guide teaches the model (internal/programguide):
// `whole repository`, or `changes` with an optional `since <ref>`, and
// optionally `quick` or `thorough`. Anything else in the brief is the person's
// own words about the audit, kept on the report.
//
// A BRIEF THAT SAYS NOTHING ABOUT SCOPE IS THE WHOLE REPOSITORY. That is what
// sec-af always audited, and an audit that guessed at a narrower scope would
// report less than was asked without saying so.

import (
	"regexp"
	"strings"
)

// Scope is the audit a brief asks for.
type Scope struct {
	// Changes is an audit of what changed since Base rather than of the whole
	// repository.
	Changes bool
	// Base is the revision the changes are measured from; empty is the
	// branch's own base, found when the audit starts ([findBase]).
	Base string
	// Depth is `quick`, `standard` or `thorough`.
	Depth string
	// Words is the brief as the person or the conversation wrote it.
	Words string
}

var (
	// changesWord opens a brief that asks for the changes: `changes`, `the
	// changes`, `diff`, `changes on this branch`, `changes since main`.
	changesWord = regexp.MustCompile(`(?i)^\s*(?:only\s+)?(?:the\s+)?(?:changes|changed\s+files|diff)\b`)
	// sinceRef is the base a brief names: `since <ref>`, `against <ref>`,
	// `from <ref>`, `vs <ref>`.
	sinceRef = regexp.MustCompile(`(?i)\b(?:since|against|from|vs\.?|versus|relative\s+to)\s+([^\s,;]+)`)
	// depthWord is a depth the brief names anywhere.
	depthWord = regexp.MustCompile(`(?i)\b(quick|thorough)\b`)
)

// ReadScope reads a brief, with the shell's own flags over it: a flag the
// person typed wins over a word in the brief.
func ReadScope(brief string, changesFlag bool, baseFlag, depthFlag string) Scope {
	scope := Scope{Depth: "standard", Words: strings.TrimSpace(brief)}
	first := firstLine(scope.Words)
	if changesWord.MatchString(first) {
		scope.Changes = true
		if match := sinceRef.FindStringSubmatch(first); match != nil {
			scope.Base = strings.Trim(match[1], "`'\".")
		}
	}
	if match := depthWord.FindStringSubmatch(first); match != nil {
		scope.Depth = strings.ToLower(match[1])
	}
	if changesFlag || strings.TrimSpace(baseFlag) != "" {
		scope.Changes = true
	}
	if base := strings.TrimSpace(baseFlag); base != "" {
		scope.Base = base
	}
	if depth := strings.ToLower(strings.TrimSpace(depthFlag)); depth != "" {
		scope.Depth = depth
	}
	return scope
}

// Describe is the scope in a few words, for the first line of the report.
func (s Scope) Describe() string {
	what := "the whole repository"
	if s.Changes {
		what = "the changes on this branch"
		if s.Base != "" {
			what = "the changes since " + s.Base
		}
	}
	if s.Depth != "" && s.Depth != "standard" {
		what += ", " + s.Depth
	}
	return what
}

func firstLine(text string) string {
	if at := strings.IndexByte(text, '\n'); at >= 0 {
		return text[:at]
	}
	return text
}
