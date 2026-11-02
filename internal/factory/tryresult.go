package factory

import (
	"fmt"
	"strconv"
	"strings"
)

// TryBlocked is the word for a try a mechanical interruption ended (the
// report's ExecutionBlocked): it neither met nor missed its rule.
const TryBlocked = "blocked"

// TryResult is how one finished try of a step ended, as a person reads it on
// the job page: a word and a short reason. THE WORD IS THE STEP'S OWN RULE
// ([Stage.Until]: done, clean, green or proven), met or "not" met, as [Met]
// judges it; the reason is counted from the report's structured fields
// (findings, exit, tests, claims) and never read from the step's prose, so a
// step that writes "all good" with two open findings still reads "not clean ·
// 2 findings". A checkpoint is not a finished try and has neither.
func TryResult(s Stage, r StageResult) (word, reason string) {
	if r.Checkpoint {
		return "", ""
	}
	if r.ExecutionBlocked != "" {
		return TryBlocked, ""
	}
	until := strings.TrimSpace(s.Until)
	if until == "" {
		until = UntilDone
	}
	if Met(s, r) {
		return until, metReason(until, r)
	}
	if r.Tests != nil && r.Tests.Failed > 0 {
		return "not " + until, strconv.Itoa(r.Tests.Failed) + " failed"
	}
	return "not " + until, Shortfall(r)
}

// metReason is the count that shows a met rule, "" when the rule has none.
func metReason(until string, r StageResult) string {
	switch until {
	case UntilClean:
		return "no findings"
	case UntilGreen:
		if r.Tests != nil && r.Tests.Passed > 0 {
			return strconv.Itoa(r.Tests.Passed) + " passed"
		}
	case UntilProven:
		return claimsShown(r.Claims)
	}
	return ""
}

// Shortfall is why a report did not meet its rule, in a few counted words:
// no result, the findings, a failed exit, or how many claims were shown.
func Shortfall(r StageResult) string {
	switch {
	case !r.Done:
		return "no result"
	case r.Findings == 1:
		return "1 finding"
	case r.Findings > 1:
		return strconv.Itoa(r.Findings) + " findings"
	case r.Exit != 0:
		return "exit " + strconv.Itoa(r.Exit)
	case len(r.Claims) == 0:
		return "no claims"
	}
	return claimsShown(r.Claims)
}

// claimsShown counts the claims marked ok with evidence against all of them.
func claimsShown(claims []Claim) string {
	shown := 0
	for _, cl := range claims {
		if cl.OK && strings.TrimSpace(cl.Evidence) != "" {
			shown++
		}
	}
	return fmt.Sprintf("%d of %d claims shown", shown, len(claims))
}
