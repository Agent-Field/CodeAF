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
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/ollamalimits"
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
	unloaded, unasked := appOllamaLimits(context.Background(), models, sources, stub.Client(), &ollamalimits.Memory{})

	// Nothing is loaded and nothing sets a window, so the window is the
	// server's to decide when it loads the model: no window limit is reported.
	want := []appwire.ModelLimit{{Kind: appwire.LimitNoTools}}
	for i, model := range models {
		if owned := model.Provider == "ollama"; owned != slices.Equal(model.Limits, want) || (!owned && model.Limits != nil) {
			t.Fatalf("row %d %s: limits = %+v", i, model.ID, model.Limits)
		}
	}
	slices.Sort(stub.asked)
	if !slices.Equal(stub.asked, []string{"library/gemma3", "qwen3:8b"}) {
		t.Fatalf("the server was asked about %q", stub.asked)
	}
	if len(unloaded) != 2 || !unloaded["qwen3:8b"] || !unloaded["library/gemma3"] || unasked != nil {
		t.Fatalf("models the server is not running = %v, not asked about = %v", unloaded, unasked)
	}

	data, err := json.Marshal(models[:2])
	wire := `[{"id":"qwen/qwen3:8b","name":"","provider":"openrouter"},{"id":"ollama/qwen3:8b","name":"","provider":"ollama","limits":[{"kind":"no_tools"}]}]`
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
	if unloaded, unasked := appOllamaLimits(context.Background(), models, sources, stub.Client(), &ollamalimits.Memory{}); unloaded != nil || unasked != nil {
		t.Fatalf("models not running = %v, not asked about = %v", unloaded, unasked)
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
	if appOllamaLimits(context.Background(), models, sources, stub.Client(), nil); stub.hits != 0 || models[0].Limits != nil {
		t.Fatalf("an Ollama with no listed model was asked %d questions", stub.hits)
	}

	stub.Close()
	models = []appwire.Model{{ID: "ollama/qwen3:8b", Provider: "ollama"}}
	if unloaded, _ := appOllamaLimits(context.Background(), models, sources, nil, nil); unloaded != nil || models[0].Limits != nil {
		t.Fatalf("an absent server reported %+v, %v", models[0].Limits, unloaded)
	}
}

// A turn on an Ollama model the server was not running at the last list tells
// the app to list again, once: the server can say the window now that the
// model is loaded. A turn on any other model, or on a model that was already
// loaded, says nothing.
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

// A list whose time ran out before the server could be asked keeps what the
// last list found: the row's limits, and that the server was not running the
// model, so the turn after it still asks for a new list.
func TestAppALateListKeepsTheLastListsLimitsAndUnloadedModels(t *testing.T) {
	var mu sync.Mutex
	ps := `{"models":[{"name":"qwen3:8b","model":"qwen3:8b","context_length":4096}]}`
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/api/ps" {
			io.WriteString(w, ps)
			return
		}
		io.WriteString(w, `{"capabilities":["completion","tools"],"model_info":{"general.architecture":"qwen3","qwen3.context_length":40960}}`)
	}))
	defer stub.Close()
	sources := modelsource.NewSet(modelsource.Connected{Source: appVendoredSource(t, "ollama"), Address: stub.URL + "/v1"})
	backend := &appBackend{}
	row := func() []appwire.Model { return []appwire.Model{{ID: "ollama/qwen3:8b", Provider: "ollama"}} }

	loaded := row()
	backend.addModelLimits(time.Now().Add(time.Second), loaded, sources)
	if len(loaded[0].Limits) != 1 || loaded[0].Limits[0].Source != appwire.WindowLoaded || len(backend.ollamaUnloaded) != 0 {
		t.Fatalf("loaded: limits %+v, not running %v", loaded[0].Limits, backend.ollamaUnloaded)
	}
	mu.Lock()
	ps = `{"models":[]}`
	mu.Unlock()
	unloaded := row()
	backend.addModelLimits(time.Now().Add(time.Second), unloaded, sources)
	if len(unloaded[0].Limits) != 1 || unloaded[0].Limits[0].Source != appwire.WindowLastLoaded || !backend.ollamaUnloaded["qwen3:8b"] {
		t.Fatalf("unloaded: limits %+v, not running %v", unloaded[0].Limits, backend.ollamaUnloaded)
	}

	late := row()
	backend.addModelLimits(time.Now().Add(-time.Second), late, sources)
	if !slices.Equal(late[0].Limits, unloaded[0].Limits) || !backend.ollamaUnloaded["qwen3:8b"] {
		t.Fatalf("late: limits %+v, not running %v", late[0].Limits, backend.ollamaUnloaded)
	}
}

// THE WHOLE ROAD, THROUGH THE APP'S OWN DOORS. models.list asks the Ollama
// service while the model is not loaded and reports no window; a turn on that
// model ends and the engine says models.changed once; the next list, with the
// server now running the model at 4,096 tokens, says so; and once the server
// unloads it, the list says the window it had when last loaded. Each step needs
// its wire: the list's limits (ListModels), the turn's end (pump's
// modelLoadedBy) and the notice's door (watchModelCompletions' tellModels).
func TestAppATurnOnAnUnloadedOllamaModelBringsItsWindowToTheList(t *testing.T) {
	_, workspace := wakeEnvironment(t, resolvedTempDir(t))
	var mu sync.Mutex
	running := false
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"object":"list","data":[{"id":"qwen3:8b","object":"model","owned_by":"library"}]}`)
		case "/api/ps":
			mu.Lock()
			loaded := running
			mu.Unlock()
			if loaded {
				io.WriteString(w, `{"models":[{"name":"qwen3:8b","model":"qwen3:8b","context_length":4096}]}`)
				return
			}
			io.WriteString(w, `{"models":[]}`)
		case "/api/show":
			io.WriteString(w, `{"capabilities":["completion","tools"],"model_info":{"general.architecture":"qwen3","qwen3.context_length":40960}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ollama.Close)
	settings, err := config.LoadKeyless()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteSources(settings.ProfileDir, []config.PersistedSource{{ID: "ollama", Written: "ollama", Address: ollama.URL + "/v1"}}); err != nil {
		t.Fatal(err)
	}
	// The default service's list is remembered, so its arrival raises no
	// models.changed of its own while this test counts them.
	if err := catalog.Remember(catalog.Options{BaseURL: settings.BaseURL, Dir: settings.ProfileDir}, []catalog.Model{{ID: "vendor/alpha", Name: "Alpha", InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}); err != nil {
		t.Fatal(err)
	}
	conv := &fakeConversation{model: "ollama/qwen3:8b", script: []session.Event{{Kind: session.EventTurnDone}}}
	var opened sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &opened))
	defer backend.Close()

	row := func() appwire.Model {
		t.Helper()
		models, err := backend.ListModels()
		if err != nil {
			t.Fatal(err)
		}
		for _, model := range models {
			if model.ID == "ollama/qwen3:8b" {
				return model
			}
		}
		t.Fatalf("the Ollama model is not listed: %+v", models)
		return appwire.Model{}
	}
	// settled is the count of models.changed once no more arrive.
	settled := func() int {
		t.Helper()
		count, still := appModelsChangedCount(rec), time.Now()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if now := appModelsChangedCount(rec); now != count {
				count, still = now, time.Now()
			} else if time.Since(still) > 300*time.Millisecond {
				return count
			}
		}
		t.Fatal("models.changed never settled")
		return 0
	}
	turn := func(agent string) {
		t.Helper()
		turns := 0
		rec.mu.Lock()
		for _, event := range rec.events {
			if event == appwire.EventTurnDone {
				turns++
			}
		}
		rec.mu.Unlock()
		if _, err := backend.Send(agent, "hello", ""); err != nil {
			t.Fatal(err)
		}
		rec.waitFor(t, 10*time.Second, func(event string, _ any) bool {
			if event == appwire.EventTurnDone {
				turns--
			}
			return turns < 0
		})
	}

	if first := row(); first.Limits != nil {
		t.Fatalf("a model never loaded reported %+v", first.Limits)
	}
	before := settled()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace, Model: "ollama/qwen3:8b"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	running = true
	mu.Unlock()
	turn(agent.ID)
	if after := settled(); after != before+1 {
		t.Fatalf("a turn on a model that was not loaded raised %d models.changed, want 1", after-before)
	}

	want := []appwire.ModelLimit{{Kind: appwire.LimitSmallWindow, Window: 4096, Needs: session.LeanWindow(), Most: 40960, Source: appwire.WindowLoaded}}
	if loaded := row(); !slices.Equal(loaded.Limits, want) {
		t.Fatalf("the loaded model's limits = %+v, want %+v", loaded.Limits, want)
	}
	// It was loaded at that list, so the next turn on it asks for nothing.
	before = settled()
	turn(agent.ID)
	if after := settled(); after != before {
		t.Fatalf("a turn on a loaded model raised %d models.changed", after-before)
	}

	mu.Lock()
	running = false
	mu.Unlock()
	want[0].Source = appwire.WindowLastLoaded
	if unloaded := row(); !slices.Equal(unloaded.Limits, want) {
		t.Fatalf("the unloaded model's limits = %+v, want %+v", unloaded.Limits, want)
	}
}
