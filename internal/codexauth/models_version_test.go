package codexauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestCatalogVersionExposesNewModelsThroughBothDoors(t *testing.T) {
	for _, door := range []string{"direct", "translated"} {
		t.Run(door, func(t *testing.T) {
			now := time.Unix(1_800_000_000, 0)
			dir := t.TempDir()
			if err := Save(dir, validTokens(now)); err != nil {
				t.Fatal(err)
			}
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/models" ||
					r.Header.Get("Authorization") != "Bearer access-token-secret" ||
					r.Header.Get("chatgpt-account-id") != "account-one" ||
					r.Header.Get("originator") != Originator ||
					r.Header.Get("User-Agent") != provider.DirectUserAgent {
					http.Error(w, "unexpected catalog request", http.StatusBadRequest)
					return
				}
				rows := []map[string]any{
					{"slug": "baseline", "visibility": "list"},
					{"slug": "hidden", "visibility": "hide"},
				}
				// The backend fixture pins the released version independently of
				// clientVersion, so restoring the old advertisement hides the row.
				if r.URL.Query().Get("client_version") == "0.162.1" {
					rows = append(rows, map[string]any{
						"slug": "new-model", "visibility": "list", "context_window": 272000,
						"supported_reasoning_levels": []map[string]string{{"effort": "high"}},
					})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"models": rows})
			}))
			defer backend.Close()
			options := Options{Backend: backend.URL, HTTPClient: backend.Client(), Now: func() time.Time { return now }}
			var ids []string
			if door == "direct" {
				models, err := List(context.Background(), dir, options)
				if err != nil {
					t.Fatal(err)
				}
				for _, model := range models {
					ids = append(ids, model.ID)
				}
			} else {
				response, err := ClientWithOptions(dir, options).Get(backend.URL + "/models")
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusOK {
					t.Fatalf("catalog status = %d", response.StatusCode)
				}
				var catalog struct {
					Data []struct{ ID string } `json:"data"`
				}
				if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil {
					t.Fatal(err)
				}
				for _, model := range catalog.Data {
					ids = append(ids, model.ID)
				}
			}
			if strings.Join(ids, ",") != "baseline,new-model" {
				t.Fatalf("visible models = %v; newer catalog models must be exposed and hidden rows excluded", ids)
			}
			cached := loadModels(dir)
			if len(cached) != 2 || cached[1].ID != "new-model" || cached[1].ContextLength != 272000 || strings.Join(cached[1].ReasoningLevels, ",") != "high" {
				t.Fatalf("cached catalog metadata = %+v", cached)
			}
		})
	}
}
