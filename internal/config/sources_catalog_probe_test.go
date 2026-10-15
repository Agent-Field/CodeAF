package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
)

// The real ai& listing on 2026-10-14 was 6,920 bytes with thirteen rows. A
// connection must keep the whole listing, including the final model, rather
// than mistaking a transport-truncated JSON document for an absent service.
func TestAProviderCatalogBeyondFourKiBConnectsWithoutACompletionProbe(t *testing.T) {
	t.Setenv("AIAND_API_KEY", "")
	ids := []string{"zai-org/glm-5.3-flash", "motif-technologies/motif-3"}
	for len(ids) < 13 {
		ids = append(ids, "synthetic/model-"+strings.Repeat("x", len(ids)))
	}
	data := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		data = append(data, map[string]string{"id": id, "description": "a public model catalog row"})
	}
	payload := map[string]any{"object": "list", "data": data, "metadata": ""}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	payload["metadata"] = strings.Repeat("x", 6920-len(body))
	body, err = json.Marshal(payload)
	if err != nil || len(body) != 6920 || !json.Valid(body) || json.Valid(body[:4096]) {
		t.Fatalf("fixture must be complete 6,920-byte JSON whose first four KiB are incomplete: bytes=%d err=%v", len(body), err)
	}
	server := sourcestub.New()
	defer server.Close()
	server.Refuse(http.StatusOK, string(body))
	source := vendoredSource(t, "aiand")
	source.Address = server.URL()
	row := PersistedSource{ID: source.ID, Written: source.Written, Key: "sk-synthetic-catalog-probe-key"}
	dir := t.TempDir()
	outcome, err := ConnectService(context.Background(), dir, row, source, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected || !outcome.Listed || outcome.Models != 13 || !slices.Equal(outcome.ModelIDs, ids) {
		t.Fatalf("complete catalog did not connect with every model: outcome=%+v err=%v", outcome, err)
	}
	requests := server.Requests()
	if len(requests) != 1 || requests[0].Method != http.MethodGet {
		t.Fatalf("a valid listing must not buy a completion probe: %+v", requests)
	}
	rows := PersistedSources(dir)
	if len(rows) != 1 || rows[0].ID != source.ID || rows[0].Listed == nil || !*rows[0].Listed {
		t.Fatalf("successful listing was not saved: %+v", rows)
	}
}

// A valid document followed by whitespace still exceeds the response bound.
// Accepting its complete prefix would make the limit disappear for a server
// that kept sending bytes after its model list had already parsed.
func TestConnectionCatalogResponseBoundIncludesTheWholeBody(t *testing.T) {
	t.Setenv("AIAND_API_KEY", "")
	for _, size := range []int{catalog.MaxCatalogBytes - 1, catalog.MaxCatalogBytes, catalog.MaxCatalogBytes + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			prefix := `{"data":[{"id":"synthetic/boundary"}]}`
			body := prefix + strings.Repeat(" ", size-len(prefix))
			server := sourcestub.New()
			defer server.Close()
			server.Refuse(http.StatusOK, body)
			source := vendoredSource(t, "aiand")
			source.Address = server.URL()
			dir := t.TempDir()
			if err := WriteAPIKey(dir, "sk-existing-default-test-key"); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(BudgetConfigPath(dir))
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := ConnectService(context.Background(), dir, PersistedSource{
				ID: source.ID, Written: source.Written, Key: "sk-synthetic-catalog-probe-key",
			}, source, nil)
			if err != nil {
				t.Fatal(err)
			}
			if size <= catalog.MaxCatalogBytes {
				if outcome.Kind != modelsource.OutcomeConnected || !slices.Equal(outcome.ModelIDs, []string{"synthetic/boundary"}) || len(PersistedSources(dir)) != 1 {
					t.Fatalf("complete bounded listing did not connect: %+v", outcome)
				}
			} else {
				after, err := os.ReadFile(BudgetConfigPath(dir))
				if err != nil || outcome.Kind != modelsource.OutcomeUnanswered || string(after) != string(before) {
					t.Fatalf("oversized listing admitted a prefix or changed the profile: %+v err=%v", outcome, err)
				}
			}
			requests := server.Requests()
			if len(requests) != 1 || requests[0].Method != http.MethodGet {
				t.Fatalf("catalog bound must not turn into a paid fallback: %+v", requests)
			}
		})
	}
}

func TestUnreadableConnectionCatalogsDoNotSaveOrBuyAFallback(t *testing.T) {
	t.Setenv("AIAND_API_KEY", "")
	for _, kind := range []string{"malformed JSON", "interrupted response", "cancelled request"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				body := `{"data":[{"id":"synthetic/model"}]}`
				if kind == "malformed JSON" {
					body = `{"data":[`
				}
				if kind == "interrupted response" {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			source := vendoredSource(t, "aiand")
			source.Address = server.URL
			dir := t.TempDir()
			if err := WriteAPIKey(dir, "sk-existing-default-test-key"); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(BudgetConfigPath(dir))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantRequests := 1
			if kind == "cancelled request" {
				cancel()
				wantRequests = 0
			}
			outcome, err := ConnectService(ctx, dir, PersistedSource{
				ID: source.ID, Written: source.Written, Key: "sk-synthetic-catalog-probe-key",
			}, source, nil)
			after, readErr := os.ReadFile(BudgetConfigPath(dir))
			if err != nil || readErr != nil || outcome.Kind != modelsource.OutcomeUnanswered || string(after) != string(before) || requests.Load() != int32(wantRequests) {
				t.Fatalf("unreadable catalog was accepted or retried: %+v err=%v readErr=%v requests=%d", outcome, err, readErr, requests.Load())
			}
		})
	}
}
