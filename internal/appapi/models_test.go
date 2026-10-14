package appapi

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
)

func TestCanonicalModelRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []string
		want []string
	}{
		{
			name: "flash lineages",
			ids: []string{
				"deepseek/deepseek-v4.1-flash:batch", "deepseek/deepseek-v4.1-flash",
				"deepseek/deepseek-v4-flash-0731", "~deepseek/deepseek-v4-flash-latest", "deepseek/deepseek-v4-flash",
				"z-ai/glm-5.3-flash", "z-ai/glm-5.3-flash:batch",
			},
			want: []string{"deepseek/deepseek-v4.1-flash", "deepseek/deepseek-v4-flash", "z-ai/glm-5.3-flash"},
		},
		{
			name: "all router variants and floating aliases are absent",
			ids: []string{
				"vendor/model:batch", "vendor/model:free", "vendor/model:nitro", "vendor/model:floor",
				"vendor/model:exacto", "vendor/model:online", "vendor/model:thinking", "vendor/model:future-variant",
				"~vendor/model", "vendor/model-latest", "vendor/model",
			},
			want: []string{"vendor/model"},
		},
		{
			name: "local tags are models, not routes",
			ids:  []string{"qwen3:8b", "llama3.1:70b", "llama3:latest"},
			want: []string{"qwen3:8b", "llama3.1:70b", "llama3:latest"},
		},
		{
			name: "a lone variant is the only way to that model",
			ids:  []string{"vendor/solo:free"},
			want: []string{"vendor/solo:free"},
		},
		{
			name: "plain wins in either order",
			ids:  []string{"vendor/first", "vendor/first-20261001", "vendor/second-20261001", "vendor/second"},
			want: []string{"vendor/first", "vendor/second"},
		},
		{
			name: "plain wins through provider namespaces and spelling",
			ids: []string{
				"openrouter/deepseek/deepseek-v4-flash-0731", "openrouter/deepseek/deepseek-v4-flash",
				"accounts/fireworks/models/glm-5p3-flash-0901", "accounts/fireworks/models/glm-5p3-flash",
			},
			want: []string{"openrouter/deepseek/deepseek-v4-flash", "accounts/fireworks/models/glm-5p3-flash"},
		},
		{
			name: "newest snapshot when plain is missing",
			ids: []string{
				"deepseek/deepseek-v4-flash-0731", "vendor/model-20261001", "deepseek/deepseek-v4-flash-0901",
				"vendor/model-20261012", "vendor/model-20260901", "deepseek/deepseek-v4-flash-0801",
			},
			want: []string{"deepseek/deepseek-v4-flash-0901", "vendor/model-20261012"},
		},
		{name: "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input, want []CatalogModel
			for _, id := range tc.ids {
				row := CatalogModel{ID: id, Name: "Title of " + id, Provider: "openrouter", Available: true, Efforts: []string{"medium"}}
				input = append(input, row)
				if slices.Contains(tc.want, id) {
					want = append(want, row)
				}
			}
			// Expected rows follow lineage order, even when a later snapshot wins.
			slices.SortFunc(want, func(a, b CatalogModel) int {
				return slices.Index(tc.want, a.ID) - slices.Index(tc.want, b.ID)
			})
			before := slices.Clone(input)
			got := canonicalModelRows(input)
			if !slices.EqualFunc(got, want, func(a, b CatalogModel) bool { return reflect.DeepEqual(a, b) }) {
				t.Fatalf("rows = %+v, want %+v", got, want)
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatal("listing mutated the provider's full catalog")
			}
		})
	}
}

func TestModelsRepresentSavedRoutesWithoutRestrictingWrites(t *testing.T) {
	s := settingsServer(t)
	ids := []string{
		"deepseek/deepseek-v4.1-flash", "deepseek/deepseek-v4.1-flash:batch",
		"deepseek/deepseek-v4-flash", "deepseek/deepseek-v4-flash-0731", "~deepseek/deepseek-v4-flash-latest",
		"z-ai/glm-5.3-flash", "z-ai/glm-5.3-flash:batch",
	}
	var full []CatalogModel
	for _, id := range ids {
		full = append(full, CatalogModel{ID: id, Name: "Title of " + id, Available: true})
	}
	before := slices.Clone(full)
	s.options.ModelCatalog = func(context.Context) ([]CatalogModel, error) { return full, nil }
	for _, id := range ids {
		body, _ := json.Marshal(map[string]string{"model": id})
		w := request(t, s, "PUT", "/app/v1/models/default", string(body), bearer(), "127.0.0.1:4000")
		if w.Code != 200 {
			t.Fatalf("default %s: %d %s", id, w.Code, w.Body)
		}
	}
	pinned := []string{"deepseek/deepseek-v4.1-flash:batch", "deepseek/deepseek-v4-flash-0731", "~deepseek/deepseek-v4-flash-latest"}
	body, _ := json.Marshal(map[string][]string{"models": pinned})
	w := request(t, s, "PUT", "/app/v1/models/pinned", string(body), bearer(), "127.0.0.1:4000")
	if w.Code != 200 {
		t.Fatalf("pinned: %d %s", w.Code, w.Body)
	}
	w = request(t, s, "GET", "/app/v1/models", "", bearer(), "127.0.0.1:4000")
	var got ModelsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{ids[0], ids[2], ids[5]}
	var gotIDs []string
	for _, row := range got.Models {
		gotIDs = append(gotIDs, row.ID)
		if !row.Available || row.Name != "Title of "+row.ID {
			t.Fatalf("lost catalog metadata: %+v", row)
		}
	}
	if w.Code != 200 || !slices.Equal(gotIDs, wantIDs) || got.Default != ids[len(ids)-1] || !slices.Equal(got.Pinned, pinned) {
		t.Fatalf("models: %d %s", w.Code, w.Body)
	}
	if got := s.modelList(t.Context()); !reflect.DeepEqual(got, before) {
		t.Fatalf("non-listing consumers lost catalog rows: %+v", got)
	}
	// Saved routes still belong to the listed lineage after the provider stops
	// advertising those exact ids.
	full = canonicalModelRows(full)
	w = request(t, s, "GET", "/app/v1/models", "", bearer(), "127.0.0.1:4000")
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(got.Models) != len(wantIDs) || got.Default != ids[len(ids)-1] || !slices.Equal(got.Pinned, pinned) {
		t.Fatalf("saved routes added rows after discovery changed: %d %s", w.Code, w.Body)
	}
}

func TestModelsKeepUnlistedSavedChoicesAndTheirKnownNames(t *testing.T) {
	for _, tc := range []struct {
		name, id, title string
		listed          []CatalogModel
		remembered      []catalog.Model
	}{
		{name: "disconnected catalog name", id: "vendor/disconnected", title: "Disconnected model", remembered: []catalog.Model{{ID: "vendor/disconnected", Name: "Disconnected model"}}},
		{name: "unknown id", id: "vendor/unknown", title: "vendor/unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := settingsServer(t)
			s.options.ModelCatalog = func(context.Context) ([]CatalogModel, error) { return tc.listed, nil }
			services := config.ResolveSources(s.options.ProfileDir, "", config.DefaultBaseURL)
			if err := catalog.Remember(config.CatalogOptionsFor(services.Default(), s.options.ProfileDir), tc.remembered); err != nil {
				t.Fatal(err)
			}
			if err := config.WriteChatModel(s.options.ProfileDir, tc.id); err != nil {
				t.Fatal(err)
			}
			w := request(t, s, "GET", "/app/v1/models", "", bearer(), "127.0.0.1:4000")
			var got ModelsResponse
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || len(got.Models) != 1 || got.Models[0].ID != tc.id || got.Models[0].Name != tc.title || got.Models[0].Available {
				t.Fatalf("saved choice: %d %s", w.Code, w.Body)
			}
		})
	}
}
