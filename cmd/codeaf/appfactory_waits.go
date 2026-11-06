package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryWaitsSetter = (*appBackend)(nil)
var _ appwire.FactoryFeaturesReader = (*appBackend)(nil)

// waitsTransport keeps the status of this one write before the shared error
// mapping, so an absent typed route can be named as an older factory.
type waitsTransport struct {
	http.RoundTripper
	status int
}

func (t *waitsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	answer, err := t.RoundTripper.RoundTrip(r)
	if answer != nil {
		t.status = answer.StatusCode
	}
	return answer, err
}

// FactoryWaitsSet replaces the parents once, then reads the current job once.
func (b *appBackend) FactoryWaitsSet(ctx context.Context, p appwire.FactoryWaitsSetParams) (appwire.FactoryWaitsSet, error) {
	p, err := appwire.CheckWaitsSet(p)
	if err != nil {
		return appwire.FactoryWaitsSet{}, err
	}
	h := b.factoryHub()
	client := *h.client
	transport := &waitsTransport{RoundTripper: client.Transport}
	if transport.RoundTripper == nil {
		transport.RoundTripper = http.DefaultTransport
	}
	client.Transport = transport
	_, err = h.sendWrite(ctx, &client, http.MethodPut, p.Factory, jobPath(p.Job, "waits_on"), struct {
		WaitsOn []int       `json:"waits_on"`
		Expect  *api.Expect `json:"expect,omitempty"`
	}{p.WaitsOn, p.Expect})
	if transport.status == http.StatusNotFound || transport.status == http.StatusMethodNotAllowed || unknownFactoryField(err, "waits_on") {
		old := factoryTooOld()
		var refusal *appwire.Error
		if errors.As(old, &refusal) {
			refusal.Detail = &appwire.FactoryRefusal{Reason: "factory_too_old"}
		}
		return appwire.FactoryWaitsSet{}, old
	}
	if err != nil {
		return appwire.FactoryWaitsSet{}, err
	}
	out := appwire.FactoryWaitsSet{Factory: p.Factory, Job: p.Job, WaitsOn: append([]int{}, p.WaitsOn...)}
	var current api.Job
	if err := h.read(ctx, p.Factory, "/v1/jobs/"+strconv.Itoa(p.Job), appFactoryBody, &current); err == nil {
		out.Current = &current
	}
	return out, nil
}

// FactoryFeatures reads the factory's public about with any held authority.
func (b *appBackend) FactoryFeatures(ctx context.Context, p appwire.FactoryFeaturesParams) (appwire.FactoryFeatures, error) {
	h := b.factoryHub()
	row, held, err := h.target(p.Factory)
	if err != nil {
		return appwire.FactoryFeatures{}, err
	}
	r, err := h.send(ctx, row.URL, held, http.MethodGet, "/v1/about", nil, appFactorySmall, nil)
	if err != nil {
		return appwire.FactoryFeatures{}, err
	}
	if err := h.staleCredential(p.Factory, held); err != nil {
		return appwire.FactoryFeatures{}, err
	}
	out := appwire.FactoryFeatures{Features: []string{}}
	if r.status < 200 || r.status >= 300 {
		return appwire.FactoryFeatures{}, appFactoryFailure(r)
	}
	if err := json.Unmarshal(r.body, &out); err != nil {
		return appwire.FactoryFeatures{}, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	if out.Features == nil {
		out.Features = []string{}
	}
	h.touch(p.Factory)
	return out, nil
}
