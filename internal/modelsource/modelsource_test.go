package modelsource

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAServiceNameThatCollidesWithAModelAuthorIsRefusedWithASuggestion(t *testing.T) {
	suggestion, collided := Collides(" DeepSeek ", []string{"openrouter"}, []string{"deepseek", "qwen"})
	if !collided || suggestion != "DeepSeek-direct" {
		t.Fatalf("collision = %t, suggestion = %q", collided, suggestion)
	}
	suggestion, collided = Collides("deepseek", []string{"deepseek-direct", "deepseek-direct-2"}, []string{"DEEPSEEK"})
	if !collided || suggestion != "deepseek-direct-3" {
		t.Fatalf("numbered collision = %t, suggestion = %q", collided, suggestion)
	}
}

func TestUnqualifiedIdsStayOnTheDefaultService(t *testing.T) {
	defaultService := Connected{Source: DefaultSource("https://router.example/v1"), Key: "router-key", Address: "https://router.example/v1"}
	direct := Connected{Source: Source{ID: "deepseek", Written: "deepseek-direct"}, Key: "direct-key", Address: "https://direct.example/v1"}
	services := NewSet(defaultService, direct)

	for _, model := range []string{"~deepseek/deepseek-v4-flash-latest", "qwen/qwen3-asr-flash-2026-02-10", "gpt-oss:20b"} {
		service, bare := services.For(model)
		if service.Source.ID != DefaultID || bare != model {
			t.Errorf("For(%q) = %q, %q", model, service.Source.ID, bare)
		}
	}
	service, bare := services.For(" DEEPSEEK-DIRECT/deepseek-chat ")
	if service.Source.ID != "deepseek" || bare != "deepseek-chat" {
		t.Fatalf("qualified = %q, %q", service.Source.ID, bare)
	}
}

func TestVendoredRowsAreTheDecidedFive(t *testing.T) {
	rows := Vendored()
	want := []string{"deepseek", "z-ai", "moonshot", "ollama", "custom"}
	if len(rows) != len(want) {
		t.Fatalf("vendored rows = %d, want %d", len(rows), len(want))
	}
	for i := range want {
		if rows[i].ID != want[i] {
			t.Errorf("row %d = %q, want %q", i, rows[i].ID, want[i])
		}
		if rows[i].Plan || rows[i].KeyPrefix != "" {
			t.Errorf("row %s set phase-five facts", rows[i].ID)
		}
		if rows[i].Probe.Timeout != ProbeTimeout {
			t.Errorf("row %s probe timeout = %s, want %s", rows[i].ID, rows[i].Probe.Timeout, ProbeTimeout)
		}
	}
	if !rows[3].KeyOptional || rows[0].KeyOptional || rows[1].KeyOptional || rows[2].KeyOptional || rows[4].KeyOptional {
		t.Fatal("only Ollama may omit its key")
	}
}

func TestVendoredListingHintsAndProbeModelsMatchTheProviderSurvey(t *testing.T) {
	want := []struct {
		id         string
		listing    Listing
		probeModel string
	}{
		{"deepseek", ListingModels, ""},
		// Z.ai's listing was undocumented rather than absent. The connect door
		// asks it first; glm-5.3-flash is the current cheap fallback, never the
		// superseded glm-4.6 named by the original survey brief.
		{"z-ai", ListingNone, "glm-5.3-flash"},
		// The survey's old K2 preview is gone. With no unambiguous cheapest current
		// Moonshot model, deferring proof is safer than spending on a guessed id.
		{"moonshot", ListingNone, ""},
		{"ollama", ListingModels, ""},
		{"custom", ListingModels, ""},
	}
	rows := Vendored()
	for index, expected := range want {
		row := rows[index]
		if row.ID != expected.id || row.Listing != expected.listing || row.ProbeModel != expected.probeModel {
			t.Errorf("row %d = id %q listing %v probe %q, want %q %v %q",
				index, row.ID, row.Listing, row.ProbeModel, expected.id, expected.listing, expected.probeModel)
		}
		if row.Probe.Method != "GET" || row.Probe.Address != "/models" {
			t.Errorf("row %s does not try the listing first: %+v", row.ID, row.Probe)
		}
	}
}

func TestEveryVendoredProbeNamesTheSharedTimeout(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate modelsource.go")
	}
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(here), "modelsource.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var timeoutFields int
	ast.Inspect(file, func(node ast.Node) bool {
		field, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		name, ok := field.Key.(*ast.Ident)
		if !ok || name.Name != "Timeout" {
			return true
		}
		timeoutFields++
		value, ok := field.Value.(*ast.Ident)
		if !ok || value.Name != "ProbeTimeout" {
			t.Errorf("a probe timeout does not use ProbeTimeout: %T", field.Value)
		}
		return true
	})
	if timeoutFields != 2 {
		t.Fatalf("modelsource describes %d probe timeout fields, want 2", timeoutFields)
	}
}
