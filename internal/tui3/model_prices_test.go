package tui3

import (
	"testing"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
)

// A CONNECTED SERVICE'S MODELS CARRY WHAT THEY COST: a price by the request
// and whether the figures are real ride through, so a model priced by the
// request is never sent per-million figures and a free one reads free. Rows
// a cache kept from ids alone know no price.
func TestServiceModelsCarryWhatTheyCost(t *testing.T) {
	rows := []modelcatalog.Model{
		{ID: "by-request", PromptPrice: 0.000001, CompletionPrice: 0.000002, RequestPrice: 0.04},
		{ID: "free"},
		{ID: "router", PriceUnknown: true, PromptPrice: 1},
	}
	got := map[string]Model{}
	for _, m := range surfaceModels(rows, true) {
		got[m.ID] = m
	}
	if m := got["by-request"]; m.RequestPrice != 0.04 || !m.PriceKnown {
		t.Fatalf("by-request = %+v", m)
	}
	if m := got["free"]; !m.PriceKnown || m.PromptPrice != 0 {
		t.Fatalf("free = %+v", m)
	}
	if m := got["router"]; m.PriceKnown || m.PromptPrice != 0 || m.RequestPrice != 0 {
		t.Fatalf("router = %+v", m)
	}
	for _, m := range surfaceModels(rows, false) {
		if m.PriceKnown {
			t.Fatalf("a row a cache kept from ids alone claims its price is known: %+v", m)
		}
	}
}
