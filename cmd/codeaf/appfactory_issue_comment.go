package main

// ── codeaf app: A COMMENT ON A JOB'S GITHUB ISSUE ───────────────────────────
//
// The engine's half of appwire.FactoryIssueCommenter: one POST to the
// factory's fixed /v1/jobs/{id}/issue_comment, sent once. Who the comment is
// posted as is the factory's to decide from the bearer on this route; the body
// carries only the words.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryIssueCommenter = (*appBackend)(nil)

func (b *appBackend) FactoryIssueComment(ctx context.Context, p appwire.FactoryIssueCommentParams) (appwire.FactoryIssueCommented, error) {
	data, err := b.factoryHub().post(ctx, p.Factory, jobPath(p.Job, "issue_comment"), api.IssueCommentRequest{Body: p.Body})
	if err != nil {
		if routeMissing(err) {
			return appwire.FactoryIssueCommented{}, factoryTooOld()
		}
		return appwire.FactoryIssueCommented{}, err
	}
	var got api.IssueCommentPosted
	// A yes without the comment's id cannot say what GitHub kept, so it is
	// never answered as a posted comment; nothing is sent again.
	if json.Unmarshal(data, &got) != nil || got.Comment.ID == "" {
		return appwire.FactoryIssueCommented{}, appwire.Errorf(appwire.CodeUncertain, "the factory answered without the comment's receipt; it may have posted, so read the issue before trying again")
	}
	return appwire.FactoryIssueCommented{Factory: got.Factory, Job: p.Job, Comment: got.Comment}, nil
}

// routeMissing says whether a factory answered a route it does not have,
// which an older factory does with [api.EndpointNotFound]; a job it cannot
// find is a different sentence.
func routeMissing(err error) bool {
	var e *appwire.Error
	return errors.As(err, &e) && e.Code == appwire.CodeNotFound && e.Message == api.EndpointNotFound
}
