package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

type factoryCreateKey struct{}

// setFactoryCreateKey is used only by the create's write, never its later read.
func setFactoryCreateKey(req *http.Request) {
	if key, _ := req.Context().Value(factoryCreateKey{}).(string); key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
}

func factoryCreateBody(p appwire.FactoryJobCreateParams) api.NewJob {
	body := api.NewJob{
		Repo: p.Repo, Title: p.Title, Text: p.Text, Source: factory.AskApp,
		Assignee: p.Assignee, Pipeline: p.Pipeline, Issue: p.OpenIssue,
		Group: p.Group, Milestone: p.Milestone, Budget: p.Budget,
		SplitFrom: p.SplitFrom,
	}
	if p.Start == "now" {
		body.Start = "now"
	}
	return body
}

func (h *appFactoryHub) createJob(ctx context.Context, p appwire.FactoryJobCreateParams) (appFactoryAnswer, error) {
	path := "/v1/jobs"
	if p.OpenIssue {
		path = "/v1/issues"
	}
	ctx = context.WithValue(ctx, factoryCreateKey{}, p.Key)
	return h.write(ctx, p.Factory, path, factoryCreateBody(p))
}

func factoryCreateFailure(req *http.Request, reply appFactoryReply) error {
	err := appFactoryActionFailure(reply)
	if _, create := req.Context().Value(factoryCreateKey{}).(string); !create {
		return err
	}
	// An issue create route the Factory lacks: a 405, since every Factory that
	// serves /v1/issues takes POST there, or the mux's own 404. The current
	// Factory's JSON 404, "repository not found", keeps its not_found mapping.
	missing := req.URL.Path == "/v1/issues" &&
		(reply.status == http.StatusMethodNotAllowed || reply.status == http.StatusNotFound && missingRouteBody(reply.body))
	unknown := reply.status == http.StatusUnprocessableEntity && unknownFactoryField(err, "group", "milestone", "budget", "issue", "split_from")
	if !missing && !unknown {
		return err
	}
	err = factoryTooOld()
	var refusal *appwire.Error
	if errors.As(err, &refusal) {
		refusal.Detail = &appwire.FactoryRefusal{Reason: appwire.FactoryReasonTooOld}
	}
	return err
}
