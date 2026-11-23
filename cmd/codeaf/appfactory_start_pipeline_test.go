package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A PICKED PIPELINE TRAVELS TO THE FACTORY on start and create, and a start
// or create without one sends exactly what it always did.
func TestAppSendsThePickedPipelineOnStartAndCreate(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"job_id":"j","version":8}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		b, _ := json.Marshal(body)
		mu.Lock()
		bodies = append(bodies, r.URL.Path+" "+string(b))
		mu.Unlock()
		w.Write([]byte(`{"factory":{"name":"Stub"},"id":9}`))
	})
	b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
	var _ appwire.FactoryPipelineStarter = b
	ctx := context.Background()
	v := &api.Expect{Version: ptr(7)}
	b.FactoryJobStart(ctx, appwire.FactoryJobStartParams{Factory: conn.ID, Job: 4, Expect: v, Pipeline: "docs"})
	b.FactoryJobStart(ctx, appwire.FactoryJobStartParams{Factory: conn.ID, Job: 4, Expect: v})
	b.FactoryJobCreate(ctx, appwire.FactoryJobCreateParams{Factory: conn.ID, Repo: "o/r", Title: "Fix it", Pipeline: "docs"})
	mu.Lock()
	defer mu.Unlock()
	got := strings.Join(bodies, "\n")
	for _, want := range []string{
		`/v1/jobs/4/start_now {"expect":{"version":7},"pipeline":"docs"}`,
		`/v1/jobs/4/start_now {"expect":{"version":7}}`,
		`"pipeline":"docs"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in\n%s", want, got)
		}
	}
	if strings.Count(got, `"pipeline":"docs"`) != 2 {
		t.Fatalf("%s", got)
	}
}
