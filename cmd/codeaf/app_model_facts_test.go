package main

import (
	"math"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// The picker row's prices are per token; the wire's are per million. A price
// is sent only when it is known: the catalog said so, or both figures are
// positive, and the model is not priced by the request. Anything else stays
// absent, never $0.
func TestAppModelFactsSendOnlyKnownPrices(t *testing.T) {
	for _, c := range []struct {
		name  string
		row   tui3.Model
		known bool
		in    float64
	}{
		{"known", tui3.Model{PromptPrice: 0.000003, CompletionPrice: 0.000015, PriceKnown: true}, true, 3},
		{"known free", tui3.Model{PriceKnown: true}, true, 0},
		{"positive without the word", tui3.Model{PromptPrice: 0.000001, CompletionPrice: 0.000002}, true, 1},
		{"nobody said", tui3.Model{}, false, 0},
		{"half a figure", tui3.Model{PromptPrice: 0.000001}, false, 0},
		{"router", tui3.Model{PromptPrice: -0.000001, CompletionPrice: -0.000001, PriceKnown: true}, false, 0},
		{"by the request", tui3.Model{PriceKnown: true, RequestPrice: 0.005}, false, 0},
		{"by the request and the token", tui3.Model{PromptPrice: 0.000001, CompletionPrice: 0.000002, PriceKnown: true, RequestPrice: 0.005}, false, 0},
	} {
		var model appwire.Model
		appModelFacts(&model, c.row)
		if (model.PriceInPerM != nil) != c.known || (model.PriceOutPerM != nil) != c.known {
			t.Fatalf("%s: price sent = %v, want %v", c.name, model.PriceInPerM != nil, c.known)
		}
		if c.known && math.Abs(*model.PriceInPerM-c.in) > 1e-9 {
			t.Fatalf("%s: in = %v, want %v", c.name, *model.PriceInPerM, c.in)
		}
	}
	var model appwire.Model
	appModelFacts(&model, tui3.Model{ContextLength: -1, ArenaElo: -3})
	if model.ContextLength != 0 || model.ArenaElo != 0 {
		t.Fatalf("negative facts were sent: %+v", model)
	}
}
