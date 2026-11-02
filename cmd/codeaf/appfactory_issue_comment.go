package main

// ── codeaf app: ONE COMMENT ON ONE ISSUE ────────────────────────────────────
//
// The engine's half of appwire.FactoryIssueCommenter. It makes ONE POST to ONE
// route of the factory's /v1 API, /v1/repos/{owner}/{repo}/issues/{n}/comment,
// spelled here; the owner and name are validated by appwire and escaped here.
// The factory decides who may (only its operator) and posts with that
// person's own GitHub access; nothing here supplies a GitHub token.
//
// A WRITE IS SENT ONCE, with no retry and no second route. The answer is
// believed only in its documented shapes: a 201 receipt for THIS issue with a
// positive id, the canonical comment URL and a time, or the route's typed
// failure (outcome refused, or unknown at 502 for the comment stage, a known
// stage and a request id). Anything else, and every answer that arrives for a
// caller who is no longer the connection's, is appwire.CodeUncertain. The
// factory's prose is never passed on.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryIssueCommenter = (*appBackend)(nil)

// appFactoryIssueCommentTimeout covers the route's own thirty seconds.
const appFactoryIssueCommentTimeout = 40 * time.Second

func (b *appBackend) FactoryIssueComment(ctx context.Context, p appwire.FactoryIssueCommentParams) (appwire.FactoryIssueCommentDone, error) {
	raw, err := json.Marshal(api.IssueCommentRequest{Body: p.Body})
	if err != nil {
		return appwire.FactoryIssueCommentDone{}, appwire.Errorf(appwire.CodeBadParams, "that comment cannot be sent")
	}
	h := b.factoryHub()
	who, err := b.heldWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryIssueCommentDone{}, err
	}
	owner, name, _ := strings.Cut(p.Repo, "/")
	path := appFactoryRepoRoute + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/issues/" + strconv.Itoa(p.Number) + "/comment"
	client := *h.client
	client.Timeout = appFactoryIssueCommentTimeout
	r, err := h.linkDoWith(ctx, &client, who, http.MethodPost, path, json.RawMessage(raw), appFactorySmall)
	switch {
	case err == errLinkNotSent:
		return appwire.FactoryIssueCommentDone{}, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; nothing was sent")
	case err != nil || !h.callerIs(p.Factory, who.token):
		return appwire.FactoryIssueCommentDone{}, issueCommentUncertain()
	}
	done, failure := classifyIssueComment(r, p)
	if done == nil && failure == nil {
		return appwire.FactoryIssueCommentDone{}, issueCommentUncertain()
	}
	h.touch(p.Factory)
	if failure != nil {
		return appwire.FactoryIssueCommentDone{}, issueCommentFailureError(r.status, *failure)
	}
	return *done, nil
}

func issueCommentUncertain() error {
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm whether the comment was posted; look at the issue on GitHub before posting again")
}

// classifyIssueComment believes a reply only in its documented shapes. It
// answers neither value for anything else, which the caller reports as uncertain.
func classifyIssueComment(r linkReply, p appwire.FactoryIssueCommentParams) (*appwire.FactoryIssueCommentDone, *appwire.FactoryIssueCommentFailure) {
	switch {
	case r.bodyBad:
		return nil, nil
	case r.status == http.StatusCreated:
		return classifyIssueCommentDone(r, p), nil
	case r.status >= 400 && r.status < 600:
		return nil, classifyIssueCommentFailure(r, p)
	}
	return nil, nil
}

func classifyIssueCommentDone(r linkReply, p appwire.FactoryIssueCommentParams) *appwire.FactoryIssueCommentDone {
	var got api.IssueCommentResponse
	if json.Unmarshal(r.body, &got) != nil {
		return nil
	}
	c := got.Comment
	canonical := "https://github.com/" + p.Repo + "/issues/" + strconv.Itoa(p.Number) + "#issuecomment-" + strconv.FormatInt(c.ID, 10)
	if got.Repo != p.Repo || got.Number != p.Number || c.ID <= 0 || c.URL != canonical || c.CreatedAt.IsZero() {
		return nil
	}
	return &appwire.FactoryIssueCommentDone{Factory: p.Factory, Repo: p.Repo, Number: p.Number,
		Comment: appwire.FactoryIssueComment{ID: c.ID, URL: canonical, CreatedAt: c.CreatedAt.UTC()}}
}

func classifyIssueCommentFailure(r linkReply, p appwire.FactoryIssueCommentParams) *appwire.FactoryIssueCommentFailure {
	var got api.IssueCommentFailure
	if json.Unmarshal(r.body, &got) != nil || !pipelinePRRequestID.MatchString(got.RequestID) || !slices.Contains(appwire.IssueCommentStages, got.Stage) ||
		(got.GitHubStatus != 0 && (got.GitHubStatus < 100 || got.GitHubStatus > 599)) {
		return nil
	}
	switch got.Outcome {
	case api.IssueCommentRefused:
		// Nothing was posted. Only the issue read may fail at 502 (GitHub did
		// not answer before the post); any other 5xx refusal contradicts itself.
		if r.status >= 500 && (r.status != http.StatusBadGateway || got.Stage != "issue") {
			return nil
		}
	case api.IssueCommentUnknown:
		if r.status != http.StatusBadGateway || got.Stage != "comment" {
			return nil
		}
	default:
		return nil
	}
	return &appwire.FactoryIssueCommentFailure{Factory: p.Factory, Repo: p.Repo, Number: p.Number, Outcome: got.Outcome,
		Stage: got.Stage, GitHubStatus: got.GitHubStatus, RequestID: got.RequestID}
}

// issueCommentFailureError is the wire error for a typed failure, in words
// fixed here from the outcome and the factory's status.
func issueCommentFailureError(status int, f appwire.FactoryIssueCommentFailure) error {
	code, words := appwire.CodeUncertain, "The factory cannot say whether the comment was posted; look at the issue on GitHub before posting again."
	if f.Outcome == api.IssueCommentRefused {
		code, words = issueCommentRefusedCode(status), issueCommentRefusedWords(f.Stage)+" Nothing was posted."
	}
	err := appwire.Errorf(code, "%s", words)
	err.Detail = f
	return err
}

func issueCommentRefusedCode(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return appwire.CodeGitHubExpired
	case http.StatusForbidden:
		return appwire.CodeForbidden
	case http.StatusNotFound:
		return appwire.CodeNotFound
	case http.StatusConflict:
		return appwire.CodeBusy
	case http.StatusUnprocessableEntity:
		return appwire.CodeBadParams
	}
	return appwire.CodeEngine
}

func issueCommentRefusedWords(stage string) string {
	switch stage {
	case "operator":
		return "Only the factory's operator can post a comment."
	case "repository":
		return "This factory does not watch that repository."
	case "credential":
		return "GitHub access for this sign-in is missing or expired; reconnect GitHub."
	case "issue":
		return "That issue could not be used: it is missing, hidden, a pull request, or GitHub did not answer."
	case "comment":
		return "GitHub refused the comment."
	}
	return "The factory would not take that comment as it is."
}
