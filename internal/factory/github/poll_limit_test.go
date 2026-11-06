package github

import (
	"fmt"
	"testing"
	"time"

	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// B5: Spent limits have their own facts words, including wrapped errors;
// ordinary refusals and the existing 401/404 words remain unchanged.
func TestTroubleWordsNameASpentRateLimit(t *testing.T) {
	for _, tc := range []struct {
		status int
		retry  time.Duration
		want   string
	}{
		{403, time.Minute, "rate limited"},
		{429, time.Minute, "rate limited"},
		{403, 0, "not reachable"},
		{429, 0, "not reachable"},
		{500, time.Minute, "not reachable"},
		{401, 0, "token refused"},
		{401, time.Minute, "token refused"},
		{404, 0, "repository not found"},
		{404, time.Minute, "repository not found"},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.retry), func(t *testing.T) {
			err := fmt.Errorf("repository read: %w", &forge.APIError{StatusCode: tc.status, RetryAfter: tc.retry})
			if got := TroubleWords(err); got != tc.want {
				t.Fatalf("trouble words = %q, want %q", got, tc.want)
			}
		})
	}
}
