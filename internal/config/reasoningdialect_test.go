package config

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// TestOnlyDeepSeeksOwnAPIGetsItsReasoningDialect: the dialect travels from the
// service row that answers the model, so `deepseek/deepseek-v4-pro` reaches the
// vendor's own field only when the vendor's own API is connected and answering
// it. The same slug on the router keeps OpenRouter's unified object, because
// the router is what translates it for the machines behind it.
func TestOnlyDeepSeeksOwnAPIGetsItsReasoningDialect(t *testing.T) {
	var deepseek modelsource.Source
	for _, source := range modelsource.Vendored() {
		if source.ID == "deepseek" {
			deepseek = source
		}
	}
	if deepseek.Reasoning != modelsource.ReasoningEffortLowHighMax {
		t.Fatalf("the vendored DeepSeek row declares %q, want %q", deepseek.Reasoning, modelsource.ReasoningEffortLowHighMax)
	}
	router := modelsource.Connected{Source: modelsource.DefaultSource(""), Key: "router-key"}
	direct := modelsource.Connected{Source: deepseek, Key: "deepseek-key", Address: deepseek.Address}

	both := modelsource.NewSet(router, direct)
	if got := ClientConfigFor(both, "deepseek/deepseek-v4-pro").ReasoningDialect; got != modelsource.ReasoningEffortLowHighMax {
		t.Fatalf("DeepSeek's own API was built with dialect %q, want %q", got, modelsource.ReasoningEffortLowHighMax)
	}
	routerOnly := modelsource.NewSet(router)
	if got := ClientConfigFor(routerOnly, "deepseek/deepseek-v4-pro").ReasoningDialect; got != modelsource.ReasoningUnified {
		t.Fatalf("the router serving a DeepSeek model was built with dialect %q, want the unified object", got)
	}
	if got := ClientConfigFor(both, "moonshotai/kimi-k3").ReasoningDialect; got != modelsource.ReasoningUnified {
		t.Fatalf("an ordinary router model was built with dialect %q, want the unified object", got)
	}
}
