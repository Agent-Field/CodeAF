package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A lazily loaded catalog has to answer exactly what an eagerly loaded one
// answers. The only difference the caller may observe is when it waits.

func TestLoadLazyDoesNotWaitForTheFetchAndAnswersTheSame(t *testing.T) {
	release := make(chan struct{})
	served := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case served <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"vendor/seer","name":"Seer",
			 "architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},
			 "pricing":{"prompt":"0.001","completion":"0.002","request":"0.25"}}]}`))
	}))
	defer server.Close()

	options := Options{BaseURL: server.URL, Dir: t.TempDir()}

	start := time.Now()
	lazy := LoadLazy(context.Background(), options)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("LoadLazy blocked for %s; it must hand back a value immediately", elapsed)
	}

	// The fetch is already in flight before any question is asked.
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("LoadLazy never started the fetch")
	}
	close(release)

	if !lazy.Supports("vendor/seer", "input", "image") {
		t.Fatal("lazy catalog lost a modality the fetch advertised")
	}
	model, ok := lazy.Model("vendor/seer")
	if !ok || model.RequestPrice != 0.25 {
		t.Fatalf("lazy catalog returned %+v (found %v)", model, ok)
	}
	if got := lazy.ModelsWithOutput("text"); len(got) != 1 || got[0].ID != "vendor/seer" {
		t.Fatalf("lazy listing returned %+v", got)
	}
	if lazy.Supports("vendor/absent", "input", "image") {
		t.Fatal("an unknown model must answer false")
	}
	if lazy.Supports("vendor/seer", "sideways", "image") {
		t.Fatal("an unknown direction must answer false")
	}
}

// Cleaning dedupes on the literal id, so a slug and its "~" pinned variant can
// both survive into the list. A scan answered with the first of them; the index
// has to agree.
func TestIndexKeepsTheFirstRowForANormalizedID(t *testing.T) {
	indexed := &Catalog{ready: newRows([]Model{
		{ID: "~vendor/pinned", OutputModalities: []string{"text"}},
		{ID: "vendor/pinned", OutputModalities: []string{"image"}},
	})}
	model, ok := indexed.Model("vendor/pinned")
	if !ok {
		t.Fatal("the pinned variant should resolve by its bare slug")
	}
	if model.ID != "~vendor/pinned" {
		t.Fatalf("index returned %q, want the first matching row", model.ID)
	}
	if !indexed.Supports("vendor/pinned", "output", "text") {
		t.Fatal("Supports must read the same row Model does")
	}
}
