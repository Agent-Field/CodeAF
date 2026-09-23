package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/codexauth"
)

func TestW3ConnectCodexRemembersEachModelsWindow(t *testing.T) {
	// W3 (#1383): the catalog a Codex connection remembers carries each model's
	// window — the listing's own figure when it answered, the fallback rows'
	// one observed figure when it did not — because that catalog is what the
	// picker, the status line and the session's compaction read.
	for _, testCase := range []struct {
		name    string
		listing http.HandlerFunc
		want    map[string]int
	}{
		{
			name: "listing answered",
			listing: func(writer http.ResponseWriter, request *http.Request) {
				_ = json.NewEncoder(writer).Encode(map[string]any{"models": []any{
					map[string]any{"slug": "gpt-5.5", "visibility": "list", "context_window": 272000},
					map[string]any{"slug": "gpt-wide", "visibility": "list", "context_window": 400000},
				}})
			},
			want: map[string]int{"gpt-5.5": 272000, "gpt-wide": 400000},
		},
		{
			name:    "listing unreachable",
			listing: func(writer http.ResponseWriter, request *http.Request) { writer.WriteHeader(http.StatusBadGateway) },
			want: map[string]int{
				"gpt-5.5": codexauth.FallbackContextWindow, "gpt-5.6-sol": codexauth.FallbackContextWindow,
				"gpt-5.6-terra": codexauth.FallbackContextWindow, "gpt-5.6-luna": codexauth.FallbackContextWindow,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			backend := httptest.NewServer(testCase.listing)
			defer backend.Close()
			t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
			dir := t.TempDir()
			tokens := codexauth.Tokens{AccessToken: "window-access", RefreshToken: "window-refresh", IDToken: "window-identity", AccountID: "acct-window", ExpiresAt: time.Now().Add(time.Hour)}
			if _, err := ConnectCodex(context.Background(), dir, tokens); err != nil {
				t.Fatal(err)
			}
			service, ok := ResolveSources(dir, "", DefaultBaseURL).ByID("codex")
			if !ok {
				t.Fatal("connection was not kept")
			}
			remembered := catalog.Recall(CatalogOptionsFor(service, dir))
			rows := remembered.ModelsNow()
			if len(rows) != len(testCase.want) {
				t.Fatalf("remembered rows = %+v, want %d", rows, len(testCase.want))
			}
			for id, window := range testCase.want {
				if got := remembered.ContextLengthNow(id); got != window {
					t.Fatalf("remembered window of %s = %d, want %d", id, got, window)
				}
			}
		})
	}
}
