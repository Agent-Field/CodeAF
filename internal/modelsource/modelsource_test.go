package modelsource

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
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

func TestVendoredRowsAreTheDecidedSeven(t *testing.T) {
	rows := Vendored()
	want := []string{"deepseek", "z-ai", "moonshot", "minimax", "qwen", "ollama", "custom"}
	if len(rows) != len(want) {
		t.Fatalf("vendored rows = %d, want %d", len(rows), len(want))
	}
	for i := range want {
		if rows[i].ID != want[i] {
			t.Errorf("row %d = %q, want %q", i, rows[i].ID, want[i])
		}
		if rows[i].Probe.Timeout != ProbeTimeout {
			t.Errorf("row %s probe timeout = %s, want %s", rows[i].ID, rows[i].Probe.Timeout, ProbeTimeout)
		}
	}
	if !rows[5].KeyOptional {
		t.Fatal("only Ollama may omit its key")
	}
	for index, row := range rows {
		if index != 5 && row.KeyOptional {
			t.Fatalf("%s unexpectedly accepts a blank key", row.ID)
		}
	}
}

// THE ROWS RECORD THE BEST KNOWN TRUTH, AND OBSERVATION OUTRANKS THE SURVEY.
// This law was written pinning each row to B-provider-landscape.md, which is
// right only until somebody watches the endpoint answer. Z.ai is the worked
// example: the survey calls its /models undocumented, a live run got 200 and
// ten models, and reading the survey's silence as absence is what refused a
// valid key. So a row that has been observed says what was observed, and the
// survey is what the rest are held to until somebody looks.
func TestVendoredListingHintsAndProbeModelsMatchTheProviderSurvey(t *testing.T) {
	want := []struct {
		id         string
		listing    Listing
		probeModel string
	}{
		{"deepseek", ListingModels, ""},
		// OBSERVED on 2026-09-10 against api.z.ai: 200 and ten models. The row
		// says so rather than repeating the survey's "undocumented", so the
		// hint and the behaviour cannot disagree. glm-5.3-flash stays as the
		// fallback for a region that does not answer, and is the current cheap
		// model rather than the superseded glm-4.6 the first brief named.
		{"z-ai", ListingModels, "glm-5.3-flash"},
		// UNOBSERVED. The survey says undocumented, which after Z.ai is known to
		// be weak evidence — but nobody has watched this endpoint, so the hint
		// stays what the survey says and the connect door asks anyway. The
		// survey's old K2 preview is gone and no replacement is guessed: with no
		// unambiguous cheapest current model, deferring proof beats spending on
		// an invented id, which is the mistake this whole law exists about.
		{"moonshot", ListingNone, "kimi-k2.7-code"},
		{"minimax", ListingNone, "MiniMax-M3"},
		{"qwen", ListingNone, "qwen3.8-flash"},
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

func TestAKeyPrefixOrdersDoorsAndNeverSkipsOne(t *testing.T) {
	source := Source{Doors: []Door{
		{ID: "metered", Metered: true},
		{ID: "plan", KeyPrefix: "sk-plan-"},
		{ID: "other"},
	}}
	got := source.OrderedDoors("sk-plan-example")
	if len(got) != 3 || got[0].ID != "plan" || got[1].ID != "metered" || got[2].ID != "other" {
		t.Fatalf("ordered doors = %+v", got)
	}
	if ordinary := source.OrderedDoors("another-shape"); len(ordinary) != 3 || ordinary[0].ID != "metered" {
		t.Fatalf("an unmatched key changed policy order: %+v", ordinary)
	}
}

func TestOnlyWireDistinctBillingProductsShipAsSeparateDoors(t *testing.T) {
	rows := Vendored()
	byID := make(map[string]Source, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	if minimax := byID["minimax"]; len(minimax.Doors) != 0 || minimax.Address == "" {
		t.Fatalf("MiniMax claims distinguishable billing doors without wire evidence: %+v", minimax.Doors)
	}
	for _, id := range []string{"moonshot", "qwen"} {
		source := byID[id]
		if len(source.Doors) != 2 {
			t.Fatalf("%s doors = %+v, want the documented plan and metered hosts", id, source.Doors)
		}
		if strings.TrimRight(source.Doors[0].Address, "/") == strings.TrimRight(source.Doors[1].Address, "/") {
			t.Fatalf("%s labels one wire as two billing products: %+v", id, source.Doors)
		}
		metered, ok := source.MeteredDoor()
		if !ok || metered.ID != source.Doors[1].ID {
			t.Fatalf("%s metered identity = %+v, found=%t", id, metered, ok)
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
