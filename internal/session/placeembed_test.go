package session

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// A real adapter against a fake router proves the whole batching and billing door.
func TestEmbedPlacesBatchesUnitVectorsAndBillsItsRole(t *testing.T) {
	var batches []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":{"endpoints":[]}}`))
			return
		}
		var req struct {
			Model      string
			Input      []string
			Dimensions int
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/embeddings" || req.Model != PlacesEmbeddingModel || req.Dimensions != 256 {
			t.Errorf("wrong embedding request: %s %+v", r.URL.Path, req)
		}
		batches = append(batches, len(req.Input))
		rows := make([]map[string]any, len(req.Input))
		for i := range rows {
			v := make([]float32, req.Dimensions)
			v[0], v[1] = 3, 4
			rows[i] = map[string]any{"index": i, "embedding": v}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": rows, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10, "cost": 0.001}})
	}))
	defer server.Close()
	client, err := provider.NewClient(provider.Config{BaseURL: server.URL, Model: PlacesEmbeddingModel, APIKey: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, client, func(c *Config) { c.SessionFile = journal })
	vectors, err := agent.EmbedPlaces(context.Background(), make([]string, 130))
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 3 || batches[0] != 64 || batches[1] != 64 || batches[2] != 2 || len(vectors) != 130 {
		t.Fatalf("batches %v, vectors %d", batches, len(vectors))
	}
	for _, v := range vectors {
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		if len(v) != 256 || math.Abs(norm-1) > 1e-6 {
			t.Fatalf("non-unit vector: dims %d norm %g", len(v), norm)
		}
	}
	if u := agent.Usage(); u.Calls != 3 || u.Turns != 0 || u.Input != 30 || math.Abs(u.CostUSD-0.003) > 1e-9 {
		t.Fatalf("usage %+v", u)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range readLines(t, journal) {
		var entry sessionEntry
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Usage != nil {
			if entry.Usage.Role != "placeembed" {
				t.Fatalf("wrong bill role %s", entry.Usage.Role)
			}
			count++
		}
	}
	if count != 3 {
		t.Fatalf("billing rows %d", count)
	}
	if _, err := agent.EmbedPlaces(context.Background(), []string{"closed"}); err == nil {
		t.Fatal("closed agent embedded")
	}
}

// A later outage must not return a partial space or erase an earlier charge.
type failingPlaceEmbedder struct {
	*scriptedCompleter
	calls int
}

func (e *failingPlaceEmbedder) Embed(ctx context.Context, model string, texts []string, dims int) ([][]float32, *ai.Response, error) {
	e.calls++
	if e.calls == 2 {
		return nil, nil, errors.New("synthetic outage")
	}
	cost := 0.002
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = make([]float32, dims)
		vectors[i][0] = 1
	}
	return vectors, &ai.Response{Usage: &ai.Usage{PromptTokens: 7, Cost: &cost}}, nil
}

func TestEmbedPlacesFailureKeepsEarlierBillingWithoutPartialVectors(t *testing.T) {
	client := &failingPlaceEmbedder{scriptedCompleter: &scriptedCompleter{}}
	agent, _ := newTestAgent(t, client, nil)
	if v, err := agent.EmbedPlaces(context.Background(), nil); err != nil || len(v) != 0 || client.calls != 0 {
		t.Fatal("empty input made a call")
	}
	v, err := agent.EmbedPlaces(context.Background(), make([]string, 65))
	if err == nil || v != nil || client.calls != 2 {
		t.Fatalf("vectors %v, error %v, calls %d", v, err, client.calls)
	}
	if u := agent.Usage(); u.Calls != 1 || u.Input != 7 || u.CostUSD != 0.002 || u.Turns != 0 {
		t.Fatalf("usage %+v", u)
	}
}
