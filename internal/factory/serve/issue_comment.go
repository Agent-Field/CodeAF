package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── ONE COMMENT ON ONE ISSUE ────────────────────────────────────────────────
//
// POST /v1/repos/{owner}/{repo}/issues/{number}/comment posts ONE comment on
// ONE issue of a repository this factory watches, AS THE CALLER, over GitHub's
// REST API. It is how the app asks an issue's author for detail.
//
// ONLY THE OPERATOR MAY ASK, because the comment speaks for the factory in a
// public thread. The credential is the operator's own, exactly as a pipeline
// pull request's ([Handler.callerGitHubToken]): a team never falls back to the
// host, and a solo host may use its own profile token.
//
// The sequence is one read, then one write:
//
//	GET  /repos/{o}/{r}/issues/{n}            it is an issue, not a pull request
//	POST /repos/{o}/{r}/issues/{n}/comments   the comment, sent once
//
// THE POST IS SENT ONCE and never retried. A definitive 4xx says it was not
// made (outcome refused). A dropped connection, a redirect, a 5xx, or a 2xx
// that is not a readable 201 for this issue leaves it UNKNOWN: the person
// looks at the issue on GitHub before asking again. A second call for the same
// issue while one is in flight on this host is refused, so a double click
// posts once.
const issueCommentTimeout = 30 * time.Second

// issueCommentFlights holds the issues with a comment in flight on this host.
var issueCommentFlights sync.Map

type issueCommentFlow struct {
	h      *Handler
	w      http.ResponseWriter
	r      *http.Request
	token  string
	viaSes bool
	owner  string
	repo   string
	number int
}

func (h *Handler) issueComment(w http.ResponseWriter, r *http.Request) {
	owner, repo, ok := h.splitRepo(w, r)
	if !ok {
		return
	}
	f := &issueCommentFlow{h: h, w: w, r: r, owner: owner, repo: repo}
	if !h.isOperator(r) {
		f.refuse(http.StatusForbidden, "operator", "Only the factory's operator can post a comment on an issue; nothing was posted")
		return
	}
	raw := r.PathValue("number")
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || strconv.Itoa(n) != raw {
		f.refuse(http.StatusUnprocessableEntity, "request", "the issue number must be a positive whole number")
		return
	}
	f.number = n
	var body api.IssueCommentRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	if words := issueCommentRefusal(body.Body); words != "" {
		f.refuse(http.StatusUnprocessableEntity, "request", words)
		return
	}
	sw, err := h.repoSwitch()
	if h.failed(w, err) {
		return
	}
	if !sw.Watched(f.full()) {
		f.refuse(http.StatusNotFound, "repository", f.full()+" is not a repository this factory watches; nothing was posted")
		return
	}
	if f.token, f.viaSes = h.callerGitHubToken(r); f.token == "" {
		f.refuse(http.StatusUnauthorized, "credential", "Reconnect GitHub to post a comment")
		return
	}
	key := strings.ToLower(f.full()) + "#" + raw
	if _, busy := issueCommentFlights.LoadOrStore(key, struct{}{}); busy {
		f.refuse(http.StatusConflict, "request", "a comment on this issue is already being posted")
		return
	}
	defer issueCommentFlights.Delete(key)
	ctx, cancel := context.WithTimeout(r.Context(), issueCommentTimeout)
	defer cancel()
	f.run(ctx, body.Body)
}

// issueCommentRefusal is the words that refuse a comment body, or "".
func issueCommentRefusal(body string) string {
	switch {
	case strings.TrimSpace(body) == "":
		return "the comment is empty"
	case len(body) > api.IssueCommentBodyMax:
		return "the comment is longer than " + strconv.Itoa(api.IssueCommentBodyMax) + " bytes"
	case !utf8.ValidString(body) || strings.ContainsRune(body, 0):
		return "the comment must be UTF-8 text"
	}
	return ""
}

func (f *issueCommentFlow) full() string { return f.owner + "/" + f.repo }

func (f *issueCommentFlow) run(ctx context.Context, body string) {
	at := "/repos/" + url.PathEscape(f.owner) + "/" + url.PathEscape(f.repo) + "/issues/" + strconv.Itoa(f.number)
	var issue struct {
		Number      int             `json:"number"`
		PullRequest json.RawMessage `json:"pull_request"`
	}
	status, err := f.h.githubREST(ctx, f.token, http.MethodGet, at, nil, &issue)
	if err != nil || status != http.StatusOK {
		f.issueFailed(status, err)
		return
	}
	if issue.Number != f.number {
		f.fail(http.StatusBadGateway, api.IssueCommentRefused, "issue", 0, "GitHub did not return that issue; nothing was posted")
		return
	}
	if len(issue.PullRequest) > 0 && string(issue.PullRequest) != "null" {
		f.refuse(http.StatusUnprocessableEntity, "issue", "#"+strconv.Itoa(f.number)+" is a pull request, not an issue; nothing was posted")
		return
	}

	var made struct {
		ID        int64  `json:"id"`
		URL       string `json:"html_url"`
		CreatedAt string `json:"created_at"`
	}
	status, err = f.h.githubREST(ctx, f.token, http.MethodPost, at+"/comments", map[string]string{"body": body}, &made)
	if err == nil && status >= 400 && status < 500 {
		f.fail(writeRefusedStatus(status), api.IssueCommentRefused, "comment", status, "GitHub refused the comment (HTTP "+strconv.Itoa(status)+"); nothing was posted")
		return
	}
	canonical := "https://github.com/" + f.full() + "/issues/" + strconv.Itoa(f.number) + "#issuecomment-" + strconv.FormatInt(made.ID, 10)
	created, timeErr := time.Parse(time.RFC3339, made.CreatedAt)
	if err != nil || status != http.StatusCreated || made.ID <= 0 || !strings.EqualFold(made.URL, canonical) || timeErr != nil {
		f.fail(http.StatusBadGateway, api.IssueCommentUnknown, "comment", status, "GitHub's answer to the comment is unknown; look at the issue on GitHub before posting again")
		return
	}
	f.h.write(f.w, http.StatusCreated, api.IssueCommentResponse{Factory: f.h.factory, Repo: f.full(), Number: f.number,
		Comment: api.IssueCommentResult{ID: made.ID, URL: canonical, CreatedAt: created.UTC()}})
}

// issueFailed ends the call at the issue read. Nothing has been posted.
func (f *issueCommentFlow) issueFailed(status int, err error) {
	switch {
	case err != nil:
		f.fail(http.StatusBadGateway, api.IssueCommentRefused, "issue", status, "GitHub isn't answering; nothing was posted, so you can retry")
	case status == http.StatusNotFound || status == http.StatusGone:
		f.fail(http.StatusNotFound, api.IssueCommentRefused, "issue", status, "Issue #"+strconv.Itoa(f.number)+" was not found on "+f.full()+"; nothing was posted")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		f.fail(status, api.IssueCommentRefused, "issue", status, "GitHub refused to read the issue (HTTP "+strconv.Itoa(status)+"); nothing was posted")
	default:
		f.fail(http.StatusBadGateway, api.IssueCommentRefused, "issue", status, "GitHub refused to read the issue (HTTP "+strconv.Itoa(status)+"); nothing was posted")
	}
}

// writeRefusedStatus is this API's status for a definitive 4xx to a write:
// 401 is the caller's sign-in, and 403 and 404 are one answer, "not allowed".
func writeRefusedStatus(status int) int {
	if status == http.StatusNotFound {
		return http.StatusForbidden
	}
	return status
}

func (f *issueCommentFlow) refuse(status int, stage, words string) {
	f.fail(status, api.IssueCommentRefused, stage, 0, words)
}

func (f *issueCommentFlow) fail(status int, outcome, stage string, github int, words string) {
	if status == http.StatusUnauthorized && f.viaSes {
		f.h.endGitHub(f.r, f.token)
	}
	f.h.write(f.w, status, api.IssueCommentFailure{Factory: f.h.factory, Error: words, Reason: reasonFor(status), Outcome: outcome,
		Stage: stage, GitHubStatus: github, RequestID: requestID()})
}
