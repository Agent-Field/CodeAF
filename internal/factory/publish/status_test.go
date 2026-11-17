package publish

import (
	"errors"
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
		{422, "http://127.0.0.1:40000/repos/acme/api/pulls", `{"message":"A pull request already exists for acme:factory/4290-fix."}`, "pr", "unknown"},
		{403, "http://127.0.0.1:40000/repos/acme/api/pulls", `{"message":"API rate limit exceeded"}`, "pr", "rate_limited"},
	}
	for _, tt := range tests {
		err := &github.APIError{StatusCode: tt.status, URL: tt.url, Body: tt.body}
		if got := ErrorCode(Wrap(err, tt.operation)); got != tt.code {
			t.Errorf("status %d at %s: code = %q, want %q", tt.status, tt.url, got, tt.code)
		}
	}
}

// Git reports an HTTP failure as "The requested URL returned error: NNN"
// beside the remote's URL. The status after that phrase decides the code, not
// digits in the URL, and no other digits read as a status.
func TestGitHTTPStatusDecidesTheCodeNotDigitsInTheRemote(t *testing.T) {
	tests := []struct {
		msg, operation, code string
	}{
		{"fatal: unable to access 'http://127.0.0.1:42931/acme/api.git/': The requested URL returned error: 403", "push", "no_push_permission"},
		{"fatal: unable to access 'http://127.0.0.1:35021/acme/api.git/': The requested URL returned error: 403", "push", "no_push_permission"},
		{"fatal: unable to access 'http://127.0.0.1:40312/acme/api.git/': The requested URL returned error: 500", "push", "unknown"},
		{"fatal: unable to access 'http://127.0.0.1:40312/acme/api.git/': The requested URL returned error: 503", "push", "github_unreachable"},
		{"fatal: unable to access 'http://127.0.0.1:40312/acme/api.git/': The requested URL returned error: 429", "push", "rate_limited"},
		{`Post "http://127.0.0.1:40312/repos/acme/api/pulls": EOF`, "pr", "unknown"},
		{"push of factory/5021-fix failed: unexpected reply", "push", "unknown"},
	}
	for _, tt := range tests {
		if got := ErrorCode(Wrap(errors.New(tt.msg), tt.operation)); got != tt.code {
			t.Errorf("%s: code = %q, want %q", tt.msg, got, tt.code)
		}
	}
}
