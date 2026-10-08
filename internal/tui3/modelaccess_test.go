package tui3

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// A PUBLIC CATALOG IS NOT A CONNECTION. These fixtures leave public and cached
// rows available even when credentials disappear, then drive the actual picker.
func TestModelPickersOfferOnlyModelsFromUsableConnections(t *testing.T) {
	ollama := testModelSource(t, "ollama")
	local := modelsource.Connected{Source: ollama, Address: ollama.Address}
	direct := testDirectService("https://direct.example/v1")
	anonymous := direct
	anonymous.Source.ID, anonymous.Source.Written = "custom", "lab"
	anonymous.Source.KeyOptional, anonymous.Key = true, ""
	for _, test := range []struct {
		name   string
		key    string
		others []modelsource.Connected
		want   []string
	}{
		{name: "no connections"},
		{name: "OpenRouter only", key: "router-key", want: []string{"vendor/cloud"}},
		{name: "Ollama only", others: []modelsource.Connected{local}, want: []string{"ollama/qwen3:0.6b"}},
		{name: "both", key: "router-key", others: []modelsource.Connected{local}, want: []string{"vendor/cloud", "ollama/qwen3:0.6b"}},
		{name: "two direct providers", others: []modelsource.Connected{direct, local}, want: []string{"deepseek-direct/direct-chat", "ollama/qwen3:0.6b"}},
		{name: "anonymous custom", others: []modelsource.Connected{anonymous}, want: []string{"lab/local-chat"}},
		{name: "missing direct key", others: []modelsource.Connected{func() modelsource.Connected { c := direct; c.Key = "  "; return c }(), local}, want: []string{"ollama/qwen3:0.6b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			services := append([]modelsource.Connected{testDefaultService(test.key)}, test.others...)
			a := modelServiceTestApp(t, t.TempDir(), "vendor/cloud", modelsource.NewSet(services...), []Model{{ID: "vendor/cloud"}})
			// An old public cache remains readable; access must be checked first.
			if err := WriteModelCache([]Model{{ID: "vendor/cached-cloud"}}); err != nil {
				t.Fatal(err)
			}
			a.refreshLearning()
			a.sourceModels["ollama"] = []Model{{ID: "qwen3:0.6b"}}
			a.sourceModels["deepseek"] = []Model{{ID: "direct-chat"}}
			a.sourceModels["custom"] = []Model{{ID: "local-chat"}}
			assertList := func(models []Model) {
				t.Helper()
				var ids []string
				for _, model := range models {
					if !model.AddProvider && !model.Unavailable {
						ids = append(ids, model.ID)
					}
				}
				if !reflect.DeepEqual(ids, test.want) {
					t.Fatalf("selectable models = %v, want %v", ids, test.want)
				}
			}
			assertList(a.modelList())
			assertList(a.modelsFor(nil))
			a.openPicker()
			assertList(a.pick.all)
			if test.key == "" && strings.Contains(strings.Join(groupPickerLines(a), "\n"), "vendor/cloud") {
				t.Fatal("the unconnected public catalog is still drawn")
			}
			// A task room goes through the same concrete list and access rule.
			a.tasks = map[uint64]*taskNode{8: {model: "vendor/cloud"}}
			a.openTaskPicker(8)
			assertList(a.pick.all)
		})
	}
}

func TestRemovingAndRestoringAProvidersKeyChangesItsPickerModels(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "vendor/cloud", modelsource.NewSet(testDefaultService("")), []Model{{ID: "vendor/cloud"}})
	for _, key := range []string{"router-key", "", "replacement-key"} {
		if err := config.WriteAPIKey(dir, key); err != nil {
			t.Fatal(err)
		}
		a.handAPIKey()
		want := 0
		if key != "" {
			want = 1
		}
		if got := len(a.modelList()); got != want {
			t.Fatalf("after key %q, %d models, want %d", key, got, want)
		}
	}
}

func TestAnEmptyProviderShelfDoesNotResurrectItsOldPickerCache(t *testing.T) {
	ollama := testModelSource(t, "ollama")
	local := modelsource.Connected{Source: ollama, Address: ollama.Address}
	a := modelServiceTestApp(t, t.TempDir(), "ollama/removed", modelsource.NewSet(testDefaultService(""), local), nil)
	if err := WriteModelCacheFor("ollama", local.Address, []Model{{ID: "removed"}}); err != nil {
		t.Fatal(err)
	}
	a.refreshLearning()
	a.sourceModels["ollama"] = []Model{{ID: "removed"}}
	a.modelsForService = func(modelsource.Connected) []Model { return nil }
	for _, model := range a.modelList() {
		if !model.Unavailable {
			t.Fatalf("empty current shelf resurrected %q", model.ID)
		}
	}
}

func TestAKnownCatalogDoesNotBorrowOlderCachedModels(t *testing.T) {
	a := modelServiceTestApp(t, t.TempDir(), "vendor/current", modelsource.NewSet(testDefaultService("router-key")), []Model{{ID: "vendor/current"}})
	if err := WriteModelCache([]Model{{ID: "vendor/removed", Input: []string{"image"}, Output: []string{"image"}}}); err != nil {
		t.Fatal(err)
	}
	a.refreshLearning()
	if models := a.modelsFor(func(model Model) bool { return model.ID == "vendor/removed" }); len(models) != 0 {
		t.Fatalf("a filtered current catalog borrowed old rows: %+v", models)
	}
	a.models = func() []Model { return []Model{} }
	if models := a.modelList(); len(models) != 0 {
		t.Fatalf("an empty current catalog borrowed old rows: %+v", models)
	}
}

func TestTheDefaultPickerCannotBorrowAnotherAddressesCache(t *testing.T) {
	service := testDefaultService("custom-key")
	service.Source.Address, service.Address = "https://other.example/v1", "https://other.example/v1"
	a := modelServiceTestApp(t, t.TempDir(), "vendor/model", modelsource.NewSet(service), nil)
	if err := WriteModelCache([]Model{{ID: "vendor/public-router-row"}}); err != nil {
		t.Fatal(err)
	}
	a.refreshLearning()
	if models := a.modelList(); len(models) != 0 {
		t.Fatalf("another base borrowed the public router cache: %+v", models)
	}
	if err := WriteModelCacheFor(service.Source.ID, service.Address, []Model{{ID: "vendor/own-row"}}); err != nil {
		t.Fatal(err)
	}
	a.refreshLearning()
	if models := a.modelList(); len(models) != 1 || models[0].ID != "vendor/own-row" {
		t.Fatalf("the default provider could not read its own cache: %+v", models)
	}
}
