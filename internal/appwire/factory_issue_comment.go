package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── A COMMENT ON A JOB'S GITHUB ISSUE, AND THE MANAGER'S READ OF EACH ───────
//
// factory.issue_comment carries the factory's POST /v1/jobs/{id}/issue_comment:
// the person's words, posted on the job's GitHub issue as that person, with
// the token of their own factory sign-in (on a solo factory, the host's, who
// is the person). Only a job from a GitHub issue takes one. The factory keeps
// the comment on the job at once, so the job's next read carries it.
//
// THE COMMENT IS SENT ONCE. An answer that cannot say whether GitHub took it
// (a dropped connection, a timeout, any 5xx) is [CodeUncertain], and the
// person reads the issue before posting again.
//
// [FeatureFactoryIssueCommentReads] says the job read carries
// `comment_reads`: one line per new comment the job's manager handled, keyed
// by the comment's id, with an outcome the app draws under the comment. The
// factory decides them ([factory.ReadComments]); this wire only carries them.

// FeatureFactoryIssueComment is announced in hello when the engine serves
// factory.issue_comment.
const FeatureFactoryIssueComment = "factory.issue_comment"

// FeatureFactoryIssueCommentReads is announced beside
// [FeatureFactoryIssueComment]: the job read's comments carry id and url, and
// `comment_reads` says what the manager did with each new one. A factory older
// than the field sends neither, and the app shows no line.
const FeatureFactoryIssueCommentReads = "factory.issue_comment_reads"

// FactoryIssueCommentParams is one comment on one job's issue.
type FactoryIssueCommentParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
	// Body is the comment's markdown, 1 to [factory.IssueCommentMost]
	// characters once trimmed.
	Body string `json:"body"`
}

// FactoryIssueCommented is GitHub's receipt for the posted comment, in the
// shape the job read's comments use.
type FactoryIssueCommented struct {
	Factory api.Factory `json:"factory"`
	Job     int         `json:"job"`
	Comment api.Comment `json:"comment"`
}

// FactoryIssueCommenter is the engine's half of factory.issue_comment. An
// engine without it refuses the method as unknown_method and announces
// neither feature.
type FactoryIssueCommenter interface {
	FactoryIssueComment(ctx context.Context, p FactoryIssueCommentParams) (FactoryIssueCommented, error)
}

func withFactoryIssueCommentFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryIssueCommenter); ok {
		hello = withFeature(hello, FeatureFactoryIssueComment)
		hello = withFeature(hello, FeatureFactoryIssueCommentReads)
	}
	return hello
}

func init() {
	methods["factory.issue_comment"] = shaped[FactoryIssueCommentParams, FactoryIssueCommented](postIssueComment)
}

func postIssueComment(e Engine, raw json.RawMessage) (*FactoryIssueCommented, error) {
	s, ok := e.(FactoryIssueCommenter)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.issue_comment")
	}
	var p FactoryIssueCommentParams
	if err := decodeStrict("factory.issue_comment", raw, &p); err != nil {
		return nil, err
	}
	// A comment needs no version: it changes nothing the person read, and a
	// comment on a job that moved on is still the person's words on the issue.
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return nil, err
	}
	if p.Job < 1 {
		return nil, Errorf(CodeBadParams, "job must be a whole number from 1")
	}
	p.Body = strings.TrimSpace(p.Body)
	if p.Body == "" || utf8.RuneCountInString(p.Body) > factory.IssueCommentMost {
		return nil, Errorf(CodeBadParams, "%s", factory.IssueCommentBounds)
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := s.FactoryIssueComment(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
