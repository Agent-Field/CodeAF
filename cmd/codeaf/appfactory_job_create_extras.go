package main

import (
	"context"
	"errors"
	"net/http"
	"slices"

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

// createJob sends one create. A KEY GOES ONLY TO A FACTORY THAT SAYS
// keyed_create (api.FeatureKeyedCreate). An older factory keeps every keyed
// answer for a day, a refusal or a 5xx included, so a create it refused once
// could not run again under its key; a keyed create to it is refused as too
// old before any create is sent. The factory is asked on each keyed create, so
// a factory replaced since factory.capabilities answered is caught here.
func (h *appFactoryHub) createJob(ctx context.Context, p appwire.FactoryJobCreateParams) (appFactoryAnswer, error) {
	path := "/v1/jobs"
	if p.OpenIssue {
		path = "/v1/issues"
	}
	if p.Key != "" {
		keeps, err := h.keepsCreateKeys(ctx, p.Factory)
		if err != nil {
			return appFactoryAnswer{}, err
		}
		if !keeps {
			return appFactoryAnswer{}, tooOldRefusal()
		}
	}
	ctx = context.WithValue(ctx, factoryCreateKey{}, p.Key)
	return h.write(ctx, p.Factory, path, factoryCreateBody(p))
}

var _ appwire.FactoryCreateKeys = (*appBackend)(nil)

// FactoryKeepsCreateKeys is factory.capabilities' create_key for one factory:
// whether it says keyed_create now. A factory that cannot be asked is false,
// so an app sends no key and reads the queue after an uncertain create.
func (b *appBackend) FactoryKeepsCreateKeys(ctx context.Context, id string) bool {
	keeps, _ := b.factoryHub().keepsCreateKeys(ctx, id)
	return keeps
}

// keepsCreateKeys is whether the factory's GET /v1/features lists
// keyed_create. A factory older than the route keeps none; any other failure
// to ask is the read's own error. A connection this engine does not hold is
// its own not_found, never a factory's 404, so it is looked up first.
func (h *appFactoryHub) keepsCreateKeys(ctx context.Context, id string) (bool, error) {
	if _, _, err := h.lookup(id); err != nil {
		return false, err
	}
	var out api.Features
	err := h.read(ctx, id, appFactoryFeaturesRoute, appFactorySmall, &out)
	if notFound(err) {
		return false, nil
	}
	return err == nil && slices.Contains(out.Features, api.FeatureKeyedCreate), err
}

// tooOldRefusal is the too-old refusal with its reason, so an app tells it
// from any other conflict.
func tooOldRefusal() error {
	err := factoryTooOld()
	var refusal *appwire.Error
	if errors.As(err, &refusal) {
		refusal.Detail = &appwire.FactoryRefusal{Reason: appwire.FactoryReasonTooOld}
	}
	return err
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
	return tooOldRefusal()
}
