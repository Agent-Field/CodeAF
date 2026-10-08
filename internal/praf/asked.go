package praf

// What in a person's message asks for review's work (Program.Asked).
//
// REVIEW IS A WORD PEOPLE USE FOR MUCH ELSE. As pr, the program was named
// whenever a message said "PR"; as review, the bare word would claim "review
// this function" and "review my essay". So the chat hears it by its command,
// `/review`, and by its work: a message that points at a pull request and asks
// for it to be looked over. Pointing at one alone is not enough — "open a PR
// for this" and "merge PR 12" ask for no review.

import "regexp"

var (
	// pullRequestWords points at a pull request: the abbreviation, the words,
	// a pull request's link, or owner/repo#N.
	pullRequestWords = regexp.MustCompile(`(?i)\bprs?\b|\bpull[ -]?requests?\b|github\.com/[\w.-]+/[\w.-]+/pull/\d+|\b[\w.-]+/[\w.-]+#\d+\b`)
	// reviewWords asks for something to be looked over: review in any form,
	// and the ways a person asks for a pull request to be read.
	reviewWords = regexp.MustCompile(`(?i)\breview(s|ed|ing|er|ers)?\b|\blook(ing)? (over|through|at)\b|\btake a look\b|\bgo(ing)? (over|through)\b|\bfeedback\b|\bcritique\b|\bsanity[- ]check\b`)
)

// asksForReview says a message asks for a code review of a pull request: it
// points at one and asks for it to be looked over.
func asksForReview(message string) bool {
	return pullRequestWords.MatchString(message) && reviewWords.MatchString(message)
}
