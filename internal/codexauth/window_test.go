package codexauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// listingBackend answers the account listing the way the ChatGPT backend does:
// every row carries its own `context_window`, and a hidden row is not offered.
func listingBackend(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/models" {
			http.NotFound(writer, request)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"models": []any{
			map[string]any{"slug": "gpt-5.5", "display_name": "GPT-5.5", "visibility": "list", "context_window": 272000},
			map[string]any{"slug": "gpt-wide", "display_name": "GPT-Wide", "visibility": "list", "context_window": 400000},
			map[string]any{"slug": "gpt-silent", "display_name": "GPT-Silent", "visibility": "list"},
			map[string]any{"slug": "gpt-reserve", "visibility": "hide", "context_window": 272000},
		}})
	}))
}

func windows(models []Model) map[string]int {
	out := make(map[string]int, len(models))
	for _, model := range models {
		out[model.ID] = model.ContextLength
	}
	return out
}

func TestW1TheAccountListingKeepsEachModelsOwnWindow(t *testing.T) {
	// W1 (#1383): `List` keeps the listing's `context_window` per model and
	// persists it; a row that published none stays zero rather than borrowing a
	// neighbour's figure.
	now := time.Unix(1_800_000_000, 0)
	dir := t.TempDir()
	if err := Save(dir, validTokens(now)); err != nil {
		t.Fatal(err)
	}
	backend := listingBackend(t)
	defer backend.Close()
	models, err := List(context.Background(), dir, Options{Backend: backend.URL, HTTPClient: backend.Client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"gpt-5.5": 272000, "gpt-wide": 400000, "gpt-silent": 0}
	for _, got := range []map[string]int{windows(models), windows(loadModels(dir))} {
		if len(got) != len(want) {
			t.Fatalf("windows = %v, want %v", got, want)
		}
		for id, window := range want {
			if got[id] != window {
				t.Fatalf("window of %s = %d, want %d (all: %v)", id, got[id], window, got)
			}
		}
	}
}

func TestW1TheCatalogTranslationSavesTheWindowItReports(t *testing.T) {
	// W1 (#1383): the transport's own translation of the listing, which the
	// shared catalog reads, reports each window AND saves it, so the two
	// readers of the one listing leave the same answer on disk.
	now := time.Unix(1_800_000_000, 0)
	dir := t.TempDir()
	if err := Save(dir, validTokens(now)); err != nil {
		t.Fatal(err)
	}
	backend := listingBackend(t)
	defer backend.Close()
	client := ClientWithOptions(dir, Options{Backend: backend.URL, HTTPClient: backend.Client(), Now: func() time.Time { return now }})
	response, err := client.Get(backend.URL + "/models")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var translated struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&translated); err != nil {
		t.Fatal(err)
	}
	reported := make(map[string]int)
	for _, row := range translated.Data {
		reported[row.ID] = row.ContextLength
	}
	saved := windows(loadModels(dir))
	for id, window := range map[string]int{"gpt-5.5": 272000, "gpt-wide": 400000} {
		if reported[id] != window || saved[id] != window {
			t.Fatalf("%s: reported %d, saved %d, want %d", id, reported[id], saved[id], window)
		}
	}
}

func TestW2EveryFallbackRowCarriesTheObservedWindow(t *testing.T) {
	// W2 (#1383): the rows used when the listing cannot be reached carry the
	// one observed figure, so a conversation that moves onto one of them is not
	// left compacting at its previous model's window.
	if FallbackContextWindow != 272000 {
		t.Fatalf("fallback window = %d, want the listing's observed 272000", FallbackContextWindow)
	}
	if len(FallbackModels) == 0 {
		t.Fatal("no fallback rows")
	}
	for _, model := range FallbackModels {
		if model.ContextLength != FallbackContextWindow {
			t.Fatalf("fallback row %s window = %d, want %d", model.ID, model.ContextLength, FallbackContextWindow)
		}
	}
}
