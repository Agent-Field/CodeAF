package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/trigger"
)

var _ appwire.FactorySourcer = (*appBackend)(nil)

// FactorySourceAdd registers a source on the named factory through its
// POST /v1/sources and carries back the token and signing secret, shown
// this once.
func (b *appBackend) FactorySourceAdd(ctx context.Context, p appwire.FactorySourceAddParams) (trigger.Minted, error) {
	body := struct {
		Name     string `json:"name"`
		Callback string `json:"callback,omitempty"`
		Trust    string `json:"trust,omitempty"`
	}{p.Name, p.Callback, string(p.Trust)}
	data, err := b.factoryHub().post(ctx, p.Factory, serve.SourcesPath, body)
	if err != nil {
		return trigger.Minted{}, err
	}
	var out trigger.Minted
	if json.Unmarshal(data, &out) != nil || out.Token == "" {
		// The factory said yes and showed no token: it may be registered,
		// and its token cannot be shown again, so the person revokes and adds.
		return trigger.Minted{}, appwire.Errorf(appwire.CodeUncertain, "the factory registered the source but did not show its token; revoke it and add it again")
	}
	return out, nil
}

// FactorySourceList reads the named factory's GET /v1/sources.
func (b *appBackend) FactorySourceList(ctx context.Context, p appwire.FactoryRef) (appwire.FactorySources, error) {
	var out appwire.FactorySources
	err := b.factoryHub().read(ctx, p.Factory, serve.SourcesPath, appFactorySmall, &out)
	return out, err
}

// FactorySourceRevoke is the named factory's DELETE /v1/sources/{name}.
func (b *appBackend) FactorySourceRevoke(ctx context.Context, p appwire.FactorySourceParams) (appwire.FactorySourceRevoked, error) {
	h := b.factoryHub()
	data, err := h.sendWrite(ctx, h.client, http.MethodDelete, p.Factory, serve.SourcesPath+"/"+url.PathEscape(p.Name), struct{}{})
	if err != nil {
		return appwire.FactorySourceRevoked{}, err
	}
	out := appwire.FactorySourceRevoked{Revoked: p.Name}
	_ = json.Unmarshal(data, &out)
	return out, nil
}
