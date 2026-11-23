package main

// ── codeaf app: GO-AHEADS THE FACTORY FOLLOWS ──────────────────────────────
//
// The engine's half of appwire.FactoryGoAheads. Each method is one request to
// the factory's own doors (internal/factory/serve/goahead.go): GET
// /v1/features, GET and PUT /v1/policy, POST /v1/jobs/{id}/waits_on. The
// engine keeps nothing of its own: the policy lives in the factory's store,
// and the factory's go-ahead pass follows it whether or not this app is open.
//
// A WRITE IS SENT ONCE, never retried; a failure the factory did not answer is
// the hub's uncertain refusal, and a read says what was kept.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryGoAheads = (*appBackend)(nil)

const (
	appFactoryFeaturesRoute = "/v1/features"
	appFactoryPolicyRoute   = "/v1/policy"
)

// FactoryFeatures is the factory's own list. A FACTORY OLDER THAN THE ROUTE
// ANSWERS NONE, not an error, so a caller reads it as "keeps no policies".
func (b *appBackend) FactoryFeatures(ctx context.Context, id string) (appwire.FactoryFeatures, error) {
	var out api.Features
	err := b.factoryHub().read(ctx, id, appFactoryFeaturesRoute, appFactorySmall, &out)
	if notFound(err) {
		return appwire.FactoryFeatures{Features: []string{}}, nil
	}
	if err != nil {
		return appwire.FactoryFeatures{}, err
	}
	if out.Features == nil {
		out.Features = []string{}
	}
	return appwire.FactoryFeatures{Features: out.Features}, nil
}

func (b *appBackend) FactoryPolicyGet(ctx context.Context, p appwire.FactoryPolicyGetParams) (api.Policy, error) {
	var out api.Policy
	err := b.factoryHub().read(ctx, p.Factory, appFactoryPolicyRoute+"?group="+url.QueryEscape(p.Group), appFactorySmall, &out)
	if notFound(err) {
		return api.Policy{}, factoryTooOld()
	}
	return out, err
}

func (b *appBackend) FactoryPolicySet(ctx context.Context, p appwire.FactoryPolicySetParams) (api.Policy, error) {
	h := b.factoryHub()
	data, err := h.sendWrite(ctx, h.client, http.MethodPut, p.Factory, appFactoryPolicyRoute, api.PolicyPut{Group: p.Group, Stopped: p.Stopped, Allowances: p.Allowances})
	if notFound(err) {
		return api.Policy{}, factoryTooOld()
	}
	if err != nil {
		return api.Policy{}, err
	}
	var out api.Policy
	if json.Unmarshal(data, &out) != nil || out.Group != p.Group {
		return api.Policy{}, appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the go-aheads; read them before trying again")
	}
	return out, nil
}

func (b *appBackend) FactoryWaitsSet(ctx context.Context, p appwire.FactoryWaitsSetParams) (appwire.FactoryWaitsSetResult, error) {
	h := b.factoryHub()
	data, err := h.sendWrite(ctx, h.client, http.MethodPost, p.Factory, "/v1/jobs/"+strconv.Itoa(p.Job)+"/waits_on", api.WaitsPut{WaitsOn: p.WaitsOn, Expect: p.Expect})
	if err != nil {
		return appwire.FactoryWaitsSetResult{}, err
	}
	var out api.WaitsSet
	if json.Unmarshal(data, &out) != nil || out.Job != p.Job {
		return appwire.FactoryWaitsSetResult{}, appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm what the job waits on; read the job before trying again")
	}
	if out.WaitsOn == nil {
		out.WaitsOn = []int{}
	}
	if out.Current != nil {
		out.Current.LifetimeCost = max(out.Current.Cost, out.Current.LifetimeCost)
	}
	return appwire.FactoryWaitsSetResult{Factory: p.Factory, Job: out.Job, WaitsOn: out.WaitsOn, Current: out.Current}, nil
}

// notFound is whether err is the factory's 404.
func notFound(err error) bool {
	var w *appwire.Error
	return errors.As(err, &w) && w.Code == appwire.CodeNotFound
}
