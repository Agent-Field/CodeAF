package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

// ── ONE COMMENT ON ONE ISSUE ────────────────────────────────────────────────
//
// factory.issue_comment posts ONE comment on ONE issue of a repository the
// factory watches, as the person who holds the connection now, over the
// factory's route POST /v1/repos/{owner}/{repo}/issues/{number}/comment. It is
// the desk's "Ask for detail on N": the person writes the words, and this
// sends them.
//
// THE PARAMS ARE EXACTLY {factory, repo, number, body}. An unknown field is
// bad_params. Body is sent exactly as given; it must hold more than white
// space, be valid UTF-8 without NUL, and be at most [IssueCommentBodyMax]
// bytes (GitHub's own limit is 65536 characters).
//
// ONLY THE FACTORY'S OPERATOR MAY POST, with their own GitHub sign-in (a solo
// host may use its own profile token). A pull request number is refused.
//
// A WRITE IS SENT ONCE. It is never retried or replayed. The answer is the
// factory's receipt, a refusal whose Detail is a [FactoryIssueCommentFailure]
// (outcome refused: nothing was posted; outcome unknown: look at the issue on
// GitHub), or [CodeUncertain] with no Detail for anything the engine does not
// recognise. An engine that is not a [FactoryIssueCommenter] answers the method
// as unknown_method and does not announce [FeatureFactoryIssueComment].

// FeatureFactoryIssueComment is announced in hello when the engine serves
// factory.issue_comment.
const FeatureFactoryIssueComment = "factory.issue_comment"

// IssueCommentBodyMax is the route's own bound in bytes of UTF-8; a test in
// the factory's package fails when the two drift.
const IssueCommentBodyMax = 65536

// factoryIssueCommentBudget is the most one call may take: the route's own
// thirty seconds and a margin, because a shorter wait would turn a slow
// success into an uncertain answer.
const factoryIssueCommentBudget = 45 * time.Second

// The failure outcomes the route may answer.
const (
	IssueCommentOutcomeRefused = "refused"
	IssueCommentOutcomeUnknown = "unknown"
)

// IssueCommentStages are the stages the route names a failure at. A stage
// outside this list is not believed.
var IssueCommentStages = []string{"operator", "request", "repository", "credential", "issue", "comment"}

// FactoryIssueCommentParams is the whole request.
type FactoryIssueCommentParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
	Number  int    `json:"number"`
	Body    string `json:"body"`
}

// FactoryIssueComment is the comment GitHub confirmed.
type FactoryIssueComment struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

// FactoryIssueCommentDone is the receipt for a posted comment. It repeats the
// connection, repository and issue that were asked for.
type FactoryIssueCommentDone struct {
	Factory string              `json:"factory"`
	Repo    string              `json:"repo"`
	Number  int                 `json:"number"`
	Comment FactoryIssueComment `json:"comment"`
}

// FactoryIssueCommentFailure is the Detail of an error that carries one of
// the route's own failure outcomes, in the route's fields and none of its prose.
type FactoryIssueCommentFailure struct {
	Factory      string `json:"factory"`
	Repo         string `json:"repo"`
	Number       int    `json:"number"`
	Outcome      string `json:"outcome" enum:"refused,unknown"`
	Stage        string `json:"stage"`
	GitHubStatus int    `json:"github_status,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
}

// FactoryIssueCommenter is the engine's half of factory.issue_comment. It
// makes at most one POST to the factory and never sends it twice.
type FactoryIssueCommenter interface {
	FactoryIssueComment(ctx context.Context, p FactoryIssueCommentParams) (FactoryIssueCommentDone, error)
}

func withFactoryIssueCommentFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryIssueCommenter); ok {
		hello = withFeature(hello, FeatureFactoryIssueComment)
	}
	return hello
}

// CheckIssueComment bounds the request without asking anyone. It answers the
// params with the connection and repository trimmed; the body is forwarded
// exactly.
func CheckIssueComment(p FactoryIssueCommentParams) (FactoryIssueCommentParams, error) {
	var err error
	if p.Factory, p.Repo, err = checkPipelineRepo(p.Factory, p.Repo); err != nil {
		return p, err
	}
	switch {
	case p.Number <= 0:
		return p, Errorf(CodeBadParams, "number must be a positive issue number")
	case strings.TrimSpace(p.Body) == "":
		return p, Errorf(CodeBadParams, "body is required")
	case len(p.Body) > IssueCommentBodyMax:
		return p, Errorf(CodeBadParams, "body is at most %d bytes", IssueCommentBodyMax)
	case !utf8.ValidString(p.Body) || strings.ContainsRune(p.Body, 0):
		return p, Errorf(CodeBadParams, "body must be UTF-8 text")
	}
	return p, nil
}

func init() {
	const name = "factory.issue_comment"
	methods[name] = shaped[FactoryIssueCommentParams, FactoryIssueCommentDone](func(e Engine, raw json.RawMessage) (*FactoryIssueCommentDone, error) {
		c, ok := e.(FactoryIssueCommenter)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		if !json.Valid(raw) {
			return nil, Errorf(CodeBadParams, "%s: the params are not one JSON value", name)
		}
		var p FactoryIssueCommentParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		p, err := CheckIssueComment(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), factoryIssueCommentBudget)
		defer cancel()
		out, err := c.FactoryIssueComment(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
