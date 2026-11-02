package main

// ── codeaf app: READ NEW ISSUES AS THEY ARRIVE ────────────────────────────
//
// The engine's half of appwire.FactoryArrivalReader: the factory's
// `read_on_arrival` in GET /v1/settings, changed by PATCH (an operator's
// write). A factory older than the setting answers settings without it, and
// such a factory reads every arrival, so that reads on.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

var _ appwire.FactoryArrivalReader = (*appBackend)(nil)

func (b *appBackend) FactoryReadOnArrival(ctx context.Context, p appwire.FactoryReadOnArrivalParams) (appwire.FactoryReadOnArrival, error) {
	hub := b.factoryHub()
	got := struct {
		On *bool `json:"read_on_arrival"`
	}{}
	if p.On == nil {
		if err := hub.read(ctx, p.Factory, "/v1/settings", appFactorySmall, &got); err != nil {
			return appwire.FactoryReadOnArrival{}, err
		}
	} else {
		data, err := hub.sendWrite(ctx, hub.client, http.MethodPatch, p.Factory, "/v1/settings", struct {
			On bool `json:"read_on_arrival"`
		}{*p.On})
		if err != nil {
			return appwire.FactoryReadOnArrival{}, err
		}
		if json.Unmarshal(data, &got) != nil || got.On == nil {
			return appwire.FactoryReadOnArrival{}, factoryTooOld()
		}
	}
	return appwire.FactoryReadOnArrival{On: got.On == nil || *got.On}, nil
}
