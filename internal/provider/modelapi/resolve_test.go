package modelapi_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
	"github.com/Agent-Field/codeaf/internal/session"
)

// routerAccount is a machine with an OpenRouter key and nothing else.
func routerAccount() modelsource.Set {
	source := modelsource.DefaultSource("https://openrouter.ai/api/v1")
	return modelsource.NewSet(modelsource.Connected{Source: source, Key: "sk-or-v1-routerkey0000000000", Address: source.Address})
}

// proxyOnly is the owner's machine: no OpenRouter key, and one service of
// their own — an OpenAI-compatible proxy on this machine — that carries every
// conversation.
func proxyOnly() modelsource.Set {
	router := modelsource.DefaultSource("https://openrouter.ai/api/v1")
	proxy := modelsource.Source{ID: modelsource.CustomID, Written: "mybox", Name: "mybox", Address: "http://127.0.0.1:9000/v1"}
	return modelsource.NewSet(
		modelsource.Connected{Source: router, Address: router.Address},
		modelsource.Connected{Source: proxy, Key: "local", Address: proxy.Address},
	)
}

// servedBy is the account pool's own test over one machine's services — the
// door the model API is handed in production (session.ServesModel), never a
// second copy of it.
func servedBy(sources modelsource.Set) func(string) bool {
	return func(model string) bool { return session.ServesModel(sources, model) }
}

// THE RULE, ON THE MACHINES IT IS FOR: a model the person's services carry is
// honoured as asked; one they cannot reach is answered on the run's seat and
// the answer names the seat; an `openrouter/` prefix is read as the service it
// names, never as part of the model; and nothing is refused here only because
// this machine does not know the id.
func TestResolveHonoursACarriedModelAndSeatsTheRest(t *testing.T) {
	const seat = "mybox/qwen3-coder"
	for _, row := range []struct {
		name          string
		sources       modelsource.Set
		asked         string
		seats         []string
		model, served string
	}{
		{"a model the router carries", routerAccount(), "deepseek/deepseek-v4-flash-0731", []string{seat},
			"deepseek/deepseek-v4-flash-0731", ""},
		{"a prefixed id on the router's own key", routerAccount(), "openrouter/deepseek/deepseek-v4-pro", []string{seat},
			"openrouter/deepseek/deepseek-v4-pro", ""},
		{"a model no service here can reach", proxyOnly(), "moonshotai/kimi-k2.6", []string{seat},
			seat, seat},
		{"a prefixed id whose service has no key", proxyOnly(), "openrouter/z-ai/glm-5.1", []string{seat},
			seat, seat},
		{"a model the person's own service carries", proxyOnly(), "mybox/deepseek-v4-flash", []string{seat},
			"mybox/deepseek-v4-flash", ""},
		{"a seat nothing can reach is passed over for the next", proxyOnly(), "qwen/qwen3.6-plus", []string{"deepseek/deepseek-v4-pro", seat},
			seat, seat},
		{"nothing here can serve anything named", proxyOnly(), "minimax/minimax-m2.7", []string{"z-ai/glm-5.1"},
			"minimax/minimax-m2.7", ""},
		{"a call that named no model", routerAccount(), "", []string{"deepseek/deepseek-v4-flash-0731"},
			"deepseek/deepseek-v4-flash-0731", "deepseek/deepseek-v4-flash-0731"},
	} {
		model, served := modelapi.Resolve(row.asked, servedBy(row.sources), row.seats...)
		if model != row.model || served != row.served {
			t.Errorf("%s: Resolve(%q) = %q served %q, want %q served %q", row.name, row.asked, model, served, row.model, row.served)
		}
	}
	// With nobody to ask, every model is taken as written.
	if model, served := modelapi.Resolve("anything/at-all", nil, seat); model != "anything/at-all" || served != "" {
		t.Fatalf("Resolve with no door = %q %q", model, served)
	}
}
