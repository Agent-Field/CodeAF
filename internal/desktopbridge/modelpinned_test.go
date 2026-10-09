package desktopbridge

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

func pinnedCatalog(context.Context) ([]CatalogModel, error) {
	return []CatalogModel{{ID: "a/b"}, {ID: "c/d"}, {ID: "e/f"}, {ID: Model}}, nil
}

func readPinned(t *testing.T, b *Bridge) PinnedView {
	t.Helper()
	w := request(b, "GET", "/api/engine/models/pinned", "")
	var view PinnedView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || w.Code != 200 {
		t.Fatalf("%d %v %s", w.Code, err, w.Body.String())
	}
	return view
}

func TestThePinnedModelsStartOnTheThreeLabelledOnes(t *testing.T) {
	b, _, _ := modelsFixture(t, pinnedCatalog)
	view := readPinned(t, b)
	want := []PinnedModel{{"z-ai/glm-5.3-flash", "GLM Flash"}, {Model, "DS Flash"}, {"z-ai/glm-5.3", "GLM 5.3"}}
	if view.Chosen || len(view.Pinned) != 3 {
		t.Fatalf("%+v", view)
	}
	for i := range want {
		if view.Pinned[i] != want[i] {
			t.Fatalf("slot %d: %+v", i, view.Pinned[i])
		}
	}
}

func TestAPinnedChoiceIsPersistedAndRefusedWhenItIsNotThreeCatalogModels(t *testing.T) {
	b, _, profile := modelsFixture(t, pinnedCatalog)
	if w := request(b, "PUT", "/api/engine/models/pinned", `{"models":["a/b","c/d","e/f"]}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if got, chosen := config.DesktopPinned(profile); !chosen || got[0] != "a/b" || got[2] != "e/f" {
		t.Fatalf("stored %v %v", got, chosen)
	}
	for _, bad := range []string{`{"models":["a/b","c/d"]}`, `{"models":["a/b","c/d","nope/x"]}`, `{"models":["a/b","a/b","c/d"]}`} {
		if w := request(b, "PUT", "/api/engine/models/pinned", bad); w.Code != 400 {
			t.Fatalf("%s accepted: %d", bad, w.Code)
		}
	}
	if w := request(b, "PUT", "/api/engine/models/pinned", `{"models":[]}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if view := readPinned(t, b); view.Chosen {
		t.Fatalf("reset: %+v", view)
	}
}
