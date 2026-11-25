package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appOllamaStub is an Ollama whose every model takes no tools and sets no
// window, with nothing loaded. It records which models it was asked about.
type appOllamaStub struct {
	*httptest.Server
	mu    sync.Mutex
	hits  int
	asked []string
}

func newAppOllamaStub(t *testing.T) *appOllamaStub {
	t.Helper()
	stub := &appOllamaStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var ask struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &ask)
		stub.mu.Lock()
		stub.hits++
		if ask.Model != "" {
			stub.asked = append(stub.asked, ask.Model)
		}
		stub.mu.Unlock()
		if r.URL.Path == "/api/ps" {
			io.WriteString(w, `{"models":[]}`)
			return
		}
		io.WriteString(w, `{"capabilities":["completion"],"model_info":{"general.architecture":"qwen3","qwen3.context_length":40960}}`)
	}))
	t.Cleanup(stub.Close)
	return stub
}

// appVendoredSource is the source table's row for id.
func appVendoredSource(t *testing.T, id string) modelsource.Source {
	t.Helper()
	for _, source := range modelsource.Vendored() {
		if source.ID == id {
			return source
		}
	}
	t.Fatalf("no vendored source %q", id)
	return modelsource.Source{}
}

// A models.list row of an Ollama service carries what that server says, asked
// at the service's own address; the rows of every other service carry nothing
// and are never asked about, a model of the same name included.
func TestAppOllamaLimitsAreAskedOfTheOllamaServiceAlone(t *testing.T) {
	stub := newAppOllamaStub(t)
	sources := modelsource.NewSet(
		modelsource.Connected{Source: modelsource.DefaultSource(""), Key: "sk-or-test"},
		modelsource.Connected{Source: appVendoredSource(t, "ollama"), Address: stub.URL + "/v1"},
		modelsource.Connected{Source: appVendoredSource(t, "deepseek"), Key: "sk-test"},
	)
	models := []appwire.Model{
		{ID: "qwen/qwen3:8b", Provider: "openrouter"},
		{ID: "ollama/qwen3:8b", Provider: "ollama"},
		{ID: "deepseek/deepseek-chat", Provider: "deepseek"},
		{ID: "ollama/library/gemma3", Provider: "ollama"},
	}
	unloaded := appOllamaLimits(context.Background(), models, sources, stub.Client())

	want := []appwire.ModelLimit{
		{Kind: appwire.LimitSmallWindow, Needs: session.LeanWindow(), Most: 40960, Source: appwire.WindowDefault},
		{Kind: appwire.LimitNoTools},
	}
	for i, model := range models {
		if owned := model.Provider == "ollama"; owned != slices.Equal(model.Limits, want) || (!owned && model.Limits != nil) {
			t.Fatalf("row %d %s: limits = %+v", i, model.ID, model.Limits)
		}
	}
	slices.Sort(stub.asked)
	if !slices.Equal(stub.asked, []string{"library/gemma3", "qwen3:8b"}) {
		t.Fatalf("the server was asked about %q", stub.asked)
	}
	if len(unloaded) != 2 || !unloaded["qwen3:8b"] || !unloaded["library/gemma3"] {
		t.Fatalf("models on the server's default window = %v", unloaded)
	}

	data, err := json.Marshal(models[:2])
	wire := `[{"id":"qwen/qwen3:8b","name":"","provider":"openrouter"},{"id":"ollama/qwen3:8b","name":"","provider":"ollama","limits":[{"kind":"small_window","needs":32000,"most":40960,"source":"default"},{"kind":"no_tools"}]}]`
	if err != nil || string(data) != wire {
		t.Fatalf("wire = %s %v, want %s", data, err, wire)
	}
}

// With no Ollama service connected nothing is asked of anybody: not of a
// custom endpoint that happens to be an Ollama, and not of a vendor's address.
func TestAppOllamaLimitsAskNothingWithoutAnOllamaService(t *testing.T) {
	stub := newAppOllamaStub(t)
	custom := appVendoredSource(t, modelsource.CustomID)
	custom.Written = "local"
	vendor := appVendoredSource(t, "deepseek")
	sources := modelsource.NewSet(
		modelsource.Connected{Source: custom, Key: "none", Address: stub.URL + "/v1"},
		modelsource.Connected{Source: vendor, Key: "sk-test", Address: stub.URL + "/v1"},
	)
	models := []appwire.Model{{ID: "local/qwen3:8b", Provider: "local"}, {ID: "deepseek/deepseek-chat", Provider: "deepseek"}, {ID: "qwen3:8b", Provider: "local"}}
	if unloaded := appOllamaLimits(context.Background(), models, sources, stub.Client()); unloaded != nil {
		t.Fatalf("models on a default window = %v", unloaded)
	}
	for _, model := range models {
		if model.Limits != nil {
			t.Fatalf("%s: limits = %+v", model.ID, model.Limits)
		}
	}
	if stub.hits != 0 {
		t.Fatalf("a server that is not an Ollama service was asked %d questions", stub.hits)
	}
}

// An Ollama service with none of the listed models is not asked either, and a
// server that is not there leaves every row as it was.
func TestAppOllamaLimitsLeaveTheListAloneWhenThereIsNothingToAsk(t *testing.T) {
	stub := newAppOllamaStub(t)
	ollama := modelsource.Connected{Source: appVendoredSource(t, "ollama"), Address: stub.URL + "/v1"}
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(""), Key: "sk-or-test"}, ollama)
	models := []appwire.Model{{ID: "qwen/qwen3:8b", Provider: "openrouter"}}
	if appOllamaLimits(context.Background(), models, sources, stub.Client()); stub.hits != 0 || models[0].Limits != nil {
		t.Fatalf("an Ollama with no listed model was asked %d questions", stub.hits)
	}

	stub.Close()
	models = []appwire.Model{{ID: "ollama/qwen3:8b", Provider: "ollama"}}
	if unloaded := appOllamaLimits(context.Background(), models, sources, nil); unloaded != nil || models[0].Limits != nil {
		t.Fatalf("an absent server reported %+v, %v", models[0].Limits, unloaded)
	}
}

// A turn on an Ollama model the last list had no window for tells the app to
// list again, once: the server can say the window now that the model is
// loaded. A turn on any other model, or on a model whose window was already
// known, says nothing.
func TestAppTurnOnAnUnloadedOllamaModelAsksForOneNewList(t *testing.T) {
	sources := modelsource.NewSet(
		modelsource.Connected{Source: modelsource.DefaultSource(""), Key: "sk-or-test"},
		modelsource.Connected{Source: appVendoredSource(t, "ollama")},
	)
	told := 0
	backend := &appBackend{tellModels: func() { told++ }, ollamaUnloaded: map[string]bool{"qwen3:8b": true}}
	for _, model := range []string{"qwen/qwen3:8b", "qwen3:8b", "ollama/gemma3:4b", ""} {
		if backend.ollamaLoaded(sources, model); told != 0 {
			t.Fatalf("a turn on %q asked for a new list", model)
		}
	}
	backend.ollamaLoaded(sources, "ollama/qwen3:8b:high")
	backend.ollamaLoaded(sources, "ollama/qwen3:8b")
	if told != 1 {
		t.Fatalf("told the app %d times, want once", told)
	}
	// Before the app's model notices are joined there is nobody to tell.
	quiet := &appBackend{ollamaUnloaded: map[string]bool{"qwen3:8b": true}}
	quiet.ollamaLoaded(sources, "ollama/qwen3:8b")
}
