package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

var _ appwire.FactoryPolicyKeeper = (*appBackend)(nil)

func policyFactoryTooOld() error {
	err := factoryTooOld()
	var refusal *appwire.Error
	if errors.As(err, &refusal) {
		refusal.Detail = &appwire.FactoryRefusal{Reason: "factory_too_old"}
	}
	return err
}

func policyPath(group string) string { return "/v1/groups/" + url.PathEscape(group) + "/policy" }

func decodeFactoryPolicy(raw []byte) (appwire.FactoryPolicy, error) {
	var out appwire.FactoryPolicy
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, appwire.Errorf(appwire.CodeEngine, "the factory sent a policy this app cannot read")
	}
	return out, nil
}

func (b *appBackend) FactoryPolicyGet(ctx context.Context, p appwire.FactoryPolicyGetParams) (appwire.FactoryPolicy, error) {
	p, err := appwire.CheckPolicyGet(p)
	if err != nil {
		return appwire.FactoryPolicy{}, err
	}
	h := b.factoryHub()
	row, token, err := h.credentialed(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryPolicy{}, err
	}
	reply, err := h.send(ctx, row.URL, token, http.MethodGet, policyPath(p.Group), nil, appFactoryBody, nil)
	if err != nil {
		return appwire.FactoryPolicy{}, err
	}
	if err := h.staleCredential(p.Factory, token); err != nil {
		return appwire.FactoryPolicy{}, err
	}
	if reply.status == 404 || reply.status == 405 {
		return appwire.FactoryPolicy{}, policyFactoryTooOld()
	}
	if reply.status < 200 || reply.status >= 300 {
		return appwire.FactoryPolicy{}, appFactoryFailure(reply)
	}
	out, err := decodeFactoryPolicy(reply.body)
	if err == nil {
		h.touch(p.Factory)
	}
	return out, err
}

func (b *appBackend) FactoryPolicySet(ctx context.Context, p appwire.FactoryPolicySetParams) (appwire.FactoryPolicy, error) {
	p, err := appwire.CheckPolicySet(p)
	if err != nil {
		return appwire.FactoryPolicy{}, err
	}
	h := b.factoryHub()
	client := *h.client
	transport := &waitsTransport{RoundTripper: client.Transport}
	if transport.RoundTripper == nil {
		transport.RoundTripper = http.DefaultTransport
	}
	client.Transport = transport
	raw, err := h.sendWrite(ctx, &client, http.MethodPut, p.Factory, policyPath(p.Group), struct {
		Stopped    bool                `json:"stopped"`
		Allowances []factory.Allowance `json:"allowances"`
		Version    *int                `json:"version,omitempty"`
	}{p.Stopped, p.Allowances, p.Version})
	if transport.status == 404 || transport.status == 405 {
		return appwire.FactoryPolicy{}, policyFactoryTooOld()
	}
	if err != nil {
		return appwire.FactoryPolicy{}, err
	}
	return decodeFactoryPolicy(raw)
}
