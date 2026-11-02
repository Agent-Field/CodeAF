package publish

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/praf/github"
)

// A GitHub reply carries its URL, and a local server's port can hold the
// digits of another status. The reply decides the code, not digits found in
// its URL.
func TestTheReplyURLNeverDecidesTheCode(t *testing.T) {
	const denied, later = `{"message":"Resource not accessible"}`, `{"message":"try later"}`
	tests := []struct {
		status          int
		url, body       string
		operation, code string
	}{
		{403, "http://127.0.0.1:35021/repos/acme/api/pulls", denied, "pr", "pr_create_forbidden"},
		{403, "http://127.0.0.1:45033/repos/acme/api/pulls", denied, "pr", "pr_create_forbidden"},
		{403, "http://127.0.0.1:50299/repos/acme/api/pulls", denied, "push", "no_push_permission"},
		{503, "http://127.0.0.1:40300/repos/acme/api/pulls", later, "pr", "github_unreachable"},
		{502, "http://127.0.0.1:4290/repos/acme/api/pulls", later, "pr", "github_unreachable"},
		{429, "http://127.0.0.1:50300/repos/acme/api/pulls", later, "pr", "rate_limited"},
	}
	for _, tt := range tests {
		err := &github.APIError{StatusCode: tt.status, URL: tt.url, Body: tt.body}
		if got := ErrorCode(Wrap(err, tt.operation)); got != tt.code {
			t.Errorf("status %d at %s: code = %q, want %q", tt.status, tt.url, got, tt.code)
		}
	}
}
