package publish

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/praf/github"
)

// A GitHub reply carries its URL, and a local test server's port can hold the
// digits of another status. The status GitHub sent decides the code, not
// digits found elsewhere in the message.
func TestTheHTTPStatusDecidesTheCodeNotDigitsInTheURL(t *testing.T) {
	tests := []struct {
		status    int
		url       string
		operation string
		code      string
	}{
		{403, "http://127.0.0.1:35021/repos/acme/api/pulls", "pr", "pr_create_forbidden"},
		{403, "http://127.0.0.1:45033/repos/acme/api/pulls", "pr", "pr_create_forbidden"},
		{403, "http://127.0.0.1:50299/repos/acme/api/pulls", "push", "no_push_permission"},
		{503, "http://127.0.0.1:40300/repos/acme/api/pulls", "pr", "github_unreachable"},
		{502, "http://127.0.0.1:4290/repos/acme/api/pulls", "pr", "github_unreachable"},
		{429, "http://127.0.0.1:50300/repos/acme/api/pulls", "pr", "rate_limited"},
	}
	for _, tt := range tests {
		err := &github.APIError{StatusCode: tt.status, URL: tt.url, Body: `{"message":"Resource not accessible"}`}
		if tt.status != 403 {
			err.Body = `{"message":"try later"}`
		}
		if got := ErrorCode(Wrap(err, tt.operation)); got != tt.code {
			t.Errorf("status %d at %s: code = %q, want %q", tt.status, tt.url, got, tt.code)
		}
	}
}
