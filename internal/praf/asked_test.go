package praf

import "testing"

// A message asks for review's work when it points at a pull request and asks
// for it to be looked over; the word review alone, or a pull request alone,
// does not.
func TestAsksForReview(t *testing.T) {
	for message, want := range map[string]bool{
		"review PR 123":                                true,
		"can you review my pull request":               true,
		"take a look at https://github.com/o/r/pull/7": true,
		"look over o/r#12 before I merge":              true,
		"any feedback on my PRs?":                      true,
		"go through the pull-request and tell me":      true,
		"I'd like a code review of PR #5":              true,
		"review this function":                         false,
		"can you review my essay":                      false,
		"open a PR for this fix":                       false,
		"merge PR 12":                                  false,
		"the preview build is broken":                  false,
		"improve the price list":                       false,
		"look at the logs":                             false,
	} {
		if got := asksForReview(message); got != want {
			t.Errorf("asksForReview(%q) = %v, want %v", message, got, want)
		}
	}
}
