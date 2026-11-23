// Package publish owns the failure vocabulary shared by machines and clients.
package publish

import (
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/praf/github"
)

// ErrCommitChanged refuses consent after the approved object moved.
var ErrCommitChanged = errors.New("job changed since approval; approve again")

// The codes publishing itself decides on, not only reads from the forge.
const (
	CodePushRejected     = "push_rejected"
	CodeBranchNamesTaken = "branch_names_taken"
)

// Code is a stable wire identifier and the next action a person can take.
type Code struct {
	Code string `json:"code"`
	Next string `json:"next"`
}

var codes = []Code{
	{"branch_protected", "Ask a repo admin to allow codeaf, or open the PR yourself from the branch."},
	{"no_push_permission", "Ask for write access to this repo, then Retry publishing."},
	{CodePushRejected, "This pull request's branch moved on GitHub, and nothing was overwritten. Follow PR reads what landed there; approve again after that."},
	{CodeBranchNamesTaken, "This job's branch name and its spare names on GitHub hold other commits, and nothing was overwritten. Delete a branch nobody needs, then Retry publishing."},
	{"pr_create_forbidden", "Open the PR yourself from the branch; this account cannot open PRs here."},
	{"repo_archived", "The repo is archived. Cancel job."},
	{"github_unreachable", "Check the connection to GitHub, then Retry publishing."},
	{"rate_limited", "Wait for GitHub's rate limit to reset, then Retry publishing."},
	{"machine_offline", "The machine holding the work is away. Publishing waits until it is back."},
	{"worktree_missing", "The job's work is no longer on its machine. Run the job again."},
	{"unknown", "Read GitHub's message, then Retry publishing."},
}

// Codes returns a copy so a caller cannot change the shared vocabulary.
func Codes() []Code { return append([]Code(nil), codes...) }

func Known(code string) bool {
	for _, row := range codes {
		if row.Code == code {
			return true
		}
	}
	return false
}

// Reason is the failure in a person's words: what to do about a code that names
// its remedy, else the first line of what the forge said.
func Reason(code, detail string) string {
	if code != "unknown" {
		for _, row := range codes {
			if row.Code == code {
				return row.Next
			}
		}
	}
	line, _, _ := strings.Cut(strings.TrimSpace(detail), "\n")
	return strings.TrimSpace(line)
}

// Failure classifies the forge's message at the operation that failed.
// Permission to push and permission to open a PR are different remedies.
func Failure(err error, operation string) string {
	status, msg := reply(err)
	switch {
	case errors.Is(err, fs.ErrNotExist), operation == "worktree":
		return "worktree_missing"
	case strings.Contains(msg, "archived"):
		return "repo_archived"
	case strings.Contains(msg, "protected branch"), strings.Contains(msg, "gh006"), strings.Contains(msg, "gh013"), strings.Contains(msg, "repository rule"):
		return "branch_protected"
	case strings.Contains(msg, "non-fast-forward"), strings.Contains(msg, "fetch first"):
		return CodePushRejected
	case strings.Contains(msg, "rate limit"), status == http.StatusTooManyRequests:
		return "rate_limited"
	case strings.Contains(msg, "could not resolve"), strings.Contains(msg, "connection refused"), strings.Contains(msg, "timeout"), strings.Contains(msg, "network is unreachable"), status == http.StatusBadGateway, status == http.StatusServiceUnavailable:
		return "github_unreachable"
	case errors.Is(err, fs.ErrPermission), strings.Contains(msg, "permission"), status == http.StatusForbidden, strings.Contains(msg, "forbidden"), strings.Contains(msg, "cannot push"), strings.Contains(msg, "authentication failed"), strings.Contains(msg, "write access"), strings.Contains(msg, "resource not accessible"):
		if operation == "pr" {
			return "pr_create_forbidden"
		}
		return "no_push_permission"
	default:
		return "unknown"
	}
}

// gitHTTPStatus is the status git prints when a push over HTTP is refused.
var gitHTTPStatus = regexp.MustCompile(`returned error: (\d{3})\b`)

// reply is the HTTP status of a failure and the words to classify it by. A
// GitHub API error gives its status and its body; git gives the status after
// "returned error:". The status is never read from other digits in the
// message, because a URL, a port or a branch name can hold any number.
func reply(err error) (int, string) {
	var api *github.APIError
	if errors.As(err, &api) {
		return api.StatusCode, strings.ToLower(api.Body)
	}
	msg := strings.ToLower(err.Error())
	if m := gitHTTPStatus.FindStringSubmatch(msg); m != nil {
		status, _ := strconv.Atoi(m[1])
		return status, msg
	}
	return 0, msg
}

// Error preserves the message while carrying its classified wire code.
type Error struct {
	Code   string
	Detail string
	Err    error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }
func Wrap(err error, operation string) error {
	if err == nil {
		return nil
	}
	var coded *Error
	if errors.As(err, &coded) {
		return err
	}
	return &Error{Code: Failure(err, operation), Detail: err.Error(), Err: err}
}
func ErrorCode(err error) string {
	var coded *Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	return Failure(err, "")
}

// ValidCommit accepts the complete object name, never an ambiguous short SHA.
func ValidCommit(commit string) bool {
	if len(commit) != 40 && len(commit) != 64 {
		return false
	}
	_, err := hex.DecodeString(commit)
	return err == nil && commit == strings.ToLower(commit)
}

// ErrorDetail keeps the forge's full message separate from the log's first line.
func ErrorDetail(err error) string {
	var coded *Error
	if errors.As(err, &coded) && coded.Detail != "" {
		return coded.Detail
	}
	return err.Error()
}
