package main

// ── codeaf app: LOOK CLOSER ON A JOB ──────────────────────────────────────
//
// The engine's half of appwire.FactoryScouter: POST /v1/jobs/{id}/scout on
// the job's factory, which runs the look on its own checkout and model and
// keeps it on the job. A look may take its whole minute, longer than the
// hub's ordinary timeout, so it goes on a client of its own.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryScouter = (*appBackend)(nil)

// appScoutTimeout is a look's minute and the answer's margin.
const appScoutTimeout = 85 * time.Second

func (b *appBackend) FactoryScout(ctx context.Context, p appwire.FactoryScoutParams) (api.ScoutLook, error) {
	hub := b.factoryHub()
	slow := &http.Client{Transport: hub.client.Transport, Timeout: appScoutTimeout, CheckRedirect: hub.client.CheckRedirect}
	data, err := hub.postWith(ctx, slow, p.Factory, jobPath(p.Job, "scout"), struct {
		Refresh bool `json:"refresh,omitempty"`
	}{p.Refresh})
	if err != nil {
		if routeMissing(err) {
			return api.ScoutLook{}, factoryTooOld()
		}
		return api.ScoutLook{}, err
	}
	var got api.ScoutLook
	if err := json.Unmarshal(data, &got); err != nil {
		return api.ScoutLook{}, appwire.Errorf(appwire.CodeUncertain, "the factory answered the look in a shape this app cannot read")
	}
	return got, nil
}

var _ appwire.FactoryReader = (*appBackend)(nil)

// appReadTimeout is twenty reads one after another and the answer's margin.
const appReadTimeout = 170 * time.Second

// FactoryRead is a person's Read of chosen jobs: POST /v1/read on the
// factory, on a client of its own for the same reason as a look.
func (b *appBackend) FactoryRead(ctx context.Context, p appwire.FactoryReadParams) (api.ReadResult, error) {
	hub := b.factoryHub()
	slow := &http.Client{Transport: hub.client.Transport, Timeout: appReadTimeout, CheckRedirect: hub.client.CheckRedirect}
	data, err := hub.postWith(ctx, slow, p.Factory, "/v1/read", struct {
		Jobs []int `json:"jobs"`
	}{p.Jobs})
	if err != nil {
		if routeMissing(err) {
			return api.ReadResult{}, factoryTooOld()
		}
		return api.ReadResult{}, err
	}
	var got api.ReadResult
	if err := json.Unmarshal(data, &got); err != nil || got.Read == nil {
		return api.ReadResult{}, appwire.Errorf(appwire.CodeUncertain, "the factory answered the read in a shape this app cannot read")
	}
	return got, nil
}
